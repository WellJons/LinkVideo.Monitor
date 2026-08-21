import Foundation
import ScreenCaptureKit
import CoreGraphics
import CoreMedia
import CoreVideo
import AVFoundation
import AudioToolbox

struct DisplayInfo: Codable {
    let id: UInt32
    let name: String
    let x: Double
    let y: Double
    let width_points: Int
    let height_points: Int
    let width_pixels: Int
    let height_pixels: Int
    let primary: Bool
}

enum HelperError: LocalizedError {
    case noDisplays
    case displayNotFound(UInt32)
    case shareableContent(String)
    case stream(String)
    case microphonePermission
    case microphoneNotFound(String)
    case microphoneCapture(String)

    var errorDescription: String? {
        switch self {
        case .noDisplays: return "macOS не вернула доступные дисплеи"
        case .displayNotFound(let id): return "Дисплей \(id) не найден"
        case .shareableContent(let message): return "ScreenCaptureKit: \(message)"
        case .stream(let message): return "Захват экрана: \(message)"
        case .microphonePermission: return "Нет разрешения macOS на доступ к микрофону"
        case .microphoneNotFound(let name): return "Микрофон «\(name)» не найден"
        case .microphoneCapture(let message): return "Захват микрофона: \(message)"
        }
    }
}

func shareableContent() throws -> SCShareableContent {
    let semaphore = DispatchSemaphore(value: 0)
    var content: SCShareableContent?
    var capturedError: Error?
    SCShareableContent.getExcludingDesktopWindows(false, onScreenWindowsOnly: true) { value, error in
        content = value
        capturedError = error
        semaphore.signal()
    }
    semaphore.wait()
    if let error = capturedError {
        throw HelperError.shareableContent(error.localizedDescription)
    }
    guard let content else {
        throw HelperError.shareableContent("пустой ответ")
    }
    return content
}

func displayInfos(_ content: SCShareableContent) -> [DisplayInfo] {
    let mainID = CGMainDisplayID()
    return content.displays.map { display in
        let id = display.displayID
        return DisplayInfo(
            id: id,
            name: id == mainID ? "Основной дисплей" : "Дисплей \(id)",
            x: display.frame.origin.x,
            y: display.frame.origin.y,
            width_points: display.width,
            height_points: display.height,
            width_pixels: Int(CGDisplayPixelsWide(id)),
            height_pixels: Int(CGDisplayPixelsHigh(id)),
            primary: id == mainID
        )
    }.sorted { lhs, rhs in
        if lhs.primary != rhs.primary { return lhs.primary }
        return lhs.id < rhs.id
    }
}

final class FrameOutput: NSObject, SCStreamOutput, SCStreamDelegate {
    private let expectedWidth: Int
    private let expectedHeight: Int

    init(width: Int, height: Int) {
        self.expectedWidth = width
        self.expectedHeight = height
    }

    func stream(_ stream: SCStream, didOutputSampleBuffer sampleBuffer: CMSampleBuffer, of outputType: SCStreamOutputType) {
        guard outputType == .screen,
              sampleBuffer.isValid,
              let pixelBuffer = sampleBuffer.imageBuffer else { return }

        CVPixelBufferLockBaseAddress(pixelBuffer, .readOnly)
        defer { CVPixelBufferUnlockBaseAddress(pixelBuffer, .readOnly) }

        let width = CVPixelBufferGetWidth(pixelBuffer)
        let height = CVPixelBufferGetHeight(pixelBuffer)
        guard width == expectedWidth, height == expectedHeight,
              let base = CVPixelBufferGetBaseAddress(pixelBuffer) else { return }

        let rowBytes = width * 4
        let sourceRowBytes = CVPixelBufferGetBytesPerRow(pixelBuffer)
        let output = FileHandle.standardOutput

        if rowBytes == sourceRowBytes {
            output.write(Data(bytes: base, count: rowBytes * height))
            return
        }
        for row in 0..<height {
            let ptr = base.advanced(by: row * sourceRowBytes)
            output.write(Data(bytes: ptr, count: rowBytes))
        }
    }

    func stream(_ stream: SCStream, didStopWithError error: Error) {
        fputs("ScreenCaptureKit stopped: \(error.localizedDescription)\n", stderr)
        fflush(stderr)
        exit(3)
    }
}

final class AudioOutput: NSObject, SCStreamOutput, SCStreamDelegate {
    private let output = FileHandle.standardOutput

    func stream(_ stream: SCStream, didOutputSampleBuffer sampleBuffer: CMSampleBuffer, of outputType: SCStreamOutputType) {
        guard outputType == .audio, sampleBuffer.isValid else { return }
        do {
            try sampleBuffer.withAudioBufferList { audioBufferList, _ in
                guard let description = sampleBuffer.formatDescription?.audioStreamBasicDescription,
                      let format = AVAudioFormat(
                        standardFormatWithSampleRate: description.mSampleRate,
                        channels: description.mChannelsPerFrame
                      ),
                      let samples = AVAudioPCMBuffer(
                        pcmFormat: format,
                        bufferListNoCopy: audioBufferList.unsafePointer
                      ) else { return }
                writeStereoS16LE(samples)
            }
        } catch {
            fputs("ScreenCaptureKit audio sample: \(error.localizedDescription)\n", stderr)
            fflush(stderr)
        }
    }

    private func writeStereoS16LE(_ samples: AVAudioPCMBuffer) {
        guard let channels = samples.floatChannelData else { return }
        let frameCount = Int(samples.frameLength)
        let channelCount = Int(samples.format.channelCount)
        guard frameCount > 0, channelCount > 0 else { return }

        var pcm = [Int16](repeating: 0, count: frameCount * 2)
        for frame in 0..<frameCount {
            let left = channels[0][frame]
            let right = channelCount > 1 ? channels[1][frame] : left
            pcm[frame * 2] = pcm16(left)
            pcm[frame * 2 + 1] = pcm16(right)
        }
        pcm.withUnsafeBytes { bytes in
            output.write(Data(bytes))
        }
    }

    private func pcm16(_ value: Float) -> Int16 {
        let clamped = max(-1.0, min(1.0, value))
        if clamped <= -1.0 { return Int16.min }
        if clamped >= 1.0 { return Int16.max }
        return Int16(clamped * Float(Int16.max))
    }

    func stream(_ stream: SCStream, didStopWithError error: Error) {
        fputs("ScreenCaptureKit audio stopped: \(error.localizedDescription)\n", stderr)
        fflush(stderr)
        exit(3)
    }
}

final class MicrophoneOutput: NSObject, AVCaptureAudioDataOutputSampleBufferDelegate {
    private let output = FileHandle.standardOutput

    func captureOutput(_ output: AVCaptureOutput, didOutput sampleBuffer: CMSampleBuffer, from connection: AVCaptureConnection) {
        guard sampleBuffer.isValid,
              let blockBuffer = CMSampleBufferGetDataBuffer(sampleBuffer) else { return }
        let length = CMBlockBufferGetDataLength(blockBuffer)
        guard length > 0 else { return }

        var data = Data(count: length)
        let status: OSStatus = data.withUnsafeMutableBytes { rawBuffer in
            guard let destination = rawBuffer.baseAddress else { return -1 }
            return CMBlockBufferCopyDataBytes(
                blockBuffer,
                atOffset: 0,
                dataLength: length,
                destination: destination
            )
        }
        if status == 0 {
            self.output.write(data)
        } else {
            fputs("AVFoundation microphone sample error: \(status)\n", stderr)
            fflush(stderr)
        }
    }
}

func value(after key: String, in args: [String]) -> String? {
    guard let index = args.firstIndex(of: key), index + 1 < args.count else { return nil }
    return args[index + 1]
}

func selectedDisplay(_ content: SCShareableContent, requestedID: UInt32 = 0) throws -> SCDisplay {
    guard !content.displays.isEmpty else { throw HelperError.noDisplays }
    let mainID = CGMainDisplayID()
    let display = content.displays.first(where: { $0.displayID == (requestedID == 0 ? mainID : requestedID) })
        ?? (requestedID == 0 ? content.displays.first : nil)
    guard let display else { throw HelperError.displayNotFound(requestedID) }
    return display
}

func startStream(_ stream: SCStream, output: AnyObject) throws {
    let started = DispatchSemaphore(value: 0)
    var startError: Error?
    stream.startCapture { error in
        startError = error
        started.signal()
    }
    started.wait()
    if let error = startError {
        throw HelperError.stream(error.localizedDescription)
    }
    withExtendedLifetime((stream, output)) {
        RunLoop.main.run()
    }
}

func capture(args: [String]) throws {
    let content = try shareableContent()
    let requestedID = UInt32(value(after: "--display-id", in: args) ?? "0") ?? 0
    let display = try selectedDisplay(content, requestedID: requestedID)

    let nativeWidth = Int(CGDisplayPixelsWide(display.displayID))
    let nativeHeight = Int(CGDisplayPixelsHigh(display.displayID))
    let width = max(2, Int(value(after: "--width", in: args) ?? "") ?? nativeWidth)
    let height = max(2, Int(value(after: "--height", in: args) ?? "") ?? nativeHeight)
    let fps = min(60, max(1, Int(value(after: "--fps", in: args) ?? "15") ?? 15))
    let cursor = Bool(value(after: "--cursor", in: args) ?? "true") ?? true

    let filter = SCContentFilter(display: display, excludingWindows: [])
    let configuration = SCStreamConfiguration()
    configuration.width = width
    configuration.height = height
    configuration.pixelFormat = kCVPixelFormatType_32BGRA
    configuration.minimumFrameInterval = CMTime(value: 1, timescale: CMTimeScale(fps))
    configuration.queueDepth = 3
    configuration.showsCursor = cursor

    let output = FrameOutput(width: width, height: height)
    let queue = DispatchQueue(label: "ru.linkvideo.monitor.screencapture", qos: .userInteractive)
    let stream = SCStream(filter: filter, configuration: configuration, delegate: output)
    do {
        try stream.addStreamOutput(output, type: .screen, sampleHandlerQueue: queue)
    } catch {
        throw HelperError.stream(error.localizedDescription)
    }
    try startStream(stream, output: output)
}

func captureAudio() throws {
    let content = try shareableContent()
    let display = try selectedDisplay(content)
    let filter = SCContentFilter(display: display, excludingWindows: [])
    let configuration = SCStreamConfiguration()
    configuration.capturesAudio = true
    configuration.sampleRate = 48_000
    configuration.channelCount = 2
    configuration.excludesCurrentProcessAudio = true
    configuration.queueDepth = 3

    let output = AudioOutput()
    let queue = DispatchQueue(label: "ru.linkvideo.monitor.systemaudio", qos: .userInitiated)
    let stream = SCStream(filter: filter, configuration: configuration, delegate: output)
    do {
        try stream.addStreamOutput(output, type: .audio, sampleHandlerQueue: queue)
    } catch {
        throw HelperError.stream(error.localizedDescription)
    }
    try startStream(stream, output: output)
}

func availableMicrophones() -> [AVCaptureDevice] {
    let defaultID = AVCaptureDevice.default(for: .audio)?.uniqueID
    let discovery = AVCaptureDevice.DiscoverySession(
        deviceTypes: [.microphone],
        mediaType: .audio,
        position: .unspecified
    )
    return discovery.devices.sorted { lhs, rhs in
        let lhsDefault = lhs.uniqueID == defaultID
        let rhsDefault = rhs.uniqueID == defaultID
        if lhsDefault != rhsDefault { return lhsDefault }
        let nameOrder = lhs.localizedName.localizedCaseInsensitiveCompare(rhs.localizedName)
        if nameOrder != .orderedSame { return nameOrder == .orderedAscending }
        return lhs.uniqueID < rhs.uniqueID
    }
}

func listMicrophones() throws {
    var seen = Set<String>()
    let names = availableMicrophones().compactMap { device -> String? in
        let name = device.localizedName.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !name.isEmpty, !seen.contains(name) else { return nil }
        seen.insert(name)
        return name
    }
    let data = try JSONEncoder().encode(names)
    FileHandle.standardOutput.write(data)
}

func ensureMicrophonePermission() throws {
    switch AVCaptureDevice.authorizationStatus(for: .audio) {
    case .authorized:
        return
    case .notDetermined:
        let semaphore = DispatchSemaphore(value: 0)
        var granted = false
        AVCaptureDevice.requestAccess(for: .audio) { value in
            granted = value
            semaphore.signal()
        }
        semaphore.wait()
        if !granted { throw HelperError.microphonePermission }
    default:
        throw HelperError.microphonePermission
    }
}

func captureMicrophone(args: [String]) throws {
    try ensureMicrophonePermission()

    let requestedName = (value(after: "--device", in: args) ?? "")
        .trimmingCharacters(in: .whitespacesAndNewlines)
    guard !requestedName.isEmpty else {
        throw HelperError.microphoneNotFound("")
    }
    guard let device = availableMicrophones().first(where: { $0.localizedName == requestedName }) else {
        throw HelperError.microphoneNotFound(requestedName)
    }

    let sampleRate = max(8_000, min(192_000, Int(value(after: "--sample-rate", in: args) ?? "48000") ?? 48_000))
    let channels = max(1, min(2, Int(value(after: "--channels", in: args) ?? "2") ?? 2))

    let session = AVCaptureSession()
    session.beginConfiguration()

    let input: AVCaptureDeviceInput
    do {
        input = try AVCaptureDeviceInput(device: device)
    } catch {
        throw HelperError.microphoneCapture(error.localizedDescription)
    }
    guard session.canAddInput(input) else {
        throw HelperError.microphoneCapture("устройство нельзя добавить в capture session")
    }
    session.addInput(input)

    let audioOutput = AVCaptureAudioDataOutput()
    audioOutput.audioSettings = [
        AVFormatIDKey: kAudioFormatLinearPCM,
        AVSampleRateKey: sampleRate,
        AVNumberOfChannelsKey: channels,
        AVLinearPCMBitDepthKey: 16,
        AVLinearPCMIsFloatKey: false,
        AVLinearPCMIsBigEndianKey: false,
        AVLinearPCMIsNonInterleaved: false,
    ]
    guard session.canAddOutput(audioOutput) else {
        throw HelperError.microphoneCapture("PCM output нельзя добавить в capture session")
    }
    session.addOutput(audioOutput)

    let delegate = MicrophoneOutput()
    let queue = DispatchQueue(label: "ru.linkvideo.monitor.microphone", qos: .userInitiated)
    audioOutput.setSampleBufferDelegate(delegate, queue: queue)

    session.commitConfiguration()
    session.startRunning()
    guard session.isRunning else {
        throw HelperError.microphoneCapture("capture session не запустилась")
    }

    withExtendedLifetime((session, input, audioOutput, delegate, queue)) {
        RunLoop.main.run()
    }
}

func main() throws {
    let args = Array(CommandLine.arguments.dropFirst())
    if args.contains("--check-permission") {
        let granted = CGPreflightScreenCaptureAccess()
        print(granted ? "granted" : "denied")
        exit(granted ? 0 : 1)
    }
    if args.contains("--request-permission") {
        let granted = CGRequestScreenCaptureAccess()
        print(granted ? "granted" : "denied")
        exit(granted ? 0 : 1)
    }
    if args.contains("--list-displays") {
        let content = try shareableContent()
        let data = try JSONEncoder().encode(displayInfos(content))
        FileHandle.standardOutput.write(data)
        return
    }
    if args.contains("--list-microphones") {
        try listMicrophones()
        return
    }
    if args.contains("--capture-microphone") {
        try captureMicrophone(args: args)
        return
    }
    if args.contains("--capture-audio") {
        try captureAudio()
        return
    }
    if args.contains("--capture") {
        try capture(args: args)
        return
    }
    fputs("Usage: linkvideo-capture-helper --check-permission | --request-permission | --list-displays | --list-microphones | --capture | --capture-audio | --capture-microphone ...\n", stderr)
    exit(2)
}

do {
    try main()
} catch {
    fputs("\(error.localizedDescription)\n", stderr)
    exit(1)
}
import Foundation
import AppKit
import CoreGraphics
import Darwin

private let overlaySize = NSSize(width: 214, height: 36)
private let overlayMargin: CGFloat = 12

struct OverlayPosition: Codable {
    let x: Int
    let y: Int
}

func value(after key: String, in args: [String]) -> String? {
    guard let index = args.firstIndex(of: key), index + 1 < args.count else { return nil }
    return args[index + 1]
}

func displayID(for screen: NSScreen) -> CGDirectDisplayID? {
    guard let number = screen.deviceDescription[NSDeviceDescriptionKey("NSScreenNumber")] as? NSNumber else {
        return nil
    }
    return CGDirectDisplayID(number.uint32Value)
}

func cgBounds(for screen: NSScreen) -> CGRect? {
    guard let id = displayID(for: screen) else { return nil }
    return CGDisplayBounds(id)
}

func screenContainingAppKitPoint(_ point: NSPoint) -> NSScreen? {
    NSScreen.screens.first(where: { $0.frame.contains(point) }) ?? NSScreen.main ?? NSScreen.screens.first
}

func screenContainingCGPoint(_ point: CGPoint) -> NSScreen? {
    for screen in NSScreen.screens {
        if let bounds = cgBounds(for: screen), bounds.contains(point) {
            return screen
        }
    }
    return NSScreen.main ?? NSScreen.screens.first
}

func clampOrigin(_ origin: NSPoint, size: NSSize, to screen: NSScreen) -> NSPoint {
    let visible = screen.visibleFrame
    let minX = visible.minX + overlayMargin
    let minY = visible.minY + overlayMargin
    let maxX = max(minX, visible.maxX - size.width - overlayMargin)
    let maxY = max(minY, visible.maxY - size.height - overlayMargin)
    return NSPoint(
        x: min(max(origin.x, minX), maxX),
        y: min(max(origin.y, minY), maxY)
    )
}

func appKitOriginFromCG(x: Int, y: Int, size: NSSize) -> NSPoint {
    if x == -1 && y == -1 {
        let screen = NSScreen.main ?? NSScreen.screens.first!
        return clampOrigin(
            NSPoint(x: screen.visibleFrame.maxX - size.width - 16,
                    y: screen.visibleFrame.minY + 16),
            size: size,
            to: screen
        )
    }

    let center = CGPoint(x: CGFloat(x) + size.width / 2, y: CGFloat(y) + size.height / 2)
    guard let screen = screenContainingCGPoint(center), let bounds = cgBounds(for: screen) else {
        return NSPoint(x: CGFloat(x), y: CGFloat(y))
    }
    let localX = CGFloat(x) - bounds.minX
    let localYFromTop = CGFloat(y) - bounds.minY
    let origin = NSPoint(
        x: screen.frame.minX + localX,
        y: screen.frame.maxY - localYFromTop - size.height
    )
    return clampOrigin(origin, size: size, to: screen)
}

func cgPosition(for frame: NSRect) -> OverlayPosition {
    let center = NSPoint(x: frame.midX, y: frame.midY)
    guard let screen = screenContainingAppKitPoint(center), let bounds = cgBounds(for: screen) else {
        return OverlayPosition(x: Int(frame.minX.rounded()), y: Int(frame.minY.rounded()))
    }
    let localX = frame.minX - screen.frame.minX
    let localYFromTop = screen.frame.maxY - frame.maxY
    return OverlayPosition(
        x: Int((bounds.minX + localX).rounded()),
        y: Int((bounds.minY + localYFromTop).rounded())
    )
}

final class OverlayView: NSView {
    let placing: Bool
    private var dragStartMouse = NSPoint.zero
    private var dragStartOrigin = NSPoint.zero

    init(frame frameRect: NSRect, placing: Bool) {
        self.placing = placing
        super.init(frame: frameRect)
        wantsLayer = true
        layer?.backgroundColor = NSColor(calibratedWhite: placing ? 0.19 : 0.15, alpha: placing ? 0.94 : 0.86).cgColor
        layer?.cornerRadius = 10

        let label = NSTextField(labelWithString: "Ведётся запись экрана")
        label.textColor = NSColor(calibratedWhite: 0.96, alpha: 1)
        label.font = NSFont.systemFont(ofSize: 13, weight: .medium)
        label.alignment = .center
        label.translatesAutoresizingMaskIntoConstraints = false
        addSubview(label)
        NSLayoutConstraint.activate([
            label.leadingAnchor.constraint(equalTo: leadingAnchor, constant: 10),
            label.trailingAnchor.constraint(equalTo: trailingAnchor, constant: -10),
            label.centerYAnchor.constraint(equalTo: centerYAnchor),
        ])
    }

    required init?(coder: NSCoder) { nil }

    override var acceptsFirstResponder: Bool { placing }

    override func mouseDown(with event: NSEvent) {
        guard placing, let window else { return }
        dragStartMouse = NSEvent.mouseLocation
        dragStartOrigin = window.frame.origin
    }

    override func mouseDragged(with event: NSEvent) {
        guard placing, let window else { return }
        let current = NSEvent.mouseLocation
        var origin = NSPoint(
            x: dragStartOrigin.x + current.x - dragStartMouse.x,
            y: dragStartOrigin.y + current.y - dragStartMouse.y
        )
        let center = NSPoint(x: origin.x + overlaySize.width / 2, y: origin.y + overlaySize.height / 2)
        if let screen = screenContainingAppKitPoint(center) {
            origin = clampOrigin(origin, size: overlaySize, to: screen)
        }
        window.setFrameOrigin(origin)
    }

    override func mouseUp(with event: NSEvent) {
        guard placing, let window else { return }
        let position = cgPosition(for: window.frame)
        if let data = try? JSONEncoder().encode(position) {
            FileHandle.standardOutput.write(data)
            FileHandle.standardOutput.write(Data("\n".utf8))
        }
        NSApp.terminate(nil)
    }
}

func makeWindow(x: Int, y: Int, placing: Bool) -> NSWindow {
    let origin = appKitOriginFromCG(x: x, y: y, size: overlaySize)
    let window = NSWindow(
        contentRect: NSRect(origin: origin, size: overlaySize),
        styleMask: [.borderless],
        backing: .buffered,
        defer: false
    )
    window.isOpaque = false
    window.backgroundColor = .clear
    window.hasShadow = true
    window.level = .floating
    window.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .stationary]
    window.hidesOnDeactivate = false
    window.isReleasedWhenClosed = false
    window.ignoresMouseEvents = !placing
    window.contentView = OverlayView(frame: NSRect(origin: .zero, size: overlaySize), placing: placing)
    return window
}

let args = Array(CommandLine.arguments.dropFirst())
let placing = args.contains("--place-overlay")
let overlay = args.contains("--overlay")
if !placing && !overlay {
    fputs("Usage: linkvideo-overlay-helper --overlay|--place-overlay --x N --y N\n", stderr)
    exit(2)
}

let x = Int(value(after: "--x", in: args) ?? "-1") ?? -1
let y = Int(value(after: "--y", in: args) ?? "-1") ?? -1
let app = NSApplication.shared
app.setActivationPolicy(.accessory)
let window = makeWindow(x: x, y: y, placing: placing)
window.orderFrontRegardless()

var escapeMonitor: Any?
if placing {
    app.activate(ignoringOtherApps: true)
    window.makeKeyAndOrderFront(nil)
    window.makeFirstResponder(window.contentView)
    escapeMonitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { event in
        if event.keyCode == 53 {
            exit(5)
        }
        return event
    }
}

withExtendedLifetime((window, escapeMonitor)) {
    app.run()
}

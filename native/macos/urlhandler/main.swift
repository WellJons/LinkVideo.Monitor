import AppKit
import Foundation

private let supportedScheme = "linkvideomonitor"

final class URLHandlerDelegate: NSObject, NSApplicationDelegate {
    private var handledURL = false

    func applicationDidFinishLaunching(_ notification: Notification) {
        // A URL-open Apple Event is delivered immediately after launch. Avoid
        // leaving an invisible helper behind if Launch Services started it
        // without a URL for some reason.
        DispatchQueue.main.asyncAfter(deadline: .now() + 2.0) { [weak self] in
            guard let self, !self.handledURL else { return }
            NSApplication.shared.terminate(nil)
        }
    }

    func application(_ application: NSApplication, open urls: [URL]) {
        for url in urls where url.scheme?.lowercased() == supportedScheme {
            handledURL = true
            do {
                try launchMonitor(with: url.absoluteString)
            } catch {
                fputs("LinkVideo URL handler: \(error)\n", stderr)
            }
        }
        application.terminate(nil)
    }

    private func launchMonitor(with rawURL: String) throws {
        let handlerBundle = Bundle.main.bundleURL
        let parentApp = handlerBundle
            .deletingLastPathComponent() // Helpers
            .deletingLastPathComponent() // Library
            .deletingLastPathComponent() // Contents
            .deletingLastPathComponent() // LinkVideo.Monitor.app
        let executable = parentApp
            .appendingPathComponent("Contents", isDirectory: true)
            .appendingPathComponent("MacOS", isDirectory: true)
            .appendingPathComponent("LinkVideo.Monitor", isDirectory: false)

        guard FileManager.default.isExecutableFile(atPath: executable.path) else {
            throw NSError(
                domain: "ru.linkvideo.monitor.url-handler",
                code: 1,
                userInfo: [NSLocalizedDescriptionKey: "main LinkVideo.Monitor executable not found at \(executable.path)"]
            )
        }

        let process = Process()
        process.executableURL = executable
        process.arguments = [rawURL]
        try process.run()
    }
}

let app = NSApplication.shared
app.setActivationPolicy(.accessory)
let delegate = URLHandlerDelegate()
app.delegate = delegate
app.run()

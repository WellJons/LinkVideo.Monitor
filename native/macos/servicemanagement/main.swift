import Foundation
import ServiceManagement

private let agentPlistName = "ru.linkvideo.monitor.autostart.plist"

func statusName(_ status: SMAppService.Status) -> String {
    switch status {
    case .notRegistered: return "not-registered"
    case .enabled: return "enabled"
    case .requiresApproval: return "requires-approval"
    case .notFound: return "not-found"
    @unknown default: return "unknown"
    }
}

func service() -> SMAppService {
    SMAppService.agent(plistName: agentPlistName)
}

func setStartup(_ enabled: Bool) throws {
    let item = service()
    if enabled {
        switch item.status {
        case .enabled, .requiresApproval:
            break
        case .notRegistered, .notFound:
            try item.register()
        @unknown default:
            try item.register()
        }
    } else {
        switch item.status {
        case .notRegistered, .notFound:
            break
        case .enabled, .requiresApproval:
            try item.unregister()
        @unknown default:
            try item.unregister()
        }
    }
    print(statusName(item.status))
}

func parentMonitorExecutable() throws -> URL {
    let helperApp = Bundle.main.bundleURL
    let parentContents = helperApp
        .deletingLastPathComponent() // Resources
        .deletingLastPathComponent() // Contents
    let executable = parentContents
        .appendingPathComponent("MacOS", isDirectory: true)
        .appendingPathComponent("LinkVideo.Monitor", isDirectory: false)
    guard FileManager.default.isExecutableFile(atPath: executable.path) else {
        throw NSError(
            domain: "LinkVideoServiceHelper",
            code: 3,
            userInfo: [NSLocalizedDescriptionKey: "Не найден основной LinkVideo Monitor: \(executable.path)"]
        )
    }
    return executable
}

func runMonitorInBackground() throws {
    let process = Process()
    process.executableURL = try parentMonitorExecutable()
    process.arguments = ["--background"]
    try process.run()
}

func main() throws {
    let args = Array(CommandLine.arguments.dropFirst())
    if args.contains("--startup-status") {
        print(statusName(service().status))
        return
    }
    if args.contains("--autostart-run") {
        try runMonitorInBackground()
        return
    }
    if let index = args.firstIndex(of: "--set-startup"), index + 1 < args.count {
        guard let enabled = Bool(args[index + 1]) else {
            throw NSError(domain: "LinkVideoServiceHelper", code: 2, userInfo: [NSLocalizedDescriptionKey: "Неверное значение --set-startup"])
        }
        try setStartup(enabled)
        return
    }
    fputs("Usage: LinkVideoServiceHelper --startup-status | --set-startup true|false | --autostart-run\n", stderr)
    exit(2)
}

do {
    try main()
} catch {
    fputs("\(error.localizedDescription)\n", stderr)
    exit(1)
}

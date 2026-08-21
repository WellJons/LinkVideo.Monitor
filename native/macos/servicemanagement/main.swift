import Foundation

func parentMonitorExecutable() throws -> URL {
    let helperApp = Bundle.main.bundleURL
    // <Parent>.app/Contents/Library/LoginItems/LinkVideoServiceHelper.app
    // Three parents from the helper bundle lead to <Parent>.app/Contents.
    let parentContents = helperApp
        .deletingLastPathComponent() // LoginItems
        .deletingLastPathComponent() // Library
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
    if args.isEmpty || args.contains("--autostart-run") {
        try runMonitorInBackground()
        return
    }
    fputs("Usage: LinkVideoServiceHelper [--autostart-run]\n", stderr)
    exit(2)
}

do {
    try main()
} catch {
    fputs("\(error.localizedDescription)\n", stderr)
    exit(1)
}

import Foundation
import AppKit
import Darwin

func value(after key: String, in args: [String]) -> String? {
    guard let index = args.firstIndex(of: key), index + 1 < args.count else { return nil }
    return args[index + 1]
}

func emit(_ event: String) {
    FileHandle.standardOutput.write(Data((event + "\n").utf8))
}

func parentIsAlive(_ pid: pid_t) -> Bool {
    guard pid > 1 else { return true }
    if kill(pid, 0) == 0 { return true }
    return errno == EPERM
}

let args = Array(CommandLine.arguments.dropFirst())
let parentPID = pid_t(Int(value(after: "--parent-pid", in: args) ?? "0") ?? 0)
let center = NSWorkspace.shared.notificationCenter
var observers: [NSObjectProtocol] = []

observers.append(center.addObserver(
    forName: NSWorkspace.didWakeNotification,
    object: nil,
    queue: .main
) { _ in emit("wake") })

observers.append(center.addObserver(
    forName: NSWorkspace.screensDidWakeNotification,
    object: nil,
    queue: .main
) { _ in emit("screens-wake") })

observers.append(center.addObserver(
    forName: NSWorkspace.sessionDidBecomeActiveNotification,
    object: nil,
    queue: .main
) { _ in emit("session-active") })

if parentPID > 1 {
    Timer.scheduledTimer(withTimeInterval: 2.0, repeats: true) { _ in
        if !parentIsAlive(parentPID) {
            exit(0)
        }
    }
}

withExtendedLifetime(observers) {
    RunLoop.main.run()
}

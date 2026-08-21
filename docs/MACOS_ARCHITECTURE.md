# macOS platform isolation

LinkVideo Monitor keeps one shared repository, but OS-specific implementations are isolated at compile time.

- Windows implementation: `*_windows.go` and Windows native helpers.
- macOS implementation: `*_darwin.go` and `native/macos/**`.
- Linux/unsupported implementation: `*_other.go` stubs where required.
- Shared Go files contain only platform-neutral configuration, HTTP/API, stream orchestration and audio/video pipeline logic.

A platform implementation must not invoke executables, process names, session APIs or installer behavior belonging to another OS. New shared hooks should be narrow and should delegate to platform files.

Every macOS feature PR must keep Windows CI, Windows race/installer tests, Linux CI and macOS CI green before merge.

# LinkVideo Monitor macOS parity checklist

Цель macOS-версии — сохранить пользовательский функционал Windows-версии, реализуя системно-зависимые части отдельными Darwin-файлами и native helpers.

## Обязательные функциональные блоки

1. **Установка и удаление**
   - [x] Development `.app`, ZIP and DMG packaging
   - [x] Development `.pkg` installer to `/Applications`
   - [x] macOS uninstall command with ServiceManagement cleanup
   - [x] Uninstall preserves settings/logs by default and supports explicit `--purge-data`
   - [x] macOS ServiceManagement autostart/login item
   - [ ] Production Developer ID Application/Installer signing
   - [ ] Apple notarization
   - [ ] Physical-Mac installer/upgrade/uninstaller validation

2. **Трансляция потока**
   - [x] ScreenCaptureKit display capture
   - [x] Single-display capture
   - [x] Multi-display / «Все экраны» composition
   - [x] Local RTSP MediaMTX platform runtime
   - [x] Remote RTSP/RTMP shared publishing pipeline
   - [ ] Physical-Mac long-running stream validation

3. **Настройки программы**
   - [x] Shared config/API/Web UI retained
   - [x] macOS monitor enumeration
   - [x] macOS autostart implementation
   - [x] macOS recording overlay and placement
   - [x] System audio and microphone settings retained
   - [x] Privacy protection backed by macOS Accessibility geometry and shared sensitive-field rules
   - [x] `linkvideomonitor:` deep links routed through a native macOS URL handler into the shared parser
   - [ ] Physical-Mac Accessibility permission + browser password/card/OTP validation
   - [ ] Physical-Mac browser/site deep-link validation
   - [ ] Final audit of every Windows-visible setting for macOS support/wording

4. **Видеокодеки и проверки**
   - [x] H.264 VideoToolbox
   - [x] H.265 VideoToolbox
   - [x] Encoder capability probing/fallback through shared pipeline
   - [x] Universal arm64 + x86_64 app/helpers
   - [ ] Physical-Mac H.264/H.265 hardware encode validation under load

5. **Синхронизация звука и видео**
   - [x] ScreenCaptureKit system audio
   - [x] AVFoundation microphone capture
   - [x] Shared FFmpeg system+microphone mixing pipeline retained
   - [x] Existing audio advance/sync setting retained
   - [ ] Physical-Mac A/V drift validation over long recordings/streams

6. **Работа при блокировке/сне экрана**
   - [x] Darwin session-lock detection through macOS session state
   - [x] Display-sleep detection separate from session lock
   - [x] Safe stale-frame policy if ScreenCaptureKit stops after lock
   - [x] Native wake/screens-wake/session-active events
   - [x] Restart/recovery after wake retained
   - [ ] Physical-Mac lock screen / display sleep / lid close / Fast User Switching matrix

## Дополнительный parity

- [x] System audio capture
- [x] Microphone capture
- [x] Microphone mute/voice/PTT shared runtime logic
- [x] Global macOS PTT/mute hotkeys
- [x] Native recording overlay
- [x] Sleep prevention / keep display on
- [x] Platform-specific update manifest and update capability reporting
- [ ] Signed/notarized automatic macOS updater
- [ ] Bundled pinned Universal FFmpeg
- [ ] Bundled pinned Universal MediaMTX

## Правило архитектуры

Windows-specific implementations remain in `*_windows.go`/Windows native code. macOS implementations live in `*_darwin.go` and `native/macos/**`. Shared files contain only OS-neutral business logic or narrow platform interfaces. Any shared change must pass Windows CI, Windows race/installer tests, Linux CI and macOS CI before merge.

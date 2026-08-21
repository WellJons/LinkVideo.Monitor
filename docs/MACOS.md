# macOS port

LinkVideo Monitor развивается в одном репозитории для Windows, macOS и Linux. Сборки, версии, установщики и update-каналы при этом независимы.

## Архитектура

Общая бизнес-логика остаётся в `cmd/linkvideo-monitor`. Платформенные реализации подключаются через Go build constraints и нативные helpers.

Windows продолжает использовать `*_windows.go`, DXGI/GDI, WASAPI, Win32 UI, Windows installer и Windows runtime-компоненты. macOS использует `*_darwin.go`, ScreenCaptureKit/AVFoundation/AppKit/ApplicationServices helpers и отдельную `.app`/`.pkg`/`.dmg` упаковку.

## Что уже работает на macOS

- Screen Recording permission и ScreenCaptureKit display capture;
- реальный список дисплеев и выбор конкретного `SCDisplayID`;
- композиция режима «Все экраны», включая mixed Retina/non-Retina geometry;
- ScreenCaptureKit -> BGRA -> существующий `captureSupervisor`;
- системный звук через ScreenCaptureKit -> stereo 48 kHz S16LE -> общий audio bridge/FFmpeg mix;
- список микрофонов и захват выбранного микрофона через AVFoundation -> stereo 48 kHz S16LE;
- mute, level meter, voice activation, push-to-talk и глобальные PTT/mute hotkeys;
- предотвращение idle sleep/гашения дисплея через `/usr/bin/caffeinate`;
- отдельное определение session lock, display sleep, wake и Fast User Switching;
- stale-frame protection, если ScreenCaptureKit перестаёт отдавать кадры после блокировки;
- автозапуск через ServiceManagement `SMAppService` и bundled Login Item application;
- native AppKit recording overlay и изменение его положения;
- `linkvideomonitor:` deep-link handler через Launch Services;
- privacy protection через macOS Accessibility без чтения значений password/card/OTP;
- общий FFmpeg/RTSP/RTMP pipeline;
- аппаратный H.264/H.265 через Apple VideoToolbox (`h264_videotoolbox` / `hevc_videotoolbox`);
- realtime encoder probing и software fallback;
- GOP 2 секунды и отключённые B-frames;
- Universal приложение и helpers `arm64 + x86_64`;
- отдельная macOS release-версия `0.1.x` и `update-manifest-macos.json`;
- `.app`, ZIP, PKG и DMG packaging;
- минимальная система: macOS 13 Ventura.

## Системный звук

На Windows общий audio bridge получает PCM из WASAPI Loopback. На macOS тот же bridge вызывает отдельный режим ScreenCaptureKit helper (`--capture-audio`). Helper выдаёт 48 kHz stereo, преобразует системный Float32 PCM в interleaved signed 16-bit little-endian и пишет его в существующий локальный TCP audio channel.

## Микрофон

Windows по-прежнему использует существующий FFmpeg DirectShow source. На macOS список устройств и PCM приходят из AVFoundation helper (`--list-microphones` / `--capture-microphone`). После этого обработка полностью общая: level meter, mute, `always`, voice activation и push-to-talk работают в существующем Go microphone bridge, а system audio + microphone микшируются в общем FFmpeg pipeline.

`NSMicrophoneUsageDescription` включён в macOS `Info.plist`. Production build будет подписан Developer ID и notarized.

## Сон, блокировка и дисплей

`PreventSleep` и `KeepDisplayOn` на macOS используют `caffeinate`. Session lock и физический sleep дисплеев определяются отдельно системными macOS API; наличие процесса `loginwindow` само по себе не используется как критерий блокировки.

Если при lock ScreenCaptureKit продолжает отдавать настоящий lock screen, поток его сохраняет. Если кадры останавливаются, Monitor не повторяет бесконечно последний desktop frame и переключается на безопасный fallback. При фактическом sleep дисплея используется фирменный LinkVideo frame. Wake, screens-wake и session-active/Fast User Switching обрабатываются отдельным AppKit helper без Windows process assumptions.

## Автозапуск

Историческое поле конфигурации `launch_with_windows` сохраняется ради совместимости JSON/API, но на macOS означает запуск при входе пользователя в систему.

В bundle находится `Contents/Library/LoginItems/LinkVideoServiceHelper.app`. Регистрацией управляет главный процесс LinkVideo Monitor через Darwin-only bridge к `SMAppService.loginItem(identifier:)`. Legacy LaunchAgent не используется.

## MediaMTX

macOS `.app` содержит собственный pinned Universal MediaMTX, поэтому локальный RTSP после установки не зависит от Homebrew или `PATH`.

Build script скачивает официальные upstream `darwin_arm64` и `darwin_amd64` release assets, сверяет их с опубликованным `checksums.sha256`, при доступном GitHub token дополнительно проверяет GitHub attestation, а затем объединяет бинарники через `lipo` в `arm64+x86_64`. Darwin release line независима от Windows MediaMTX version/download policy.

`MEDIAMTX_BINARY` можно использовать как явный release override. `MACOS_USE_SYSTEM_MEDIAMTX=1` оставляет development-only режим поиска `LINKVIDEO_MEDIAMTX`, Homebrew и `PATH`.

## FFmpeg в development-сборке

Финальный публичный релиз должен содержать собственный проверенный Universal FFmpeg. Пока этот этап не завершён, development `.app` включает launcher с историческим именем `ffmpeg.exe`, чтобы сохранить совместимость существующей конфигурации.

Launcher ищет FFmpeg в таком порядке:

1. `LINKVIDEO_FFMPEG`;
2. `/opt/homebrew/bin/ffmpeg`;
3. `/usr/local/bin/ffmpeg`;
4. `ffmpeg` из `PATH`.

Для development-теста пока можно установить:

```bash
brew install ffmpeg
```

Доступность VideoToolbox проверяется самим FFmpeg перед запуском; при ошибке общий механизм выбора кодировщика использует software fallback.

## Что ещё требует физического Mac / production release

- Developer ID Application и Developer ID Installer signing;
- Apple notarization;
- собственный pinned Universal FFmpeg;
- подписанный/notarized automatic updater;
- install/upgrade/uninstall validation на физическом Mac;
- длительный RTSP/RTMP stream test;
- H.264/H.265 VideoToolbox load test;
- длительный A/V drift test;
- Screen Recording/Microphone/Accessibility TCC matrix;
- lock screen, display sleep, lid close и Fast User Switching matrix;
- Safari/Chrome/Firefox privacy fields и browser deep-link validation.

## Release

Windows release pipeline от macOS packaging и version channel независим. Перед публичным macOS release bundle, nested helpers и runtime-компоненты должны быть подписаны Developer ID, пакет — Developer ID Installer, затем итоговые артефакты должны пройти notarization/stapling и физическую проверку на чистой macOS 13+ системе.

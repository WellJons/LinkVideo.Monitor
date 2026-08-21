# macOS port

LinkVideo Monitor развивается в одном репозитории для Windows, macOS и Linux. Сборки, версии, установщики и update-каналы при этом независимы.

## Архитектура

Общая логика остаётся в `cmd/linkvideo-monitor`. Платформенные реализации подключаются через Go build constraints и нативные helpers.

Windows продолжает использовать `*_windows.go`, DXGI/GDI, WASAPI, Win32 UI, Windows installer и Windows FFmpeg. macOS использует `*_darwin.go`, ScreenCaptureKit helper и отдельную `.app`/`.dmg` упаковку.

## Что уже работает на macOS

- Screen Recording permission через системный API;
- получение реального списка дисплеев;
- выбор конкретного дисплея по `SCDisplayID`;
- ScreenCaptureKit -> BGRA -> существующий `captureSupervisor`;
- общий FFmpeg/RTSP/RTMP pipeline;
- Universal приложение `arm64 + x86_64`;
- независимая macOS release-версия `0.1.x`;
- отдельный `update-manifest-macos.json`;
- `.app`, ZIP и development DMG;
- минимальная система: macOS 13 Ventura.

## FFmpeg в development-сборке

Финальный публичный релиз будет содержать собственный подписанный Universal FFmpeg. Пока этого бинарника нет, development `.app` включает совместимый launcher с историческим именем `ffmpeg.exe`, чтобы не менять рабочую Windows-конфигурацию.

Launcher ищет FFmpeg в таком порядке:

1. `LINKVIDEO_FFMPEG`;
2. `/opt/homebrew/bin/ffmpeg` (Apple Silicon Homebrew);
3. `/usr/local/bin/ffmpeg` (Intel Homebrew);
4. `ffmpeg` из `PATH`.

Для первого теста на обычном Mac достаточно установить FFmpeg:

```bash
brew install ffmpeg
```

После этого можно открыть `LinkVideo.Monitor.app` из development DMG. При первом обращении к экрану macOS должна запросить разрешение Screen Recording.

## Ограничения текущего этапа

- режим одного выбранного дисплея уже сопоставляется с настоящим Mac-дисплеем;
- полноценная композиция нескольких дисплеев в режиме «все экраны» ещё не реализована;
- VideoToolbox пока не включён в общий automatic encoder selection;
- системный звук и микрофон ещё не перенесены;
- публичный релиз ещё не подписан Developer ID и не notarized;
- development FFmpeg launcher не является финальным способом поставки FFmpeg.

## Release

Публичная macOS-сборка перед включением автообновления должна получить Developer ID signing, notarization Apple и вложенный Universal FFmpeg. Windows release pipeline от этих изменений независим.

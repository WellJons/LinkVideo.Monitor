# macOS port

LinkVideo Monitor развивается в одном репозитории для Windows, macOS и Linux. Сборки, версии, установщики и update-каналы при этом независимы.

## Архитектура

Общая логика остаётся в `cmd/linkvideo-monitor`. Платформенные реализации подключаются через Go build constraints и нативные helpers.

Windows продолжает использовать `*_windows.go`, DXGI/GDI, WASAPI, Win32 UI, Windows installer и Windows FFmpeg. macOS использует `*_darwin.go`, ScreenCaptureKit/AVFoundation helper и отдельную `.app`/`.dmg` упаковку.

## Что уже работает на macOS

- Screen Recording permission через системный API;
- получение реального списка дисплеев;
- выбор конкретного дисплея по `SCDisplayID`;
- ScreenCaptureKit -> BGRA -> существующий `captureSupervisor`;
- системный звук через ScreenCaptureKit -> stereo 48 kHz S16LE -> существующий общий audio bridge/FFmpeg mix;
- список микрофонов и захват выбранного микрофона через AVFoundation -> stereo 48 kHz S16LE;
- общий microphone bridge сохраняет mute, level meter, voice activation и push-to-talk логику;
- предотвращение idle sleep и, при необходимости, гашения дисплея через системный `/usr/bin/caffeinate`;
- общий FFmpeg/RTSP/RTMP pipeline;
- аппаратный H.264/H.265 через Apple VideoToolbox (`h264_videotoolbox` / `hevc_videotoolbox`);
- общий realtime-probe и автоматический fallback с VideoToolbox на программный x264/x265, если аппаратный encoder недоступен;
- GOP остаётся фиксированным в 2 секунды и B-frames отключены так же, как на Windows;
- Universal приложение `arm64 + x86_64`;
- независимая macOS release-версия `0.1.x`;
- отдельный `update-manifest-macos.json`;
- `.app`, ZIP и development DMG;
- минимальная система: macOS 13 Ventura.

## Системный звук

На Windows общий audio bridge получает PCM из WASAPI Loopback. На macOS тот же bridge вызывает отдельный режим ScreenCaptureKit helper (`--capture-audio`). Helper просит ScreenCaptureKit выдавать 48 kHz stereo, преобразует системный Float32 PCM в interleaved signed 16-bit little-endian и пишет его в существующий локальный TCP audio channel.

## Микрофон

Windows по-прежнему использует существующий FFmpeg DirectShow source. На macOS список устройств и PCM приходят из AVFoundation helper (`--list-microphones` / `--capture-microphone`). Helper запрашивает стандартное разрешение macOS на микрофон, выбирает устройство по имени и выдаёт 48 kHz stereo S16LE.

После этого обработка полностью общая: level meter, mute, `always`, voice activation и push-to-talk работают в существующем Go microphone bridge, а system audio + microphone микшируются в общем FFmpeg pipeline.

`NSMicrophoneUsageDescription` уже включён в macOS `Info.plist`. Для development ad-hoc build запрос разрешения должен появиться при первом включении микрофона; production build будет подписан Developer ID и notarized.

## Сон и дисплей

Общие настройки `PreventSleep` и `KeepDisplayOn` теперь работают на macOS. Пока поток должен быть активен, Monitor запускает системный `caffeinate` с `-i`; если включено сохранение дисплея — дополнительно с `-d`. Параметр `-w` привязывает assertion к PID LinkVideo Monitor, поэтому macOS автоматически освободит его при завершении приложения даже после аварийного выхода.

При остановке или ошибке потока дочерний `caffeinate` завершается сразу. Windows продолжает использовать `SetThreadExecutionState`, поэтому изменения платформенно изолированы.

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

После этого можно открыть `LinkVideo.Monitor.app` из development DMG. При первом обращении к экрану или системному звуку macOS должна запросить разрешение Screen Recording, а при первом включении микрофона — отдельное разрешение Microphone. Доступность VideoToolbox дополнительно проверяется самим FFmpeg перед запуском потока; при ошибке общий механизм выбора кодировщика использует software fallback.

## Ограничения текущего этапа

- режим одного выбранного дисплея уже сопоставляется с настоящим Mac-дисплеем;
- полноценная композиция нескольких дисплеев в режиме «все экраны» ещё не реализована;
- system audio и microphone pipeline перенесены, но требуют реальной проверки TCC/уровней на физическом Mac;
- глобальные macOS hotkeys для режима push-to-talk ещё не перенесены;
- автозапуск при входе в macOS ещё не перенесён;
- публичный релиз ещё не подписан Developer ID и не notarized;
- development FFmpeg launcher не является финальным способом поставки FFmpeg.

## Release

Публичная macOS-сборка перед включением автообновления должна получить Developer ID signing, notarization Apple и вложенный Universal FFmpeg. Windows release pipeline от этих изменений независим.

# macOS port

LinkVideo Monitor развивается в одном репозитории для Windows, macOS и в дальнейшем Linux.

## Принцип

Общая логика остаётся в `cmd/linkvideo-monitor`. Платформенные реализации подключаются через Go build constraints и отдельные нативные helpers там, где системные API удобнее использовать напрямую.

Windows-сборка продолжает использовать существующие `*_windows.go`, DXGI/GDI, WASAPI, Win32 UI, Windows installer и bundled Windows FFmpeg. macOS использует ScreenCaptureKit helper и отдельную упаковку `.app`. Linux будет добавлен тем же способом отдельным платформенным слоем.

Разные платформы не входят в один установщик. Из одного исходного репозитория CI выпускает отдельные артефакты для каждой ОС.

## Текущий macOS этап

Первая стадия переноса намеренно не меняет рабочие Windows-файлы. Добавлены:

- `native/macos/screencapture/main.swift` — ScreenCaptureKit helper;
- `scripts/macos/build-app.sh` — Universal arm64 + x86_64 сборка;
- `packaging/macos/Info.plist` — bundle metadata;
- `.github/workflows/macos-ci.yml` — отдельная macOS проверка.

Helper умеет:

- проверять и запрашивать Screen Recording permission;
- перечислять дисплеи;
- захватывать выбранный дисплей;
- отдавать непрерывные BGRA-кадры через stdout для существующего FFmpeg pipeline.

## Следующий этап

После зелёных Windows и macOS CI ScreenCaptureKit helper будет подключён к существующему `captureSupervisor` как Darwin backend. Затем добавляются VideoToolbox capabilities, системный звук, микрофон, multi-display capture, автозапуск, updater и macOS packaging/signing.

Целевая минимальная версия первой macOS-версии: macOS 13 Ventura. Release build должен быть подписан Developer ID и notarized; ad-hoc signing используется только для CI/dev.

# Платформенная схема LinkVideo Monitor

LinkVideo Monitor хранится в одном репозитории, но каждая операционная система имеет независимую сборку, версию, установщик и канал обновлений.

## Версии

- Windows: текущая стабильная линия `0.8.x`.
- macOS: отдельная линия разработки `0.1.x`.
- Linux: будет иметь собственную линию после появления рабочего capture backend.

Исправление только macOS не требует выпуска новой Windows-версии. Общие исправления ядра становятся доступны каждой ОС при её следующем собственном релизе.

## Платформенный код

Go автоматически выбирает файлы по build tags и суффиксам:

- `*_windows.go` — Windows;
- `*_darwin.go` — macOS;
- `*_linux.go` — Linux;
- общие `.go` — общий core.

Платформенные ресурсы и упаковка разделены аналогично:

- `native/macos/`, `packaging/macos/`, `scripts/macos/`;
- Windows installer и Windows scripts остаются независимыми;
- `packaging/linux/` и `scripts/linux/` будут добавляться по мере реализации Linux-клиента.

## Обновления

Каналы обновлений разделены в `WellJons/LinkVideo.Monitor.Updates`:

- Windows: `update-manifest.json`;
- macOS: `update-manifest-macos.json`;
- Linux: `update-manifest-linux.json` после появления Linux-релиза.

Клиент проверяет не только версию, но и ОС/архитектуру. Для macOS Go-архитектуры называются `arm64` и `amd64` (Intel).

## CI

Изменения общего кода должны компилироваться и тестироваться на Windows, macOS и Linux. Платформенный баг может исправляться только в соответствующем файле, но CI остальных ОС защищает общий core от регрессий.

macOS release build дополнительно должен пройти Developer ID signing и Apple notarization перед включением публичного автоматического обновления.

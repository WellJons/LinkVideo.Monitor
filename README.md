# LinkVideo Monitor

Текущая версия: **0.8.12**.

LinkVideo Monitor — Windows-клиент для передачи изображения с экрана, системного звука и микрофона в LinkVideo по RTSP/RTMP.

## Возможности

- захват всех мониторов или отдельного экрана;
- Desktop Duplication с резервным GDI-захватом;
- H.264 и H.265;
- Intel Quick Sync, NVIDIA NVENC, AMD AMF и программное кодирование;
- системный звук и микрофон в AAC;
- работа с UAC, Secure Desktop и экраном блокировки Windows;
- скрытие паролей, PIN/OTP и платёжных полей в передаваемом изображении;
- автоматический запуск, восстановление после сна и переподключение потока;
- локальный RTSP для LinkVideo Server;
- удалённое применение настроек и обновлений.

## Сборка

Для сборки с поддержкой Windows 7 используется Go 1.20.x. Также нужны Python 3 и Git LFS.

```bat
git lfs pull
scripts\windows\build-release.cmd
```

Готовые файлы появятся в каталоге `build`:

- `LinkVideo.Monitor.exe`
- `LinkVideo.Monitor_0.8.12_Setup.exe`
- `Uninstall.exe`

## Структура проекта

- `cmd/linkvideo-monitor` — приложение, захват экрана, звук, интерфейс и Windows-служба;
- `installer` — установщик и деинсталлятор;
- `api` — описание удалённого API;
- `docs` — техническая документация;
- `scripts/windows` — скрипты сборки;
- `third_party/ffmpeg` — FFmpeg runtime;
- `tools` — вспомогательные утилиты.

## Проверка

Основные тесты запускаются через GitHub Actions. Для ручной проверки Windows-сценариев используется `docs/TESTING.md`.

Перед выпуском отдельно проверяются установка/обновление, Win+L и UAC, несколько мониторов, H.264/H.265, аппаратные кодировщики, звук и длительная RTSP/RTMP-трансляция.

## Документация

- `docs/BUILD.md` — сборка;
- `docs/TESTING.md` — проверка версии;
- `docs/TECHNICAL.md` — технические детали;
- `docs/REMOTE_API.md` — удалённое управление;
- `docs/UPDATES.md` — обновления;
- `docs/SIGNING.md` — подпись Windows-бинарников.

Пользовательское соглашение находится в `licenses/LICENSE_RU.txt`.

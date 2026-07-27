# LinkVideo Monitor 0.7.6 Beta

Windows-приложение для захвата экранов и системного звука с публикацией трансляции в LinkVideo по RTSP/RTMP.

## Возможности

- захват всех мониторов или выбранного экрана с сохранением расположения Windows;
- Desktop Duplication с резервным GDI-захватом;
- работа с горизонтальными и вертикальными мониторами;
- разрешения: исходное, Full HD и HD;
- 10/15/20/25 FPS;
- автоматический и ручной выбор Intel Quick Sync, NVIDIA NVENC, AMD AMF или libx264;
- системный звук AAC;
- служба `LinkVideoMonitorCapture` для UAC и настоящего экрана блокировки Windows;
- пикселизация паролей, PIN, OTP/2FA, CVV/CVC и платёжных полей;
- автозапуск, восстановление после сна и удалённая синхронизация настроек;
- локальный веб-интерфейс.

## Быстрый старт для разработчика

1. Установить Go 1.20 или новее и Python 3.
2. Установить Git LFS и получить FFmpeg: `git lfs pull`.
3. На Windows запустить:

```bat
scripts\windows\build-release.cmd
```

Результаты появятся в `build`:

- `LinkVideo.Monitor.exe`;
- `LinkVideo.Monitor_0.7.6_Setup.exe`.

## Где находится код

- `cmd/linkvideo-monitor` — основное приложение, UI, захват, звук, UAC-служба и вспомогательные режимы;
- `installer` — однофайловый установщик;
- `api` — OpenAPI-описание удалённой синхронизации;
- `docs` — архитектура, сборка, API и передача проекта;
- `scripts/windows` — проверка и сборка;
- `third_party/ffmpeg` — используемый FFmpeg runtime;
- `tools` — служебные инструменты.

Начать изучение рекомендуется с `cmd/linkvideo-monitor/main.go`, затем `capture_pipeline.go`, `encoder_auto.go`, `secure_capture_windows.go`, `uac_service_windows.go` и `privacy_windows.go`.

## Важное ограничение

Захват Secure Desktop/UAC невозможно полноценно проверить вне реальной Windows-системы. Перед выпуском обязательны проверки из `docs/TESTING.md`.

## Лицензирование

Пользовательское соглашение продукта находится в `licenses/LICENSE_RU.txt`. FFmpeg является сторонним компонентом; его условия распространения необходимо проверять отдельно.

## Обновления

Кнопка проверки обновлений использует HTTPS-манифест. Настройка приватного источника описана в `docs/UPDATES.md`.

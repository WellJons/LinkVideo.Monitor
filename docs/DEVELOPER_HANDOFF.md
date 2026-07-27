# Передача проекта разработчикам

## Точки входа

- `cmd/linkvideo-monitor/main.go` — запуск приложения, конфигурация и жизненный цикл потока;
- `cmd/linkvideo-monitor/webui.go` — HTML/CSS/JS локального интерфейса;
- `cmd/linkvideo-monitor/capture_pipeline.go` — канал кадров и переключение методов захвата;
- `cmd/linkvideo-monitor/gdi_capture_windows.go` — GDI-захват и курсор;
- `cmd/linkvideo-monitor/dxgi_outputs_windows.go` — мониторы и DXGI-адаптеры;
- `cmd/linkvideo-monitor/encoder_auto.go` — выбор и отказоустойчивость кодировщиков;
- `cmd/linkvideo-monitor/audio_bridge.go`, `wasapi_windows.go` — системный звук;
- `cmd/linkvideo-monitor/privacy_windows.go`, `privacy_rules.go` — маскирование полей;
- `cmd/linkvideo-monitor/secure_capture_windows.go`, `uac_service_windows.go` — UAC/Secure Desktop;
- `cmd/linkvideo-monitor/remote_control.go` — удалённая синхронизация;
- `installer/main.go` — установка, обновление, служба и удаление.

## Сборка

Используйте только `scripts/windows/build-release.cmd`: он проверяет приложение, собирает EXE, добавляет иконку, формирует временный payload и компилирует установщик.

## Логи

- основной журнал: папка данных LinkVideo Monitor пользователя;
- служба UAC: `C:\ProgramData\LinkVideo.Monitor\uac-service.log`.

## Что требуется проверить первым

1. UAC и возврат на обычный desktop без разрыва RTSP;
2. блокировка/разблокировка и смена сеанса;
3. ложные маски в Chrome/Edge/Firefox и движение маски при прокрутке;
4. синхронизация системного звука;
5. несколько мониторов, включая отрицательные координаты и разные GPU;
6. закрепление аппаратного кодировщика после успешного запуска;
7. архив LinkVideo не менее 30 минут.

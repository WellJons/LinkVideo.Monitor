# Проверка обновлений

Кнопка «Проверить обновления» запрашивает небольшой JSON-манифест по HTTPS.
Адрес встраивается в релизную сборку:

```powershell
go build -ldflags="-s -w -H=windowsgui -X main.defaultUpdateManifestURL=https://updates.example.ru/monitor.json" -o LinkVideo.Monitor.exe ./cmd/linkvideo-monitor
```

Формат приведён в `examples/update-manifest.json`.

Не следует встраивать GitHub Personal Access Token в клиент. Для приватного
репозитория используйте корпоративный API/прокси: он обращается к GitHub с
токеном на сервере, а клиенту отдаёт публичный манифест и временную HTTPS-ссылку
на установщик. До задания адреса кнопка корректно сообщает, что сервер
обновлений не настроен.

# LinkVideo Monitor — API синхронизации v1

Monitor использует уже введённую индивидуальную ссылку `linkvideomonitor:...` как идентификатор. Отдельные ID камеры, аккаунта или компьютера не требуются.

## Запрос

```http
POST /api/monitor/sync
Content-Type: application/json
```

Синхронизация выполняется после запуска, после выхода из сна, после локального сохранения настроек и далее раз в 5 минут. В пользовательском интерфейсе нет переключателя API.

```json
{
  "api_version": 1,
  "connection_link": "linkvideomonitor:...",
  "client": {
    "product": "LinkVideo Monitor",
    "version": "0.6.0-beta",
    "computer_name": "OFFICE-PC",
    "os": "windows",
    "architecture": "amd64"
  },
  "state": {
    "process_running": true,
    "process_pid": 4312,
    "stream_desired": true,
    "streaming": true,
    "ffmpeg_pid": 8216,
    "started_at": "2026-07-26T19:00:00+07:00",
    "restarts": 1,
    "last_error": "",
    "applied_revision": 18,
    "last_command_id": "146"
  },
  "settings": {
    "protocol": "rtsp",
    "fps": 10,
    "bitrate_kbps": 1024,
    "audio_enabled": false
  }
}
```

## Ответ

Сервер возвращает только параметры, которые нужно изменить. Все поля `settings` необязательны.

```json
{
  "success": true,
  "revision": 19,
  "settings": {
    "protocol": "rtsp",
    "fps": 15,
    "bitrate_kbps": 512,
    "audio_enabled": true
  },
  "command": {
    "id": "147",
    "action": "restart_stream"
  }
}
```

## Поддерживаемые настройки

- `protocol`: `rtsp` или `rtmp`;
- `fps`: `10`, `15`, `20` или `25`;
- `bitrate_kbps`: `256`, `512` или `1024`;
- `audio_enabled`: `true` или `false`.

Ссылка подключения дистанционно не меняется.

## Команда

Поддерживается одна команда:

```text
restart_stream
```

`command.id` обязателен. Monitor сохраняет последний выполненный ID и не выполняет одну команду повторно при следующих запросах.

## Revision

`revision` увеличивается сервером при изменении настроек. Monitor сохраняет последнюю применённую ревизию. Если настройки изменились, работающий поток автоматически перезапускается. Отдельно отправлять `restart_stream` в таком случае необязательно.

## Ошибки

Недоступность API или неверный ответ не останавливают текущую трансляцию. Ошибка записывается в локальный журнал, а синхронизация повторяется позже.

## Встраивание адреса API

Адрес endpoint встраивается при сборке и не отображается пользователю:

```cmd
set GOOS=windows
set GOARCH=amd64
go build -trimpath -ldflags="-s -w -H=windowsgui -X main.defaultRemoteAPIURL=https://admin.example/api/monitor/sync" -o LinkVideo.Monitor.exe .
```

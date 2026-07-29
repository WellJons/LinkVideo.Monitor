# LinkVideo Monitor — API дистанционного управления v2

Monitor обращается к скрытому HTTPS endpoint, встроенному в производственную сборку. В пользовательском интерфейсе адрес API и ключ не отображаются и не изменяются.

Индивидуальная ссылка `linkvideomonitor:...` используется как идентификатор установленного устройства. Отдельный локальный ID не требуется.

## Синхронизация

```http
POST /api/monitor/sync
Content-Type: application/json
Authorization: Bearer <build-time-api-key>
```

Запрос выполняется после запуска, после выхода из сна, после локального сохранения настроек и далее раз в 5 минут.

Пример сокращённого запроса:

```json
{
  "api_version": 2,
  "connection_link": "linkvideomonitor:...",
  "client": {
    "product": "LinkVideo Monitor",
    "version": "0.8.11-beta",
    "computer_name": "OFFICE-PC",
    "os": "windows",
    "architecture": "amd64"
  },
  "state": {
    "process_running": true,
    "stream_desired": true,
    "streaming": true,
    "session_locked": false,
    "capture_backend": "Desktop Duplication",
    "applied_revision": 18,
    "last_command_id": "146"
  },
  "settings": {
    "protocol": "rtsp",
    "resolution_profile": "full_hd",
    "capture_mode": "monitor",
    "monitor_number": 2,
    "fps": 15,
    "bitrate_kbps": 1024,
    "codec": "h264",
    "encoder": "libx264",
    "cursor": true,
    "privacy_protection": false,
    "audio_enabled": true,
    "microphone_enabled": false,
    "microphone_mode": "voice",
    "microphone_voice_db": -42,
    "microphone_ptt_hotkey": "Ctrl+Alt+Space",
    "microphone_toggle_hotkey": "Ctrl+Alt+M",
    "overlay_enabled": true,
    "launch_with_windows": true
  },
  "capabilities": {
    "protocols": ["rtsp", "rtmp"],
    "fps": [15, 25, 30],
    "bitrates_kbps": [256, 512, 1024],
    "resolution_profiles": ["original", "full_hd", "hd"],
    "capture_modes": ["full", "monitor"],
    "encoders": [
      {"name": "libx264", "label": "Программный H.264", "codec": "h264", "available": true},
      {"name": "h264_qsv", "label": "Intel Quick Sync · H.264", "codec": "h264", "available": false, "reason": "Подходящий видеоадаптер не обнаружен"}
    ],
    "microphone": true
  }
}
```

## Ответ

Сервер возвращает только поля, которые необходимо изменить. Все поля `settings` необязательны.

```json
{
  "success": true,
  "revision": 19,
  "settings": {
    "fps": 25,
    "bitrate_kbps": 512,
    "codec": "h265",
    "encoder": "libx265",
    "overlay_enabled": true
  },
  "command": {
    "id": "147",
    "action": "restart_stream"
  }
}
```

## Управляемые настройки

- `protocol`: `rtsp`, `rtmp`;
- `resolution_profile`: `original`, `full_hd`, `hd`;
- `capture_mode`: `full`, `monitor`;
- `monitor_number`: номер монитора Windows;
- `fps`: `15`, `25`, `30`;
- `bitrate_kbps`: `256`, `512`, `1024`;
- `codec`: `h264`, `h265`;
- `encoder`: только кодировщик, который Monitor передал как `available: true`;
- `cursor`, `privacy_protection`;
- `audio_enabled`, `microphone_enabled`, `microphone_device`;
- `microphone_mode`: `always`, `voice`, `push_to_talk`;
- `microphone_voice_db`, `microphone_ptt_hotkey`, `microphone_toggle_hotkey`;
- `overlay_enabled`;
- `launch_with_windows`;
- `prevent_sleep`, `keep_display_on`.

Ссылка подключения, адрес API и ключ API дистанционно не меняются.

## Команды

- `start_stream`;
- `stop_stream`;
- `restart_stream`;
- `restart_application`.

`command.id` обязателен. Последний выполненный ID сохраняется, поэтому одна команда не выполняется повторно при следующих запросах.

## Revision

`revision` увеличивается сервером при изменении настроек. Monitor сохраняет последнюю применённую ревизию. Если изменились параметры работающего потока, Monitor автоматически перезапускает кодировщик. Для простого изменения настроек отдельная команда `restart_stream` не требуется.

## Ошибки

Недоступность API или неверный ответ не останавливают текущую трансляцию. Ошибка записывается в локальный журнал, а синхронизация повторяется позже. Неподдерживаемое значение или недоступный аппаратный кодировщик отклоняются с текстовой причиной.

## Производственная сборка

```bat
set LINKVIDEO_REMOTE_API_URL=https://example.linkvideo.ru/api/monitor/sync
set LINKVIDEO_REMOTE_API_KEY=replace-with-private-api-key
scripts\windows\build-release.cmd
```

Пароль администратора также можно заменить только при внутренней сборке, передав SHA-256 digest через `LINKVIDEO_ADMIN_PASSWORD_SHA256`. Клиентский интерфейс менять пароль не позволяет.

Без `LINKVIDEO_REMOTE_API_URL` дистанционная синхронизация отключена. Это защищает тестовую сборку от отправки данных на случайный или фиктивный адрес.

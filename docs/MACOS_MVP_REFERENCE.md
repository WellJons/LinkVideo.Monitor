# macOS MVP reference

Эта заметка сохраняет проверенные решения из первоначального отдельного macOS-прототипа после переноса разработки в основной репозиторий.

## Capture contract

`native/macos/screencapture/main.swift` отдаёт непрерывный raw BGRA stream через stdout. Основной Monitor уже имеет совместимый контракт: `captureSupervisor` читает кадры размером `OutputWidth * OutputHeight * 4` и передаёт их в существующий encoder pipeline.

Команда helper для первого этапа:

```text
linkvideo-capture-helper --capture \
  --display-id 0 \
  --width <OutputWidth> \
  --height <OutputHeight> \
  --fps <FPS> \
  --cursor true|false
```

`--display-id 0` означает основной дисплей. Полноценное объединение нескольких macOS-дисплеев будет добавлено отдельно.

## VideoToolbox settings to preserve

Первоначальный MVP успешно использовал FFmpeg encoders:

```text
h264_videotoolbox
hevc_videotoolbox
```

Raw input contract:

```text
-f rawvideo
-pixel_format bgra
-video_size <width>x<height>
-framerate <fps>
-i pipe:0
```

Базовые параметры LinkVideo:

```text
-c:v h264_videotoolbox | hevc_videotoolbox
-b:v <bitrate>k
-maxrate <bitrate>k
-bufsize <2*bitrate>k
-g <fps*2>
-bf 0
-pix_fmt yuv420p
```

Для RTSP:

```text
-rtsp_transport tcp -f rtsp <url>
```

Для RTMP:

```text
-f flv <url>
```

В основном Monitor эти параметры должны быть реализованы внутри существующего `buildEncoderFFmpegDetailed`, а не отдельным macOS `main.go`. VideoToolbox должен быть аппаратным кандидатом macOS рядом с существующими NVENC/QSV/AMF Windows-кандидатами.

## Packaging

Приоритет поиска helper:

1. `LINKVIDEO_CAPTURE_HELPER`;
2. `LinkVideo.Monitor.app/Contents/Resources/linkvideo-capture-helper`;
3. helper рядом с исполняемым файлом;
4. `$PATH`.

Release `.app` также должен содержать macOS-сборку FFmpeg в `Contents/Resources/ffmpeg`. Release signing: Developer ID + notarization; ad-hoc signing допустим только для CI/dev.

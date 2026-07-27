# Сборка LinkVideo Monitor 0.7.6 Beta

## Требования

- Windows 10/11 x64 для основной проверенной сборки;
- Go 1.20+;
- Python 3 для добавления иконки;
- PowerShell 5+ для формирования payload;
- Git LFS для получения `third_party/ffmpeg/bin/ffmpeg.exe`.

## Полная сборка

Из корня проекта:

```bat
scripts\windows\build-release.cmd
```

Сценарий:

1. запускает тесты основного приложения;
2. собирает `build\LinkVideo.Monitor.exe`;
3. добавляет оригинальную иконку;
4. формирует временный `installer\payload.zip`;
5. собирает однофайловый `build\LinkVideo.Monitor_0.7.6_Setup.exe`;
6. удаляет временный payload.

## Только приложение

```bat
scripts\windows\build-app.cmd
```

## Только установщик

Сначала должен существовать `build\LinkVideo.Monitor.exe`:

```bat
scripts\windows\build-installer.cmd
```

## Ручная компиляция основного EXE

```bat
set GOOS=windows
set GOARCH=amd64
set GOAMD64=v1
go test ./cmd/linkvideo-monitor
go build -trimpath -ldflags="-s -w -H=windowsgui" -o build\LinkVideo.Monitor.exe ./cmd/linkvideo-monitor
python tools\patch_pe_icon.py build\LinkVideo.Monitor.exe assets\icons\linkvideo_original.ico
```

## Встраивание адреса Remote API

```bat
go build -trimpath -ldflags="-s -w -H=windowsgui -X main.defaultRemoteAPIURL=https://admin.example/api/monitor/sync" -o build\LinkVideo.Monitor.exe ./cmd/linkvideo-monitor
```

# Установщик

`main.go` — исходный код однофайлового установщика LinkVideo Monitor.

Файл `payload.zip` намеренно не хранится в Git: он создаётся автоматически из собранного приложения, FFmpeg и документации командой:

```bat
scripts\windows\build-release.cmd
```

После сборки временный `installer/payload.zip` удаляется. Так в репозитории нет второй копии `ffmpeg.exe`.

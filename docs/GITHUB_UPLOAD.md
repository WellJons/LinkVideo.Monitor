# Первая загрузка полного проекта на GitHub

1. Создать пустой **Private**-репозиторий без README, `.gitignore` и лицензии.
2. В GitHub Desktop нажать `Clone` и выбрать локальную папку.
3. Скопировать в неё всё содержимое этого архива.
4. Открыть `Repository → Open in Command Prompt` и выполнить:

```powershell
git lfs install
git lfs track "third_party/ffmpeg/bin/ffmpeg.exe"
git lfs ls-files
```

5. В GitHub Desktop проверить, что не добавлены логи, реальные ссылки подключения, токены, пароли и сертификаты.
6. Создать коммит `Initial import: LinkVideo Monitor 0.7.1`.
7. Нажать `Push origin`.
8. Готовый установщик публиковать через GitHub Releases, а не коммитить в репозиторий.

Файл `.gitattributes` уже подготовлен. Команда `git lfs track` безопасно подтвердит настройку.

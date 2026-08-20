# LinkVideo Monitor 0.8.12 — developer handoff

Документ сопровождает финальную ветку аудита LinkVideo Monitor 0.8.12.

## Целевая версия

- Product version: `0.8.12`
- Windows: Windows 7 и более новые версии
- Shipping Go toolchain: Go 1.20.x для сохранения совместимости с Windows 7
- MediaMTX на Windows 7: `1.0.3`, SHA-256 `f3cffd7ec6113895e8742346644cd5856bd007e6535797ef41e4303cf4bc0d6c`
- MediaMTX на Windows 8+: `1.19.3`, SHA-256 `5d82148d1032a6a190d9909a2997d9989457aaadf49af87dd02cd4512d31bebe`

## Что исправлено и усилено

- Транзакционная установка: новая версия сначала полностью подготавливается, затем активируется; при критической ошибке выполняется откат файлов и службы.
- Защита распаковки payload от path traversal и абсолютных/rooted путей.
- SYSTEM-служба больше не обходит пользовательский параметр `LaunchWithWindows`; её задача — Secure Desktop/UAC/Winlogon capture.
- Локальный HTTP control surface остаётся только на loopback и защищён от сторонних cross-origin запросов, изменяющих состояние.
- Усилена проверка удалённых endpoint-ов и запросов Secure Desktop capture.
- Удалён устаревший код старого window capture и заменённого выбора кодировщика.
- Версия приложения зафиксирована как `0.8.12`, без `-beta`.
- MediaMTX выбирается по версии Windows и проверяется по SHA-256 перед использованием.
- Неиспользуемые сетевые сервисы локального MediaMTX отключены; для локального сценария оставлен необходимый RTSP.

## Автоматические release gates

Для текущего release-кандидата обязательны оба зелёных workflow:

1. `CI`: unit tests + Windows build приложения, installer и uninstaller.
2. `Full Audit`: unit tests, race tests, `go vet`, `gofmt`, `staticcheck`, `govulncheck`, installer tests/vet, затем полный Windows build.

Точные run ID, commit SHA и SHA-256 собранных EXE передаются вместе с финальным developer package и фиксируются в описании PR #6.

## Что проверить вручную перед публичным выпуском

- чистая установка на Windows 7 и Windows 10/11;
- обновление поверх предыдущей версии с сохранением настроек;
- искусственно прерванная/неудачная установка и корректный rollback;
- локальный RTSP и просмотр потока;
- Secure Desktop/UAC, Win+L и поведение при выключении физического дисплея;
- выключенный `LaunchWithWindows` не должен самопроизвольно включать фоновый запуск;
- RTSP/RTMP отправка в LinkVideo и автоматическое переподключение;
- H.264/H.265 и fallback аппаратного кодировщика;
- системный звук и микрофон;
- production-подпись EXE сертификатом компании и проверка подписи после подписания.

## Подпись

GitHub Actions test artifacts специально рассматриваются как тестовые и могут быть без Authenticode-подписи. Публичный production build должен быть подписан штатным сертификатом LinkVideo/компании после сборки и до публикации обновления.

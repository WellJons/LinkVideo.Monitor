from pathlib import Path


def replace_once(path, old, new):
    p = Path(path)
    value = p.read_text(encoding="utf-8")
    count = value.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected exactly one match for {old!r}, got {count}")
    p.write_text(value.replace(old, new, 1), encoding="utf-8")


replace_once(
    "installer/backend.go",
    'errors.New("Windows не вернула пути LOCALAPPDATA и APPDATA")',
    'errors.New("пути LOCALAPPDATA и APPDATA не предоставлены Windows")',
)
replace_once(
    "installer/backend.go",
    'errors.New("Windows не вернула путь профиля пользователя")',
    'errors.New("путь профиля пользователя не предоставлен Windows")',
)
replace_once(
    "installer/backend.go",
    'errors.New("Windows не вернула путь PROGRAMDATA")',
    'errors.New("путь PROGRAMDATA не предоставлен Windows")',
)
replace_once(
    "installer/backend.go",
    'fmt.Errorf("Windows не позволила удалить некоторые файлы:\\n%s", strings.Join(remaining, "\\n"))',
    'fmt.Errorf("некоторые файлы не удалось удалить в Windows:\\n%s", strings.Join(remaining, "\\n"))',
)
replace_once(
    "installer/main.go",
    '\n\tprocSetWindowPos       = user32DLL.NewProc("SetWindowPos")',
    '',
)

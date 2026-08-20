from pathlib import Path


def text(path):
    return Path(path).read_text(encoding="utf-8")


def write(path, value):
    Path(path).write_text(value, encoding="utf-8")


def replace_once(path, old, new):
    value = text(path)
    count = value.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected exactly one match, got {count}: {old!r}")
    write(path, value.replace(old, new, 1))


def replace_between(path, start, end, replacement):
    value = text(path)
    i = value.find(start)
    j = value.find(end, i + len(start))
    if i < 0 or j < 0:
        raise SystemExit(f"{path}: markers not found: {start!r} / {end!r}")
    write(path, value[:i] + replacement + value[j:])


# 1. The ZIP path guard must reject rooted paths such as /evil.exe and \\evil.exe
# on Windows in addition to drive-qualified and traversal paths.
replace_once(
    "installer/backend.go",
    '''func payloadTargetPath(dest, archiveName string) (string, string, error) {
\tclean := filepath.Clean(strings.ReplaceAll(archiveName, "/", string(filepath.Separator)))
\tif clean == "." || filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" {
\t\treturn "", "", fmt.Errorf("недопустимый путь в пакете: %s", archiveName)
\t}
''',
    '''func payloadTargetPath(dest, archiveName string) (string, string, error) {
\tnormalized := strings.ReplaceAll(strings.TrimSpace(archiveName), "/", string(filepath.Separator))
\tif normalized == "" || strings.HasPrefix(normalized, string(filepath.Separator)) {
\t\treturn "", "", fmt.Errorf("недопустимый путь в пакете: %s", archiveName)
\t}
\tclean := filepath.Clean(normalized)
\tif clean == "." || filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" {
\t\treturn "", "", fmt.Errorf("недопустимый путь в пакете: %s", archiveName)
\t}
''',
)


# 2. Make normal/manual installation transactional as well. The full payload is
# staged before the live installation is stopped, and a failed activation/service
# update restores the previous directory and service where possible.
install_replacement = r'''func findRollbackApp(roots ...string) string {
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" || filepath.Clean(root) == "." {
			continue
		}
		for _, name := range []string{"LinkVideo.Monitor.exe", "LinkVideo.ScreenSender.exe"} {
			candidate := filepath.Join(root, name)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate
			}
		}
	}
	return ""
}

func renameInstallDirectoryWithRetry(from, to string) error {
	var lastErr error
	for attempt := 0; attempt < 20; attempt++ {
		if err := os.Rename(from, to); err == nil {
			return nil
		} else {
			lastErr = err
		}
		time.Sleep(250 * time.Millisecond)
	}
	return lastErr
}

func installProduct(opts installOptions, progress progressFunc) (string, []string, error) {
	local := os.Getenv("LOCALAPPDATA")
	roaming := os.Getenv("APPDATA")
	if local == "" || roaming == "" {
		return "", nil, errors.New("Windows не вернула пути LOCALAPPDATA и APPDATA")
	}

	dest := defaultInstallDir()
	stage := dest + ".install-new"
	backup := dest + ".install-old"
	legacyLocalDest := filepath.Join(local, "Programs", "LinkVideo.Monitor")
	oldDest := filepath.Join(local, "Programs", "LinkVideo.ScreenSender")
	legacyProgramFiles := filepath.Join(os.Getenv("ProgramFiles"), "LinkVideo.Monitor")
	legacyProgramFilesX86 := filepath.Join(os.Getenv("ProgramFiles(x86)"), "LinkVideo.Monitor")
	warnings := make([]string, 0, 3)

	progress(5, "Подготовка файлов…")
	_ = os.RemoveAll(stage)
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return "", warnings, fmt.Errorf("не удалось создать временную папку установки: %w", err)
	}
	if err := extractPayload(stage, func(done, total int, name string) {
		percent := 8
		if total > 0 {
			percent += done * 50 / total
		}
		progress(percent, "Подготовка: "+name)
	}); err != nil {
		_ = os.RemoveAll(stage)
		return "", warnings, fmt.Errorf("не удалось подготовить пакет установки: %w", err)
	}
	_ = os.Remove(filepath.Join(stage, "LinkVideo.ScreenOverlay.exe"))
	_ = os.Remove(filepath.Join(stage, "LinkVideo.AudioLoopback.exe"))
	stagedApp := filepath.Join(stage, "LinkVideo.Monitor.exe")
	if info, err := os.Stat(stagedApp); err != nil || info.IsDir() {
		_ = os.RemoveAll(stage)
		if err == nil {
			err = errors.New("путь основного файла является папкой")
		}
		return "", warnings, fmt.Errorf("основной файл программы отсутствует в подготовленном пакете: %w", err)
	}

	rollbackApp := findRollbackApp(dest, legacyLocalDest, oldDest, legacyProgramFiles, legacyProgramFilesX86)
	_, destErr := os.Stat(dest)
	hadDest := destErr == nil
	if destErr != nil && !os.IsNotExist(destErr) {
		_ = os.RemoveAll(stage)
		return "", warnings, fmt.Errorf("не удалось проверить текущую установку: %w", destErr)
	}

	progress(61, "Остановка предыдущей версии…")
	if existingInstallation() {
		if err := prepareUpgradeElevated(); err != nil {
			_ = os.RemoveAll(stage)
			return "", warnings, fmt.Errorf("не удалось остановить фоновую службу перед обновлением: %w", err)
		}
	}
	stopInstalledProcesses(dest, legacyLocalDest, oldDest, legacyProgramFiles, legacyProgramFilesX86)

	_ = os.RemoveAll(backup)
	if hadDest {
		if err := renameInstallDirectoryWithRetry(dest, backup); err != nil {
			_ = os.RemoveAll(stage)
			return "", warnings, fmt.Errorf("не удалось подготовить откат предыдущей версии: %w", err)
		}
	}
	if err := renameInstallDirectoryWithRetry(stage, dest); err != nil {
		if hadDest {
			_ = renameInstallDirectoryWithRetry(backup, dest)
		}
		return "", warnings, fmt.Errorf("не удалось активировать подготовленную версию: %w", err)
	}

	appPath := filepath.Join(dest, "LinkVideo.Monitor.exe")
	rollback := func(cause error) error {
		stopCaptureServiceForUpgrade()
		stopInstalledProcesses(dest)
		failedDir := dest + ".install-failed"
		_ = os.RemoveAll(failedDir)
		if _, err := os.Stat(dest); err == nil {
			if moveErr := renameInstallDirectoryWithRetry(dest, failedDir); moveErr != nil {
				return fmt.Errorf("%v; дополнительно не удалось изолировать неудачную новую версию: %w", cause, moveErr)
			}
		}
		if hadDest {
			if restoreErr := renameInstallDirectoryWithRetry(backup, dest); restoreErr != nil {
				return fmt.Errorf("%v; дополнительно не удалось вернуть предыдущие файлы: %w", cause, restoreErr)
			}
		}
		_ = os.RemoveAll(failedDir)

		recoveryApp := rollbackApp
		if hadDest && recoveryApp != "" && strings.EqualFold(filepath.Clean(filepath.Dir(recoveryApp)), filepath.Clean(dest)) {
			recoveryApp = filepath.Join(dest, filepath.Base(recoveryApp))
		}
		if recoveryApp != "" {
			if info, err := os.Stat(recoveryApp); err == nil && !info.IsDir() {
				if serviceErr := installUACServiceElevated(recoveryApp); serviceErr != nil {
					return fmt.Errorf("%v; предыдущие файлы возвращены, но служба не восстановилась: %w", cause, serviceErr)
				}
				if strings.EqualFold(filepath.Base(recoveryApp), "LinkVideo.Monitor.exe") {
					_ = registerUninstall(recoveryApp, filepath.Dir(recoveryApp))
				}
				return cause
			}
		}

		_ = runHidden("sc.exe", "stop", "LinkVideoMonitorCapture")
		_ = runHidden("sc.exe", "delete", "LinkVideoMonitorCapture")
		_ = runHidden("reg.exe", "delete", `HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\LinkVideo Monitor`, "/f")
		return cause
	}

	progress(72, "Регистрация LinkVideo Monitor в Windows…")
	if err := registerUninstall(appPath, dest); err != nil {
		return "", warnings, rollback(fmt.Errorf("не удалось зарегистрировать удаление программы: %w", err))
	}
	progress(80, "Установка фоновой службы…")
	if err := installUACServiceElevated(appPath); err != nil {
		return "", warnings, rollback(fmt.Errorf("не удалось установить службу защищённого рабочего стола: %w", err))
	}

	progress(87, "Создание ярлыков…")
	if err := createShortcuts(appPath, dest, opts.DesktopShortcut); err != nil {
		warnings = append(warnings, "не удалось создать один из ярлыков")
	}
	progress(91, "Настройка запуска вместе с Windows…")
	if err := setStartup(appPath, opts.AutoStart); err != nil {
		warnings = append(warnings, "не удалось настроить автозапуск Windows")
	}
	progress(94, "Регистрация ссылки LinkVideo…")
	if err := registerURLProtocol(appPath); err != nil {
		warnings = append(warnings, "не удалось зарегистрировать ссылку linkvideomonitor:")
	}

	_ = os.RemoveAll(backup)
	removeLegacyRegistration(roaming)
	for _, legacyDir := range []string{legacyLocalDest, oldDest} {
		if !strings.EqualFold(filepath.Clean(legacyDir), filepath.Clean(dest)) {
			_ = os.RemoveAll(legacyDir)
		}
	}
	progress(100, "Установка завершена")
	return appPath, warnings, nil
}

'''
replace_between("installer/backend.go", "func installProduct", "func payloadTargetPath", install_replacement)


# 3. The SYSTEM service is responsible for protected-desktop capture, not for
# bypassing the user's LaunchWithWindows choice. Normal startup remains the
# per-user HKCU Run registration maintained by the application/installer.
service_path = "cmd/linkvideo-monitor/uac_service_windows.go"
value = text(service_path)
value = value.replace("\tinvalidSessionID         = 0xffffffff\n", "")
value = value.replace("\twtsapi32Service                          = syscall.NewLazyDLL(\"wtsapi32.dll\")\n", "")
value = value.replace("\tprocWTSQueryUserTokenService            = wtsapi32Service.NewProc(\"WTSQueryUserToken\")\n", "")
value = value.replace("\tprocWTSGetActiveConsoleSessionIDService = kernel32Service.NewProc(\"WTSGetActiveConsoleSessionId\")\n", "")
value = value.replace("\tvar backgroundAgent *secureAgentProcess\n", "")
value = value.replace("\t\tstopSecureAgent(backgroundAgent)\n", "")
start = value.find("\t\t\tactiveSession := activeConsoleSessionID()\n")
end = value.find("\n\t\t\trequests := loadSecureCaptureRequests(sessionsDir)", start)
if start < 0 or end < 0:
    raise SystemExit("uac service: background-agent loop markers not found")
value = value[:start] + value[end + 1 :]
start = value.find("func activeConsoleSessionID() uint32 {")
end = value.find("func installedAppPathFile()", start)
if start < 0 or end < 0:
    raise SystemExit("uac service: activeConsoleSessionID markers not found")
value = value[:start] + value[end:]
start = value.find("func launchBackgroundAgent(")
end = value.find("func quoteWindowsCommand(", start)
if start < 0 or end < 0:
    raise SystemExit("uac service: launchBackgroundAgent markers not found")
value = value[:start] + value[end:]
write(service_path, value)


# 4. Make the audit fail on real regressions. Intentional low-level Win32
# uintptr conversions are reviewed separately, so vet's unsafeptr heuristic is
# disabled while all other vet analyzers remain enabled.
workflow = ".github/workflows/full-audit.yml"
value = text(workflow)
value = value.replace("      - name: Application race tests\n        continue-on-error: true\n        run: go test -race -count=1 ./cmd/linkvideo-monitor\n", "      - name: Application race tests\n        run: go test -race -count=1 ./cmd/linkvideo-monitor\n")
value = value.replace("      - name: Application vet\n        continue-on-error: true\n        run: go vet ./cmd/linkvideo-monitor\n", "      - name: Application vet\n        run: go vet -unsafeptr=false ./cmd/linkvideo-monitor\n")
value = value.replace("      - name: Installer vet\n        continue-on-error: true\n        working-directory: installer\n        run: go vet ./...\n", "      - name: Installer vet\n        working-directory: installer\n        run: go vet -unsafeptr=false ./...\n")
value = value.replace("      - name: Check gofmt\n        continue-on-error: true\n", "      - name: Check gofmt\n")
value = value.replace("      - name: Staticcheck application for Windows\n        continue-on-error: true\n", "      - name: Staticcheck application for Windows\n")
value = value.replace("      - name: Govulncheck application for Windows\n        continue-on-error: true\n", "      - name: Govulncheck application for Windows\n")
value = value.replace("      - name: Staticcheck installer for Windows\n        continue-on-error: true\n", "      - name: Staticcheck installer for Windows\n")
value = value.replace("      - name: Govulncheck installer for Windows\n        continue-on-error: true\n", "      - name: Govulncheck installer for Windows\n")
value = value.replace("      - name: Python syntax check\n        continue-on-error: true\n", "      - name: Python syntax check\n")
# Keep the test build downloadable without publishing a release.
needle = "      - name: Build installer and uninstaller\n        shell: cmd\n        run: scripts\\windows\\build-installer.cmd\n"
addition = needle + "      - name: Upload test binaries\n        uses: actions/upload-artifact@v4\n        with:\n          name: LinkVideo-Monitor-0.8.12-test\n          if-no-files-found: error\n          path: |\n            build/LinkVideo.Monitor.exe\n            build/LinkVideo.Monitor_0.8.12_Setup.exe\n            build/Uninstall.exe\n"
if needle not in value:
    raise SystemExit("full audit: build marker not found")
value = value.replace(needle, addition, 1)
write(workflow, value)

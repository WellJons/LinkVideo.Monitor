//go:build windows

package main

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	utf16pkg "unicode/utf16"
	"unsafe"
)

type installOptions struct {
	DesktopShortcut bool
	AutoStart       bool
}

type progressFunc func(percent int, status string)

func existingInstallation() bool {
	local := os.Getenv("LOCALAPPDATA")
	programFiles := os.Getenv("ProgramFiles")
	programFilesX86 := os.Getenv("ProgramFiles(x86)")
	candidates := []string{
		filepath.Join(local, "Programs", "LinkVideo.Monitor", "LinkVideo.Monitor.exe"),
		filepath.Join(local, "Programs", "LinkVideo.ScreenSender", "LinkVideo.ScreenSender.exe"),
		filepath.Join(programFiles, "LinkVideo.Monitor", "LinkVideo.Monitor.exe"),
		filepath.Join(programFilesX86, "LinkVideo.Monitor", "LinkVideo.Monitor.exe"),
		filepath.Join(local, "LinkVideo.Monitor", "config.json"),
		filepath.Join(local, "LinkVideo.ScreenSender", "config.json"),
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" || filepath.Clean(candidate) == "." {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return true
		}
	}
	return false
}

func defaultInstallDir() string {
	if programFiles := strings.TrimSpace(os.Getenv("ProgramFiles")); programFiles != "" {
		return filepath.Join(programFiles, "LinkVideo.Monitor")
	}
	return `C:\Program Files\LinkVideo.Monitor`
}

func findRollbackApp(roots ...string) string {
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
		return "", nil, errors.New("пути LOCALAPPDATA и APPDATA не предоставлены Windows")
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

func payloadTargetPath(dest, archiveName string) (string, string, error) {
	normalized := strings.ReplaceAll(strings.TrimSpace(archiveName), "/", string(filepath.Separator))
	if normalized == "" || strings.HasPrefix(normalized, string(filepath.Separator)) {
		return "", "", fmt.Errorf("недопустимый путь в пакете: %s", archiveName)
	}
	clean := filepath.Clean(normalized)
	if clean == "." || filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" {
		return "", "", fmt.Errorf("недопустимый путь в пакете: %s", archiveName)
	}
	target := filepath.Join(dest, clean)
	rel, err := filepath.Rel(filepath.Clean(dest), filepath.Clean(target))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("недопустимый путь в пакете: %s", archiveName)
	}
	return clean, target, nil
}

func extractPayload(dest string, onFile func(done, total int, name string)) error {
	if len(payload) == 0 {
		return errors.New("установочный пакет не содержит файлов программы")
	}
	zr, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return fmt.Errorf("повреждён встроенный пакет установки: %w", err)
	}
	files := make([]*zip.File, 0, len(zr.File))
	for _, f := range zr.File {
		if !f.FileInfo().IsDir() {
			files = append(files, f)
		}
	}
	done := 0
	for _, f := range zr.File {
		clean, target, err := payloadTargetPath(dest, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if onFile != nil {
			onFile(done, len(files), filepath.Base(clean))
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		tmp := target + ".new"
		w, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			r.Close()
			return fmt.Errorf("не удалось создать %s: %w", filepath.Base(target), err)
		}
		_, copyErr := io.Copy(w, r)
		closeErr := w.Close()
		r.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if err := replaceInstalledFile(tmp, target); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("не удалось заменить %s: %w", filepath.Base(target), err)
		}
		done++
		if onFile != nil {
			onFile(done, len(files), filepath.Base(clean))
		}
	}
	return nil
}

func prepareUpgradeElevated() error {
	if isProcessElevated() {
		stopCaptureServiceForUpgrade()
		return nil
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	return runElevatedAndWait(self, "--prepare-upgrade", 30000)
}

func stopCaptureServiceForUpgrade() {
	_ = runHidden("sc.exe", "stop", "LinkVideoMonitorCapture")
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		cmd := exec.Command("sc.exe", "query", "LinkVideoMonitorCapture")
		hideChildWindow(cmd)
		out, _ := cmd.CombinedOutput()
		text := strings.ToUpper(string(out))
		if strings.Contains(text, "STOPPED") || strings.Contains(text, "1060") {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	_ = runHidden("taskkill.exe", "/IM", "LinkVideo.Monitor.Service.exe", "/T", "/F")
}

func stopInstalledProcesses(paths ...string) {
	for _, image := range []string{"LinkVideo.Monitor.exe", "LinkVideo.Monitor.Service.exe", "LinkVideo.ScreenOverlay.exe", "LinkVideo.AudioLoopback.exe", "LinkVideo.ScreenSender.exe"} {
		_ = runHidden("taskkill.exe", "/IM", image, "/T", "/F")
	}
	quoted := make([]string, 0, len(paths))
	for _, p := range paths {
		if strings.TrimSpace(p) == "" || filepath.Clean(p) == "." {
			continue
		}
		quoted = append(quoted, "'"+psEscape(p)+"'")
	}
	if len(quoted) == 0 {
		return
	}
	script := `$roots=@(` + strings.Join(quoted, ",") + `); ` +
		`Get-CimInstance Win32_Process -ErrorAction SilentlyContinue | Where-Object { $p=$_.ExecutablePath; $p -and ($roots | Where-Object { $p.StartsWith($_,[System.StringComparison]::OrdinalIgnoreCase) }) } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }`
	_ = runHidden("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	time.Sleep(700 * time.Millisecond)
}

func createShortcuts(appPath, dest string, desktop bool) error {
	roaming := strings.TrimSpace(os.Getenv("APPDATA"))
	profile := strings.TrimSpace(os.Getenv("USERPROFILE"))
	if roaming == "" || profile == "" {
		return errors.New("путь профиля пользователя не предоставлен Windows")
	}
	startMenuDir := filepath.Join(roaming, `Microsoft\Windows\Start Menu\Programs`)
	desktopDir := filepath.Join(profile, "Desktop")
	if err := os.MkdirAll(startMenuDir, 0o755); err != nil {
		return err
	}
	if desktop {
		if err := os.MkdirAll(desktopDir, 0o755); err != nil {
			return err
		}
	}
	paths := []string{filepath.Join(startMenuDir, "LinkVideo Monitor.lnk")}
	if desktop {
		paths = append(paths, filepath.Join(desktopDir, "LinkVideo Monitor.lnk"))
	}
	quoted := make([]string, 0, len(paths))
	for _, path := range paths {
		quoted = append(quoted, "'"+psEscape(path)+"'")
	}
	script := `$ErrorActionPreference='Stop';$w=New-Object -ComObject WScript.Shell;` +
		`$paths=@(` + strings.Join(quoted, ",") + `);foreach($p in $paths){` +
		`$s=$w.CreateShortcut($p);$s.TargetPath='` + psEscape(appPath) + `';` +
		`$s.WorkingDirectory='` + psEscape(dest) + `';$s.IconLocation='` + psEscape(appPath) + `,0';` +
		`$s.Description='LinkVideo Monitor';$s.Save()}`
	if err := runPowerShellEncoded(script); err != nil {
		return err
	}
	if !desktop {
		_ = os.Remove(filepath.Join(desktopDir, "LinkVideo Monitor.lnk"))
	}
	return nil
}

func setStartup(appPath string, enabled bool) error {
	key := `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	for _, name := range []string{"LinkVideo Screen Sender", "LinkVideo.Monitor", productName} {
		_ = runHidden("reg.exe", "delete", key, "/v", name, "/f")
	}
	if !enabled {
		return nil
	}
	value := `"` + appPath + `" --background`
	return runHidden("reg.exe", "add", key, "/v", productName, "/t", "REG_SZ", "/d", value, "/f")
}

func registerURLProtocol(appPath string) error {
	base := `HKCU\Software\Classes\linkvideomonitor`
	commands := [][]string{
		{"add", base, "/ve", "/t", "REG_SZ", "/d", "URL:LinkVideo Monitor Protocol", "/f"},
		{"add", base, "/v", "URL Protocol", "/t", "REG_SZ", "/d", "", "/f"},
		{"add", base + `\DefaultIcon`, "/ve", "/t", "REG_SZ", "/d", appPath + ",0", "/f"},
		{"add", base + `\shell\open\command`, "/ve", "/t", "REG_SZ", "/d", `"` + appPath + `" "%1"`, "/f"},
	}
	for _, args := range commands {
		if err := runHidden("reg.exe", args...); err != nil {
			return err
		}
	}
	return nil
}

func registerUninstall(appPath, dest string) error {
	key := `HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\LinkVideo Monitor`
	uninstaller := filepath.Join(dest, "Uninstall.exe")
	values := [][]string{
		{"/v", "DisplayName", "/t", "REG_SZ", "/d", productName},
		{"/v", "DisplayVersion", "/t", "REG_SZ", "/d", version},
		{"/v", "Publisher", "/t", "REG_SZ", "/d", "LinkVideo"},
		{"/v", "InstallLocation", "/t", "REG_SZ", "/d", dest},
		{"/v", "DisplayIcon", "/t", "REG_SZ", "/d", appPath + ",0"},
		{"/v", "UninstallString", "/t", "REG_SZ", "/d", `"` + uninstaller + `"`},
		{"/v", "URLInfoAbout", "/t", "REG_SZ", "/d", "https://linkvideo.ru/"},
		{"/v", "HelpLink", "/t", "REG_SZ", "/d", "https://linkvideo.ru/"},
		{"/v", "NoModify", "/t", "REG_DWORD", "/d", "1"},
		{"/v", "NoRepair", "/t", "REG_DWORD", "/d", "1"},
		{"/v", "EstimatedSize", "/t", "REG_DWORD", "/d", "135000"},
	}
	for _, v := range values {
		args := append([]string{"add", key}, v...)
		args = append(args, "/f")
		if err := runHidden("reg.exe", args...); err != nil {
			return err
		}
	}
	return nil
}

func removeLegacyRegistration(roaming string) {
	_ = os.Remove(filepath.Join(roaming, `Microsoft\Windows\Start Menu\Programs\LinkVideo Screen Sender.lnk`))
	if desktop := os.Getenv("USERPROFILE"); desktop != "" {
		_ = os.Remove(filepath.Join(desktop, `Desktop\LinkVideo Screen Sender.lnk`))
	}
}

type shellExecuteInfoW struct {
	CBSize        uint32
	Mask          uint32
	Hwnd          uintptr
	Verb          *uint16
	File          *uint16
	Parameters    *uint16
	Directory     *uint16
	Show          int32
	Instance      uintptr
	IDList        uintptr
	Class         *uint16
	ClassKey      uintptr
	HotKey        uint32
	IconOrMonitor uintptr
	Process       uintptr
}

func installUACServiceElevated(appPath string) error {
	if isProcessElevated() {
		return installUACServiceWorker(appPath)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	params := `--service-install "` + strings.ReplaceAll(appPath, `"`, `\"`) + `"`
	return runElevatedAndWait(self, params, 120000)
}

func runElevatedAndWait(filePath, params string, timeout uint32) error {
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(filePath)
	paramPtr, _ := syscall.UTF16PtrFromString(params)
	info := shellExecuteInfoW{
		CBSize:     uint32(unsafe.Sizeof(shellExecuteInfoW{})),
		Mask:       0x00000040,
		Verb:       verb,
		File:       file,
		Parameters: paramPtr,
		Show:       1,
	}
	ok, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if ok == 0 || info.Process == 0 {
		return fmt.Errorf("запрос прав администратора отменён или не выполнен: %v", callErr)
	}
	defer procCloseHandle.Call(info.Process)
	procWaitForSingleObject.Call(info.Process, uintptr(timeout))
	var exitCode uint32
	if ok, _, callErr := procGetExitCodeProcess.Call(info.Process, uintptr(unsafe.Pointer(&exitCode))); ok == 0 {
		return fmt.Errorf("не удалось получить результат: %v", callErr)
	}
	if exitCode != 0 {
		return fmt.Errorf("код %d", exitCode)
	}
	return nil
}

func installUACServiceWorker(appPath string) error {
	if _, err := os.Stat(appPath); err != nil {
		return fmt.Errorf("основной EXE не найден: %w", err)
	}
	programData := os.Getenv("PROGRAMDATA")
	if programData == "" {
		return errors.New("путь PROGRAMDATA не предоставлен Windows")
	}
	serviceDir := filepath.Join(programData, "LinkVideo.Monitor", "Service")
	sessionsDir := filepath.Join(programData, "LinkVideo.Monitor", "Sessions")
	if err := os.MkdirAll(serviceDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(serviceDir, "app-path.txt"), []byte(filepath.Clean(appPath)+"\r\n"), 0o644); err != nil {
		return fmt.Errorf("не удалось сохранить путь фонового агента: %w", err)
	}
	// Service installation happens with the Monitor stopped, so stale request
	// files can be discarded and the directory ACL rebuilt from a known state.
	_ = os.RemoveAll(sessionsDir)
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		return err
	}
	servicePath := filepath.Join(serviceDir, "LinkVideo.Monitor.Service.exe")
	_ = runHidden("sc.exe", "stop", "LinkVideoMonitorCapture")
	time.Sleep(700 * time.Millisecond)
	_ = runHidden("taskkill.exe", "/IM", "LinkVideo.Monitor.Service.exe", "/T", "/F")
	if err := copyFile(appPath, servicePath); err != nil {
		return fmt.Errorf("не удалось обновить файл службы: %w", err)
	}
	_ = runHidden("icacls.exe", sessionsDir, "/inheritance:r")
	_ = runHidden("icacls.exe", sessionsDir, "/grant:r", "*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F", "*S-1-3-0:(OI)(CI)(IO)F", "*S-1-5-32-545:(RX,WD,AD)")
	_ = runHidden("sc.exe", "delete", "LinkVideoMonitorCapture")
	time.Sleep(400 * time.Millisecond)
	binPath := `"` + servicePath + `" --uac-service`
	if err := runHidden("sc.exe", "create", "LinkVideoMonitorCapture", "binPath=", binPath, "start=", "auto", "obj=", "LocalSystem", "DisplayName=", "LinkVideo Monitor — фоновая служба"); err != nil {
		return err
	}
	_ = runHidden("sc.exe", "description", "LinkVideoMonitorCapture", "Фоновый запуск, захват UAC и защищённого рабочего стола для LinkVideo Monitor")
	_ = runHidden("sc.exe", "failure", "LinkVideoMonitorCapture", "reset=", "86400", "actions=", "restart/5000/restart/15000/restart/60000")
	return runHidden("sc.exe", "start", "LinkVideoMonitorCapture")
}

func launchElevatedUninstaller(removeData bool) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(self)
	if err != nil {
		return err
	}
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("LinkVideo-Monitor-Uninstall-%d.exe", os.Getpid()))
	if err := os.WriteFile(tmp, data, 0o700); err != nil {
		return err
	}
	flag := "0"
	if removeData {
		flag = "1"
	}
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(tmp)
	params, _ := syscall.UTF16PtrFromString("--uninstall-elevated " + flag)
	info := shellExecuteInfoW{
		CBSize:     uint32(unsafe.Sizeof(shellExecuteInfoW{})),
		Mask:       0x00000040,
		Verb:       verb,
		File:       file,
		Parameters: params,
		Show:       1,
	}
	ok, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if ok == 0 || info.Process == 0 {
		_ = os.Remove(tmp)
		return fmt.Errorf("удаление отменено или Windows не разрешила получить права администратора: %v", callErr)
	}
	procCloseHandle.Call(info.Process)
	return nil
}

func uninstallProduct(removeData bool, progress progressFunc) error {
	progress(5, "Остановка LinkVideo Monitor…")
	for _, image := range []string{"LinkVideo.Monitor.exe", "LinkVideo.Monitor.Service.exe", "LinkVideo.ScreenOverlay.exe", "LinkVideo.AudioLoopback.exe", "LinkVideo.ScreenSender.exe"} {
		runCleanupCommand("taskkill.exe", "/IM", image, "/T", "/F")
	}
	progress(15, "Остановка фоновой службы…")
	for _, name := range []string{"LinkVideoMonitorCapture", "LinkVideo Monitor", "LinkVideo.Monitor", "LinkVideo Screen Sender", "LinkVideo.ScreenSender"} {
		runCleanupCommand("schtasks.exe", "/Delete", "/TN", name, "/F")
		runCleanupCommand("sc.exe", "stop", name)
		runCleanupCommand("sc.exe", "delete", name)
	}

	progress(28, "Удаление регистрации Windows…")
	for _, key := range []string{
		`HKCU\Software\Classes\linkvideomonitor`,
		`HKLM\Software\Classes\linkvideomonitor`,
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\LinkVideo Monitor`,
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\LinkVideo.Monitor`,
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\LinkVideo Screen Sender`,
		`HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\LinkVideo Monitor`,
		`HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\LinkVideo.Monitor`,
		`HKLM\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\LinkVideo Monitor`,
		`HKLM\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\LinkVideo.Monitor`,
	} {
		runCleanupCommand("reg.exe", "delete", key, "/f")
	}
	for _, key := range []string{
		`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`,
		`HKLM\Software\Microsoft\Windows\CurrentVersion\Run`,
		`HKLM\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Run`,
	} {
		for _, name := range []string{"LinkVideo Monitor", "LinkVideo.Monitor", "LinkVideo Screen Sender"} {
			runCleanupCommand("reg.exe", "delete", key, "/v", name, "/f")
		}
	}

	progress(42, "Удаление ярлыков…")
	local := os.Getenv("LOCALAPPDATA")
	roaming := os.Getenv("APPDATA")
	programData := os.Getenv("PROGRAMDATA")
	programFiles := os.Getenv("ProgramFiles")
	programFilesX86 := os.Getenv("ProgramFiles(x86)")
	userProfile := os.Getenv("USERPROFILE")
	shortcutDirs := []string{
		filepath.Join(userProfile, "Desktop"),
		filepath.Join(roaming, `Microsoft\Windows\Start Menu\Programs`),
		filepath.Join(programData, `Microsoft\Windows\Start Menu\Programs`),
		filepath.Join(os.Getenv("PUBLIC"), "Desktop"),
	}
	for _, dir := range shortcutDirs {
		for _, name := range []string{"LinkVideo Monitor.lnk", "LinkVideo.Monitor.lnk", "LinkVideo Screen Sender.lnk"} {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}

	progress(55, "Удаление файлов программы…")
	roots := []string{
		filepath.Join(local, "Programs", "LinkVideo.Monitor"),
		filepath.Join(local, "Programs", "LinkVideo.ScreenSender"),
		filepath.Join(programFiles, "LinkVideo.Monitor"),
		filepath.Join(programFilesX86, "LinkVideo.Monitor"),
		filepath.Join(programData, "LinkVideo.Monitor", "Service"),
		filepath.Join(programData, "LinkVideo.Monitor", "Sessions"),
	}
	if removeData {
		roots = append(roots,
			filepath.Join(local, "LinkVideo.Monitor"),
			filepath.Join(local, "LinkVideo.ScreenSender"),
			filepath.Join(roaming, "LinkVideo.Monitor"),
			filepath.Join(roaming, "LinkVideo.ScreenSender"),
			filepath.Join(programData, "LinkVideo.Monitor"),
			filepath.Join(programData, "LinkVideo.ScreenSender"),
		)
	}
	remaining := make([]string, 0)
	for i, root := range roots {
		if strings.TrimSpace(root) == "" || filepath.Clean(root) == "." {
			continue
		}
		progress(55+i*35/maxInt(1, len(roots)), "Удаление: "+filepath.Base(root))
		for attempt := 0; attempt < 6; attempt++ {
			_ = os.RemoveAll(root)
			if _, err := os.Stat(root); os.IsNotExist(err) {
				break
			}
			time.Sleep(350 * time.Millisecond)
		}
		if _, err := os.Stat(root); err == nil {
			remaining = append(remaining, root)
		}
	}
	progress(96, "Завершение удаления…")
	if len(remaining) > 0 {
		return fmt.Errorf("некоторые файлы не удалось удалить в Windows:\n%s", strings.Join(remaining, "\n"))
	}
	progress(100, "LinkVideo Monitor удалён")
	return nil
}

func scheduleSelfDelete() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command("cmd.exe", "/C", fmt.Sprintf(`ping 127.0.0.1 -n 3 >nul & del /f /q "%s"`, self))
	hideChildWindow(cmd)
	_ = cmd.Start()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := replaceInstalledFile(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func replaceInstalledFile(tempPath, targetPath string) error {
	tempPtr, err := syscall.UTF16PtrFromString(tempPath)
	if err != nil {
		return err
	}
	targetPtr, err := syscall.UTF16PtrFromString(targetPath)
	if err != nil {
		return err
	}
	const moveFileReplaceExisting = 0x00000001
	const moveFileWriteThrough = 0x00000008
	ok, _, callErr := procMoveFileExW.Call(
		uintptr(unsafe.Pointer(tempPtr)),
		uintptr(unsafe.Pointer(targetPtr)),
		moveFileReplaceExisting|moveFileWriteThrough,
	)
	if ok != 0 {
		return nil
	}
	lastErr := callErr
	for attempt := 0; attempt < 20; attempt++ {
		time.Sleep(250 * time.Millisecond)
		ok, _, callErr = procMoveFileExW.Call(
			uintptr(unsafe.Pointer(tempPtr)),
			uintptr(unsafe.Pointer(targetPtr)),
			moveFileReplaceExisting|moveFileWriteThrough,
		)
		if ok != 0 {
			return nil
		}
		lastErr = callErr
	}
	return fmt.Errorf("MoveFileExW: %w", lastErr)
}

func isProcessElevated() bool {
	ok, _, _ := shell32DLL.NewProc("IsUserAnAdmin").Call()
	return ok != 0
}

func launchInstalledApplication(appPath string) error {
	// The installer runs elevated, but the application itself must open in the
	// normal interactive user context. Passing the path to the existing Explorer
	// shell prevents LinkVideo Monitor from inheriting administrator rights.
	cmd := exec.Command("explorer.exe", appPath)
	hideChildWindow(cmd)
	return cmd.Start()
}

func runPowerShellEncoded(script string) error {
	encoded := base64.StdEncoding.EncodeToString([]byte(stringsToUTF16LE(script)))
	return runHidden("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encoded)
}

func stringsToUTF16LE(value string) string {
	runes := utf16pkg.Encode([]rune(value))
	buf := make([]byte, len(runes)*2)
	for i, r := range runes {
		binary.LittleEndian.PutUint16(buf[i*2:], r)
	}
	return string(buf)
}

func runHidden(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	hideChildWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %v (%s)", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func hideChildWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

func runCleanupCommand(name string, args ...string) {
	cmd := exec.Command(name, args...)
	hideChildWindow(cmd)
	_ = cmd.Run()
}

func psEscape(value string) string { return strings.ReplaceAll(value, "'", "''") }

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func boolArg(s string) bool {
	v, _ := strconv.ParseBool(s)
	return v || s == "1"
}

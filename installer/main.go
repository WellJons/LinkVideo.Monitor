package main

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	productName = "LinkVideo Monitor"
	version     = "0.7.10"
)

//go:embed payload.zip
var payload []byte

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	messageBoxW       = user32.NewProc("MessageBoxW")
	shell32Installer  = syscall.NewLazyDLL("shell32.dll")
	kernel32Installer = syscall.NewLazyDLL("kernel32.dll")
)

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--service-install" {
		if err := installUACServiceWorker(os.Args[2]); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(20)
		}
		return
	}
	upgrade := existingInstallation()
	prompt := "Установить LinkVideo Monitor?"
	if upgrade {
		prompt = "Обновить LinkVideo Monitor?\n\nТекущая ссылка подключения и настройки будут сохранены."
	}
	answer := messageBox(prompt, productName, 0x00000004|0x00000020)
	if answer != 6 { // IDYES
		return
	}
	if err := install(); err != nil {
		messageBox("Не удалось установить программу:\n\n"+err.Error()+"\n\nПроверьте журнал защиты Windows, если файл был заблокирован.", productName, 0x00000010)
		return
	}
	done := "LinkVideo Monitor установлен."
	if upgrade {
		done = "LinkVideo Monitor обновлён. Настройки сохранены."
	}
	messageBox(done+"\n\nПрограмма добавлена в меню «Пуск», на рабочий стол и в автозагрузку Windows. Служба захвата UAC установлена.", productName, 0x00000040)
}

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

func install() error {
	local := os.Getenv("LOCALAPPDATA")
	roaming := os.Getenv("APPDATA")
	if local == "" || roaming == "" {
		return errors.New("Windows не вернула пути LOCALAPPDATA и APPDATA")
	}
	dest := filepath.Join(local, "Programs", "LinkVideo.Monitor")
	oldDest := filepath.Join(local, "Programs", "LinkVideo.ScreenSender")
	legacyProgramFiles := filepath.Join(os.Getenv("ProgramFiles"), "LinkVideo.Monitor")
	legacyProgramFilesX86 := filepath.Join(os.Getenv("ProgramFiles(x86)"), "LinkVideo.Monitor")
	appPath := filepath.Join(dest, "LinkVideo.Monitor.exe")

	stopInstalledProcesses(dest, oldDest, legacyProgramFiles, legacyProgramFilesX86)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("не удалось создать папку установки: %w", err)
	}
	if err := extractPayload(dest); err != nil {
		return err
	}
	// Начиная с 0.6.0 вспомогательные режимы находятся внутри основного EXE.
	// Удаляем копии, оставшиеся после обновления старых beta-версий.
	_ = os.Remove(filepath.Join(dest, "LinkVideo.ScreenOverlay.exe"))
	_ = os.Remove(filepath.Join(dest, "LinkVideo.AudioLoopback.exe"))
	if _, err := os.Stat(appPath); err != nil {
		return fmt.Errorf("основной файл программы не найден после распаковки: %w", err)
	}

	if err := createShortcuts(appPath, dest); err != nil {
		return fmt.Errorf("не удалось создать ярлыки: %w", err)
	}
	if err := registerStartup(appPath); err != nil {
		return fmt.Errorf("не удалось включить автозапуск: %w", err)
	}
	if err := registerURLProtocol(appPath); err != nil {
		return fmt.Errorf("не удалось зарегистрировать ссылку запуска linkvideomonitor: %w", err)
	}
	if err := registerUninstall(appPath, dest); err != nil {
		return fmt.Errorf("не удалось зарегистрировать удаление программы: %w", err)
	}
	if err := installUACServiceElevated(appPath); err != nil {
		return fmt.Errorf("не удалось установить службу защищённого рабочего стола: %w", err)
	}
	removeLegacyRegistration(roaming)

	cmd := exec.Command(appPath)
	cmd.Dir = dest
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("программа установлена, но не запустилась: %w", err)
	}
	return nil
}

func extractPayload(dest string) error {
	zr, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		return fmt.Errorf("повреждён встроенный пакет установки: %w", err)
	}
	for _, f := range zr.File {
		clean := filepath.Clean(f.Name)
		if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
			return fmt.Errorf("недопустимый путь в пакете: %s", f.Name)
		}
		target := filepath.Join(dest, clean)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
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
	}
	return nil
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

func createShortcuts(appPath, dest string) error {
	script := `$w=New-Object -ComObject WScript.Shell;` +
		`$start=[Environment]::GetFolderPath('StartMenu')+'\Programs\LinkVideo Monitor.lnk';` +
		`$desktop=[Environment]::GetFolderPath('Desktop')+'\LinkVideo Monitor.lnk';` +
		`foreach($p in @($start,$desktop)){` +
		`$s=$w.CreateShortcut($p);$s.TargetPath='` + psEscape(appPath) + `';$s.WorkingDirectory='` + psEscape(dest) + `';$s.IconLocation='` + psEscape(appPath) + `,0';$s.Description='LinkVideo Monitor';$s.Save()}`
	return runHidden("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
}

func registerStartup(appPath string) error {
	key := `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	_ = runHidden("reg.exe", "delete", key, "/v", "LinkVideo Screen Sender", "/f")
	_ = runHidden("reg.exe", "delete", key, "/v", "LinkVideo.Monitor", "/f")
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
	key := `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\LinkVideo Monitor`
	values := [][]string{
		{"/v", "DisplayName", "/t", "REG_SZ", "/d", productName},
		{"/v", "DisplayVersion", "/t", "REG_SZ", "/d", version},
		{"/v", "Publisher", "/t", "REG_SZ", "/d", "LinkVideo"},
		{"/v", "InstallLocation", "/t", "REG_SZ", "/d", dest},
		{"/v", "DisplayIcon", "/t", "REG_SZ", "/d", appPath + ",0"},
		{"/v", "UninstallString", "/t", "REG_SZ", "/d", `"` + appPath + `" --uninstall`},
		{"/v", "NoModify", "/t", "REG_DWORD", "/d", "1"},
		{"/v", "NoRepair", "/t", "REG_DWORD", "/d", "1"},
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
	self, err := os.Executable()
	if err != nil {
		return err
	}
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(self)
	params, _ := syscall.UTF16PtrFromString(`--service-install "` + strings.ReplaceAll(appPath, `"`, `\"`) + `"`)
	info := shellExecuteInfoW{
		CBSize: uint32(unsafe.Sizeof(shellExecuteInfoW{})),
		Mask:   0x00000040, // SEE_MASK_NOCLOSEPROCESS
		Verb:   verb, File: file, Parameters: params, Show: 0,
	}
	proc := shell32Installer.NewProc("ShellExecuteExW")
	ok, _, callErr := proc.Call(uintptr(unsafe.Pointer(&info)))
	if ok == 0 || info.Process == 0 {
		return fmt.Errorf("запрос прав администратора отменён или не выполнен: %v", callErr)
	}
	defer kernel32Installer.NewProc("CloseHandle").Call(info.Process)
	wait := kernel32Installer.NewProc("WaitForSingleObject")
	getExit := kernel32Installer.NewProc("GetExitCodeProcess")
	wait.Call(info.Process, 120000)
	var exitCode uint32
	if ok, _, callErr := getExit.Call(info.Process, uintptr(unsafe.Pointer(&exitCode))); ok == 0 {
		return fmt.Errorf("не удалось получить результат установки службы: %v", callErr)
	}
	if exitCode != 0 {
		return fmt.Errorf("служба не установлена, код %d", exitCode)
	}
	return nil
}

func installUACServiceWorker(appPath string) error {
	if _, err := os.Stat(appPath); err != nil {
		return fmt.Errorf("основной EXE не найден: %w", err)
	}
	programData := os.Getenv("PROGRAMDATA")
	if programData == "" {
		return errors.New("Windows не вернула путь PROGRAMDATA")
	}
	serviceDir := filepath.Join(programData, "LinkVideo.Monitor", "Service")
	sessionsDir := filepath.Join(programData, "LinkVideo.Monitor", "Sessions")
	if err := os.MkdirAll(serviceDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(sessionsDir, 0o777); err != nil {
		return err
	}
	servicePath := filepath.Join(serviceDir, "LinkVideo.Monitor.Service.exe")
	_ = runHidden("sc.exe", "stop", "LinkVideoMonitorCapture")
	time.Sleep(700 * time.Millisecond)
	_ = runHidden("taskkill.exe", "/IM", "LinkVideo.Monitor.Service.exe", "/T", "/F")
	if err := copyFile(appPath, servicePath); err != nil {
		return fmt.Errorf("не удалось обновить файл службы: %w", err)
	}
	// Language-independent SID for the built-in Users group. Ordinary users
	// may only create and refresh their own session request files here.
	_ = runHidden("icacls.exe", sessionsDir, "/grant", "*S-1-5-32-545:(OI)(CI)M", "/T", "/C")
	_ = runHidden("sc.exe", "delete", "LinkVideoMonitorCapture")
	time.Sleep(400 * time.Millisecond)
	binPath := `"` + servicePath + `" --uac-service`
	if err := runHidden("sc.exe", "create", "LinkVideoMonitorCapture", "binPath=", binPath, "start=", "auto", "obj=", "LocalSystem", "DisplayName=", "LinkVideo Monitor — защищённый рабочий стол"); err != nil {
		return err
	}
	_ = runHidden("sc.exe", "description", "LinkVideoMonitorCapture", "Захват UAC и защищённого рабочего стола для LinkVideo Monitor")
	_ = runHidden("sc.exe", "failure", "LinkVideoMonitorCapture", "reset=", "86400", "actions=", "restart/5000/restart/15000/restart/60000")
	if err := runHidden("sc.exe", "start", "LinkVideoMonitorCapture"); err != nil {
		return err
	}
	return nil
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
	ok, _, callErr := kernel32Installer.NewProc("MoveFileExW").Call(
		uintptr(unsafe.Pointer(tempPtr)),
		uintptr(unsafe.Pointer(targetPtr)),
		moveFileReplaceExisting|moveFileWriteThrough,
	)
	if ok == 0 {
		return fmt.Errorf("MoveFileExW: %w", callErr)
	}
	return nil
}

func runHidden(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %v (%s)", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func psEscape(value string) string { return strings.ReplaceAll(value, "'", "''") }

func messageBox(text, title string, flags uintptr) uintptr {
	t, _ := syscall.UTF16PtrFromString(text)
	h, _ := syscall.UTF16PtrFromString(title)
	result, _, _ := messageBoxW.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(h)), flags)
	return result
}

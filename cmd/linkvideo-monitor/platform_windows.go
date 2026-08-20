//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	esContinuous      = 0x80000000
	esSystemRequired  = 0x00000001
	esDisplayRequired = 0x00000002

	processSetInformation          = 0x0200
	processQueryLimitedInformation = 0x1000
	belowNormalPriorityClass       = 0x00004000
)

var (
	kernel32Platform               = syscall.NewLazyDLL("kernel32.dll")
	procSetThreadExecutionState    = kernel32Platform.NewProc("SetThreadExecutionState")
	procOpenProcess                = kernel32Platform.NewProc("OpenProcess")
	procSetPriorityClass           = kernel32Platform.NewProc("SetPriorityClass")
	procCloseHandle                = kernel32Platform.NewProc("CloseHandle")
	procQueryFullProcessImageNameW = kernel32Platform.NewProc("QueryFullProcessImageNameW")

	procGetWindowTextLengthW     = user32.NewProc("GetWindowTextLengthW")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")

	shell32Platform   = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteW = shell32Platform.NewProc("ShellExecuteW")
)

func lowerProcessPriority(pid int) {
	if pid <= 0 {
		return
	}
	h, _, _ := procOpenProcess.Call(processSetInformation, 0, uintptr(pid))
	if h == 0 {
		return
	}
	defer procCloseHandle.Call(h)
	_, _, _ = procSetPriorityClass.Call(h, belowNormalPriorityClass)
}

func syncStartupRegistration(enabled bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	key := `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
	name := "LinkVideo Monitor"
	// Удаляем запись прошлых beta-версий, чтобы Windows не запускала две программы.
	old := exec.Command("reg.exe", "delete", key, "/v", "LinkVideo Screen Sender", "/f")
	hideChildWindow(old)
	_ = old.Run()
	if !enabled {
		cmd := exec.Command("reg.exe", "delete", key, "/v", name, "/f")
		hideChildWindow(cmd)
		_ = cmd.Run()
		return nil
	}
	value := fmt.Sprintf(`"%s" --background`, exe)
	cmd := exec.Command("reg.exe", "add", key, "/v", name, "/t", "REG_SZ", "/d", value, "/f")
	hideChildWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("не удалось добавить программу в автозагрузку: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func syncURLProtocolRegistration() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	base := `HKCU\Software\Classes\linkvideomonitor`
	commands := [][]string{
		{"add", base, "/ve", "/t", "REG_SZ", "/d", "URL:LinkVideo Monitor Protocol", "/f"},
		{"add", base, "/v", "URL Protocol", "/t", "REG_SZ", "/d", "", "/f"},
		{"add", base + `\DefaultIcon`, "/ve", "/t", "REG_SZ", "/d", exe + ",0", "/f"},
		{"add", base + `\shell\open\command`, "/ve", "/t", "REG_SZ", "/d", fmt.Sprintf(`"%s" "%%1"`, exe), "/f"},
	}
	for _, args := range commands {
		cmd := exec.Command("reg.exe", args...)
		hideChildWindow(cmd)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("не удалось зарегистрировать linkvideomonitor: %v (%s)", err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

type sleepPreventionRequest struct {
	enabled       bool
	keepDisplayOn bool
	done          chan struct{}
}

var (
	sleepPreventionOnce sync.Once
	sleepPreventionCh   chan sleepPreventionRequest
)

func startSleepPreventionWorker() {
	sleepPreventionCh = make(chan sleepPreventionRequest)
	go func() {
		// SetThreadExecutionState is attached to the calling Windows thread, not
		// the whole process. Keep all set/reset calls on one persistent OS thread.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		for req := range sleepPreventionCh {
			flags := uintptr(esContinuous)
			if req.enabled {
				flags |= esSystemRequired
				if req.keepDisplayOn {
					flags |= esDisplayRequired
				}
			}
			_, _, _ = procSetThreadExecutionState.Call(flags)
			close(req.done)
		}
	}()
}

func setSleepPrevention(enabled, keepDisplayOn bool) {
	sleepPreventionOnce.Do(startSleepPreventionWorker)
	done := make(chan struct{})
	sleepPreventionCh <- sleepPreventionRequest{enabled: enabled, keepDisplayOn: keepDisplayOn, done: done}
	<-done
}

func processImageName(pid uint32) string {
	if pid == 0 {
		return ""
	}
	h, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer procCloseHandle.Call(h)
	buf := make([]uint16, 32768)
	size := uint32(len(buf))
	ok, _, _ := procQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if ok == 0 || size == 0 {
		return ""
	}
	return filepath.Base(syscall.UTF16ToString(buf[:size]))
}

func runUninstaller() {
	if exe, err := os.Executable(); err == nil {
		modern := filepath.Join(filepath.Dir(exe), "Uninstall.exe")
		if _, statErr := os.Stat(modern); statErr == nil {
			cmd := exec.Command(modern)
			hideChildWindow(cmd)
			if cmd.Start() == nil {
				return
			}
		}
	}
	text, _ := syscall.UTF16PtrFromString("Удалить LinkVideo Monitor и все связанные данные с этого компьютера?\n\nБудут удалены настройки, журналы и файлы предыдущих версий программы.")
	title, _ := syscall.UTF16PtrFromString("LinkVideo Monitor")
	answer, _, _ := user32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x00000004|0x00000030)
	if answer != 6 { // IDYES
		return
	}

	exe, err := os.Executable()
	if err != nil {
		showUninstallMessage("Не удалось запустить удаление: "+err.Error(), 0x00000010)
		return
	}
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("LinkVideo-Monitor-Uninstall-%d.exe", os.Getpid()))
	data, err := os.ReadFile(exe)
	if err != nil {
		showUninstallMessage("Не удалось подготовить удаление: "+err.Error(), 0x00000010)
		return
	}
	if err := os.WriteFile(tmp, data, 0o700); err != nil {
		showUninstallMessage("Не удалось подготовить удаление: "+err.Error(), 0x00000010)
		return
	}

	params := fmt.Sprintf(`--uninstall-worker %d`, os.Getpid())
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(tmp)
	paramPtr, _ := syscall.UTF16PtrFromString(params)
	result, _, _ := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), uintptr(unsafe.Pointer(paramPtr)), 0, 0)
	if result <= 32 {
		_ = os.Remove(tmp)
		showUninstallMessage("Удаление отменено или Windows не разрешила получить права администратора.", 0x00000010)
	}
}

func runUninstallWorker(parentPID int) {
	// Отдельный временный EXE работает с повышенными правами, поэтому окно PowerShell больше не требуется.
	for _, image := range []string{"LinkVideo.Monitor.exe", "LinkVideo.Monitor.Service.exe", "LinkVideo.ScreenOverlay.exe", "LinkVideo.AudioLoopback.exe", "LinkVideo.ScreenSender.exe"} {
		runCleanupCommand("taskkill.exe", "/IM", image, "/T", "/F")
	}
	if parentPID > 0 {
		for i := 0; i < 30; i++ {
			cmd := exec.Command("tasklist.exe", "/FI", fmt.Sprintf("PID eq %d", parentPID), "/NH")
			hideChildWindow(cmd)
			out, _ := cmd.Output()
			if !strings.Contains(string(out), fmt.Sprintf("%d", parentPID)) {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
	}

	local := os.Getenv("LOCALAPPDATA")
	roaming := os.Getenv("APPDATA")
	programData := os.Getenv("PROGRAMDATA")
	programFiles := os.Getenv("ProgramFiles")
	programFilesX86 := os.Getenv("ProgramFiles(x86)")
	userProfile := os.Getenv("USERPROFILE")
	roots := []string{
		filepath.Join(local, "Programs", "LinkVideo.Monitor"),
		filepath.Join(local, "Programs", "LinkVideo.ScreenSender"),
		filepath.Join(local, "LinkVideo.Monitor"),
		filepath.Join(local, "LinkVideo.ScreenSender"),
		filepath.Join(roaming, "LinkVideo.Monitor"),
		filepath.Join(roaming, "LinkVideo.ScreenSender"),
		filepath.Join(programData, "LinkVideo.Monitor"),
		filepath.Join(programData, "LinkVideo.ScreenSender"),
		filepath.Join(programFiles, "LinkVideo.Monitor"),
		filepath.Join(programFilesX86, "LinkVideo.Monitor"),
	}

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

	for _, name := range []string{"LinkVideoMonitorCapture", "LinkVideo Monitor", "LinkVideo.Monitor", "LinkVideo Screen Sender", "LinkVideo.ScreenSender"} {
		runCleanupCommand("schtasks.exe", "/Delete", "/TN", name, "/F")
		runCleanupCommand("sc.exe", "stop", name)
		runCleanupCommand("sc.exe", "delete", name)
	}

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

	var remaining []string
	for _, root := range roots {
		if strings.TrimSpace(root) == "" || filepath.Clean(root) == "." {
			continue
		}
		for attempt := 0; attempt < 4; attempt++ {
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

	if len(remaining) == 0 {
		showUninstallMessage("LinkVideo Monitor и связанные данные удалены.", 0x00000040)
	} else {
		showUninstallMessage("Программа удалена, но Windows не позволила очистить некоторые файлы:\n\n"+strings.Join(remaining, "\n"), 0x00000030)
	}

	self, _ := os.Executable()
	cmd := exec.Command("cmd.exe", "/C", fmt.Sprintf(`ping 127.0.0.1 -n 3 >nul & del /f /q "%s"`, self))
	hideChildWindow(cmd)
	_ = cmd.Start()
}

func runCleanupCommand(name string, args ...string) {
	cmd := exec.Command(name, args...)
	hideChildWindow(cmd)
	_ = cmd.Run()
}

func showUninstallMessage(text string, flags uintptr) {
	message, _ := syscall.UTF16PtrFromString(text)
	title, _ := syscall.UTF16PtrFromString("LinkVideo Monitor")
	_, _, _ = user32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(message)), uintptr(unsafe.Pointer(title)), flags)
}

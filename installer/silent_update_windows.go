//go:build windows && !uninstaller

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func init() {
	if len(os.Args) < 2 || os.Args[1] != "--silent-update-elevated" {
		return
	}
	defer scheduleSelfDelete()
	if !isProcessElevated() {
		appendSilentUpdateLog("silent update rejected: installer is not elevated")
		os.Exit(21)
	}
	time.Sleep(1500 * time.Millisecond)
	if err := installProductSilentSystem(); err != nil {
		appendSilentUpdateLog("silent update failed: " + err.Error())
		if recoveryErr := recoverCaptureServiceAfterFailedUpdate(); recoveryErr != nil {
			appendSilentUpdateLog("service recovery after failed update also failed: " + recoveryErr.Error())
		} else {
			appendSilentUpdateLog("service recovered after failed update")
		}
		os.Exit(22)
	}
	appendSilentUpdateLog("silent update completed: " + version)
	os.Exit(0)
}

func installProductSilentSystem() error {
	dest := defaultInstallDir()
	legacyLocalDest := filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "LinkVideo.Monitor")
	oldDest := filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "LinkVideo.ScreenSender")
	legacyProgramFiles := filepath.Join(os.Getenv("ProgramFiles"), "LinkVideo.Monitor")
	legacyProgramFilesX86 := filepath.Join(os.Getenv("ProgramFiles(x86)"), "LinkVideo.Monitor")
	appPath := filepath.Join(dest, "LinkVideo.Monitor.exe")
	stopCaptureServiceForUpgrade()
	stopInstalledProcesses(dest, legacyLocalDest, oldDest, legacyProgramFiles, legacyProgramFilesX86)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("не удалось создать папку установки: %w", err)
	}
	if err := extractPayload(dest, nil); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(dest, "LinkVideo.ScreenOverlay.exe"))
	_ = os.Remove(filepath.Join(dest, "LinkVideo.AudioLoopback.exe"))
	if _, err := os.Stat(appPath); err != nil {
		return fmt.Errorf("основной файл программы не найден после обновления: %w", err)
	}
	if err := registerUninstall(appPath, dest); err != nil {
		return fmt.Errorf("не удалось обновить регистрацию программы: %w", err)
	}
	if err := installUACServiceElevated(appPath); err != nil {
		return fmt.Errorf("не удалось обновить фоновую службу: %w", err)
	}
	return nil
}

func recoverCaptureServiceAfterFailedUpdate() error {
	appPath := filepath.Join(defaultInstallDir(), "LinkVideo.Monitor.exe")
	if _, err := os.Stat(appPath); err != nil {
		return fmt.Errorf("основной EXE недоступен для восстановления службы: %w", err)
	}
	return installUACServiceElevated(appPath)
}

func appendSilentUpdateLog(message string) {
	base := os.Getenv("PROGRAMDATA")
	if base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "LinkVideo.Monitor")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "update-install.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s %s\r\n", time.Now().Format("2006-01-02 15:04:05"), message)
}

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
	stage := dest + ".update-new"
	backup := dest + ".update-old"
	legacyLocalDest := filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "LinkVideo.Monitor")
	oldDest := filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "LinkVideo.ScreenSender")
	legacyProgramFiles := filepath.Join(os.Getenv("ProgramFiles"), "LinkVideo.Monitor")
	legacyProgramFilesX86 := filepath.Join(os.Getenv("ProgramFiles(x86)"), "LinkVideo.Monitor")

	// Build the complete new installation beside the live directory first. The
	// currently running stream is not interrupted while the payload is unpacked.
	_ = os.RemoveAll(stage)
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return fmt.Errorf("не удалось создать временную папку обновления: %w", err)
	}
	if err := extractPayload(stage, nil); err != nil {
		_ = os.RemoveAll(stage)
		return fmt.Errorf("не удалось подготовить пакет обновления: %w", err)
	}
	stagedApp := filepath.Join(stage, "LinkVideo.Monitor.exe")
	if _, err := os.Stat(stagedApp); err != nil {
		_ = os.RemoveAll(stage)
		return fmt.Errorf("основной файл программы отсутствует в подготовленном обновлении: %w", err)
	}

	// Only after the new payload is complete do we stop capture and swap the
	// installation directory. Settings and the LinkVideo connection live in the
	// user profile, outside Program Files, and are never touched here.
	stopCaptureServiceForUpgrade()
	stopInstalledProcesses(dest, legacyLocalDest, oldDest, legacyProgramFiles, legacyProgramFilesX86)
	_ = os.RemoveAll(backup)
	hadPrevious := false
	if _, err := os.Stat(dest); err == nil {
		hadPrevious = true
		if err := os.Rename(dest, backup); err != nil {
			_ = os.RemoveAll(stage)
			return fmt.Errorf("не удалось подготовить откат предыдущей версии: %w", err)
		}
	}
	if err := os.Rename(stage, dest); err != nil {
		if hadPrevious {
			_ = os.Rename(backup, dest)
		}
		return fmt.Errorf("не удалось активировать подготовленное обновление: %w", err)
	}

	appPath := filepath.Join(dest, "LinkVideo.Monitor.exe")
	rollback := func(cause error) error {
		stopCaptureServiceForUpgrade()
		_ = os.RemoveAll(stage)
		failedDir := dest + ".update-failed"
		_ = os.RemoveAll(failedDir)
		_ = os.Rename(dest, failedDir)
		if hadPrevious {
			if err := os.Rename(backup, dest); err != nil {
				return fmt.Errorf("%v; дополнительно не удалось вернуть предыдущую версию: %w", cause, err)
			}
			oldApp := filepath.Join(dest, "LinkVideo.Monitor.exe")
			if err := installUACServiceElevated(oldApp); err != nil {
				return fmt.Errorf("%v; предыдущие файлы возвращены, но служба не восстановилась: %w", cause, err)
			}
		}
		_ = os.RemoveAll(failedDir)
		return cause
	}

	if err := installUACServiceElevated(appPath); err != nil {
		return rollback(fmt.Errorf("не удалось обновить фоновую службу: %w", err))
	}
	if err := registerUninstall(appPath, dest); err != nil {
		// The program and service are already updated correctly. Do not tear down a
		// working stream only because Windows could not refresh Add/Remove Programs.
		appendSilentUpdateLog("warning: uninstall registration was not refreshed: " + err.Error())
	}
	_ = os.RemoveAll(backup)
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

//go:build windows && !uninstaller

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type silentUpdateFailureMarker struct {
	Version  string `json:"version"`
	Failures int    `json:"failures"`
	AtUnix   int64  `json:"at_unix"`
}

func init() {
	if len(os.Args) < 2 || os.Args[1] != "--silent-update-elevated" {
		return
	}
	targetVersion := version
	if len(os.Args) > 2 && strings.TrimSpace(os.Args[2]) != "" {
		targetVersion = strings.TrimSpace(os.Args[2])
	}
	if !isProcessElevated() {
		appendSilentUpdateLog("silent update rejected: installer is not elevated")
		finishSilentUpdate(21)
	}
	time.Sleep(1500 * time.Millisecond)
	if err := installProductSilentSystem(); err != nil {
		appendSilentUpdateLog("silent update failed: " + err.Error())
		recordSilentUpdateFailure(targetVersion)
		if recoveryErr := recoverCaptureServiceAfterFailedUpdate(); recoveryErr != nil {
			appendSilentUpdateLog("service recovery after failed update also failed: " + recoveryErr.Error())
		} else {
			appendSilentUpdateLog("service recovered after failed update")
		}
		finishSilentUpdate(22)
	}
	clearSilentUpdateFailure()
	appendSilentUpdateLog("silent update completed: " + targetVersion)
	finishSilentUpdate(0)
}

func finishSilentUpdate(code int) {
	// os.Exit does not run deferred functions, therefore schedule cleanup before
	// terminating the temporary installer process.
	scheduleSelfDelete()
	os.Exit(code)
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
		if err := renameDirectoryWithRetry(dest, backup); err != nil {
			_ = os.RemoveAll(stage)
			return fmt.Errorf("не удалось подготовить откат предыдущей версии: %w", err)
		}
	}
	if err := renameDirectoryWithRetry(stage, dest); err != nil {
		if hadPrevious {
			if restoreErr := renameDirectoryWithRetry(backup, dest); restoreErr != nil {
				return fmt.Errorf("не удалось активировать подготовленное обновление: %v; дополнительно не удалось вернуть предыдущие файлы: %w", err, restoreErr)
			}
		}
		return fmt.Errorf("не удалось активировать подготовленное обновление: %w", err)
	}

	appPath := filepath.Join(dest, "LinkVideo.Monitor.exe")
	rollback := func(cause error) error {
		stopCaptureServiceForUpgrade()
		_ = os.RemoveAll(stage)
		failedDir := dest + ".update-failed"
		_ = os.RemoveAll(failedDir)
		if err := renameDirectoryWithRetry(dest, failedDir); err != nil {
			return fmt.Errorf("%v; дополнительно не удалось изолировать неудачную новую версию: %w", cause, err)
		}
		if hadPrevious {
			if err := renameDirectoryWithRetry(backup, dest); err != nil {
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

func renameDirectoryWithRetry(from, to string) error {
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

func recoverCaptureServiceAfterFailedUpdate() error {
	appPath := filepath.Join(defaultInstallDir(), "LinkVideo.Monitor.exe")
	if _, err := os.Stat(appPath); err != nil {
		return fmt.Errorf("основной EXE недоступен для восстановления службы: %w", err)
	}
	return installUACServiceElevated(appPath)
}

func silentUpdateFailurePath() string {
	base := strings.TrimSpace(os.Getenv("PROGRAMDATA"))
	if base == "" {
		return ""
	}
	return filepath.Join(base, "LinkVideo.Monitor", "update-failure.json")
}

func normalizeSilentUpdateVersion(value string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "v")
}

func recordSilentUpdateFailure(targetVersion string) {
	path := silentUpdateFailurePath()
	if path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	marker := silentUpdateFailureMarker{Version: targetVersion, Failures: 1, AtUnix: time.Now().Unix()}
	if data, err := os.ReadFile(path); err == nil {
		var previous silentUpdateFailureMarker
		if json.Unmarshal(data, &previous) == nil && normalizeSilentUpdateVersion(previous.Version) == normalizeSilentUpdateVersion(targetVersion) && previous.Failures > 0 {
			marker.Failures = previous.Failures + 1
		}
	}
	data, err := json.Marshal(marker)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, path)
	}
}

func clearSilentUpdateFailure() {
	if path := silentUpdateFailurePath(); path != "" {
		_ = os.Remove(path)
		_ = os.Remove(path + ".tmp")
	}
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

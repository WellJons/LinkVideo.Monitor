//go:build darwin

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const macOSUninstallerName = "Uninstall LinkVideo Monitor.command"

func lowerProcessPriority(pid int) {
	if pid <= 0 {
		return
	}
	// A process may always lower the scheduling priority of one of its own
	// children. Unlike Windows SetPriorityClass this does not require a native
	// bridge and keeps the adaptive performance policy platform isolated.
	cmd := exec.Command("/usr/bin/renice", "10", "-p", strconv.Itoa(pid))
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Run()
}

func listWindows() ([]WindowInfo, error) {
	return nil, errors.New("выбор отдельного окна не используется текущими режимами захвата")
}

func selectRegionInteractive() (Region, error) {
	return Region{}, errors.New("интерактивный выбор области не используется текущими режимами захвата")
}

func runOverlay(startedUnix int64, x, y int) error {
	cmd, err := recordingOverlayCommand(startedUnix, x, y)
	if err != nil {
		return err
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("индикатор записи macOS: %w", err)
	}
	return nil
}

func placeOverlayInteractive(x, y int) (OverlayPosition, error) {
	cmd, err := overlayPlacementCommand(x, y)
	if err != nil {
		return OverlayPosition{}, err
	}
	out, err := cmd.Output()
	if err != nil {
		return OverlayPosition{}, fmt.Errorf("перемещение индикатора macOS: %w", err)
	}
	var position OverlayPosition
	if err := json.Unmarshal(out, &position); err != nil {
		return OverlayPosition{}, fmt.Errorf("не удалось разобрать позицию индикатора macOS: %w", err)
	}
	return position, nil
}

func runUninstaller() {
	path, err := macOSUninstallerPath()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return
	}
	// The uninstaller needs an interactive Terminal because its privileged
	// stage uses sudo. LaunchServices opens .command files in Terminal while
	// keeping the Monitor process itself unprivileged.
	cmd := exec.Command("/usr/bin/open", path)
	if err := cmd.Start(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "не удалось открыть удаление LinkVideo Monitor:", err)
	}
}

func runUninstallWorker(parentPID int) {
	// macOS removal is implemented by the installed .command script. It handles
	// ServiceManagement cleanup, process termination, pkg receipt removal and an
	// optional purge stage instead of spawning a Windows-style worker process.
}

func macOSUninstallerPath() (string, error) {
	if value := strings.TrimSpace(os.Getenv("LINKVIDEO_UNINSTALLER")); value != "" {
		if info, err := os.Stat(value); err == nil && !info.IsDir() {
			return value, nil
		}
		return "", fmt.Errorf("указанный LINKVIDEO_UNINSTALLER не найден: %s", value)
	}

	exe, _ := os.Executable()
	for _, candidate := range macOSUninstallerCandidates(exe) {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", errors.New("не найден установленный удалятель LinkVideo Monitor")
}

func macOSUninstallerCandidates(executable string) []string {
	candidates := make([]string, 0, 2)
	if executable != "" {
		macOSDir := filepath.Dir(executable)
		contentsDir := filepath.Dir(macOSDir)
		appDir := filepath.Dir(contentsDir)
		if filepath.Base(macOSDir) == "MacOS" && filepath.Base(contentsDir) == "Contents" && strings.HasSuffix(filepath.Base(appDir), ".app") {
			candidates = append(candidates, filepath.Join(filepath.Dir(appDir), macOSUninstallerName))
		}
	}
	installed := filepath.Join("/Applications", macOSUninstallerName)
	for _, candidate := range candidates {
		if filepath.Clean(candidate) == filepath.Clean(installed) {
			return candidates
		}
	}
	return append(candidates, installed)
}

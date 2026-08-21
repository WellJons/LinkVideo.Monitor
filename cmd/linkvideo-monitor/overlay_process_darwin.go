//go:build darwin

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func recordingOverlayCommand(startedUnix int64, x, y int) (*exec.Cmd, error) {
	helper, err := macOSOverlayHelperPath()
	if err != nil {
		return nil, err
	}
	return exec.Command(helper, "--overlay", "--started", strconv.FormatInt(startedUnix, 10), "--x", strconv.Itoa(x), "--y", strconv.Itoa(y)), nil
}

func overlayPlacementCommand(x, y int) (*exec.Cmd, error) {
	helper, err := macOSOverlayHelperPath()
	if err != nil {
		return nil, err
	}
	return exec.Command(helper, "--place-overlay", "--x", strconv.Itoa(x), "--y", strconv.Itoa(y)), nil
}

func cleanupLegacyOverlayProcesses() {}

func macOSOverlayHelperPath() (string, error) {
	if value := strings.TrimSpace(os.Getenv("LINKVIDEO_OVERLAY_HELPER")); value != "" {
		return value, nil
	}
	if exe, err := os.Executable(); err == nil && exe != "" {
		base := filepath.Dir(exe)
		candidates := []string{
			filepath.Clean(filepath.Join(base, "..", "Resources", "linkvideo-overlay-helper")),
			filepath.Join(base, "linkvideo-overlay-helper"),
		}
		for _, candidate := range candidates {
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}
	if path, err := exec.LookPath("linkvideo-overlay-helper"); err == nil {
		return path, nil
	}
	return "", errors.New("не найден helper индикатора записи macOS")
}

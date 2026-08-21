//go:build windows

package main

import (
	"os/exec"
	"strconv"
	"time"
)

func recordingOverlayCommand(startedUnix int64, x, y int) (*exec.Cmd, error) {
	exe, err := helperExecutable("LinkVideo.ScreenOverlay.exe")
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, "--overlay", strconv.FormatInt(startedUnix, 10), strconv.Itoa(x), strconv.Itoa(y))
	hideChildWindow(cmd)
	return cmd, nil
}

func overlayPlacementCommand(x, y int) (*exec.Cmd, error) {
	exe, err := helperExecutable("LinkVideo.ScreenOverlay.exe")
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, "--place-overlay", strconv.Itoa(x), strconv.Itoa(y))
	hideChildWindow(cmd)
	return cmd, nil
}

func cleanupLegacyOverlayProcesses() {
	cmd := exec.Command("taskkill.exe", "/IM", "LinkVideo.ScreenOverlay.exe", "/T", "/F")
	hideChildWindow(cmd)
	_ = cmd.Run()
	time.Sleep(180 * time.Millisecond)
}

//go:build !windows && !darwin

package main

import (
	"errors"
	"os/exec"
)

func recordingOverlayCommand(startedUnix int64, x, y int) (*exec.Cmd, error) {
	return nil, errors.New("индикатор записи не поддерживается на этой платформе")
}

func overlayPlacementCommand(x, y int) (*exec.Cmd, error) {
	return nil, errors.New("перемещение индикатора не поддерживается на этой платформе")
}

func cleanupLegacyOverlayProcesses() {}

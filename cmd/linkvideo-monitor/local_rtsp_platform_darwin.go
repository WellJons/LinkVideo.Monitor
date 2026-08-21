//go:build darwin

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const macOSMediaMTXVersion = "1.20.0"

func selectedMediaMTXRelease() mediaMTXRelease {
	// Keep the macOS release line independent from the Windows managed runtime.
	// The app bundle contains this verified Universal build, while Windows keeps
	// its separately tested mediaMTXCurrentRelease version and download policy.
	return mediaMTXRelease{Version: macOSMediaMTXVersion}
}

func isManagedMediaMTXPath(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || strings.EqualFold(value, "mediamtx.exe") || strings.EqualFold(value, "mediamtx")
}

func mediaMTXDefaultPathForExecutable(executable string) string {
	return filepath.Join(filepath.Dir(executable), "mediamtx")
}

func mediaMTXDefaultPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return mediaMTXDefaultPathForExecutable(exe), nil
}

func ensureManagedMediaMTX(a *app, release mediaMTXRelease) (string, error) {
	path, err := mediaMTXDefaultPath()
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
		return path, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if resolved, lookErr := exec.LookPath("mediamtx"); lookErr == nil {
		a.appendLog("Встроенный MediaMTX не найден; используется mediamtx из PATH")
		return resolved, nil
	}
	return "", fmt.Errorf("встроенный MediaMTX %s для macOS не найден", release.Version)
}

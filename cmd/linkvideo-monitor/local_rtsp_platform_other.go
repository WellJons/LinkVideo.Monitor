//go:build !windows && !darwin

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

func selectedMediaMTXRelease() mediaMTXRelease { return mediaMTXCurrentRelease }

func isManagedMediaMTXPath(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || strings.EqualFold(value, "mediamtx.exe") || strings.EqualFold(value, "mediamtx")
}

func ensureManagedMediaMTX(_ *app, release mediaMTXRelease) (string, error) {
	if path, err := exec.LookPath("mediamtx"); err == nil {
		return path, nil
	}
	return "", fmt.Errorf("MediaMTX %s не найден в PATH", release.Version)
}

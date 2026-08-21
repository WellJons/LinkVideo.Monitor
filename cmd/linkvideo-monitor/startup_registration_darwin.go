//go:build darwin

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func syncStartupRegistration(enabled bool) error {
	helper, err := macOSServiceHelperPath()
	if err != nil {
		return err
	}
	cmd := exec.Command(helper, macOSStartupHelperArgs(enabled)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return fmt.Errorf("ServiceManagement: %s", detail)
		}
		return fmt.Errorf("ServiceManagement: %w", err)
	}
	return nil
}

func macOSStartupHelperArgs(enabled bool) []string {
	return []string{"--set-startup", fmt.Sprint(enabled)}
}

func macOSServiceHelperPath() (string, error) {
	if value := strings.TrimSpace(os.Getenv("LINKVIDEO_SERVICE_HELPER")); value != "" {
		return value, nil
	}
	if exe, err := os.Executable(); err == nil && exe != "" {
		base := filepath.Dir(exe)
		candidates := []string{
			filepath.Join(base, "linkvideo-service-helper"),
			filepath.Clean(filepath.Join(base, "..", "Resources", "linkvideo-service-helper")),
		}
		for _, candidate := range candidates {
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}
	if path, err := exec.LookPath("linkvideo-service-helper"); err == nil {
		return path, nil
	}
	return "", errors.New("не найден macOS ServiceManagement helper")
}

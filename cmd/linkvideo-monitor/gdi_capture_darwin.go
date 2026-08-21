//go:build darwin

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// runGDICapture is the platform capture entrypoint used by captureSupervisor.
// On macOS the historical function name is kept only to preserve the existing
// process boundary: the actual producer is ScreenCaptureKit, not GDI.
func runGDICapture(out io.Writer, x, y, width, height, outputWidth, outputHeight, fps int, drawCursor bool) error {
	helper, err := macOSCaptureHelperPath()
	if err != nil {
		return err
	}
	if outputWidth < 2 || outputHeight < 2 {
		return errors.New("invalid macOS capture dimensions")
	}
	if fps < 1 {
		fps = 15
	}

	displayID := macOSDisplayIDForCaptureRect(x, y, width, height)
	args := []string{
		"--capture",
		"--display-id", strconv.FormatUint(uint64(displayID), 10),
		"--width", strconv.Itoa(outputWidth),
		"--height", strconv.Itoa(outputHeight),
		"--fps", strconv.Itoa(fps),
		"--cursor", strconv.FormatBool(drawCursor),
	}
	cmd := exec.Command(helper, args...)
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ScreenCaptureKit helper: %w", err)
	}
	return nil
}

func macOSCaptureHelperPath() (string, error) {
	if value := strings.TrimSpace(os.Getenv("LINKVIDEO_CAPTURE_HELPER")); value != "" {
		return value, nil
	}

	exe, err := os.Executable()
	if err == nil && exe != "" {
		base := filepath.Dir(exe)
		candidates := []string{
			filepath.Clean(filepath.Join(base, "..", "Resources", "linkvideo-capture-helper")),
			filepath.Join(base, "linkvideo-capture-helper"),
		}
		for _, candidate := range candidates {
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}

	if path, lookErr := exec.LookPath("linkvideo-capture-helper"); lookErr == nil {
		return path, nil
	}
	return "", errors.New("не найден macOS ScreenCaptureKit helper")
}

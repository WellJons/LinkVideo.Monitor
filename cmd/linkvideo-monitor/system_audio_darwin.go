//go:build darwin

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

// Keep the historical function name used by the shared audio bridge. On macOS
// the producer is ScreenCaptureKit system audio, not WASAPI.
func runWASAPILoopback(out io.Writer) error {
	helper, err := macOSCaptureHelperPath()
	if err != nil {
		return err
	}
	cmd := exec.Command(helper, macOSSystemAudioHelperArgs()...)
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ScreenCaptureKit system audio helper: %w", err)
	}
	return nil
}

func macOSSystemAudioHelperArgs() []string {
	return []string{"--capture-audio"}
}

//go:build darwin

package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func syncStartupRegistration(enabled bool) error {
	helper, err := macOSCaptureHelperPath()
	if err != nil {
		return err
	}
	cmd := exec.Command(helper, "--set-startup", fmt.Sprint(enabled))
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

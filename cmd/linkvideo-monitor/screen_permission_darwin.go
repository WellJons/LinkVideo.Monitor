//go:build darwin

package main

import (
	"errors"
	"os/exec"
	"strings"
)

type screenPermissionStatus struct {
	Supported bool `json:"supported"`
	Granted   bool `json:"granted"`
}

func screenCapturePermissionStatus() screenPermissionStatus {
	helper, err := macOSCaptureHelperPath()
	if err != nil {
		return screenPermissionStatus{Supported: true}
	}
	out, err := exec.Command(helper, "--check-permission").CombinedOutput()
	return screenPermissionStatus{Supported: true, Granted: err == nil && parseScreenPermissionGranted(string(out))}
}

func requestScreenCapturePermission() (screenPermissionStatus, error) {
	helper, err := macOSCaptureHelperPath()
	if err != nil {
		return screenPermissionStatus{Supported: true}, err
	}
	out, runErr := exec.Command(helper, "--request-permission").CombinedOutput()
	granted := parseScreenPermissionGranted(string(out))
	status := screenPermissionStatus{Supported: true, Granted: granted}
	if granted {
		return status, nil
	}
	if runErr != nil && !parseScreenPermissionDenied(string(out)) {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = runErr.Error()
		}
		return status, errors.New("не удалось запросить разрешение macOS на запись экрана: " + detail)
	}
	// A denied/not-yet-granted TCC prompt is a valid permission state, not an
	// HTTP/server failure. The UI can direct the user to System Settings.
	return status, nil
}

func parseScreenPermissionGranted(output string) bool {
	return strings.EqualFold(strings.TrimSpace(output), "granted")
}

func parseScreenPermissionDenied(output string) bool {
	return strings.EqualFold(strings.TrimSpace(output), "denied")
}

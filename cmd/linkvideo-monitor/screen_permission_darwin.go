//go:build darwin

package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

type screenPermissionStatus struct {
	Granted bool
}

func startMacOSScreenPermissionOnboarding(a *app) {
	if len(os.Args) > 1 && os.Args[1] == "--background" {
		return
	}
	go func() {
		// Let the local UI/browser open first. The system prompt then has a clear
		// foreground context instead of appearing during process bootstrap.
		time.Sleep(800 * time.Millisecond)
		if screenCapturePermissionStatus().Granted {
			return
		}
		a.appendLog("macOS: требуется разрешение на запись экрана")
		status, err := requestScreenCapturePermission()
		if err != nil {
			a.appendLog("Разрешение записи экрана macOS: " + err.Error())
			return
		}
		if status.Granted {
			a.appendLog("macOS разрешила запись экрана")
			return
		}
		a.appendLog("Запись экрана не разрешена. Включите LinkVideo Monitor: Системные настройки → Конфиденциальность и безопасность → Запись экрана и системного аудио")
	}()
}

func screenCapturePermissionStatus() screenPermissionStatus {
	helper, err := macOSCaptureHelperPath()
	if err != nil {
		return screenPermissionStatus{}
	}
	out, err := exec.Command(helper, "--check-permission").CombinedOutput()
	return screenPermissionStatus{Granted: err == nil && parseScreenPermissionGranted(string(out))}
}

func requestScreenCapturePermission() (screenPermissionStatus, error) {
	helper, err := macOSCaptureHelperPath()
	if err != nil {
		return screenPermissionStatus{}, err
	}
	out, runErr := exec.Command(helper, "--request-permission").CombinedOutput()
	granted := parseScreenPermissionGranted(string(out))
	status := screenPermissionStatus{Granted: granted}
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
	return status, nil
}

func parseScreenPermissionGranted(output string) bool {
	return strings.EqualFold(strings.TrimSpace(output), "granted")
}

func parseScreenPermissionDenied(output string) bool {
	return strings.EqualFold(strings.TrimSpace(output), "denied")
}

//go:build darwin

package main

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const macOSWorkspaceEventDebounce = 4 * time.Second

func startResumeWatcher(a *app) {
	go runMacOSWorkspaceWatcher(a)
}

func runMacOSWorkspaceWatcher(a *app) {
	var lastRecovery time.Time
	for {
		helper, err := macOSWorkspaceHelperPath()
		if err != nil {
			a.appendLog("События сна macOS недоступны: " + err.Error())
			runMacOSResumeGapFallback(a)
			return
		}
		cmd := exec.Command(helper, "--parent-pid", strconv.Itoa(os.Getpid()))
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			message, ok := macOSWorkspaceResumeMessage(scanner.Text())
			if !ok {
				continue
			}
			now := time.Now()
			if !lastRecovery.IsZero() && now.Sub(lastRecovery) < macOSWorkspaceEventDebounce {
				continue
			}
			lastRecovery = now
			handleResumeRecovery(a, message)
		}
		_ = cmd.Wait()
		time.Sleep(2 * time.Second)
	}
}

func runMacOSResumeGapFallback(a *app) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	last := time.Now()
	for now := range ticker.C {
		gap := now.Sub(last)
		last = now
		if gap >= 20*time.Second {
			handleResumeRecovery(a, "macOS возобновила работу после сна")
		}
	}
}

func macOSWorkspaceResumeMessage(raw string) (string, bool) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "wake":
		return "macOS вышла из сна", true
	case "screens-wake":
		return "Дисплеи macOS вышли из сна", true
	case "session-active":
		return "Пользовательская сессия macOS снова активна", true
	default:
		return "", false
	}
}

func macOSWorkspaceHelperPath() (string, error) {
	if value := strings.TrimSpace(os.Getenv("LINKVIDEO_WORKSPACE_HELPER")); value != "" {
		return value, nil
	}
	exe, err := os.Executable()
	if err == nil && exe != "" {
		base := filepath.Dir(exe)
		candidates := []string{
			filepath.Clean(filepath.Join(base, "..", "Resources", "linkvideo-workspace-helper")),
			filepath.Join(base, "linkvideo-workspace-helper"),
		}
		for _, candidate := range candidates {
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}
	if path, lookErr := exec.LookPath("linkvideo-workspace-helper"); lookErr == nil {
		return path, nil
	}
	return "", errors.New("не найден helper системных событий macOS")
}

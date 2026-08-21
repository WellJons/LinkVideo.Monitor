//go:build darwin

package main

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type macOSHotkeyConfig struct {
	enabled bool
	toggle  string
	ptt     string
}

func startMicrophoneHotkeys(a *app) {
	go runMacOSMicrophoneHotkeys(a)
}

func runMacOSMicrophoneHotkeys(a *app) {
	var lastPathError string
	for {
		cfg := macOSCurrentHotkeyConfig(a)
		if !cfg.enabled || (cfg.toggle == "" && cfg.ptt == "") {
			a.applyMicrophoneHotkey(false, false)
			time.Sleep(750 * time.Millisecond)
			continue
		}

		helper, err := macOSHotkeyHelperPath()
		if err != nil {
			if err.Error() != lastPathError {
				a.appendLog("Горячие клавиши микрофона macOS: " + err.Error())
				lastPathError = err.Error()
			}
			time.Sleep(2 * time.Second)
			continue
		}
		lastPathError = ""

		cmd := exec.Command(helper, "--toggle", cfg.toggle, "--ptt", cfg.ptt)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			a.appendLog("Горячие клавиши микрофона macOS: " + err.Error())
			time.Sleep(time.Second)
			continue
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			a.appendLog("Горячие клавиши микрофона macOS: " + err.Error())
			time.Sleep(time.Second)
			continue
		}
		if err := cmd.Start(); err != nil {
			a.appendLog("Горячие клавиши микрофона macOS: " + err.Error())
			time.Sleep(time.Second)
			continue
		}

		done := make(chan struct{})
		go func(expected macOSHotkeyConfig) {
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					if macOSCurrentHotkeyConfig(a) != expected {
						if cmd.Process != nil {
							_ = cmd.Process.Kill()
						}
						return
					}
				}
			}
		}(cfg)

		go func() {
			s := bufio.NewScanner(stderr)
			for s.Scan() {
				line := strings.TrimSpace(s.Text())
				if line != "" {
					a.appendLog("hotkeys: " + line)
				}
			}
		}()

		pttActive := false
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			switch strings.TrimSpace(scanner.Text()) {
			case "toggle":
				a.applyMicrophoneHotkey(true, pttActive)
			case "ptt-down":
				pttActive = true
				a.applyMicrophoneHotkey(false, true)
			case "ptt-up":
				pttActive = false
				a.applyMicrophoneHotkey(false, false)
			}
		}
		_ = cmd.Wait()
		close(done)
		if pttActive {
			a.applyMicrophoneHotkey(false, false)
		}
		if macOSCurrentHotkeyConfig(a) == cfg {
			time.Sleep(time.Second)
		}
	}
}

func macOSCurrentHotkeyConfig(a *app) macOSHotkeyConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	return macOSHotkeyConfig{
		enabled: a.cfg.MicrophoneEnabled,
		toggle:  strings.TrimSpace(a.cfg.MicrophoneToggleHotkey),
		ptt:     strings.TrimSpace(a.cfg.MicrophonePTTHotkey),
	}
}

func (a *app) applyMicrophoneHotkey(toggle, ptt bool) {
	var message string
	a.mu.Lock()
	if toggle {
		a.microphoneMuted = !a.microphoneMuted
		if a.microphoneMuted {
			message = "Микрофон выключен горячей клавишей"
		} else {
			message = "Микрофон включён горячей клавишей"
		}
	}
	if a.cfg.MicrophoneMode == "push_to_talk" {
		a.microphonePTTActive = ptt
	} else {
		a.microphonePTTActive = false
	}
	a.mu.Unlock()
	if message != "" {
		a.appendLog(message)
	}
}

func macOSHotkeyHelperPath() (string, error) {
	if value := strings.TrimSpace(os.Getenv("LINKVIDEO_HOTKEY_HELPER")); value != "" {
		return value, nil
	}
	if exe, err := os.Executable(); err == nil && exe != "" {
		base := filepath.Dir(exe)
		for _, candidate := range []string{
			filepath.Clean(filepath.Join(base, "..", "Resources", "linkvideo-hotkey-helper")),
			filepath.Join(base, "linkvideo-hotkey-helper"),
		} {
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}
	if path, err := exec.LookPath("linkvideo-hotkey-helper"); err == nil {
		return path, nil
	}
	return "", errors.New("не найден helper глобальных горячих клавиш macOS")
}

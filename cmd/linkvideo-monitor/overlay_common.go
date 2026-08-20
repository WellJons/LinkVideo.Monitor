package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

type Region struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type OverlayPosition struct {
	X int `json:"x"`
	Y int `json:"y"`
}

var overlayLifecycleMu sync.Mutex
var overlayPlacementMu sync.Mutex

func (a *app) stopOverlayUnlocked() {
	a.mu.Lock()
	old := a.overlayCmd
	done := a.overlayDone
	a.overlayCmd = nil
	a.overlayDone = nil
	a.mu.Unlock()
	if old != nil && old.Process != nil {
		_ = old.Process.Kill()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(1200 * time.Millisecond):
		}
	}
}

func (a *app) stopOverlay() {
	overlayLifecycleMu.Lock()
	defer overlayLifecycleMu.Unlock()
	a.stopOverlayUnlocked()
}

func (a *app) setOverlayStatus(active bool, _ string) {
	overlayLifecycleMu.Lock()
	defer overlayLifecycleMu.Unlock()

	a.stopOverlayUnlocked()
	a.mu.Lock()
	enabled := a.cfg.OverlayEnabled
	cfg := a.cfg
	x, y := cfg.OverlayX, cfg.OverlayY
	started := a.startedAt
	a.mu.Unlock()
	if !enabled || !active {
		return
	}
	if started.IsZero() {
		started = time.Now()
	}
	// A saved overlay position may belong to another display. When a single
	// monitor is captured, keep the indicator on that monitor automatically.
	if plan, err := resolveCapturePlan(cfg); err == nil && plan.Mode == "monitor" {
		x, y = overlayPositionForCaptureMonitor(x, y, plan.X, plan.Y, plan.Width, plan.Height, 214, 36)
	}
	exe, err := helperExecutable("LinkVideo.ScreenOverlay.exe")
	if err != nil {
		return
	}
	cmd := exec.Command(exe, "--overlay", strconv.FormatInt(started.Unix(), 10), strconv.Itoa(x), strconv.Itoa(y))
	hideChildWindow(cmd)
	if err := cmd.Start(); err != nil {
		a.appendLog("Индикатор записи: " + err.Error())
		a.scheduleOverlayRetry()
		return
	}
	done := make(chan struct{})
	a.mu.Lock()
	a.overlayCmd = cmd
	a.overlayDone = done
	a.mu.Unlock()
	go func() {
		_ = cmd.Wait()
		close(done)
		restart := false
		a.mu.Lock()
		if a.overlayCmd == cmd {
			a.overlayCmd = nil
			a.overlayDone = nil
			restart = a.desired && a.running && a.cfg.OverlayEnabled
		}
		a.mu.Unlock()
		if restart {
			a.appendLog("Индикатор записи завершился; выполняется автоматический перезапуск")
			a.scheduleOverlayRetry()
		}
	}()
}

func (a *app) scheduleOverlayRetry() {
	a.mu.Lock()
	if a.overlayRetryPending || !a.desired || !a.running || !a.cfg.OverlayEnabled {
		a.mu.Unlock()
		return
	}
	a.overlayRetryPending = true
	a.mu.Unlock()

	go func() {
		time.Sleep(2 * time.Second)
		a.mu.Lock()
		a.overlayRetryPending = false
		shouldStart := a.desired && a.running && a.cfg.OverlayEnabled && a.overlayCmd == nil
		a.mu.Unlock()
		if shouldStart {
			a.setOverlayStatus(true, "")
		}
	}()
}

func printRegionJSON(r Region) error                   { return json.NewEncoder(os.Stdout).Encode(r) }
func printOverlayPositionJSON(p OverlayPosition) error { return json.NewEncoder(os.Stdout).Encode(p) }

// overlayPositionInsideWorkArea keeps the recording indicator inside the
// usable monitor area, so it cannot cover the taskbar, notification area or
// clock. Work-area coordinates are absolute virtual-screen coordinates.
func overlayPositionInsideWorkArea(x, y, width, height, left, top, right, bottom, margin int) (int, int) {
	if width < 1 || height < 1 || right <= left || bottom <= top {
		return x, y
	}
	if margin < 0 {
		margin = 0
	}
	minX := left + margin
	minY := top + margin
	maxX := right - width - margin
	maxY := bottom - height - margin
	if maxX < minX {
		minX = left
		maxX = right - width
	}
	if maxY < minY {
		minY = top
		maxY = bottom - height
	}
	if x < minX {
		x = minX
	}
	if x > maxX {
		x = maxX
	}
	if y < minY {
		y = minY
	}
	if y > maxY {
		y = maxY
	}
	return x, y
}

// overlayPositionForCaptureMonitor preserves a user-selected position only when
// the centre of the indicator is inside the captured monitor. Otherwise it
// moves the indicator to the bottom-right of that monitor. The Windows helper
// performs the final work-area clamp so the taskbar and clock stay uncovered.
func overlayPositionForCaptureMonitor(x, y, monitorX, monitorY, monitorWidth, monitorHeight, overlayWidth, overlayHeight int) (int, int) {
	if monitorWidth < 1 || monitorHeight < 1 || overlayWidth < 1 || overlayHeight < 1 {
		return x, y
	}
	cx := x + overlayWidth/2
	cy := y + overlayHeight/2
	inside := !(x == -1 && y == -1)
	inside = inside && cx >= monitorX && cx < monitorX+monitorWidth && cy >= monitorY && cy < monitorY+monitorHeight
	if inside {
		return x, y
	}
	return monitorX + monitorWidth - overlayWidth - 16, monitorY + monitorHeight - overlayHeight - 16
}

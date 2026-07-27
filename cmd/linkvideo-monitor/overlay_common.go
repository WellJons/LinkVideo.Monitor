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
	x, y := a.cfg.OverlayX, a.cfg.OverlayY
	started := a.startedAt
	a.mu.Unlock()
	if !enabled || !active {
		return
	}
	if started.IsZero() {
		started = time.Now()
	}
	exe, err := helperExecutable("LinkVideo.ScreenOverlay.exe")
	if err != nil {
		return
	}
	cmd := exec.Command(exe, "--overlay", strconv.FormatInt(started.Unix(), 10), strconv.Itoa(x), strconv.Itoa(y))
	hideChildWindow(cmd)
	if err := cmd.Start(); err != nil {
		a.appendLog("Индикатор записи: " + err.Error())
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
		a.mu.Lock()
		if a.overlayCmd == cmd {
			a.overlayCmd = nil
			a.overlayDone = nil
		}
		a.mu.Unlock()
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

//go:build darwin

package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
)

type macOSDisplayInfo struct {
	ID           uint32  `json:"id"`
	Name         string  `json:"name"`
	X            float64 `json:"x"`
	Y            float64 `json:"y"`
	WidthPoints  int     `json:"width_points"`
	HeightPoints int     `json:"height_points"`
	WidthPixels  int     `json:"width_pixels"`
	HeightPixels int     `json:"height_pixels"`
	Primary      bool    `json:"primary"`
}

func listMonitors() ([]Monitor, error) {
	helper, err := macOSCaptureHelperPath()
	if err != nil {
		return nil, err
	}
	out, err := exec.Command(helper, "--list-displays").Output()
	if err != nil {
		return nil, fmt.Errorf("не удалось получить список дисплеев macOS: %w", err)
	}
	var displays []macOSDisplayInfo
	if err := json.Unmarshal(out, &displays); err != nil {
		return nil, fmt.Errorf("не удалось разобрать список дисплеев macOS: %w", err)
	}
	if len(displays) == 0 {
		return nil, fmt.Errorf("macOS не вернула доступные дисплеи")
	}

	monitors := make([]Monitor, 0, len(displays))
	for i, display := range displays {
		width, height := display.WidthPixels, display.HeightPixels
		if width < 2 || height < 2 {
			width, height = display.WidthPoints, display.HeightPoints
		}
		name := display.Name
		if name == "" {
			name = fmt.Sprintf("Дисплей %d", i+1)
		}
		monitors = append(monitors, Monitor{
			Index:         i,
			DisplayNumber: i + 1,
			Name:          name,
			DeviceID:      strconv.FormatUint(uint64(display.ID), 10),
			X:             int(display.X),
			Y:             int(display.Y),
			Width:         even(width),
			Height:        even(height),
			Primary:       display.Primary,
			OutputIndex:   i,
		})
	}
	return monitors, nil
}

func macOSDisplayIDForCaptureRect(x, y, width, height int) uint32 {
	monitors, err := listMonitors()
	if err != nil {
		return 0
	}
	for _, monitor := range monitors {
		if monitor.X == x && monitor.Y == y && monitor.Width == even(width) && monitor.Height == even(height) {
			id, err := strconv.ParseUint(monitor.DeviceID, 10, 32)
			if err == nil {
				return uint32(id)
			}
		}
	}
	return 0
}

func hideChildWindow(cmd *exec.Cmd) {}

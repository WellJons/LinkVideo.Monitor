//go:build !windows && !darwin

package main

import "errors"

func lowerProcessPriority(pid int) {}
func listWindows() ([]WindowInfo, error) {
	return nil, errors.New("выбор окна поддерживается только в Windows")
}
func selectRegionInteractive() (Region, error) {
	return Region{}, errors.New("выбор области мышью поддерживается только в Windows")
}
func runOverlay(startedUnix int64, x, y int) error {
	return errors.New("индикатор поддерживается только в Windows")
}
func placeOverlayInteractive(x, y int) (OverlayPosition, error) {
	return OverlayPosition{}, errors.New("перемещение индикатора поддерживается только в Windows")
}
func runUninstaller()                  {}
func runUninstallWorker(parentPID int) {}

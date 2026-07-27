//go:build !windows

package main

import "errors"

func captureMonitorPNG(index int) ([]byte, error) {
	return nil, errors.New("предпросмотр монитора поддерживается только в Windows")
}

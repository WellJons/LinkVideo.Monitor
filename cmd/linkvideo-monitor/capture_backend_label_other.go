//go:build !windows && !darwin

package main

func compatibleCaptureBackendLabel() string {
	return "Совместимый захват экрана"
}

//go:build !windows

package main

import "errors"

func newSecureDesktopBridge(_ capturePlan, _ Config) (secureDesktopBridge, error) {
	return nil, errors.New("secure desktop capture is only available on Windows")
}

func runSecureGDICapture(_ []string) error {
	return errors.New("secure desktop capture is only available on Windows")
}
func runUACService() error   { return errors.New("Windows service mode is unavailable") }
func isWindowsService() bool { return false }

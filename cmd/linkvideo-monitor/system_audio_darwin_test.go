//go:build darwin

package main

import "testing"

func TestMacOSSystemAudioUsesScreenCaptureKitHelper(t *testing.T) {
	args := macOSSystemAudioHelperArgs()
	if len(args) != 1 || args[0] != "--capture-audio" {
		t.Fatalf("unexpected macOS system audio helper args: %#v", args)
	}
}

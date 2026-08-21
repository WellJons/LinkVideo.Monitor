//go:build darwin

package main

import (
	"reflect"
	"testing"
)

func TestMacOSOverlayCommandContract(t *testing.T) {
	t.Setenv("LINKVIDEO_OVERLAY_HELPER", "/tmp/linkvideo-overlay-helper")

	cmd, err := recordingOverlayCommand(123, 10, 20)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/tmp/linkvideo-overlay-helper", "--overlay", "--started", "123", "--x", "10", "--y", "20"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("recording overlay args=%v want %v", cmd.Args, want)
	}

	cmd, err = overlayPlacementCommand(-1, -1)
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"/tmp/linkvideo-overlay-helper", "--place-overlay", "--x", "-1", "--y", "-1"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("placement args=%v want %v", cmd.Args, want)
	}
}

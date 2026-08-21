//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMacOSHotkeyHelperPathUsesEnvironmentOverride(t *testing.T) {
	old := os.Getenv("LINKVIDEO_HOTKEY_HELPER")
	t.Cleanup(func() { _ = os.Setenv("LINKVIDEO_HOTKEY_HELPER", old) })
	want := filepath.Join(t.TempDir(), "custom-hotkey-helper")
	if err := os.Setenv("LINKVIDEO_HOTKEY_HELPER", want); err != nil {
		t.Fatal(err)
	}
	got, err := macOSHotkeyHelperPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("helper path=%q want %q", got, want)
	}
}

func TestMacOSCurrentHotkeyConfigReadsExistingSettings(t *testing.T) {
	a := &app{cfg: defaultConfig()}
	a.cfg.MicrophoneEnabled = true
	a.cfg.MicrophoneToggleHotkey = " Ctrl+Alt+M "
	a.cfg.MicrophonePTTHotkey = " Ctrl+Alt+Space "
	got := macOSCurrentHotkeyConfig(a)
	if !got.enabled || got.toggle != "Ctrl+Alt+M" || got.ptt != "Ctrl+Alt+Space" {
		t.Fatalf("unexpected hotkey config: %+v", got)
	}
}

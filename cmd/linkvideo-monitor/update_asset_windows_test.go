//go:build windows

package main

import "testing"

func TestWindowsAutomaticUpdateAssetNameRemainsSetupEXE(t *testing.T) {
	cases := map[string]string{
		"0.8.13":         "linkvideo.monitor_0.8.13_setup.exe",
		"v0.8.13-beta":   "linkvideo.monitor_0.8.13_setup.exe",
		"1.0.0+build.42": "linkvideo.monitor_1.0.0_setup.exe",
	}
	for version, want := range cases {
		if got := expectedAutomaticUpdateAssetName(version); got != want {
			t.Fatalf("asset(%q)=%q want %q", version, got, want)
		}
	}
}

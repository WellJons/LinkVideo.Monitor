//go:build darwin

package main

import "testing"

func TestAutomaticUpdateInstallSupportedOnDarwin(t *testing.T) {
	if !automaticUpdateInstallSupported() {
		t.Fatal("macOS signed PKG updater must report automatic install support")
	}
}

func TestDarwinAutomaticUpdateAssetName(t *testing.T) {
	cases := map[string]string{
		"0.1.2":       "linkvideo.monitor_macos_0.1.2.pkg",
		"v0.1.2-beta": "linkvideo.monitor_macos_0.1.2-beta.pkg",
	}
	for version, want := range cases {
		if got := expectedAutomaticUpdateAssetName(version); got != want {
			t.Fatalf("asset(%q)=%q want %q", version, got, want)
		}
	}
}

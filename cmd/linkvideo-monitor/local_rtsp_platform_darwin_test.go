//go:build darwin

package main

import "testing"

func TestDarwinManagedMediaMTXPath(t *testing.T) {
	for _, value := range []string{"mediamtx.exe", "mediamtx", ""} {
		if !isManagedMediaMTXPath(value) {
			t.Fatalf("expected managed MediaMTX path for %q", value)
		}
	}
	if isManagedMediaMTXPath("/tmp/custom-mediamtx") {
		t.Fatal("custom absolute path must not be treated as managed")
	}
}

func TestDarwinMediaMTXDefaultPathUsesAppMacOSDirectory(t *testing.T) {
	got := mediaMTXDefaultPathForExecutable("/Applications/LinkVideo.Monitor.app/Contents/MacOS/LinkVideo.Monitor")
	want := "/Applications/LinkVideo.Monitor.app/Contents/MacOS/mediamtx"
	if got != want {
		t.Fatalf("default MediaMTX path=%q want %q", got, want)
	}
}

func TestDarwinMediaMTXUsesModernConfig(t *testing.T) {
	release := selectedMediaMTXRelease()
	if release.Version != "1.19.3" || release.LegacyConfig {
		t.Fatalf("unexpected Darwin MediaMTX release: %+v", release)
	}
}

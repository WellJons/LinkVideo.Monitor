package main

import "testing"

func TestValidateAutomaticUpdateDownload(t *testing.T) {
	good := "https://github.com/WellJons/LinkVideo.Monitor.Updates/releases/download/v0.8.13-beta/LinkVideo.Monitor_0.8.13_Setup.exe"
	sha := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := validateAutomaticUpdateDownload(good, sha, "0.8.13-beta"); err != nil {
		t.Fatalf("official update URL rejected: %v", err)
	}
	bad := []string{
		"http://github.com/WellJons/LinkVideo.Monitor.Updates/releases/download/v0.8.13-beta/LinkVideo.Monitor_0.8.13_Setup.exe",
		"https://example.com/WellJons/LinkVideo.Monitor.Updates/releases/download/v0.8.13-beta/LinkVideo.Monitor_0.8.13_Setup.exe",
		"https://github.com/WellJons/Other/releases/download/v0.8.13-beta/LinkVideo.Monitor_0.8.13_Setup.exe",
		"https://github.com/WellJons/LinkVideo.Monitor.Updates/releases/download/v0.8.14-beta/LinkVideo.Monitor_0.8.14_Setup.exe",
		"https://github.com/WellJons/LinkVideo.Monitor.Updates/releases/download/v0.8.13-beta/not-an-installer.zip",
	}
	for _, raw := range bad {
		if validateAutomaticUpdateDownload(raw, sha, "0.8.13-beta") == nil {
			t.Fatalf("unsafe or mismatched URL accepted: %s", raw)
		}
	}
	if validateAutomaticUpdateDownload(good, "bad", "0.8.13-beta") == nil {
		t.Fatal("invalid SHA-256 was accepted")
	}
}

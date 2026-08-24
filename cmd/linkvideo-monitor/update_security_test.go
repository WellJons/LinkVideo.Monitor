package main

import "testing"

func TestValidateAutomaticUpdateDownloadForAsset(t *testing.T) {
	expected := "LinkVideo.Monitor_0.8.13_Setup.exe"
	good := "https://github.com/WellJons/LinkVideo.Monitor.Updates/releases/download/v0.8.13-beta/" + expected
	sha := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := validateAutomaticUpdateDownloadForAsset(good, sha, "0.8.13-beta", expected); err != nil {
		t.Fatalf("official update URL rejected: %v", err)
	}
	bad := []string{
		"http://github.com/WellJons/LinkVideo.Monitor.Updates/releases/download/v0.8.13-beta/" + expected,
		"https://example.com/WellJons/LinkVideo.Monitor.Updates/releases/download/v0.8.13-beta/" + expected,
		"https://github.com/WellJons/Other/releases/download/v0.8.13-beta/" + expected,
		"https://github.com/WellJons/LinkVideo.Monitor.Updates/releases/download/v0.8.14-beta/LinkVideo.Monitor_0.8.14_Setup.exe",
		"https://github.com/WellJons/LinkVideo.Monitor.Updates/releases/download/v0.8.13-beta/LinkVideo.Monitor_0.8.12_Setup.exe",
		"https://github.com/WellJons/LinkVideo.Monitor.Updates/releases/download/v0.8.13-beta/not-an-installer.zip",
	}
	for _, raw := range bad {
		if validateAutomaticUpdateDownloadForAsset(raw, sha, "0.8.13-beta", expected) == nil {
			t.Fatalf("unsafe or mismatched URL accepted: %s", raw)
		}
	}
	if validateAutomaticUpdateDownloadForAsset(good, "bad", "0.8.13-beta", expected) == nil {
		t.Fatal("invalid SHA-256 was accepted")
	}
}

func TestUpdateAssetVersionBase(t *testing.T) {
	cases := map[string]string{
		"v0.8.13-beta":   "0.8.13",
		"0.8.12.1":       "0.8.12.1",
		"1.0.0+build.42": "1.0.0",
	}
	for input, want := range cases {
		if got := updateAssetVersionBase(input); got != want {
			t.Fatalf("updateAssetVersionBase(%q)=%q want %q", input, got, want)
		}
	}
}

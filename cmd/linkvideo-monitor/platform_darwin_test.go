//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMacOSUninstallerCandidatesUseBundleParent(t *testing.T) {
	exe := filepath.Join(string(filepath.Separator), "Applications", "LinkVideo.Monitor.app", "Contents", "MacOS", "LinkVideo.Monitor")
	got := macOSUninstallerCandidates(exe)
	if len(got) != 1 {
		t.Fatalf("expected one deduplicated installed candidate, got %#v", got)
	}
	want := filepath.Join(string(filepath.Separator), "Applications", macOSUninstallerName)
	if filepath.Clean(got[0]) != filepath.Clean(want) {
		t.Fatalf("candidate=%q want %q", got[0], want)
	}
}

func TestMacOSUninstallerCandidatesSupportDMGLayout(t *testing.T) {
	exe := filepath.Join(string(filepath.Separator), "Volumes", "LinkVideo Monitor", "LinkVideo.Monitor.app", "Contents", "MacOS", "LinkVideo.Monitor")
	got := macOSUninstallerCandidates(exe)
	if len(got) != 2 {
		t.Fatalf("expected DMG and /Applications candidates, got %#v", got)
	}
	want := filepath.Join(string(filepath.Separator), "Volumes", "LinkVideo Monitor", macOSUninstallerName)
	if filepath.Clean(got[0]) != filepath.Clean(want) {
		t.Fatalf("first candidate=%q want %q", got[0], want)
	}
}

func TestMacOSUninstallerPathEnvironmentOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), macOSUninstallerName)
	if err := os.WriteFile(path, []byte("#!/bin/bash\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LINKVIDEO_UNINSTALLER", path)
	got, err := macOSUninstallerPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("path=%q want %q", got, path)
	}
}

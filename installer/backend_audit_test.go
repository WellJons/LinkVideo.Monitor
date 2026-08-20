//go:build windows

package main

import (
	"path/filepath"
	"testing"
)

func TestPayloadTargetPathRejectsTraversal(t *testing.T) {
	dest := filepath.Join(`C:\Program Files`, "LinkVideo.Monitor")
	bad := []string{"../evil.exe", "../../evil.exe", `C:\evil.exe`, `/evil.exe`}
	for _, name := range bad {
		if _, _, err := payloadTargetPath(dest, name); err == nil {
			t.Fatalf("unsafe archive path accepted: %q", name)
		}
	}
	if _, target, err := payloadTargetPath(dest, "bin/ffmpeg.exe"); err != nil {
		t.Fatal(err)
	} else if filepath.Dir(target) != filepath.Join(dest, "bin") {
		t.Fatalf("unexpected safe target: %q", target)
	}
}

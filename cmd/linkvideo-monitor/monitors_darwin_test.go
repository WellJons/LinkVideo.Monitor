//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "linkvideo-monitor-macos-test-")
	if err != nil {
		panic(err)
	}
	helper := filepath.Join(dir, "linkvideo-capture-helper")
	content := `#!/bin/sh
if [ "$1" = "--list-displays" ]; then
  printf '%s' '[{"id":1,"name":"Test Display","x":0,"y":0,"width_points":1920,"height_points":1080,"width_pixels":1920,"height_pixels":1080,"primary":true}]'
  exit 0
fi
exit 0
`
	if err := os.WriteFile(helper, []byte(content), 0o700); err != nil {
		_ = os.RemoveAll(dir)
		panic(err)
	}
	previous, hadPrevious := os.LookupEnv("LINKVIDEO_CAPTURE_HELPER")
	_ = os.Setenv("LINKVIDEO_CAPTURE_HELPER", helper)

	code := m.Run()
	if hadPrevious {
		_ = os.Setenv("LINKVIDEO_CAPTURE_HELPER", previous)
	} else {
		_ = os.Unsetenv("LINKVIDEO_CAPTURE_HELPER")
	}
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

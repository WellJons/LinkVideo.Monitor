package main

import (
	"strings"
	"testing"
)

func TestScreenRecordingPermissionUIIsPresent(t *testing.T) {
	for _, want := range []string{
		`id="macPermissionCard"`,
		`/api/screen-permission`,
		`Разрешить запись экрана`,
		`Запись экрана и системного аудио`,
	} {
		if !strings.Contains(indexHTML, want) {
			t.Fatalf("screen permission UI is missing %q", want)
		}
	}
}

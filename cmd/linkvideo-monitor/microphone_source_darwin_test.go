//go:build darwin

package main

import (
	"strings"
	"testing"
)

func TestMacOSMicrophoneCaptureArgs(t *testing.T) {
	args := macOSMicrophoneCaptureArgs("MacBook Pro Microphone")
	joined := " " + strings.Join(args, " ") + " "
	for _, want := range []string{
		" --capture-microphone ",
		" --device MacBook Pro Microphone ",
		" --sample-rate 48000 ",
		" --channels 2 ",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("macOS microphone helper args missing %q: %s", want, joined)
		}
	}
}

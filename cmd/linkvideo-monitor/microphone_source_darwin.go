//go:build darwin

package main

import (
	"context"
	"fmt"
	"os/exec"
)

func microphoneCaptureCommand(ctx context.Context, cfg Config, device string) (*exec.Cmd, error) {
	helper, err := macOSCaptureHelperPath()
	if err != nil {
		return nil, err
	}
	args := macOSMicrophoneCaptureArgs(device)
	return exec.CommandContext(ctx, helper, args...), nil
}

func macOSMicrophoneCaptureArgs(device string) []string {
	return []string{
		"--capture-microphone",
		"--device", device,
		"--sample-rate", fmt.Sprint(microphoneSampleRate),
		"--channels", fmt.Sprint(microphoneChannels),
	}
}

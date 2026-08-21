//go:build windows

package main

import (
	"context"
	"fmt"
	"os/exec"
)

func microphoneCaptureCommand(ctx context.Context, cfg Config, device string) (*exec.Cmd, error) {
	args := []string{
		"-hide_banner", "-loglevel", "warning",
		"-f", "dshow", "-audio_buffer_size", "50",
		"-i", "audio=" + device,
		"-ac", fmt.Sprint(microphoneChannels), "-ar", fmt.Sprint(microphoneSampleRate),
		"-f", "s16le", "pipe:1",
	}
	cmd := exec.CommandContext(ctx, resolveExecutable(cfg.FFmpegPath), args...)
	hideChildWindow(cmd)
	return cmd, nil
}

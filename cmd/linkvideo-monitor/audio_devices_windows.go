//go:build windows

package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var dshowAudioLine = regexp.MustCompile(`(?m)^.*?"([^"]+)"\s*\(audio\)\s*$`)

func listAudioDevices(ffmpegPath string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, resolveExecutable(ffmpegPath), "-hide_banner", "-list_devices", "true", "-f", "dshow", "-i", "dummy")
	hideChildWindow(cmd)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	_ = cmd.Run() // FFmpeg normally returns a non-zero code after listing devices.
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("FFmpeg слишком долго получал список аудиоустройств")
	}

	seen := map[string]bool{}
	var result []string
	for _, m := range dshowAudioLine.FindAllStringSubmatch(out.String(), -1) {
		name := strings.TrimSpace(m[1])
		if name != "" && !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("аудиоустройства DirectShow не найдены. Проверьте сборку FFmpeg и включите микрофон или «Стерео микшер»")
	}
	return result, nil
}

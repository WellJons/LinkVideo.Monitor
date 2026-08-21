//go:build darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func listAudioDevices(_ string) ([]string, error) {
	helper, err := macOSCaptureHelperPath()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, helper, "--list-microphones")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("macOS слишком долго получала список микрофонов")
	}
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return nil, fmt.Errorf("не удалось получить список микрофонов: %s", detail)
		}
		return nil, fmt.Errorf("не удалось получить список микрофонов: %w", err)
	}

	var devices []string
	if err := json.Unmarshal(out, &devices); err != nil {
		return nil, fmt.Errorf("неверный ответ macOS helper со списком микрофонов: %w", err)
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("микрофоны не найдены")
	}
	return devices, nil
}

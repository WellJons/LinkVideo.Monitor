//go:build !windows && !darwin

package main

import (
	"context"
	"fmt"
	"os/exec"
)

func microphoneCaptureCommand(ctx context.Context, cfg Config, device string) (*exec.Cmd, error) {
	return nil, fmt.Errorf("захват микрофона не поддерживается на этой платформе")
}

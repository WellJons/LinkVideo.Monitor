package main

import (
	"fmt"
	"strings"
	"time"
)

func transportReconnectDelay(consecutive int) time.Duration {
	switch {
	case consecutive <= 1:
		return 700 * time.Millisecond
	case consecutive == 2:
		return 1500 * time.Millisecond
	case consecutive == 3:
		return 3 * time.Second
	default:
		return 5 * time.Second
	}
}

func transportInterruptedReason(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "rtmp":
		return "Сеанс RTMP завершился; выполняется автоматическое переподключение"
	default:
		return "Сеанс RTSP завершился; выполняется автоматическое переподключение"
	}
}

func reconnectTelemetryLine(encoder string, targetFPS int, streamDuration time.Duration, fps, speed float64, dup, drop int, retry time.Duration) string {
	return fmt.Sprintf(
		"Диагностика перед переподключением: encoder=%s, работа=%s, цель=%d FPS, фактически=%.2f FPS, speed=%.2fx, dup=%d, drop=%d, повтор через %s",
		encoderLabel(encoder), streamDuration.Round(time.Second), targetFPS, fps, speed, dup, drop, retry,
	)
}

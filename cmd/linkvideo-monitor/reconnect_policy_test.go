package main

import (
	"strings"
	"testing"
	"time"
)

func TestTransportReconnectDelayBackoff(t *testing.T) {
	cases := []struct {
		n    int
		want time.Duration
	}{
		{1, 700 * time.Millisecond},
		{2, 1500 * time.Millisecond},
		{3, 3 * time.Second},
		{4, 5 * time.Second},
		{9, 5 * time.Second},
	}
	for _, tc := range cases {
		if got := transportReconnectDelay(tc.n); got != tc.want {
			t.Fatalf("attempt %d: got %s want %s", tc.n, got, tc.want)
		}
	}
}

func TestTransportInterruptedReasonDoesNotBlameServer(t *testing.T) {
	for _, protocol := range []string{"rtsp", "rtmp"} {
		got := strings.ToLower(transportInterruptedReason(protocol))
		if strings.Contains(got, "сервер") || strings.Contains(got, "server") {
			t.Fatalf("reason must stay neutral: %q", got)
		}
		if !strings.Contains(got, protocol) {
			t.Fatalf("reason %q does not mention protocol %s", got, protocol)
		}
	}
}

func TestReconnectTelemetryContainsRuntimeMetrics(t *testing.T) {
	got := reconnectTelemetryLine("libx264", 15, 42*time.Second, 14.8, 1.03, 2, 1, 700*time.Millisecond)
	for _, want := range []string{"14.80 FPS", "speed=1.03x", "dup=2", "drop=1", "700ms"} {
		if !strings.Contains(got, want) {
			t.Fatalf("telemetry missing %q: %s", want, got)
		}
	}
}

func TestFFmpegTransportFailureReasonDoesNotInventServerCause(t *testing.T) {
	got := ffmpegTransportFailureReason("error submitting a packet to the muxer: broken pipe")
	lower := strings.ToLower(got)
	if got == "" || !strings.Contains(lower, "broken pipe") {
		t.Fatalf("expected concrete EPIPE reason, got %q", got)
	}
	if strings.Contains(lower, "сервер закрыл") || strings.Contains(lower, "server closed") {
		t.Fatalf("reason must not claim an unproven server-side close: %q", got)
	}
	if !isTransportFailureReason(got) {
		t.Fatalf("reason must be classified as transport failure: %q", got)
	}
}

func TestFFmpegTransportFailureReasonClassifiesCommonNetworkErrors(t *testing.T) {
	for _, line := range []string{
		"connection reset by peer",
		"connection timed out",
		"connection refused",
		"failed reading rtsp data",
	} {
		if got := ffmpegTransportFailureReason(line); got == "" {
			t.Fatalf("expected classification for %q", line)
		}
	}
}

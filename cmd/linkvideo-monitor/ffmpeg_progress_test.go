package main

import (
	"strings"
	"testing"
)

func TestStructuredFFmpegProgressUpdatesRuntimeMetrics(t *testing.T) {
	a := &app{}
	for _, line := range []string{
		"frame=123",
		"fps=14.90",
		"speed=1.12x",
		"dup_frames=3",
		"drop_frames=2",
		"progress=continue",
	} {
		if !a.processFFmpegLine("ffmpeg-progress", line) {
			t.Fatalf("structured progress line was not consumed: %q", line)
		}
	}
	if !a.encoderStartupConfirmed {
		t.Fatal("structured progress did not confirm encoder startup")
	}
	if a.videoFPS != 14.90 || a.videoSpeed != 1.12 || a.videoDup != 3 || a.videoDrop != 2 {
		t.Fatalf("unexpected progress metrics: fps=%v speed=%v dup=%d drop=%d", a.videoFPS, a.videoSpeed, a.videoDup, a.videoDrop)
	}
}

func TestEncoderUsesMachineReadableProgressChannel(t *testing.T) {
	cfg := defaultConfig()
	cfg.OutputMode = "local"
	plan := capturePlan{Mode: "monitor", Width: 1280, Height: 720, OutputWidth: 1280, OutputHeight: 720}
	_, args, _, err := buildEncoderFFmpegDetailed(cfg, plan, "libx264", "", "")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"-nostats", "-stats_period 1", "-progress pipe:1"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in ffmpeg args: %s", want, joined)
		}
	}
}

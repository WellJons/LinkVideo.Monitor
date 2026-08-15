package main

import "testing"

func TestRuntimePerformanceCandidatesDoNotChangeUIConfig(t *testing.T) {
	cfg := defaultConfig()
	cfg.FPS = 15
	cfg.Preset = "veryfast"
	items := runtimePerformanceCandidates(cfg, "libx264")
	want := []runtimePerformanceCandidate{{15, "veryfast"}, {15, "superfast"}, {15, "ultrafast"}, {12, "ultrafast"}, {10, "ultrafast"}}
	if len(items) != len(want) {
		t.Fatalf("candidate count=%d want=%d: %#v", len(items), len(want), items)
	}
	for i := range want {
		if items[i] != want[i] {
			t.Fatalf("candidate[%d]=%#v want=%#v", i, items[i], want[i])
		}
	}
	if cfg.FPS != 15 || cfg.Preset != "veryfast" {
		t.Fatalf("candidate generation mutated user config: %+v", cfg)
	}
}
func TestRuntimePerformanceCandidatesPreserveFasterPreset(t *testing.T) {
	cfg := defaultConfig()
	cfg.FPS = 30
	cfg.Preset = "ultrafast"
	items := runtimePerformanceCandidates(cfg, "libx264")
	want := []runtimePerformanceCandidate{{30, "ultrafast"}, {15, "ultrafast"}, {12, "ultrafast"}, {10, "ultrafast"}}
	if len(items) != len(want) {
		t.Fatalf("candidate count=%d want=%d: %#v", len(items), len(want), items)
	}
	for i := range want {
		if items[i] != want[i] {
			t.Fatalf("candidate[%d]=%#v want=%#v", i, items[i], want[i])
		}
	}
}
func TestRuntimePerformanceCandidatesSkipHardware(t *testing.T) {
	if items := runtimePerformanceCandidates(defaultConfig(), "h264_qsv"); len(items) != 0 {
		t.Fatalf("hardware encoder should not use CPU fallback ladder: %#v", items)
	}
}
func TestHiddenFallbackFPSKeepsTwoSecondGOP(t *testing.T) {
	cfg := defaultConfig()
	cfg.OutputMode = "local"
	cfg.FPS = 12
	cfg.Preset = "ultrafast"
	plan := capturePlan{Mode: "monitor", Width: 1280, Height: 720, OutputWidth: 1280, OutputHeight: 720}
	_, args, _, err := buildEncoderFFmpegDetailed(cfg, plan, "libx264", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-g" {
			if args[i+1] != "24" {
				t.Fatalf("GOP=%s want=24 for 12 FPS", args[i+1])
			}
			return
		}
	}
	t.Fatal("-g was not generated")
}

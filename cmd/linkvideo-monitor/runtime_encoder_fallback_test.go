package main

import "testing"

func TestRuntimePresetCandidatesUseFasterPresetsAtSameFPS(t *testing.T) {
	cfg := defaultConfig()
	cfg.Preset = "veryfast"
	got := runtimePresetCandidates(cfg, 15)
	want := []runtimePerformanceCandidate{{15, "veryfast"}, {15, "superfast"}, {15, "ultrafast"}}
	if len(got) != len(want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("candidate[%d]=%#v want %#v", i, got[i], want[i])
		}
	}
}

func TestH265FallbackPrefersMatchingH264Hardware(t *testing.T) {
	cases := map[string]string{
		"hevc_nvenc":        "h264_nvenc",
		"hevc_qsv":          "h264_qsv",
		"hevc_amf":          "h264_amf",
		"hevc_videotoolbox": "h264_videotoolbox",
		"libx265":           "",
	}
	for input, want := range cases {
		if got := h264FallbackEncoderName(input); got != want {
			t.Fatalf("%s -> %s, want %s", input, got, want)
		}
	}
}

func TestSoftwareEncoderKeepsNormalPriority(t *testing.T) {
	if !keepNormalStreamingPriority("libx264", false) {
		t.Fatal("software H.264 must keep normal Windows priority")
	}
	if !keepNormalStreamingPriority("libx265", false) {
		t.Fatal("software H.265 must keep normal Windows priority")
	}
	if !keepNormalStreamingPriority("h264_qsv", false) {
		t.Fatal("hardware H.264 streaming must keep normal Windows priority")
	}
	if !keepNormalStreamingPriority("h264_qsv", true) {
		t.Fatal("fallback/optimized hardware path should keep normal priority")
	}
}

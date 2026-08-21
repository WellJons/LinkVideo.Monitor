//go:build darwin

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFreshDarwinConfigSelectsVideoToolbox(t *testing.T) {
	cfg := defaultConfig()
	if cfg.Encoder != "libx264" {
		t.Fatalf("platform-neutral default encoder=%q want libx264", cfg.Encoder)
	}

	a := &app{cfg: cfg, cfgPath: filepath.Join(t.TempDir(), "config.json")}
	if err := a.loadConfig(); err != nil {
		t.Fatal(err)
	}
	if a.cfg.Encoder != "h264_videotoolbox" {
		t.Fatalf("fresh Darwin config encoder=%q want h264_videotoolbox", a.cfg.Encoder)
	}

	h264 := encoderCandidatesForCodec("h264")
	if len(h264) != 2 || h264[0].Name != "h264_videotoolbox" || h264[1].Name != "libx264" {
		t.Fatalf("unexpected Darwin H.264 candidates: %#v", h264)
	}
	h265 := encoderCandidatesForCodec("h265")
	if len(h265) != 2 || h265[0].Name != "hevc_videotoolbox" || h265[1].Name != "libx265" {
		t.Fatalf("unexpected Darwin H.265 candidates: %#v", h265)
	}
}

func TestVideoToolboxFFmpegArguments(t *testing.T) {
	cfg := defaultConfig()
	cfg.OutputMode = "local"
	cfg.Codec = "h264"
	cfg.Encoder = "h264_videotoolbox"
	cfg.FPS = 15
	plan := capturePlan{Mode: "monitor", Width: 1920, Height: 1080, OutputWidth: 1920, OutputHeight: 1080}

	_, args, _, err := buildEncoderFFmpegDetailed(cfg, plan, cfg.Encoder, "", "")
	if err != nil {
		t.Fatal(err)
	}
	joined := " " + strings.Join(args, " ") + " "
	for _, want := range []string{
		" -c:v h264_videotoolbox ",
		" -realtime 1 ",
		" -allow_sw 0 ",
		" -prio_speed 1 ",
		" -bf 0 ",
		" -g 30 ",
		" -keyint_min 30 ",
		" -pix_fmt nv12 ",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("VideoToolbox args missing %q: %s", want, joined)
		}
	}
}

func TestVideoToolboxHEVCFallsBackToMatchingH264Encoder(t *testing.T) {
	if got := h264FallbackEncoderName("hevc_videotoolbox"); got != "h264_videotoolbox" {
		t.Fatalf("fallback=%q want h264_videotoolbox", got)
	}
}

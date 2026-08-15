package main

import "testing"

func queueBeforeInput(args []string, input string) string {
	for i := 0; i < len(args); i++ {
		if args[i] != "-i" || i+1 >= len(args) || args[i+1] != input {
			continue
		}
		for j := i - 1; j >= 1 && j >= i-12; j-- {
			if args[j-1] == "-thread_queue_size" {
				return args[j]
			}
		}
	}
	return ""
}

func TestEncoderUsesBoundedResilienceQueues(t *testing.T) {
	cfg := defaultConfig()
	cfg.OutputMode = "local"
	cfg.AudioEnabled = true
	cfg.AudioSource = "system"
	cfg.MicrophoneEnabled = true
	cfg.MicrophoneDevice = "test microphone"
	plan := capturePlan{Mode: "monitor", Width: 1920, Height: 1080, OutputWidth: 1920, OutputHeight: 1080}
	systemURL := "tcp://127.0.0.1:12345"
	micURL := "tcp://127.0.0.1:12346"
	_, args, _, err := buildEncoderFFmpegDetailed(cfg, plan, "libx264", systemURL, micURL)
	if err != nil {
		t.Fatal(err)
	}
	if got := queueBeforeInput(args, systemURL); got != "32" {
		t.Fatalf("system audio queue=%q want 32", got)
	}
	if got := queueBeforeInput(args, micURL); got != "32" {
		t.Fatalf("microphone queue=%q want 32", got)
	}
	if got := queueBeforeInput(args, "pipe:0"); got != "4" {
		t.Fatalf("raw video queue=%q want 4", got)
	}
}

func TestAllStreamingEncodersKeepNormalPriority(t *testing.T) {
	for _, encoder := range []string{"libx264", "libx265", "h264_qsv", "hevc_qsv", "h264_nvenc", "hevc_nvenc", "h264_amf", "hevc_amf"} {
		if !keepNormalStreamingPriority(encoder, false) {
			t.Fatalf("%s unexpectedly eligible for below-normal streaming priority", encoder)
		}
	}
}

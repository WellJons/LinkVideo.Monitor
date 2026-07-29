package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type encoderOption struct {
	Name  string
	Label string
}

type encoderFailureState struct {
	Count         int
	LastFailure   time.Time
	DisabledUntil time.Time
	Reason        string
}

var encoderLabels = map[string]string{
	"h264_nvenc": "NVIDIA NVENC · H.264",
	"h264_qsv":   "Intel Quick Sync · H.264",
	"h264_amf":   "AMD AMF · H.264",
	"libx264":    "Программный H.264",
	"hevc_nvenc": "NVIDIA NVENC · H.265",
	"hevc_qsv":   "Intel Quick Sync · H.265",
	"hevc_amf":   "AMD AMF · H.265",
	"libx265":    "Программный H.265",
}

func encoderLabel(name string) string {
	if label := encoderLabels[name]; label != "" {
		return label
	}
	return name
}

func (a *app) selectVideoEncoder(cfg Config, plan capturePlan) string {
	now := time.Now()
	requested := cfg.Encoder
	if requested == "" || requested == "auto" || encoderCodec(requested) != cfg.Codec {
		requested = softwareEncoderForCodec(cfg.Codec)
	}
	software := softwareEncoderForCodec(cfg.Codec)
	if requested == software {
		a.setVideoEncoder(requested)
		return requested
	}

	a.mu.Lock()
	state := a.encoderFailures[requested]
	a.mu.Unlock()
	if state.DisabledUntil.After(now) {
		a.setVideoEncoder(software)
		a.appendLog(fmt.Sprintf("Кодировщик %s временно отключён после ошибки до %s; используется %s", encoderLabel(requested), state.DisabledUntil.Format("15:04"), encoderLabel(software)))
		return software
	}
	if err := probeVideoEncoder(cfg, requested, plan); err == nil {
		a.setVideoEncoder(requested)
		a.appendLog("Кодировщик: " + encoderLabel(requested) + " (проверен перед запуском)")
		return requested
	} else {
		a.mu.Lock()
		a.encoderFailures[requested] = encoderFailureState{Count: 1, LastFailure: now, DisabledUntil: now.Add(30 * time.Minute), Reason: err.Error()}
		a.mu.Unlock()
		a.setVideoEncoder(software)
		a.appendLog("Выбранный кодировщик " + encoderLabel(requested) + " недоступен: " + err.Error())
		a.appendLog("Для непрерывной работы используется " + encoderLabel(software))
		return software
	}
}

func automaticEncoderCandidates(codec string) []encoderOption {
	names := strings.ToLower(strings.Join(videoAdapterNames(), "\n"))
	result := make([]encoderOption, 0, 4)
	add := func(name string) {
		for _, item := range result {
			if item.Name == name {
				return
			}
		}
		result = append(result, encoderOption{Name: name, Label: encoderLabel(name)})
	}
	prefix := "h264"
	software := "libx264"
	if codec == "h265" {
		prefix = "hevc"
		software = "libx265"
	}
	if strings.Contains(names, "nvidia") {
		add(prefix + "_nvenc")
	}
	if strings.Contains(names, "intel") {
		add(prefix + "_qsv")
	}
	if strings.Contains(names, "amd") || strings.Contains(names, "radeon") || strings.Contains(names, "advanced micro devices") {
		add(prefix + "_amf")
	}
	add(software)
	return result
}

func probeVideoEncoder(cfg Config, encoder string, plan capturePlan) error {
	width, height := even(plan.OutputWidth), even(plan.OutputHeight)
	if width < 128 || height < 72 {
		width, height = 128, 72
	}
	fps := cfg.FPS
	if fps < 1 {
		fps = 15
	}
	frames := fps / 2
	if frames < 4 {
		frames = 4
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	pixFmt := "nv12"
	if !isHardwareEncoder(encoder) {
		pixFmt = "yuv420p"
	}
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", fmt.Sprintf("color=c=black:s=%dx%d:r=%d", width, height, fps),
		"-frames:v", fmt.Sprint(frames),
		"-vf", "format=" + pixFmt,
		"-c:v", encoder,
		"-b:v", fmt.Sprintf("%dk", cfg.BitrateKbps),
		"-maxrate", fmt.Sprintf("%dk", cfg.MaxrateKbps),
		"-bufsize", fmt.Sprintf("%dk", cfg.BufsizeKbps),
		"-g", fmt.Sprint(fps * 2),
		"-bf", "0",
	}
	switch encoder {
	case "h264_nvenc", "hevc_nvenc":
		args = append(args, "-preset", "p4", "-tune", "ll", "-rc", "vbr", "-multipass", "qres", "-spatial_aq", "1")
	case "h264_qsv", "hevc_qsv":
		args = append(args, "-preset", "medium", "-look_ahead", "0")
	case "h264_amf", "hevc_amf":
		args = append(args, "-usage", "lowlatency", "-quality", "balanced", "-rc", "vbr_peak")
	case "libx264":
		args = append(args, "-preset", "veryfast", "-tune", "zerolatency")
	case "libx265":
		args = append(args, "-preset", "veryfast", "-tune", "zerolatency")
	}
	args = append(args, "-f", "null", "-")
	cmd := exec.CommandContext(ctx, resolveExecutable(cfg.FFmpegPath), args...)
	hideChildWindow(cmd)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return fmt.Errorf("проверка превысила 8 секунд")
	}
	if err != nil {
		text := strings.TrimSpace(string(out))
		if len(text) > 320 {
			text = text[:320]
		}
		if text == "" {
			text = err.Error()
		}
		return fmt.Errorf("%s", text)
	}
	return nil
}

func (a *app) setVideoEncoder(name string) {
	a.mu.Lock()
	a.videoEncoder = name
	a.mu.Unlock()
}

func (a *app) setCaptureBackend(name string) {
	a.mu.Lock()
	a.captureBackend = name
	a.mu.Unlock()
}

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
	"h264_nvenc": "NVIDIA NVENC",
	"h264_qsv":   "Intel Quick Sync",
	"h264_amf":   "AMD AMF",
	"libx264":    "Программный H.264",
}

func encoderLabel(name string) string {
	if label := encoderLabels[name]; label != "" {
		return label
	}
	return name
}

func (a *app) selectVideoEncoder(cfg Config, plan capturePlan) string {
	now := time.Now()
	manual := cfg.Encoder != "" && cfg.Encoder != "auto"
	requested := cfg.Encoder
	if manual {
		if requested == "libx264" {
			a.setVideoEncoder(requested)
			return requested
		}
		a.mu.Lock()
		state := a.encoderFailures[requested]
		a.mu.Unlock()
		if state.DisabledUntil.After(now) {
			a.setVideoEncoder("libx264")
			a.appendLog(fmt.Sprintf("Кодировщик %s временно отключён после подтверждённой ошибки до %s; используется программный H.264", encoderLabel(requested), state.DisabledUntil.Format("15:04")))
			return "libx264"
		}
		if err := probeVideoEncoder(cfg, requested, plan); err == nil {
			a.setVideoEncoder(requested)
			a.appendLog("Кодировщик: " + encoderLabel(requested) + " (выбран вручную)")
			return requested
		} else {
			a.mu.Lock()
			a.encoderFailures[requested] = encoderFailureState{Count: 1, LastFailure: now, DisabledUntil: now.Add(30 * time.Minute), Reason: err.Error()}
			a.mu.Unlock()
			a.setVideoEncoder("libx264")
			a.appendLog("Выбранный кодировщик " + encoderLabel(requested) + " не прошёл проверку на текущем разрешении: " + err.Error())
			a.appendLog("Для непрерывной работы используется программный H.264")
			return "libx264"
		}
	}

	a.mu.Lock()
	cached := a.videoEncoder
	cachedState := a.encoderFailures[cached]
	a.mu.Unlock()
	if cached != "" && (cached == "libx264" || !cachedState.DisabledUntil.After(now)) {
		return cached
	}

	candidates := automaticEncoderCandidates()
	for _, candidate := range candidates {
		a.mu.Lock()
		state := a.encoderFailures[candidate.Name]
		a.mu.Unlock()
		if state.DisabledUntil.After(now) {
			continue
		}
		if candidate.Name == "libx264" {
			a.setVideoEncoder(candidate.Name)
			a.appendLog("Кодировщик: " + candidate.Label + " (автоматически)")
			return candidate.Name
		}
		if err := probeVideoEncoder(cfg, candidate.Name, plan); err == nil {
			a.mu.Lock()
			delete(a.encoderFailures, candidate.Name)
			a.mu.Unlock()
			a.setVideoEncoder(candidate.Name)
			a.appendLog("Кодировщик: " + candidate.Label + " (проверен на текущем разрешении)")
			return candidate.Name
		} else {
			a.mu.Lock()
			a.encoderFailures[candidate.Name] = encoderFailureState{Count: 1, LastFailure: now, DisabledUntil: now.Add(30 * time.Minute), Reason: err.Error()}
			a.mu.Unlock()
			a.appendLog(candidate.Label + " не прошёл проверку и не будет повторно проверяться 30 минут")
		}
	}

	a.setVideoEncoder("libx264")
	a.appendLog("Кодировщик: Программный H.264")
	return "libx264"
}

func automaticEncoderCandidates() []encoderOption {
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

	if strings.Contains(names, "nvidia") {
		add("h264_nvenc")
	}
	if strings.Contains(names, "intel") {
		add("h264_qsv")
	}
	if strings.Contains(names, "amd") || strings.Contains(names, "radeon") || strings.Contains(names, "advanced micro devices") {
		add("h264_amf")
	}
	if len(result) == 0 {
		add("h264_nvenc")
		add("h264_qsv")
		add("h264_amf")
	}
	add("libx264")
	return result
}

func probeVideoEncoder(cfg Config, encoder string, plan capturePlan) error {
	if encoder == "libx264" {
		return nil
	}
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
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", fmt.Sprintf("color=c=black:s=%dx%d:r=%d", width, height, fps),
		"-frames:v", fmt.Sprint(frames),
		"-vf", "format=nv12",
		"-c:v", encoder,
		"-b:v", fmt.Sprintf("%dk", cfg.BitrateKbps),
		"-maxrate", fmt.Sprintf("%dk", cfg.MaxrateKbps),
		"-bufsize", fmt.Sprintf("%dk", cfg.BufsizeKbps),
		"-g", fmt.Sprint(fps * 2),
		"-bf", "0",
	}
	switch encoder {
	case "h264_nvenc":
		args = append(args, "-preset", "p4", "-tune", "ll", "-rc", "vbr", "-multipass", "qres", "-spatial_aq", "1")
	case "h264_qsv":
		args = append(args, "-preset", "medium", "-look_ahead", "0")
	case "h264_amf":
		args = append(args, "-usage", "lowlatency", "-quality", "balanced", "-rc", "vbr_peak")
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

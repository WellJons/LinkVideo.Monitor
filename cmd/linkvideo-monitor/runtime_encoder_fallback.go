package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const runtimeBenchmarkCacheTTL = 30 * time.Minute

type encoderBenchmarkCacheEntry struct {
	ActualFPS float64
	ErrText   string
	At        time.Time
}

var encoderBenchmarkCache = struct {
	sync.Mutex
	items map[string]encoderBenchmarkCacheEntry
}{items: make(map[string]encoderBenchmarkCacheEntry)}

var encoderBenchmarkRunMu sync.Mutex

func runtimePresetCandidates(cfg Config, fps int) []runtimePerformanceCandidate {
	preset := strings.ToLower(strings.TrimSpace(cfg.Preset))
	if preset == "" {
		preset = "veryfast"
	}
	result := make([]runtimePerformanceCandidate, 0, 4)
	add := func(name string) {
		candidate := runtimePerformanceCandidate{FPS: fps, Preset: name}
		for _, existing := range result {
			if existing == candidate {
				return
			}
		}
		result = append(result, candidate)
	}
	add(preset)
	currentRank := softwarePresetRank(preset)
	for _, faster := range []string{"veryfast", "superfast", "ultrafast"} {
		if softwarePresetRank(faster) > currentRank {
			add(faster)
		}
	}
	return result
}

func benchmarkCacheKey(cfg Config, plan capturePlan, encoder string, candidate runtimePerformanceCandidate) string {
	return fmt.Sprintf("%s|%s|%dx%d|fps=%d|preset=%s|b=%d|max=%d|buf=%d|rc=%s",
		resolveExecutable(cfg.FFmpegPath), encoder, even(plan.OutputWidth), even(plan.OutputHeight),
		candidate.FPS, strings.ToLower(candidate.Preset), cfg.BitrateKbps, cfg.MaxrateKbps, cfg.BufsizeKbps, cfg.RateControl)
}

func cachedEncoderBenchmark(cfg Config, plan capturePlan, encoder string, candidate runtimePerformanceCandidate) (float64, error) {
	key := benchmarkCacheKey(cfg, plan, encoder, candidate)
	encoderBenchmarkCache.Lock()
	if item, ok := encoderBenchmarkCache.items[key]; ok && time.Since(item.At) < runtimeBenchmarkCacheTTL {
		encoderBenchmarkCache.Unlock()
		if item.ErrText != "" {
			return item.ActualFPS, fmt.Errorf("%s", item.ErrText)
		}
		return item.ActualFPS, nil
	}
	encoderBenchmarkCache.Unlock()

	encoderBenchmarkRunMu.Lock()
	defer encoderBenchmarkRunMu.Unlock()

	// A second caller can reach this point while another benchmark is running.
	// Re-check after acquiring the global gate so the same expensive probe is
	// never executed twice back-to-back.
	encoderBenchmarkCache.Lock()
	if item, ok := encoderBenchmarkCache.items[key]; ok && time.Since(item.At) < runtimeBenchmarkCacheTTL {
		encoderBenchmarkCache.Unlock()
		if item.ErrText != "" {
			return item.ActualFPS, fmt.Errorf("%s", item.ErrText)
		}
		return item.ActualFPS, nil
	}
	encoderBenchmarkCache.Unlock()

	actualFPS, err := benchmarkEncoderRealtime(cfg, plan, encoder, candidate)
	entry := encoderBenchmarkCacheEntry{ActualFPS: actualFPS, At: time.Now()}
	if err != nil {
		entry.ErrText = err.Error()
	}
	encoderBenchmarkCache.Lock()
	encoderBenchmarkCache.items[key] = entry
	encoderBenchmarkCache.Unlock()
	return actualFPS, err
}

func benchmarkEncoderRealtime(cfg Config, plan capturePlan, encoder string, candidate runtimePerformanceCandidate) (float64, error) {
	width, height := even(plan.OutputWidth), even(plan.OutputHeight)
	if width < 128 || height < 72 {
		width, height = 128, 72
	}
	fps := candidate.FPS
	if fps < 1 {
		fps = cfg.FPS
	}
	if fps < 1 {
		fps = 15
	}
	frames := fps * 2
	if frames < 20 {
		frames = 20
	}
	mediaDuration := time.Duration(float64(frames) / float64(fps) * float64(time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	pixFmt := "nv12"
	if !isHardwareEncoder(encoder) {
		pixFmt = "yuv420p"
	}
	// Force BGRA before the encoder format conversion so the benchmark includes
	// approximately the same colour-conversion work as the real raw desktop pipe.
	inputFilter := fmt.Sprintf("testsrc2=s=%dx%d:r=%d,format=bgra", width, height, fps)
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", inputFilter,
		"-frames:v", fmt.Sprint(frames),
		"-vf", "format=" + pixFmt,
		"-c:v", encoder,
	}
	switch encoder {
	case "h264_nvenc", "hevc_nvenc":
		rc := "vbr"
		if cfg.RateControl == "cbr" {
			rc = "cbr"
		}
		args = append(args, "-preset", "p4", "-tune", "ll", "-rc", rc, "-multipass", "qres", "-spatial_aq", "1", "-bf", "0")
	case "h264_qsv", "hevc_qsv":
		args = append(args, "-preset", "medium", "-look_ahead", "0", "-bf", "0")
	case "h264_amf", "hevc_amf":
		rc := "vbr_peak"
		if cfg.RateControl == "cbr" {
			rc = "cbr"
		}
		args = append(args, "-usage", "lowlatency", "-quality", "balanced", "-rc", rc, "-bf", "0")
	case "h264_videotoolbox", "hevc_videotoolbox":
		args = append(args, "-realtime", "1", "-allow_sw", "0", "-prio_speed", "1", "-bf", "0")
	case "libx264", "libx265":
		preset := strings.TrimSpace(candidate.Preset)
		if preset == "" {
			preset = strings.TrimSpace(cfg.Preset)
		}
		if preset == "" {
			preset = "veryfast"
		}
		// threads=0 explicitly asks libx264/libx265 to use their automatic
		// multithreaded scheduler instead of constraining them to one core.
		args = append(args, "-threads", "0", "-preset", preset, "-tune", "zerolatency", "-bf", "0")
	default:
		return 0, fmt.Errorf("неизвестный кодировщик %s", encoder)
	}
	args = append(args,
		"-b:v", fmt.Sprintf("%dk", cfg.BitrateKbps),
		"-maxrate", fmt.Sprintf("%dk", cfg.MaxrateKbps),
		"-bufsize", fmt.Sprintf("%dk", cfg.BufsizeKbps),
		"-g", fmt.Sprint(fps*2),
		"-keyint_min", fmt.Sprint(fps*2),
		"-sc_threshold", "0",
		"-pix_fmt", pixFmt,
		"-f", "null", "-",
	)

	cmd := exec.CommandContext(ctx, resolveExecutable(cfg.FFmpegPath), args...)
	hideChildWindow(cmd)
	started := time.Now()
	output, err := cmd.CombinedOutput()
	elapsed := time.Since(started)
	if ctx.Err() != nil {
		return 0, fmt.Errorf("тест производительности превысил 12 секунд")
	}
	if err != nil {
		text := strings.TrimSpace(string(output))
		if len(text) > 320 {
			text = text[:320]
		}
		if text == "" {
			text = err.Error()
		}
		return 0, fmt.Errorf("%s", text)
	}
	if elapsed <= 0 {
		return 0, nil
	}
	actualFPS := float64(frames) / elapsed.Seconds()
	headroom := 1.10
	if !isHardwareEncoder(encoder) {
		// Software encoders share CPU time with capture, colour conversion, audio,
		// Windows and the user's applications, so require a larger reserve.
		headroom = 1.20
	}
	maxElapsed := time.Duration(float64(mediaDuration) / headroom)
	if elapsed > maxElapsed {
		return actualFPS, fmt.Errorf("%.1f кадр/с при требуемых %d кадр/с (нужен запас %.0f%%)", actualFPS, fps, (headroom-1)*100)
	}
	return actualFPS, nil
}

func runtimeEncoderOrder(codec, requested string) []string {
	result := make([]string, 0, 5)
	add := func(name string) {
		if name == "" || encoderCodec(name) != codec {
			return
		}
		for _, existing := range result {
			if existing == name {
				return
			}
		}
		result = append(result, name)
	}
	add(requested)
	for _, option := range automaticEncoderCandidates(codec) {
		add(option.Name)
	}
	add(softwareEncoderForCodec(codec))
	return result
}

func h264FallbackEncoderName(h265Encoder string) string {
	switch h265Encoder {
	case "hevc_nvenc":
		return "h264_nvenc"
	case "hevc_qsv":
		return "h264_qsv"
	case "hevc_amf":
		return "h264_amf"
	case "hevc_videotoolbox":
		return "h264_videotoolbox"
	default:
		return ""
	}
}

func tryEncoderAtFPS(cfg Config, plan capturePlan, encoder string, fps int) (Config, float64, error) {
	trial := cfg
	trial.Codec = encoderCodec(encoder)
	trial.Encoder = encoder
	trial.FPS = fps
	if isHardwareEncoder(encoder) {
		actual, err := cachedEncoderBenchmark(trial, plan, encoder, runtimePerformanceCandidate{FPS: fps})
		return trial, actual, err
	}
	var lastActual float64
	var lastErr error
	for _, candidate := range runtimePresetCandidates(trial, fps) {
		actual, err := cachedEncoderBenchmark(trial, plan, encoder, candidate)
		lastActual, lastErr = actual, err
		if err == nil {
			trial.Preset = candidate.Preset
			return trial, actual, nil
		}
	}
	return trial, lastActual, lastErr
}

func runtimeEncodingChanged(original, runtimeCfg Config, originalEncoder, runtimeEncoder string) bool {
	return original.Codec != runtimeCfg.Codec || original.FPS != runtimeCfg.FPS ||
		!strings.EqualFold(original.Preset, runtimeCfg.Preset) || originalEncoder != runtimeEncoder
}

func (a *app) selectH264Runtime(cfg Config, plan capturePlan, requested string, fromH265 bool) (Config, string, bool) {
	cfg.Codec = "h264"
	if cfg.FPS < 1 {
		cfg.FPS = 15
	}
	original := cfg
	for _, encoder := range runtimeEncoderOrder("h264", requested) {
		trial, actual, err := tryEncoderAtFPS(cfg, plan, encoder, cfg.FPS)
		if err != nil {
			continue
		}
		optimized := runtimeEncodingChanged(original, trial, requested, encoder) || fromH265
		if optimized {
			a.appendLog(fmt.Sprintf("Автовыбор кодировщика: %s, %d FPS, preset=%s (тест %.1f кадр/с). Сохранённые настройки не изменены", encoderLabel(encoder), trial.FPS, trial.Preset, actual))
		} else {
			a.appendLog(fmt.Sprintf("Проверка производительности: %s выдерживает текущие параметры (тест %.1f кадр/с)", encoderLabel(encoder), actual))
		}
		return trial, encoder, optimized
	}

	// Only after every H.264 encoder failed at the requested FPS do we reduce
	// frame rate. These values stay internal and never change config.json/UI.
	for _, fps := range []int{15, 12, 10} {
		if fps >= cfg.FPS {
			continue
		}
		trial := cfg
		trial.Preset = "ultrafast"
		trial.FPS = fps
		actual, err := cachedEncoderBenchmark(trial, plan, "libx264", runtimePerformanceCandidate{FPS: fps, Preset: "ultrafast"})
		if err != nil {
			continue
		}
		trial.Codec = "h264"
		trial.Encoder = "libx264"
		a.appendLog(fmt.Sprintf("Автооптимизация слабого ПК: Программный H.264, ultrafast, %d FPS (тест %.1f кадр/с). GOP остаётся 2 секунды; сохранённые настройки не изменены", fps, actual))
		return trial, "libx264", true
	}

	fallback := cfg
	fallback.Codec = "h264"
	fallback.Encoder = "libx264"
	fallback.Preset = "ultrafast"
	if fallback.FPS > 10 {
		fallback.FPS = 10
	}
	a.appendLog("Ни один кодировщик не подтвердил realtime-производительность; используется минимальный аварийный режим H.264 ultrafast. Сохранённые настройки не изменены")
	return fallback, "libx264", true
}

func (a *app) selectRuntimeEncoding(cfg Config, plan capturePlan) (Config, string, bool) {
	requested := cfg.Encoder
	if requested == "" || requested == "auto" || encoderCodec(requested) != cfg.Codec {
		requested = softwareEncoderForCodec(cfg.Codec)
	}
	if cfg.FPS < 1 {
		cfg.FPS = 15
	}

	if cfg.Codec == "h265" {
		for _, encoder := range runtimeEncoderOrder("h265", requested) {
			trial, actual, err := tryEncoderAtFPS(cfg, plan, encoder, cfg.FPS)
			if err != nil {
				continue
			}
			optimized := runtimeEncodingChanged(cfg, trial, requested, encoder)
			if optimized {
				a.appendLog(fmt.Sprintf("Автовыбор H.265: %s, %d FPS, preset=%s (тест %.1f кадр/с). Сохранённые настройки не изменены", encoderLabel(encoder), trial.FPS, trial.Preset, actual))
			} else {
				a.appendLog(fmt.Sprintf("Проверка производительности: %s выдерживает H.265 в realtime (тест %.1f кадр/с)", encoderLabel(encoder), actual))
			}
			return trial, encoder, optimized
		}

		a.appendLog("H.265 не выдерживает текущую частоту кадров ни на одном доступном кодировщике; выполняется автоматический переход на H.264")
		h264 := cfg
		h264.Codec = "h264"
		h264.Encoder = "libx264"
		preferred := h264FallbackEncoderName(requested)
		return a.selectH264Runtime(h264, plan, preferred, true)
	}

	return a.selectH264Runtime(cfg, plan, requested, false)
}

func probeEncoderForCapabilities(cfg Config, plan capturePlan, encoder string) error {
	fps := cfg.FPS
	if fps < 1 {
		fps = 15
	}
	if isHardwareEncoder(encoder) {
		// Hardware encoders only need a short functional probe in the settings UI.
		// The selected encoder still receives the full realtime benchmark before
		// a stream starts, so capability discovery cannot overload the machine.
		return probeVideoEncoder(cfg, encoder, plan)
	}
	for _, candidate := range runtimePresetCandidates(cfg, fps) {
		if _, err := cachedEncoderBenchmark(cfg, plan, encoder, candidate); err == nil {
			return nil
		}
	}
	if encoder == "libx264" {
		for _, fallbackFPS := range []int{15, 12, 10} {
			if fallbackFPS >= fps {
				continue
			}
			if _, err := cachedEncoderBenchmark(cfg, plan, encoder, runtimePerformanceCandidate{FPS: fallbackFPS, Preset: "ultrafast"}); err == nil {
				return nil
			}
		}
	}
	return fmt.Errorf("кодировщик не обеспечивает realtime для текущих параметров")
}

func keepNormalStreamingPriority(encoder string, optimized bool) bool {
	// Keep the whole streaming pipeline at normal priority. Even when video is
	// encoded in hardware, this FFmpeg process still performs BGRA->NV12 colour
	// conversion, audio work, muxing and network I/O on the CPU. Demoting the
	// process can starve those threads on a busy/low-end PC and create avoidable
	// stalls before the encoder or RTSP socket.
	_ = encoder
	_ = optimized
	return true
}

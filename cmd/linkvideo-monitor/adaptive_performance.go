package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const adaptivePerformanceHeadroom = 1.10

type runtimePerformanceCandidate struct {
	FPS    int
	Preset string
}

func softwarePresetRank(name string) int {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "placebo":
		return 0
	case "veryslow":
		return 1
	case "slower":
		return 2
	case "slow":
		return 3
	case "medium":
		return 4
	case "fast":
		return 5
	case "faster":
		return 6
	case "veryfast":
		return 7
	case "superfast":
		return 8
	case "ultrafast":
		return 9
	default:
		return 7
	}
}

func runtimePerformanceCandidates(cfg Config, encoder string) []runtimePerformanceCandidate {
	if encoder != "libx264" && encoder != "libx265" {
		return nil
	}
	fps := cfg.FPS
	if fps < 1 {
		fps = 15
	}
	preset := strings.ToLower(strings.TrimSpace(cfg.Preset))
	if preset == "" {
		preset = "veryfast"
	}
	result := make([]runtimePerformanceCandidate, 0, 7)
	add := func(candidate runtimePerformanceCandidate) {
		if candidate.FPS < 1 || candidate.Preset == "" {
			return
		}
		for _, existing := range result {
			if existing == candidate {
				return
			}
		}
		result = append(result, candidate)
	}
	add(runtimePerformanceCandidate{FPS: fps, Preset: preset})
	currentRank := softwarePresetRank(preset)
	for _, faster := range []string{"veryfast", "superfast", "ultrafast"} {
		if softwarePresetRank(faster) > currentRank {
			add(runtimePerformanceCandidate{FPS: fps, Preset: faster})
		}
	}
	if fps > 15 {
		add(runtimePerformanceCandidate{FPS: 15, Preset: "ultrafast"})
	}
	if fps > 12 {
		add(runtimePerformanceCandidate{FPS: 12, Preset: "ultrafast"})
	}
	if fps > 10 {
		add(runtimePerformanceCandidate{FPS: 10, Preset: "ultrafast"})
	}
	return result
}

func benchmarkRuntimePerformance(cfg Config, plan capturePlan, encoder string, candidate runtimePerformanceCandidate) (float64, error) {
	width, height := even(plan.OutputWidth), even(plan.OutputHeight)
	if width < 128 || height < 72 {
		width, height = 128, 72
	}
	fps := candidate.FPS
	if fps < 1 {
		return 0, fmt.Errorf("некорректная частота кадров %d", fps)
	}
	frames := fps * 2
	if frames < 20 {
		frames = 20
	}
	mediaDuration := time.Duration(float64(frames) / float64(fps) * float64(time.Second))
	const timeout = 12 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	args := []string{"-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", fmt.Sprintf("testsrc2=s=%dx%d:r=%d", width, height, fps), "-frames:v", fmt.Sprint(frames), "-vf", "format=yuv420p", "-c:v", encoder, "-preset", candidate.Preset, "-tune", "zerolatency", "-b:v", fmt.Sprintf("%dk", cfg.BitrateKbps), "-maxrate", fmt.Sprintf("%dk", cfg.MaxrateKbps), "-bufsize", fmt.Sprintf("%dk", cfg.BufsizeKbps), "-g", fmt.Sprint(fps * 2), "-keyint_min", fmt.Sprint(fps * 2), "-sc_threshold", "0", "-bf", "0", "-f", "null", "-"}
	cmd := exec.CommandContext(ctx, resolveExecutable(cfg.FFmpegPath), args...)
	hideChildWindow(cmd)
	started := time.Now()
	output, err := cmd.CombinedOutput()
	elapsed := time.Since(started)
	if ctx.Err() != nil {
		return 0, fmt.Errorf("тест производительности превысил %d секунд", int(timeout/time.Second))
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
	maxElapsed := time.Duration(float64(mediaDuration) / adaptivePerformanceHeadroom)
	if elapsed > maxElapsed {
		return actualFPS, fmt.Errorf("%.1f кадр/с при требуемых %d кадр/с", actualFPS, fps)
	}
	return actualFPS, nil
}

func (a *app) runtimeConfigForPerformance(cfg Config, plan capturePlan, encoder string) (Config, bool) {
	candidates := runtimePerformanceCandidates(cfg, encoder)
	if len(candidates) == 0 {
		return cfg, false
	}
	var lastFPS float64
	var lastErr error
	for index, candidate := range candidates {
		actualFPS, err := benchmarkRuntimePerformance(cfg, plan, encoder, candidate)
		lastFPS, lastErr = actualFPS, err
		if err != nil {
			continue
		}
		runtimeCfg := cfg
		runtimeCfg.FPS = candidate.FPS
		runtimeCfg.Preset = candidate.Preset
		optimized := runtimeCfg.FPS != cfg.FPS || !strings.EqualFold(runtimeCfg.Preset, cfg.Preset)
		if optimized {
			a.appendLog(fmt.Sprintf("Автооптимизация слабого ПК: %s, %d FPS (тест %.1f кадр/с). Сохранённые настройки не изменены", candidate.Preset, candidate.FPS, actualFPS))
		} else if index == 0 {
			a.appendLog(fmt.Sprintf("Проверка производительности: текущий режим выдерживает нагрузку (тест %.1f кадр/с)", actualFPS))
		}
		return runtimeCfg, optimized
	}
	fallback := candidates[len(candidates)-1]
	runtimeCfg := cfg
	runtimeCfg.FPS = fallback.FPS
	runtimeCfg.Preset = fallback.Preset
	if lastErr != nil {
		a.appendLog(fmt.Sprintf("Автооптимизация слабого ПК: включён минимальный режим %s, %d FPS; последний тест: %.1f кадр/с (%v)", fallback.Preset, fallback.FPS, lastFPS, lastErr))
	}
	return runtimeCfg, runtimeCfg.FPS != cfg.FPS || !strings.EqualFold(runtimeCfg.Preset, cfg.Preset)
}

package main

import "strings"

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

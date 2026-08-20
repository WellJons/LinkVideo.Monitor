package main

import (
	"strings"
	"time"
)

type EncoderCapability struct {
	Name      string `json:"name"`
	Label     string `json:"label"`
	Codec     string `json:"codec"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

func encoderCodec(name string) string {
	if strings.HasPrefix(name, "hevc_") || name == "libx265" {
		return "h265"
	}
	return "h264"
}

func softwareEncoderForCodec(codec string) string {
	if codec == "h265" {
		return "libx265"
	}
	return "libx264"
}

func isHardwareEncoder(name string) bool {
	return name != "" && name != "libx264" && name != "libx265"
}

func encoderCandidatesForCodec(codec string) []encoderOption {
	if codec == "h265" {
		return []encoderOption{
			{Name: "libx265", Label: encoderLabel("libx265")},
			{Name: "hevc_qsv", Label: encoderLabel("hevc_qsv")},
			{Name: "hevc_nvenc", Label: encoderLabel("hevc_nvenc")},
			{Name: "hevc_amf", Label: encoderLabel("hevc_amf")},
		}
	}
	return []encoderOption{
		{Name: "libx264", Label: encoderLabel("libx264")},
		{Name: "h264_qsv", Label: encoderLabel("h264_qsv")},
		{Name: "h264_nvenc", Label: encoderLabel("h264_nvenc")},
		{Name: "h264_amf", Label: encoderLabel("h264_amf")},
	}
}

func adapterMatchesEncoder(adapterNames, encoder string) bool {
	names := strings.ToLower(adapterNames)
	switch {
	case strings.HasSuffix(encoder, "_qsv"):
		return strings.Contains(names, "intel")
	case strings.HasSuffix(encoder, "_nvenc"):
		return strings.Contains(names, "nvidia")
	case strings.HasSuffix(encoder, "_amf"):
		return strings.Contains(names, "amd") || strings.Contains(names, "radeon") || strings.Contains(names, "advanced micro devices")
	default:
		return true
	}
}

func capabilityProbePlan(cfg Config) capturePlan {
	width, height := even(cfg.Width), even(cfg.Height)
	if cfg.ResolutionProfile == "full_hd" {
		width, height = 1920, 1080
	} else if cfg.ResolutionProfile == "hd" {
		width, height = 1280, 720
	}
	if width < 128 || height < 72 {
		width, height = 1920, 1080
	}
	return capturePlan{Width: width, Height: height, OutputWidth: width, OutputHeight: height}
}

func (a *app) getEncoderCapabilities(force bool) []EncoderCapability {
	a.mu.Lock()
	cfg := a.cfg
	cached := append([]EncoderCapability(nil), a.encoderCapabilities...)
	cachedAt := a.encoderCapabilitiesAt
	a.mu.Unlock()
	if !force && len(cached) > 0 && time.Since(cachedAt) < 30*time.Minute {
		return cached
	}

	plan := capabilityProbePlan(cfg)
	if resolvedPlan, err := resolveCapturePlan(cfg); err == nil {
		plan = resolvedPlan
	}
	adapters := strings.Join(videoAdapterNames(), "\n")
	candidates := append(encoderCandidatesForCodec("h264"), encoderCandidatesForCodec("h265")...)
	results := make([]EncoderCapability, len(candidates))

	// Run probes in a deterministic order instead of starting every software and
	// hardware encoder benchmark at the same time. On low-end PCs the previous
	// fan-out could consume all CPU/GPU resources while the real stream was
	// starting, making both the probe and the stream look slower than they were.
	for i, candidate := range candidates {
		results[i] = EncoderCapability{Name: candidate.Name, Label: candidate.Label, Codec: encoderCodec(candidate.Name)}
		if isHardwareEncoder(candidate.Name) && !adapterMatchesEncoder(adapters, candidate.Name) {
			results[i].Reason = "Подходящий видеоадаптер не обнаружен"
			continue
		}
		probeCfg := cfg
		probeCfg.Codec = encoderCodec(candidate.Name)
		probeCfg.Encoder = candidate.Name
		if probeCfg.FPS < 1 {
			probeCfg.FPS = 15
		}
		if err := probeEncoderForCapabilities(probeCfg, plan, candidate.Name); err != nil {
			results[i].Reason = err.Error()
			continue
		}
		results[i].Available = true
	}

	a.mu.Lock()
	a.encoderCapabilities = append([]EncoderCapability(nil), results...)
	a.encoderCapabilitiesAt = time.Now()
	a.mu.Unlock()
	return results
}

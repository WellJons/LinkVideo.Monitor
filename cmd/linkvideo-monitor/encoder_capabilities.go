package main

import (
	"strings"
	"sync"
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

func (a *app) getEncoderCapabilities(force bool) []EncoderCapability {
	a.mu.Lock()
	if !force && len(a.encoderCapabilities) > 0 && time.Since(a.encoderCapabilitiesAt) < 30*time.Minute {
		cached := append([]EncoderCapability(nil), a.encoderCapabilities...)
		a.mu.Unlock()
		return cached
	}
	cfg := a.cfg
	a.mu.Unlock()

	plan := capturePlan{Width: 1280, Height: 720, OutputWidth: 1280, OutputHeight: 720}
	adapters := strings.Join(videoAdapterNames(), "\n")
	candidates := append(encoderCandidatesForCodec("h264"), encoderCandidatesForCodec("h265")...)
	results := make([]EncoderCapability, len(candidates))
	var wg sync.WaitGroup
	for i, candidate := range candidates {
		i, candidate := i, candidate
		results[i] = EncoderCapability{Name: candidate.Name, Label: candidate.Label, Codec: encoderCodec(candidate.Name)}
		if isHardwareEncoder(candidate.Name) && !adapterMatchesEncoder(adapters, candidate.Name) {
			results[i].Reason = "Подходящий видеоадаптер не обнаружен"
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			probeCfg := cfg
			probeCfg.Codec = encoderCodec(candidate.Name)
			probeCfg.Encoder = candidate.Name
			if probeCfg.FPS < 1 {
				probeCfg.FPS = 15
			}
			if err := probeVideoEncoder(probeCfg, candidate.Name, plan); err != nil {
				results[i].Reason = err.Error()
				return
			}
			results[i].Available = true
		}()
	}
	wg.Wait()

	a.mu.Lock()
	a.encoderCapabilities = append([]EncoderCapability(nil), results...)
	a.encoderCapabilitiesAt = time.Now()
	a.mu.Unlock()
	return results
}

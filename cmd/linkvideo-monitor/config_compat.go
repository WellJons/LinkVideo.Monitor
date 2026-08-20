package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// UnmarshalJSON keeps compatibility with the first prototype and with browsers
// that sent numeric select values as JSON strings (for example monitor_index: "0").
func (c *Config) UnmarshalJSON(data []byte) error {
	type plain Config
	base := plain(defaultConfig())

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Decode all non-numeric fields through the standard decoder first.
	cleaned := make(map[string]json.RawMessage, len(raw))
	numeric := map[string]bool{
		"local_rtsp_port": true, "monitor_index": true, "monitor_number": true,
		"offset_x": true, "offset_y": true, "width": true, "height": true, "fps": true,
		"bitrate_kbps": true, "maxrate_kbps": true, "bufsize_kbps": true,
		"audio_bitrate_kbps": true, "audio_sample_rate": true,
		"audio_channels": true, "audio_advance_ms": true, "restart_delay_s": true,
		"overlay_x": true, "overlay_y": true, "microphone_voice_db": true,
		"remote_sync_interval_min": true,
	}
	for k, v := range raw {
		if !numeric[k] {
			cleaned[k] = v
		}
	}
	if b, err := json.Marshal(cleaned); err != nil {
		return err
	} else if err := json.Unmarshal(b, &base); err != nil {
		return err
	}

	set := func(key string, dst *int) error {
		v, ok := raw[key]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return nil
		}
		n, err := flexibleInt(v)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		*dst = n
		return nil
	}

	fields := []struct {
		key string
		dst *int
	}{
		{"local_rtsp_port", &base.LocalRTSPPort},
		{"monitor_index", &base.MonitorIndex}, {"monitor_number", &base.MonitorNumber},
		{"offset_x", &base.OffsetX}, {"offset_y", &base.OffsetY},
		{"width", &base.Width}, {"height", &base.Height},
		{"fps", &base.FPS}, {"bitrate_kbps", &base.BitrateKbps},
		{"maxrate_kbps", &base.MaxrateKbps}, {"bufsize_kbps", &base.BufsizeKbps},
		{"audio_bitrate_kbps", &base.AudioBitrateKbps},
		{"audio_sample_rate", &base.AudioSampleRate},
		{"audio_channels", &base.AudioChannels},
		{"audio_advance_ms", &base.AudioAdvanceMs},
		{"restart_delay_s", &base.RestartDelayS},
		{"overlay_x", &base.OverlayX}, {"overlay_y", &base.OverlayY},
		{"microphone_voice_db", &base.MicrophoneVoiceDB},
		{"remote_sync_interval_min", &base.RemoteSyncIntervalMin},
	}
	for _, f := range fields {
		if err := set(f.key, f.dst); err != nil {
			return err
		}
	}
	// Конфиги 0.2 не содержали quality_profile. Сохраняем их ручной
	// битрейт вместо принудительного перехода на профиль 1024 Кбит/с.
	if _, hasProfile := raw["quality_profile"]; !hasProfile {
		if _, hasBitrate := raw["bitrate_kbps"]; hasBitrate {
			base.QualityProfile = "custom"
		}
	}
	if _, hasAudioQuality := raw["audio_quality"]; !hasAudioQuality {
		switch {
		case base.AudioBitrateKbps <= 80:
			base.AudioQuality = "low"
		case base.AudioBitrateKbps >= 160:
			base.AudioQuality = "high"
		default:
			base.AudioQuality = "medium"
		}
		// Звук становится явной опцией: после обновления со старой beta-версии
		// пользователь включает его самостоятельно.
		base.AudioEnabled = false
	}

	*c = Config(base)
	return nil
}

func flexibleInt(raw json.RawMessage) (int, error) {
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, fmt.Errorf("ожидалось число или строка с числом")
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%q не является целым числом", s)
	}
	return v, nil
}

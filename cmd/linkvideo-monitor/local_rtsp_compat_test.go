package main

import (
	"strings"
	"testing"
)

func TestMediaMTXReleaseForWindows7(t *testing.T) {
	release := mediaMTXReleaseForWindows(6, 1)
	if release.Version != "1.0.3" || !release.LegacyConfig {
		t.Fatalf("unexpected Windows 7 MediaMTX release: %+v", release)
	}
}

func TestMediaMTXReleaseForModernWindows(t *testing.T) {
	for _, version := range [][2]uint32{{6, 2}, {6, 3}, {10, 0}, {0, 0}} {
		release := mediaMTXReleaseForWindows(version[0], version[1])
		if release.Version != "1.19.3" || release.LegacyConfig {
			t.Fatalf("unexpected modern MediaMTX release for %d.%d: %+v", version[0], version[1], release)
		}
	}
}

func TestMediaMTXLegacyConfigIsRTSPOnlyAndLoopbackPublish(t *testing.T) {
	cfg := defaultConfig()
	cfg.LocalRTSPPath = "screen"
	cfg.LocalRTSPPort = 8554
	text := string(mediaMTXConfig(cfg, mediaMTXWindows7Release))
	for _, required := range []string{
		"protocols: [tcp]",
		"rtmp: no",
		"hls: no",
		"webrtc: no",
		"srt: no",
		`publishIPs: [127.0.0.1, "::1"]`,
		"readIPs: []",
		`"screen":`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("legacy MediaMTX config is missing %q:\n%s", required, text)
		}
	}
	if strings.Contains(text, "authInternalUsers:") {
		t.Fatalf("legacy config contains unsupported modern authentication schema:\n%s", text)
	}
}

func TestMediaMTXModernConfigDisablesUnusedServers(t *testing.T) {
	cfg := defaultConfig()
	cfg.LocalRTSPPath = "screen"
	cfg.LocalRTSPPort = 8554
	text := string(mediaMTXConfig(cfg, mediaMTXCurrentRelease))
	for _, required := range []string{
		"rtspTransports: [tcp]",
		"rtmp: false",
		"hls: false",
		"webrtc: false",
		"srt: false",
		"moq: false",
		"authInternalUsers:",
		`ips: ["127.0.0.1", "::1"]`,
		`path: "screen"`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("modern MediaMTX config is missing %q:\n%s", required, text)
		}
	}
}

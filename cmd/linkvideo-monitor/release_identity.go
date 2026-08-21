package main

import (
	"runtime"
	"strings"
)

// platformBuildVersion is injected by platform build scripts. Windows keeps
// appVersion as the compatibility default; macOS/Linux can evolve independently.
var platformBuildVersion string

func currentReleaseVersion() string {
	if value := strings.TrimSpace(platformBuildVersion); value != "" {
		return value
	}
	switch runtime.GOOS {
	case "darwin":
		return "0.1.0-dev"
	case "linux":
		return "0.1.0-dev"
	default:
		return appVersion
	}
}

func defaultUpdateManifestForPlatform(goos string) string {
	switch goos {
	case "windows":
		// Keep the legacy Windows manifest path so every already deployed 0.8.x
		// client remains compatible with the existing public update channel.
		return "https://raw.githubusercontent.com/WellJons/LinkVideo.Monitor.Updates/main/update-manifest.json"
	case "darwin":
		return "https://raw.githubusercontent.com/WellJons/LinkVideo.Monitor.Updates/main/update-manifest-macos.json"
	case "linux":
		return "https://raw.githubusercontent.com/WellJons/LinkVideo.Monitor.Updates/main/update-manifest-linux.json"
	default:
		return ""
	}
}

func architectureAllowed(allowed []string, current string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, item := range allowed {
		if strings.EqualFold(strings.TrimSpace(item), current) {
			return true
		}
	}
	return false
}

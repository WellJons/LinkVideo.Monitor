//go:build darwin

package main

func expectedAutomaticUpdateAssetName(targetVersion string) string {
	return "linkvideo.monitor_macos_" + canonicalUpdateVersion(targetVersion) + ".pkg"
}

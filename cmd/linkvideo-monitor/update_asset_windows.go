//go:build windows

package main

func expectedAutomaticUpdateAssetName(targetVersion string) string {
	return "linkvideo.monitor_" + updateAssetVersionBase(targetVersion) + "_setup.exe"
}

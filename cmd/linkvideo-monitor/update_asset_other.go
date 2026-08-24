//go:build !windows && !darwin

package main

func expectedAutomaticUpdateAssetName(targetVersion string) string { return "" }

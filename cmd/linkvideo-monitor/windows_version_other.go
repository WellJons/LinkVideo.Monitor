//go:build !windows

package main

func windowsVersion() (major, minor, build uint32) { return 0, 0, 0 }
func supportsDesktopDuplication() bool             { return false }

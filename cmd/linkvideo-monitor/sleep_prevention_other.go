//go:build !windows && !darwin

package main

func setSleepPrevention(enabled, keepDisplayOn bool) {}

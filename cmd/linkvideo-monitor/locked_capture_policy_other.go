//go:build !windows && !darwin

package main

func shouldProtectStaleLockedFrame() bool { return false }

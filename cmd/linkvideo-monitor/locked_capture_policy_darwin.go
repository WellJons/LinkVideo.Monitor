//go:build darwin

package main

func shouldProtectStaleLockedFrame() bool { return true }

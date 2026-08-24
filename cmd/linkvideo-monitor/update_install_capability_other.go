//go:build !windows && !darwin

package main

// Linux and other platforms do not have a managed signed installer yet.
func automaticUpdateInstallSupported() bool { return false }

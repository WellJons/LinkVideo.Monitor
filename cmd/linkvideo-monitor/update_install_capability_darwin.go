//go:build darwin

package main

// macOS automatic updates use a signed/notarized LinkVideo .pkg whose package
// signature and Team ID are verified before the system administrator prompt.
func automaticUpdateInstallSupported() bool { return true }

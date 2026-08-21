//go:build !windows

package main

// macOS will switch this to true only after the updater can verify a signed,
// notarized bundle and replace the installed app safely. Linux has no managed
// installer yet either.
func automaticUpdateInstallSupported() bool { return false }

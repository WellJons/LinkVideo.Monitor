//go:build darwin

package main

// Report automatic install support only for the production-managed app. An
// ad-hoc development build can still check for updates, but must never claim
// that it can install a package automatically.
func automaticUpdateInstallSupported() bool {
	_, err := macOSManagedUpdateTeamID()
	return err == nil
}

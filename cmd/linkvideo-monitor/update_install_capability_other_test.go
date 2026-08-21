//go:build !windows

package main

import "testing"

func TestAutomaticUpdateInstallUnsupportedWithoutManagedInstaller(t *testing.T) {
	if automaticUpdateInstallSupported() {
		t.Fatal("platform without a managed signed installer must not report automatic install support")
	}
}

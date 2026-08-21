//go:build windows

package main

import "testing"

func TestAutomaticUpdateInstallSupportedOnWindows(t *testing.T) {
	if !automaticUpdateInstallSupported() {
		t.Fatal("Windows managed installer must keep automatic update capability")
	}
}

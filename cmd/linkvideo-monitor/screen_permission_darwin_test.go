//go:build darwin

package main

import "testing"

func TestParseScreenPermissionOutput(t *testing.T) {
	if !parseScreenPermissionGranted("granted\n") {
		t.Fatal("granted screen recording permission was not recognized")
	}
	if parseScreenPermissionGranted("denied\n") {
		t.Fatal("denied screen recording permission was accepted")
	}
	if !parseScreenPermissionDenied(" denied \n") {
		t.Fatal("denied screen recording permission was not recognized")
	}
}

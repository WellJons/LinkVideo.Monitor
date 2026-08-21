//go:build darwin

package main

import (
	"reflect"
	"testing"
)

func TestMacOSStartupHelperArgs(t *testing.T) {
	if got, want := macOSStartupHelperArgs(true), []string{"--set-startup", "true"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("enable args=%v want %v", got, want)
	}
	if got, want := macOSStartupHelperArgs(false), []string{"--set-startup", "false"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("disable args=%v want %v", got, want)
	}
}

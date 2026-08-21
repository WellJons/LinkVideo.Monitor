//go:build darwin

package main

import (
	"reflect"
	"testing"
)

func TestMacOSCaffeinateArgs(t *testing.T) {
	if got, want := macOSCaffeinateArgs(false, 1234), []string{"-i", "-w", "1234"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sleep args=%v want %v", got, want)
	}
	if got, want := macOSCaffeinateArgs(true, 1234), []string{"-i", "-d", "-w", "1234"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("display args=%v want %v", got, want)
	}
}

//go:build darwin

package main

import "testing"

func TestMacOSWorkspaceResumeMessage(t *testing.T) {
	cases := map[string]string{
		"wake":           "macOS вышла из сна",
		"screens-wake":   "Дисплеи macOS вышли из сна",
		"session-active": "Пользовательская сессия macOS снова активна",
	}
	for event, want := range cases {
		got, ok := macOSWorkspaceResumeMessage(event)
		if !ok || got != want {
			t.Fatalf("event %q => %q,%v; want %q,true", event, got, ok, want)
		}
	}
	if _, ok := macOSWorkspaceResumeMessage("noise"); ok {
		t.Fatal("unknown workspace event must be ignored")
	}
}

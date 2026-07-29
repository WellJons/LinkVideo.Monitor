package main

import "testing"

func TestSelectOutputFrameUsesRealLockScreenWhenDisplayIsOn(t *testing.T) {
	latest := []byte("old-user-frame")
	secure := []byte("real-windows-lock-screen")
	got := selectOutputFrame(latest, secure, []byte("fallback"), []byte("protected"), true, false, true, true)
	if string(got) != string(secure) {
		t.Fatalf("expected secure Windows lock-screen frame, got %q", got)
	}
}

func TestSelectOutputFrameUsesBrandedFallbackOnlyWhenDisplayPowersOff(t *testing.T) {
	latest := []byte("last-real-user-frame")
	fallback := []byte("linkvideo-display-off-frame")
	got := selectOutputFrame(latest, []byte("real-lock-screen"), fallback, []byte("protected-wait"), true, true, true, true)
	if string(got) != string(fallback) {
		t.Fatalf("expected branded display-off frame, got %q", got)
	}
}

func TestSelectOutputFrameKeepsLatestDuringNormalLockHandover(t *testing.T) {
	latest := []byte("last-real-user-frame")
	got := selectOutputFrame(latest, []byte("empty"), []byte("fallback"), []byte("protected-wait"), true, false, false, false)
	if string(got) != string(latest) {
		t.Fatalf("expected last real frame while waiting for Winlogon, got %q", got)
	}
}

func TestSelectOutputFrameDoesNotShowLockedMessageWhenUnlockedDisplayTurnsOff(t *testing.T) {
	latest := []byte("last-real-user-frame")
	got := selectOutputFrame(latest, []byte("empty"), []byte("fallback"), []byte("protected-wait"), false, true, false, false)
	if string(got) != string(latest) {
		t.Fatalf("expected current frame for an unlocked session, got %q", got)
	}
}

func TestSelectOutputFrameKeepsLastRealFrameDuringUACStartup(t *testing.T) {
	latest := []byte("last-real-user-frame")
	got := selectOutputFrame(latest, []byte("empty"), []byte("fallback"), []byte("protected-wait"), false, false, true, false)
	if string(got) != string(latest) {
		t.Fatalf("expected last real frame while secure helper starts, got %q", got)
	}
}

package main

import "testing"

func TestSelectOutputFrameUsesRealLockScreenWhenDisplayIsOn(t *testing.T) {
	latest := []byte("old-user-frame")
	secure := []byte("real-windows-lock-screen")
	got := selectOutputFrame(latest, secure, []byte("fallback"), []byte("protected"), true, false, true, true, false)
	if string(got) != string(secure) {
		t.Fatalf("expected secure Windows lock-screen frame, got %q", got)
	}
}

func TestSelectOutputFrameUsesBrandedFallbackOnlyWhenDisplayPowersOff(t *testing.T) {
	latest := []byte("last-real-user-frame")
	fallback := []byte("linkvideo-display-off-frame")
	got := selectOutputFrame(latest, []byte("real-lock-screen"), fallback, []byte("protected-wait"), true, true, true, true, false)
	if string(got) != string(fallback) {
		t.Fatalf("expected branded display-off frame, got %q", got)
	}
}

func TestSelectOutputFrameKeepsLatestDuringNormalLockHandover(t *testing.T) {
	latest := []byte("last-real-user-frame")
	got := selectOutputFrame(latest, []byte("empty"), []byte("fallback"), []byte("protected-wait"), true, false, false, false, false)
	if string(got) != string(latest) {
		t.Fatalf("expected last real frame while waiting for Winlogon, got %q", got)
	}
}

func TestSelectOutputFrameDoesNotShowLockedMessageWhenUnlockedDisplayTurnsOff(t *testing.T) {
	latest := []byte("last-real-user-frame")
	got := selectOutputFrame(latest, []byte("empty"), []byte("fallback"), []byte("protected-wait"), false, true, false, false, false)
	if string(got) != string(latest) {
		t.Fatalf("expected current frame for an unlocked session, got %q", got)
	}
}

func TestSelectOutputFrameKeepsLastRealFrameDuringUACStartup(t *testing.T) {
	latest := []byte("last-real-user-frame")
	got := selectOutputFrame(latest, []byte("empty"), []byte("fallback"), []byte("protected-wait"), false, false, true, false, false)
	if string(got) != string(latest) {
		t.Fatalf("expected last real frame while secure helper starts, got %q", got)
	}
}

func TestSelectOutputFrameProtectsStaleMacLockCapture(t *testing.T) {
	latest := []byte("pre-lock-desktop")
	protected := []byte("safe-locked-placeholder")
	got := selectOutputFrame(latest, nil, []byte("display-off"), protected, true, false, false, false, true)
	if string(got) != string(protected) {
		t.Fatalf("expected safe stale-lock placeholder, got %q", got)
	}
}

func TestSelectOutputFramePrefersRealProtectedFrameOverStaleFallback(t *testing.T) {
	secure := []byte("real-protected-desktop")
	got := selectOutputFrame([]byte("old"), secure, []byte("display-off"), []byte("safe"), true, false, true, true, true)
	if string(got) != string(secure) {
		t.Fatalf("expected real protected frame, got %q", got)
	}
}

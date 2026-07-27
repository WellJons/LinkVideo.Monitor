package main

import "testing"

func TestSelectOutputFrameUsesRealLockScreenWhenAvailable(t *testing.T) {
	latest := []byte("secure-lock-screen")
	locked := []byte("linkvideo-fallback")
	protected := []byte("protected-wait")

	got := selectOutputFrame(latest, locked, protected, true, true, true)
	if string(got) != string(latest) {
		t.Fatalf("expected secure Windows lock-screen frame, got %q", got)
	}
}

func TestSelectOutputFrameUsesBrandedFallbackWhileLockScreenUnavailable(t *testing.T) {
	latest := []byte("old-unlocked-desktop")
	locked := []byte("linkvideo-fallback")
	protected := []byte("protected-wait")

	got := selectOutputFrame(latest, locked, protected, true, false, false)
	if string(got) != string(locked) {
		t.Fatalf("expected safe branded fallback, got %q", got)
	}
}

func TestSelectOutputFrameDoesNotExposeOldDesktopDuringLockedSecureHandover(t *testing.T) {
	latest := []byte("old-unlocked-desktop")
	locked := []byte("linkvideo-fallback")
	protected := []byte("protected-wait")

	got := selectOutputFrame(latest, locked, protected, true, true, false)
	if string(got) != string(locked) {
		t.Fatalf("expected fallback until a fresh secure frame arrives, got %q", got)
	}
}

func TestSelectOutputFrameUsesProtectedFallbackForUAC(t *testing.T) {
	latest := []byte("desktop")
	locked := []byte("linkvideo-fallback")
	protected := []byte("protected-wait")

	got := selectOutputFrame(latest, locked, protected, false, true, false)
	if string(got) != string(protected) {
		t.Fatalf("expected protected-desktop fallback, got %q", got)
	}
}

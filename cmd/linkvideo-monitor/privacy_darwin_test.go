//go:build darwin

package main

import "testing"

func TestMacOSPrivacySecureFieldIsSensitiveWithoutMetadata(t *testing.T) {
	sample := macOSPrivacySample{secure: true, editable: true, enabled: true, focused: true}
	if !macOSPrivacySampleSensitive(sample) {
		t.Fatal("AXSecureTextField must always be protected")
	}
}

func TestMacOSPrivacyUsesSharedAutocompleteRules(t *testing.T) {
	sample := macOSPrivacySample{
		editable:   true,
		enabled:    true,
		focused:    true,
		role:       "AXTextField",
		domClass:   "checkout-field security-code",
		ariaProps:  "autocomplete=cc-csc",
		process:    "com.google.Chrome",
		identifier: "payment-security",
	}
	if !macOSPrivacySampleSensitive(sample) {
		t.Fatal("macOS AX metadata must reuse shared CVV/autocomplete detection")
	}
}

func TestMacOSPrivacyRetinaCoordinatesUseNativePixels(t *testing.T) {
	tracker := &macOSPrivacyTracker{displays: []macOSDisplayInfo{{
		X: 0, Y: 0,
		WidthPoints: 1512, HeightPoints: 982,
		WidthPixels: 3024, HeightPixels: 1964,
	}}}
	rect, ok := tracker.captureRect(100, 50, 200, 40)
	if !ok {
		t.Fatal("expected valid Retina privacy rectangle")
	}
	want := privacyScreenRect{Left: 192, Top: 92, Right: 608, Bottom: 188}
	if rect != want {
		t.Fatalf("Retina privacy rectangle = %+v, want %+v", rect, want)
	}
}

func TestMacOSPrivacyMultiDisplayCoordinatesStayInPoints(t *testing.T) {
	tracker := &macOSPrivacyTracker{displays: []macOSDisplayInfo{
		{X: 0, Y: 0, WidthPoints: 1512, HeightPoints: 982, WidthPixels: 3024, HeightPixels: 1964},
		{X: -1920, Y: 0, WidthPoints: 1920, HeightPoints: 1080, WidthPixels: 1920, HeightPixels: 1080},
	}}
	rect, ok := tracker.captureRect(-1800, 100, 300, 50)
	if !ok {
		t.Fatal("expected valid multi-display privacy rectangle")
	}
	want := privacyScreenRect{Left: -1808, Top: 92, Right: -1492, Bottom: 158}
	if rect != want {
		t.Fatalf("multi-display privacy rectangle = %+v, want %+v", rect, want)
	}
}

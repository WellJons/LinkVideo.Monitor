package main

import "testing"

func TestSecondaryMonitorFallbackRegionsForFullDesktop(t *testing.T) {
	plan := capturePlan{X: 0, Y: 0, Width: 3840, Height: 1080, OutputWidth: 1920, OutputHeight: 540}
	monitors := []Monitor{
		{X: 0, Y: 0, Width: 1920, Height: 1080, Primary: true},
		{X: 1920, Y: 0, Width: 1920, Height: 1080, Primary: false},
	}
	regions := secondaryMonitorFallbackRegions(plan, monitors)
	if len(regions) != 1 {
		t.Fatalf("expected one secondary region, got %d", len(regions))
	}
	want := secureMonitorFallbackRegion{X: 960, Y: 0, Width: 960, Height: 540}
	if regions[0] != want {
		t.Fatalf("unexpected region: got %+v want %+v", regions[0], want)
	}
}

func TestSecondaryMonitorFallbackRegionsForSelectedSecondary(t *testing.T) {
	plan := capturePlan{X: 1920, Y: 0, Width: 1920, Height: 1080, OutputWidth: 1920, OutputHeight: 1080}
	monitors := []Monitor{
		{X: 0, Y: 0, Width: 1920, Height: 1080, Primary: true},
		{X: 1920, Y: 0, Width: 1920, Height: 1080, Primary: false},
	}
	regions := secondaryMonitorFallbackRegions(plan, monitors)
	if len(regions) != 1 || regions[0] != (secureMonitorFallbackRegion{X: 0, Y: 0, Width: 1920, Height: 1080}) {
		t.Fatalf("selected secondary monitor must cover the whole output, got %+v", regions)
	}
}

func TestSecureRegionLooksBlankAndCopy(t *testing.T) {
	frame := make([]byte, 8*4*4)
	region := secureMonitorFallbackRegion{X: 4, Y: 0, Width: 4, Height: 4}
	if !secureRegionLooksBlank(frame, 8, 4, region) {
		t.Fatal("zero-filled secondary region must be blank")
	}
	fallback := make([]byte, 4*4*4)
	for i := 0; i < len(fallback); i += 4 {
		fallback[i], fallback[i+1], fallback[i+2], fallback[i+3] = 40, 50, 60, 255
	}
	if !copyBGRARegion(frame, 8, 4, region, fallback) {
		t.Fatal("copyBGRARegion failed")
	}
	if secureRegionLooksBlank(frame, 8, 4, region) {
		t.Fatal("filled secondary region must not remain blank")
	}
	if frame[(0*8+4)*4+2] != 60 {
		t.Fatal("fallback pixels were not copied to the requested region")
	}
}

func TestCaptureMonitorFallbackRegionsIncludesEveryDisplay(t *testing.T) {
	plan := capturePlan{X: 0, Y: 0, Width: 3840, Height: 1080, OutputWidth: 1920, OutputHeight: 540}
	monitors := []Monitor{
		{X: 0, Y: 0, Width: 1920, Height: 1080, Primary: true},
		{X: 1920, Y: 0, Width: 1920, Height: 1080, Primary: false},
	}
	regions := captureMonitorFallbackRegions(plan, monitors)
	if len(regions) != 2 {
		t.Fatalf("expected two monitor regions, got %d", len(regions))
	}
	wantFirst := secureMonitorFallbackRegion{X: 0, Y: 0, Width: 960, Height: 540}
	wantSecond := secureMonitorFallbackRegion{X: 960, Y: 0, Width: 960, Height: 540}
	if regions[0] != wantFirst || regions[1] != wantSecond {
		t.Fatalf("unexpected regions: got %+v", regions)
	}
}

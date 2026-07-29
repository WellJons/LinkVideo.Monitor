package main

import "testing"

func TestOverlayPositionAvoidsBottomTaskbar(t *testing.T) {
	x, y := overlayPositionInsideWorkArea(1682, 1028, 214, 36, 0, 0, 1920, 1040, 12)
	if x != 1682 || y != 992 {
		t.Fatalf("unexpected position: %d,%d", x, y)
	}
}

func TestOverlayPositionAvoidsSideTaskbar(t *testing.T) {
	x, y := overlayPositionInsideWorkArea(1700, 900, 214, 36, 0, 0, 1760, 1080, 12)
	if x != 1534 || y != 900 {
		t.Fatalf("unexpected position: %d,%d", x, y)
	}
}

func TestOverlayPositionSupportsNegativeMonitorCoordinates(t *testing.T) {
	x, y := overlayPositionInsideWorkArea(-5, 900, 214, 36, -1920, 0, 0, 1040, 12)
	if x != -226 || y != 900 {
		t.Fatalf("unexpected position: %d,%d", x, y)
	}
}

func TestOverlayMovesToSelectedMonitor(t *testing.T) {
	x, y := overlayPositionForCaptureMonitor(1680, 980, -1920, 0, 1920, 1080, 214, 36)
	if x != -230 || y != 1028 {
		t.Fatalf("unexpected target position: %d,%d", x, y)
	}
}

func TestOverlayKeepsPositionOnSelectedMonitor(t *testing.T) {
	x, y := overlayPositionForCaptureMonitor(-500, 300, -1920, 0, 1920, 1080, 214, 36)
	if x != -500 || y != 300 {
		t.Fatalf("position unexpectedly changed: %d,%d", x, y)
	}
}

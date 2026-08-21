//go:build darwin

package main

import "testing"

func TestMacOSCaptureTilesMixedDPIUsesPointGeometry(t *testing.T) {
	displays := []macOSDisplayInfo{
		{ID: 1, X: 0, Y: 0, WidthPoints: 1728, HeightPoints: 1116, WidthPixels: 3456, HeightPixels: 2232, Primary: true},
		{ID: 2, X: 1728, Y: 0, WidthPoints: 1920, HeightPoints: 1080, WidthPixels: 1920, HeightPixels: 1080},
	}
	tiles := macOSCaptureTiles(displays, 0, 0, 3648, 1116, 3648, 1116)
	if len(tiles) != 2 {
		t.Fatalf("tiles=%v; want 2 displays", tiles)
	}
	if got := tiles[0]; got.X != 0 || got.Y != 0 || got.Width != 1728 || got.Height != 1116 {
		t.Fatalf("first tile=%+v", got)
	}
	if got := tiles[1]; got.X != 1728 || got.Y != 0 || got.Width != 1920 || got.Height != 1080 {
		t.Fatalf("second tile=%+v", got)
	}
}

func TestMacOSCaptureTilesPreserveNegativeDesktopOrigin(t *testing.T) {
	displays := []macOSDisplayInfo{
		{ID: 1, X: -1920, Y: 0, WidthPoints: 1920, HeightPoints: 1080, WidthPixels: 1920, HeightPixels: 1080},
		{ID: 2, X: 0, Y: 0, WidthPoints: 1920, HeightPoints: 1080, WidthPixels: 1920, HeightPixels: 1080, Primary: true},
	}
	tiles := macOSCaptureTiles(displays, -1920, 0, 3840, 1080, 1920, 540)
	if len(tiles) != 2 {
		t.Fatalf("tiles=%v; want 2 displays", tiles)
	}
	if tiles[0].X != 0 || tiles[0].Width != 960 || tiles[1].X != 960 || tiles[1].Width != 960 {
		t.Fatalf("scaled tile placement=%+v", tiles)
	}
}

func TestMacOSCompositeFramePlacesLatestDisplayFrames(t *testing.T) {
	left := &macOSCaptureStream{
		tile:   macOSCaptureTile{DisplayID: 1, X: 0, Y: 0, Width: 2, Height: 2},
		latest: solidBGRAFrame(2, 2, 0x11),
		ready:  true,
	}
	right := &macOSCaptureStream{
		tile:   macOSCaptureTile{DisplayID: 2, X: 2, Y: 0, Width: 2, Height: 2},
		latest: solidBGRAFrame(2, 2, 0x77),
		ready:  true,
	}
	canvas := make([]byte, 4*2*4)
	if !macOSCompositeFrame(canvas, 4, 2, []*macOSCaptureStream{left, right}) {
		t.Fatal("composite frame unexpectedly not ready")
	}
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			want := byte(0x11)
			if x >= 2 {
				want = 0x77
			}
			if got := canvas[(y*4+x)*4]; got != want {
				t.Fatalf("pixel %d,%d=%x want %x", x, y, got, want)
			}
		}
	}
}

func TestMacOSCompositeFrameWaitsForEveryDisplay(t *testing.T) {
	stream := &macOSCaptureStream{tile: macOSCaptureTile{DisplayID: 1, Width: 2, Height: 2}}
	if macOSCompositeFrame(make([]byte, 2*2*4), 2, 2, []*macOSCaptureStream{stream}) {
		t.Fatal("composite should wait for the first complete display frame")
	}
}

func TestMacOSCompositeFrameRejectsOutOfBoundsTile(t *testing.T) {
	stream := &macOSCaptureStream{
		tile:   macOSCaptureTile{DisplayID: 1, X: 3, Y: 0, Width: 2, Height: 2},
		latest: solidBGRAFrame(2, 2, 0x33),
		ready:  true,
	}
	if macOSCompositeFrame(make([]byte, 4*2*4), 4, 2, []*macOSCaptureStream{stream}) {
		t.Fatal("out-of-bounds tile must be rejected instead of copied")
	}
}

func TestMacOSRawFrameSizeRejectsExcessiveCanvas(t *testing.T) {
	if _, err := macOSRawFrameSize(40000, 40000); err == nil {
		t.Fatal("expected oversized canvas to be rejected")
	}
}

func solidBGRAFrame(width, height int, value byte) []byte {
	frame := make([]byte, width*height*4)
	for i := range frame {
		frame[i] = value
	}
	return frame
}

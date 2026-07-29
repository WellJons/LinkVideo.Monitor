package main

import "testing"

func TestSecureFrameLooksBlank(t *testing.T) {
	black := make([]byte, 1920*1080*4)
	if !secureFrameLooksBlank(black) {
		t.Fatal("all-black secure frame must be treated as blank")
	}

	nearBlack := make([]byte, 320*180*4)
	for i := 0; i < len(nearBlack); i += 4 {
		nearBlack[i] = 6
		nearBlack[i+1] = 7
		nearBlack[i+2] = 8
		nearBlack[i+3] = 255
	}
	if !secureFrameLooksBlank(nearBlack) {
		t.Fatal("uniform near-black secure frame must be treated as blank")
	}

	darkWithClock := make([]byte, 320*180*4)
	for i := 0; i < len(darkWithClock); i += 4 {
		darkWithClock[i+3] = 255
	}
	// A small but meaningful bright region represents clock/password controls.
	for y := 40; y < 70; y++ {
		for x := 80; x < 180; x++ {
			i := (y*320 + x) * 4
			darkWithClock[i] = 220
			darkWithClock[i+1] = 220
			darkWithClock[i+2] = 220
		}
	}
	if secureFrameLooksBlank(darkWithClock) {
		t.Fatal("dark lock screen with visible UI must not be treated as blank")
	}
}

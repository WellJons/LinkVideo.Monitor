//go:build darwin

package main

import "testing"

func TestDarwinSessionWatcherReportsOnlyTransitions(t *testing.T) {
	states := []struct {
		locked bool
		known  bool
	}{
		{false, true},
		{true, true},
		{true, true},
		{false, true},
		{true, false},
	}
	index := 0
	watcher := &darwinSessionStateWatcher{probe: func() (bool, bool) {
		state := states[index]
		index++
		return state.locked, state.known
	}}
	var got []bool
	for range states {
		watcher.check(func(locked bool) { got = append(got, locked) })
	}
	if len(got) != 2 || !got[0] || got[1] {
		t.Fatalf("transitions=%v; want [true false]", got)
	}
}

func TestDarwinDisplayOffFrameUsesBundledArtwork(t *testing.T) {
	frame := makeSessionLockedFrame(320, 180)
	if len(frame) != 320*180*4 {
		t.Fatalf("frame bytes=%d", len(frame))
	}
	first := frame[0]
	varied := false
	for i := 4; i < len(frame); i += 4 {
		if frame[i] != first {
			varied = true
			break
		}
	}
	if !varied {
		t.Fatal("expected bundled display-off artwork, got a uniform frame")
	}
}

//go:build darwin

package main

import "testing"

func TestDarwinDisplayWatcherReportsOnlyTransitions(t *testing.T) {
	states := []struct {
		off   bool
		known bool
	}{
		{false, true},
		{true, true},
		{true, true},
		{false, true},
		{true, false},
	}
	index := 0
	watcher := &darwinDisplayPowerStateWatcher{probe: func() (bool, bool) {
		state := states[index]
		index++
		return state.off, state.known
	}}
	var got []bool
	for range states {
		watcher.check(func(off bool) { got = append(got, off) })
	}
	if len(got) != 2 || !got[0] || got[1] {
		t.Fatalf("transitions=%v; want [true false]", got)
	}
}

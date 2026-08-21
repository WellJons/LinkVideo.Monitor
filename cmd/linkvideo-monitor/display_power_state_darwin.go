//go:build darwin

package main

/*
#cgo LDFLAGS: -framework CoreGraphics
#include <CoreGraphics/CoreGraphics.h>
#include <stdlib.h>

static int lv_query_all_displays_asleep(int *known) {
    *known = 0;
    uint32_t count = 0;
    if (CGGetOnlineDisplayList(0, NULL, &count) != kCGErrorSuccess || count == 0) {
        return 0;
    }

    CGDirectDisplayID *displays = (CGDirectDisplayID *)calloc(count, sizeof(CGDirectDisplayID));
    if (displays == NULL) {
        return 0;
    }
    uint32_t actual = count;
    CGError result = CGGetOnlineDisplayList(count, displays, &actual);
    if (result != kCGErrorSuccess || actual == 0) {
        free(displays);
        return 0;
    }

    int allAsleep = 1;
    for (uint32_t i = 0; i < actual; i++) {
        if (!CGDisplayIsAsleep(displays[i])) {
            allAsleep = 0;
            break;
        }
    }
    free(displays);
    *known = 1;
    return allAsleep;
}
*/
import "C"

import (
	"context"
	"sync/atomic"
	"time"
)

type darwinDisplayPowerStateWatcher struct {
	off   atomic.Bool
	probe func() (bool, bool)
}

func newDisplayPowerStateWatcher() displayPowerStateWatcher {
	return &darwinDisplayPowerStateWatcher{probe: queryDarwinDisplaysAsleep}
}

func (w *darwinDisplayPowerStateWatcher) check(changed func(bool)) {
	off, known := w.probe()
	if !known {
		return
	}
	old := w.off.Swap(off)
	if old != off {
		changed(off)
	}
}

func (w *darwinDisplayPowerStateWatcher) Run(ctx context.Context, changed func(bool)) {
	w.check(changed)
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.check(changed)
		}
	}
}

func queryDarwinDisplaysAsleep() (bool, bool) {
	var known C.int
	asleep := C.lv_query_all_displays_asleep(&known)
	return asleep != 0, known != 0
}

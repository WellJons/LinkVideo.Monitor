//go:build darwin

package main

/*
#cgo LDFLAGS: -framework CoreGraphics -framework CoreFoundation
#include <CoreGraphics/CoreGraphics.h>
#include <CoreGraphics/CGSession.h>
#include <CoreFoundation/CoreFoundation.h>

static int lv_session_bool(CFDictionaryRef dictionary, CFStringRef key, int *present) {
    const void *value = CFDictionaryGetValue(dictionary, key);
    if (value == NULL || CFGetTypeID(value) != CFBooleanGetTypeID()) {
        *present = 0;
        return 0;
    }
    *present = 1;
    return CFBooleanGetValue((CFBooleanRef)value) ? 1 : 0;
}

static int lv_query_session_locked(int *known) {
    *known = 0;
    CFDictionaryRef session = CGSessionCopyCurrentDictionary();
    if (session == NULL) {
        // No Quartz GUI session is safer to treat as protected than as a live desktop.
        *known = 1;
        return 1;
    }

    int loginPresent = 0;
    int consolePresent = 0;
    int lockPresent = 0;
    int loginDone = lv_session_bool(session, kCGSessionLoginDoneKey, &loginPresent);
    int onConsole = lv_session_bool(session, kCGSessionOnConsoleKey, &consolePresent);

    // Apple documents login/on-console state but does not expose a public lock-key
    // constant. WindowServer has provided this boolean in the session dictionary for
    // many macOS releases; use it only as an additional compatibility signal.
    CFStringRef lockKey = CFSTR("CGSSessionScreenIsLocked");
    int screenLocked = lv_session_bool(session, lockKey, &lockPresent);
    CFRelease(session);

    *known = 1;
    if (!loginPresent || !consolePresent) {
        return 1;
    }
    if (!loginDone || !onConsole) {
        return 1;
    }
    return lockPresent && screenLocked ? 1 : 0;
}
*/
import "C"

import (
	"bytes"
	"context"
	_ "embed"
	"image/color"
	"image/png"
	"sync/atomic"
	"time"
)

type darwinSessionStateWatcher struct {
	locked atomic.Bool
	probe  func() (bool, bool)
}

func newSessionStateWatcher() sessionStateWatcher {
	return &darwinSessionStateWatcher{probe: queryDarwinSessionLocked}
}

func (w *darwinSessionStateWatcher) check(changed func(bool)) {
	locked, known := w.probe()
	if !known {
		return
	}
	old := w.locked.Swap(locked)
	if old != locked {
		changed(locked)
	}
}

func (w *darwinSessionStateWatcher) Run(ctx context.Context, changed func(bool)) {
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

func queryDarwinSessionLocked() (bool, bool) {
	var known C.int
	locked := C.lv_query_session_locked(&known)
	return locked != 0, known != 0
}

//go:embed linkvideo_display_off.png
var darwinDisplayOffPNG []byte

func makeSessionLockedFrame(width, height int) []byte {
	if frame := makeDarwinStatusPNGFrame(width, height, darwinDisplayOffPNG); len(frame) == width*height*4 {
		return frame
	}
	return makeFallbackStatusFrame(width, height)
}

// A stale ScreenCaptureKit stream after loginwindow takes over must never expose
// the pre-lock desktop. Keep this neutral; the branded image is reserved for real
// display sleep, matching the existing product behaviour.
func makeProtectedDesktopFrame(width, height int) []byte {
	return makeFallbackStatusFrame(width, height)
}

func makeDarwinStatusPNGFrame(width, height int, pngData []byte) []byte {
	if width < 1 || height < 1 || len(pngData) == 0 {
		return nil
	}
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return nil
	}
	bounds := img.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	if srcW < 1 || srcH < 1 {
		return nil
	}

	cropX, cropY, cropW, cropH := 0, 0, srcW, srcH
	if srcW*height > srcH*width {
		cropW = srcH * width / height
		cropX = (srcW - cropW) / 2
	} else if srcW*height < srcH*width {
		cropH = srcW * height / width
		cropY = (srcH - cropH) / 2
	}

	frame := make([]byte, width*height*4)
	for y := 0; y < height; y++ {
		sy := bounds.Min.Y + cropY + y*cropH/height
		for x := 0; x < width; x++ {
			sx := bounds.Min.X + cropX + x*cropW/width
			pixel := color.NRGBAModel.Convert(img.At(sx, sy)).(color.NRGBA)
			offset := (y*width + x) * 4
			frame[offset+0] = pixel.B
			frame[offset+1] = pixel.G
			frame[offset+2] = pixel.R
			frame[offset+3] = 255
		}
	}
	return frame
}

//go:build darwin

package main

/*
#cgo LDFLAGS: -framework ApplicationServices -framework AppKit -framework Foundation
#include "privacy_darwin.h"
*/
import "C"

import (
	"context"
	"math"
	"strings"
	"sync"
	"time"
)

const (
	macOSPrivacyPollInterval = 60 * time.Millisecond
	macOSPrivacyHoldDuration = 1500 * time.Millisecond
	macOSPrivacyExpiry       = 2 * time.Minute
	macOSPrivacyMaxFields    = 12
)

type macOSPrivacyElement struct {
	handle    uintptr
	signature string
	rect      privacyScreenRect
	expires   time.Time
	holdUntil time.Time
}

type macOSPrivacyTracker struct {
	mu       sync.RWMutex
	elements []macOSPrivacyElement
	displays []macOSDisplayInfo
}

type macOSPrivacySample struct {
	handle      uintptr
	secure      bool
	editable    bool
	enabled     bool
	focused     bool
	offscreen   bool
	x           float64
	y           float64
	width       float64
	height      float64
	name        string
	identifier  string
	role        string
	subrole     string
	help        string
	process     string
	windowTitle string
	domClass    string
	ariaRole    string
	ariaProps   string
}

func newPrivacyTracker() privacyTracker {
	displays, _ := macOSDisplayInfos()
	return &macOSPrivacyTracker{displays: displays}
}

func (t *macOSPrivacyTracker) Run(ctx context.Context) {
	defer t.releaseAll()

	// Prompt once when the user explicitly enabled privacy protection. Apple's
	// Accessibility prompt is asynchronous, so continue polling and activate
	// automatically after access is granted to LinkVideo.Monitor.app.
	_ = C.lv_privacy_is_trusted(1)

	ticker := time.NewTicker(macOSPrivacyPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if C.lv_privacy_is_trusted(0) == 0 {
				t.clearTracked()
				continue
			}
			t.sample()
		}
	}
}

func (t *macOSPrivacyTracker) sample() {
	t.refreshTracked()

	var native C.LVPrivacySample
	if C.lv_privacy_copy_focused(&native) == 0 {
		return
	}
	defer C.lv_privacy_free_sample(&native)

	sample := macOSPrivacySampleFromC(&native)
	if sample.handle == 0 {
		return
	}
	adopted := false
	defer func() {
		if !adopted {
			C.lv_privacy_release(C.uintptr_t(sample.handle))
		}
	}()

	if !sample.enabled || !sample.focused || sample.offscreen || !sample.editable || !macOSPrivacySampleSensitive(sample) {
		return
	}
	rect, ok := t.captureRect(sample.x, sample.y, sample.width, sample.height)
	if !ok {
		return
	}
	meta := sample.metadata()
	signature := strings.Join([]string{
		strings.ToLower(meta.ProcessName),
		normalizePrivacyText(meta.AutomationID),
		normalizePrivacyText(sample.role),
		normalizePrivacyText(sample.subrole),
		normalizePrivacyText(meta.Name),
	}, "|")
	adopted = t.remember(sample.handle, signature, rect)
}

func (sample macOSPrivacySample) metadata() privacyElementMetadata {
	return privacyElementMetadata{
		Name:         sample.name,
		AutomationID: sample.identifier,
		ClassName:    strings.TrimSpace(strings.Join([]string{sample.domClass, sample.role, sample.subrole}, " ")),
		HelpText:     sample.help,
		AriaRole:     sample.ariaRole,
		AriaProps:    sample.ariaProps,
		ProcessName:  sample.process,
		WindowTitle:  sample.windowTitle,
	}
}

func macOSPrivacySampleSensitive(sample macOSPrivacySample) bool {
	return sample.secure || privacyMetadataIsSensitive(sample.metadata())
}

func macOSPrivacySampleFromC(sample *C.LVPrivacySample) macOSPrivacySample {
	if sample == nil || sample.valid == 0 {
		return macOSPrivacySample{}
	}
	return macOSPrivacySample{
		handle:      uintptr(sample.handle),
		secure:      sample.secure != 0,
		editable:    sample.editable != 0,
		enabled:     sample.enabled != 0,
		focused:     sample.focused != 0,
		offscreen:   sample.offscreen != 0,
		x:           float64(sample.x),
		y:           float64(sample.y),
		width:       float64(sample.width),
		height:      float64(sample.height),
		name:        cString(sample.name),
		identifier:  cString(sample.identifier),
		role:        cString(sample.role),
		subrole:     cString(sample.subrole),
		help:        cString(sample.help),
		process:     cString(sample.process),
		windowTitle: cString(sample.window_title),
		domClass:    cString(sample.dom_class),
		ariaRole:    cString(sample.aria_role),
		ariaProps:   cString(sample.aria_props),
	}
}

func cString(value *C.char) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(C.GoString(value))
}

func (t *macOSPrivacyTracker) refreshTracked() {
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()

	keep := t.elements[:0]
	for _, item := range t.elements {
		if item.handle == 0 || !item.expires.After(now) {
			if item.handle != 0 {
				C.lv_privacy_release(C.uintptr_t(item.handle))
			}
			continue
		}

		var geometry C.LVPrivacyGeometry
		valid := C.lv_privacy_refresh(C.uintptr_t(item.handle), &geometry) != 0 && geometry.valid != 0
		rect, rectOK := t.captureRect(
			float64(geometry.x), float64(geometry.y),
			float64(geometry.width), float64(geometry.height),
		)
		if !valid || geometry.enabled == 0 || geometry.offscreen != 0 || !rectOK {
			// Keep the last confirmed rectangle very briefly while a browser
			// rebuilds its accessibility tree during navigation/reload.
			if item.holdUntil.After(now) {
				item.expires = item.holdUntil
				keep = append(keep, item)
				continue
			}
			C.lv_privacy_release(C.uintptr_t(item.handle))
			continue
		}
		if rectDistance(item.rect, rect) <= 3 {
			rect = item.rect
		}
		item.rect = rect
		item.expires = now.Add(macOSPrivacyExpiry)
		item.holdUntil = now.Add(macOSPrivacyHoldDuration)
		keep = append(keep, item)
	}
	t.elements = keep
}

// remember adopts the incoming native retain when it returns true. When the
// same AX object is already tracked it returns false so the caller releases the
// extra retain obtained from AXFocusedUIElement.
func (t *macOSPrivacyTracker) remember(handle uintptr, signature string, rect privacyScreenRect) bool {
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()

	for i := range t.elements {
		item := &t.elements[i]
		if item.signature != signature || rectDistance(item.rect, rect) > 24 {
			continue
		}
		if rectDistance(item.rect, rect) <= 3 {
			rect = item.rect
		}
		adopted := false
		if item.handle != handle {
			C.lv_privacy_release(C.uintptr_t(item.handle))
			item.handle = handle
			adopted = true
		}
		item.rect = rect
		item.expires = now.Add(macOSPrivacyExpiry)
		item.holdUntil = now.Add(macOSPrivacyHoldDuration)
		return adopted
	}

	if len(t.elements) >= macOSPrivacyMaxFields {
		C.lv_privacy_release(C.uintptr_t(t.elements[0].handle))
		t.elements = t.elements[1:]
	}
	t.elements = append(t.elements, macOSPrivacyElement{
		handle: handle, signature: signature, rect: rect,
		expires: now.Add(macOSPrivacyExpiry), holdUntil: now.Add(macOSPrivacyHoldDuration),
	})
	return true
}

func (t *macOSPrivacyTracker) Regions() []privacyScreenRect {
	now := time.Now()
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]privacyScreenRect, 0, len(t.elements))
	for _, item := range t.elements {
		if item.expires.After(now) {
			result = append(result, item.rect)
		}
	}
	return result
}

func (t *macOSPrivacyTracker) clearTracked() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.elements) == 0 {
		return
	}
	for _, item := range t.elements {
		if item.handle != 0 {
			C.lv_privacy_release(C.uintptr_t(item.handle))
		}
	}
	t.elements = nil
}

func (t *macOSPrivacyTracker) releaseAll() {
	t.clearTracked()
}

// captureRect converts Accessibility point coordinates to the same coordinate
// space used by capturePlan. Multi-display plans already use ScreenCaptureKit
// points. A one-display Retina Mac deliberately keeps native capture pixels, so
// its AX geometry is scaled by the physical/logical display ratio first.
func (t *macOSPrivacyTracker) captureRect(x, y, width, height float64) (privacyScreenRect, bool) {
	if width < 4 || height < 4 || math.IsNaN(x) || math.IsNaN(y) ||
		math.IsInf(x, 0) || math.IsInf(y, 0) {
		return privacyScreenRect{}, false
	}
	if len(t.displays) == 1 {
		d := t.displays[0]
		if d.WidthPoints > 0 && d.HeightPoints > 0 && d.WidthPixels > 0 && d.HeightPixels > 0 {
			sx := float64(d.WidthPixels) / float64(d.WidthPoints)
			sy := float64(d.HeightPixels) / float64(d.HeightPoints)
			x = d.X + (x-d.X)*sx
			y = d.Y + (y-d.Y)*sy
			width *= sx
			height *= sy
		}
	}
	const expand = 8.0
	return privacyScreenRect{
		Left:   int(math.Floor(x - expand)),
		Top:    int(math.Floor(y - expand)),
		Right:  int(math.Ceil(x + width + expand)),
		Bottom: int(math.Ceil(y + height + expand)),
	}, true
}

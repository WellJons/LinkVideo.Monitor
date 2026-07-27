//go:build windows

package main

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	coinitApartmentThreaded = 0x2
	clsctxInprocServer      = 0x1
	uiaEditControlTypeID    = 50004
	gaRoot                  = 2
)

var (
	ole32Privacy                = syscall.NewLazyDLL("ole32.dll")
	oleaut32Privacy             = syscall.NewLazyDLL("oleaut32.dll")
	procPrivacyCoInitializeEx   = ole32Privacy.NewProc("CoInitializeEx")
	procPrivacyCoUninitialize   = ole32Privacy.NewProc("CoUninitialize")
	procPrivacyCoCreateInstance = ole32Privacy.NewProc("CoCreateInstance")
	procPrivacySysStringLen     = oleaut32Privacy.NewProc("SysStringLen")
	procPrivacySysFreeString    = oleaut32Privacy.NewProc("SysFreeString")
	procGetForegroundWindow     = user32.NewProc("GetForegroundWindow")
	procGetAncestor             = user32.NewProc("GetAncestor")
	procIsWindow                = user32.NewProc("IsWindow")

	clsidCUIAutomation = winGUID{0xff48dba4, 0x60ef, 0x4201, [8]byte{0xaa, 0x87, 0x54, 0x10, 0x3e, 0xef, 0x59, 0x4e}}
	iidIUIAutomation   = winGUID{0x30cbe57d, 0xd9d0, 0x452a, [8]byte{0xab, 0x13, 0x7a, 0xc5, 0xac, 0x48, 0x25, 0xee}}
)

type privacyTrackedElement struct {
	HWND      uintptr
	Title     string
	Signature string
	Element   uintptr
	Rect      privacyScreenRect
	Expires   time.Time
}

type windowsPrivacyTracker struct {
	mu       sync.RWMutex
	elements []privacyTrackedElement
}

func newPrivacyTracker() privacyTracker {
	return &windowsPrivacyTracker{}
}

func (t *windowsPrivacyTracker) Run(ctx context.Context) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer t.releaseAll()

	hr, _, _ := procPrivacyCoInitializeEx.Call(0, coinitApartmentThreaded)
	initialized := int32(hr) >= 0
	if initialized {
		defer procPrivacyCoUninitialize.Call()
	}

	var automation uintptr
	hr, _, _ = procPrivacyCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidCUIAutomation)),
		0,
		clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIUIAutomation)),
		uintptr(unsafe.Pointer(&automation)),
	)
	if int32(hr) < 0 || automation == 0 {
		return
	}
	defer comRelease(automation)

	// The privacy worker is intentionally independent from video capture. It
	// inspects only the focused input and refreshes already-confirmed fields.
	ticker := time.NewTicker(120 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.sample(automation)
		}
	}
}

func (t *windowsPrivacyTracker) sample(automation uintptr) {
	// Refresh known fields first. Their real UI Automation rectangles move with
	// browser scrolling, so masks never remain at stale screen coordinates.
	t.refreshTracked()

	getFocused := comVTableMethod(automation, 8)
	if getFocused == 0 {
		return
	}
	var element uintptr
	hr, _, _ := syscall.SyscallN(getFocused, automation, uintptr(unsafe.Pointer(&element)))
	if int32(hr) < 0 || element == 0 {
		return
	}
	defer comRelease(element)

	controlType := uiaCurrentInt32(element, 21)
	isPassword := uiaCurrentInt32(element, 35)
	hasKeyboardFocus := uiaCurrentInt32(element, 26)
	isKeyboardFocusable := uiaCurrentInt32(element, 27)
	isEnabled := uiaCurrentInt32(element, 28)
	isOffscreen := uiaCurrentInt32(element, 38)

	// Password fields are accepted by the native IsPassword flag. Heuristic
	// categories (OTP/CVV/card/PIN/etc.) are accepted only for the actual,
	// focused, enabled Edit control. Page text, tabs and document nodes are never
	// candidates.
	if hasKeyboardFocus == 0 || isKeyboardFocusable == 0 || isEnabled == 0 || isOffscreen != 0 ||
		(isPassword == 0 && controlType != uiaEditControlTypeID) {
		return
	}

	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return
	}
	if root, _, _ := procGetAncestor.Call(hwnd, gaRoot); root != 0 {
		hwnd = root
	}
	var pid uint32
	_, _, _ = procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	meta := privacyElementMetadata{
		Name:         uiaCurrentString(element, 23),
		AutomationID: uiaCurrentString(element, 29),
		ClassName:    uiaCurrentString(element, 30),
		HelpText:     uiaCurrentString(element, 31),
		AriaRole:     uiaCurrentString(element, 45),
		AriaProps:    uiaCurrentString(element, 46),
		ProcessName:  processImageName(pid),
		WindowTitle:  privacyWindowTitle(hwnd),
	}
	if isPassword == 0 && !privacyMetadataIsSensitive(meta) {
		return
	}

	rect, ok := privacyElementRect(element)
	if !ok {
		return
	}
	signature := strings.Join([]string{
		strings.ToLower(meta.ProcessName), normalizePrivacyText(meta.AutomationID),
		normalizePrivacyText(meta.ClassName), normalizePrivacyText(meta.Name),
	}, "|")
	t.remember(hwnd, meta.WindowTitle, signature, element, rect)
}

func (t *windowsPrivacyTracker) refreshTracked() {
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()

	keep := t.elements[:0]
	for _, item := range t.elements {
		valid, _, _ := procIsWindow.Call(item.HWND)
		if valid == 0 || !item.Expires.After(now) || item.Element == 0 {
			comRelease(item.Element)
			continue
		}
		if title := privacyWindowTitle(item.HWND); item.Title != "" && title != "" && title != item.Title {
			comRelease(item.Element)
			continue
		}
		if uiaCurrentInt32(item.Element, 38) != 0 {
			comRelease(item.Element)
			continue
		}
		rect, ok := privacyElementRect(item.Element)
		if !ok {
			comRelease(item.Element)
			continue
		}
		if rectDistance(item.Rect, rect) <= 3 {
			rect = item.Rect
		}
		item.Rect = rect
		// A valid, visible field stays protected while it exists. This keeps CVV,
		// card and OTP fields covered after focus moves, without fixed stale masks.
		item.Expires = now.Add(2 * time.Minute)
		keep = append(keep, item)
	}
	t.elements = keep
}

func privacyElementRect(element uintptr) (privacyScreenRect, bool) {
	var rect winRect
	method := comVTableMethod(element, 43)
	if method == 0 {
		return privacyScreenRect{}, false
	}
	hr, _, _ := syscall.SyscallN(method, element, uintptr(unsafe.Pointer(&rect)))
	if int32(hr) < 0 || rect.Right-rect.Left < 4 || rect.Bottom-rect.Top < 4 {
		return privacyScreenRect{}, false
	}
	expand := int32(8)
	return privacyScreenRect{
		Left:   int(rect.Left - expand),
		Top:    int(rect.Top - expand),
		Right:  int(rect.Right + expand),
		Bottom: int(rect.Bottom + expand),
	}, true
}

func uiaCurrentInt32(element uintptr, methodIndex int) int32 {
	method := comVTableMethod(element, methodIndex)
	if method == 0 {
		return 0
	}
	var value int32
	_, _, _ = syscall.SyscallN(method, element, uintptr(unsafe.Pointer(&value)))
	return value
}

func uiaCurrentString(element uintptr, methodIndex int) string {
	method := comVTableMethod(element, methodIndex)
	if method == 0 {
		return ""
	}
	var bstr uintptr
	hr, _, _ := syscall.SyscallN(method, element, uintptr(unsafe.Pointer(&bstr)))
	if int32(hr) < 0 || bstr == 0 {
		return ""
	}
	defer procPrivacySysFreeString.Call(bstr)
	n, _, _ := procPrivacySysStringLen.Call(bstr)
	if n == 0 || n > 16384 {
		return ""
	}
	chars := unsafe.Slice((*uint16)(unsafe.Pointer(bstr)), int(n))
	return strings.TrimSpace(syscall.UTF16ToString(chars))
}

func privacyWindowTitle(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, int(n)+1)
	got, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if got == 0 {
		return ""
	}
	return strings.TrimSpace(syscall.UTF16ToString(buf[:got]))
}

func (t *windowsPrivacyTracker) remember(hwnd uintptr, title, signature string, element uintptr, rect privacyScreenRect) {
	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()

	for i := range t.elements {
		item := &t.elements[i]
		if item.HWND != hwnd || item.Signature != signature || rectDistance(item.Rect, rect) > 24 {
			continue
		}
		if rectDistance(item.Rect, rect) <= 3 {
			rect = item.Rect
		}
		item.Rect = rect
		item.Title = title
		item.Expires = now.Add(2 * time.Minute)
		return
	}

	comAddRef(element)
	if len(t.elements) >= 12 {
		comRelease(t.elements[0].Element)
		t.elements = t.elements[1:]
	}
	t.elements = append(t.elements, privacyTrackedElement{
		HWND: hwnd, Title: title, Signature: signature, Element: element,
		Rect: rect, Expires: now.Add(2 * time.Minute),
	})
}

func (t *windowsPrivacyTracker) Regions() []privacyScreenRect {
	now := time.Now()
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]privacyScreenRect, 0, len(t.elements))
	for _, item := range t.elements {
		if item.Expires.After(now) {
			result = append(result, item.Rect)
		}
	}
	return result
}

func (t *windowsPrivacyTracker) releaseAll() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, item := range t.elements {
		comRelease(item.Element)
	}
	t.elements = nil
}

func comAddRef(object uintptr) {
	if object == 0 {
		return
	}
	if method := comVTableMethod(object, 1); method != 0 {
		_, _, _ = syscall.SyscallN(method, object)
	}
}

func rectDistance(a, b privacyScreenRect) int {
	d := absInt(a.Left - b.Left)
	if v := absInt(a.Top - b.Top); v > d {
		d = v
	}
	if v := absInt(a.Right - b.Right); v > d {
		d = v
	}
	if v := absInt(a.Bottom - b.Bottom); v > d {
		d = v
	}
	return d
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

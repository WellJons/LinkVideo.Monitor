//go:build windows

package main

import (
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

const (
	whKeyboardLL    = 13
	micWMKeyDown    = 0x0100
	micWMKeyUp      = 0x0101
	micWMSysKeyDown = 0x0104
	micWMSysKeyUp   = 0x0105
)

type keyboardHookStruct struct {
	VKCode    uint32
	ScanCode  uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type winMessage struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
	Private uint32
}

type parsedHotkey struct {
	ctrl, alt, shift, win bool
	key                   uint32
}

var (
	procSetWindowsHookExW = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx    = user32.NewProc("CallNextHookEx")
	procMicGetMessageW    = user32.NewProc("GetMessageW")
	procUnhookWindowsHook = user32.NewProc("UnhookWindowsHookEx")
	micHookOnce           sync.Once
	micHookState          struct {
		sync.Mutex
		app  *app
		down map[uint32]bool
	}
)

func startMicrophoneHotkeys(a *app) {
	micHookOnce.Do(func() {
		micHookState.app = a
		micHookState.down = make(map[uint32]bool)
		go microphoneHookLoop()
	})
}

func microphoneHookLoop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	callback := syscall.NewCallback(microphoneKeyboardProc)
	hook, _, _ := procSetWindowsHookExW.Call(whKeyboardLL, callback, 0, 0)
	if hook == 0 {
		return
	}
	defer procUnhookWindowsHook.Call(hook)
	var msg winMessage
	for {
		r, _, _ := procMicGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
	}
}

func microphoneKeyboardProc(code int, wParam uintptr, lParam uintptr) uintptr {
	if code >= 0 && lParam != 0 {
		k := (*keyboardHookStruct)(unsafe.Pointer(lParam))
		downEvent := wParam == micWMKeyDown || wParam == micWMSysKeyDown
		upEvent := wParam == micWMKeyUp || wParam == micWMSysKeyUp
		if downEvent || upEvent {
			micHookState.Lock()
			wasDown := micHookState.down[k.VKCode]
			if downEvent {
				micHookState.down[k.VKCode] = true
			} else {
				delete(micHookState.down, k.VKCode)
			}
			a := micHookState.app
			down := cloneKeyState(micHookState.down)
			micHookState.Unlock()
			if a != nil {
				a.mu.Lock()
				enabled := a.cfg.MicrophoneEnabled
				mode := a.cfg.MicrophoneMode
				toggleSpec := a.cfg.MicrophoneToggleHotkey
				pttSpec := a.cfg.MicrophonePTTHotkey
				a.mu.Unlock()
				if enabled {
					toggle := parseHotkey(toggleSpec)
					ptt := parseHotkey(pttSpec)
					togglePressed := downEvent && !wasDown && k.VKCode == toggle.key && hotkeyModifiersDown(toggle, down)
					pttActive := mode == "push_to_talk" && hotkeyDown(ptt, down)
					a.applyMicrophoneHotkey(togglePressed, pttActive)
				}
			}
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, uintptr(code), wParam, lParam)
	return r
}

func cloneKeyState(in map[uint32]bool) map[uint32]bool {
	out := make(map[uint32]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (a *app) applyMicrophoneHotkey(toggle, ptt bool) {
	var message string
	a.mu.Lock()
	if toggle {
		a.microphoneMuted = !a.microphoneMuted
		if a.microphoneMuted {
			message = "Микрофон выключен горячей клавишей"
		} else {
			message = "Микрофон включён горячей клавишей"
		}
	}
	a.microphonePTTActive = ptt
	a.mu.Unlock()
	if message != "" {
		a.appendLog(message)
	}
}

func hotkeyDown(h parsedHotkey, down map[uint32]bool) bool {
	return h.key != 0 && keyIsDown(h.key, down) && hotkeyModifiersDown(h, down)
}

func hotkeyModifiersDown(h parsedHotkey, down map[uint32]bool) bool {
	return (!h.ctrl || modifierDown(0x11, 0xA2, 0xA3, down)) &&
		(!h.alt || modifierDown(0x12, 0xA4, 0xA5, down)) &&
		(!h.shift || modifierDown(0x10, 0xA0, 0xA1, down)) &&
		(!h.win || modifierDown(0x5B, 0x5B, 0x5C, down))
}

func modifierDown(generic, left, right uint32, down map[uint32]bool) bool {
	return down[generic] || down[left] || down[right]
}
func keyIsDown(key uint32, down map[uint32]bool) bool { return down[key] }

func parseHotkey(raw string) parsedHotkey {
	var h parsedHotkey
	parts := strings.Split(raw, "+")
	for _, p := range parts {
		p = strings.ToUpper(strings.TrimSpace(p))
		switch p {
		case "CTRL", "CONTROL":
			h.ctrl = true
		case "ALT":
			h.alt = true
		case "SHIFT":
			h.shift = true
		case "WIN", "WINDOWS":
			h.win = true
		default:
			if vk := hotkeyVK(p); vk != 0 {
				h.key = vk
			}
		}
	}
	return h
}

func hotkeyVK(s string) uint32 {
	if len(s) == 1 {
		c := s[0]
		if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			return uint32(c)
		}
	}
	if strings.HasPrefix(s, "F") {
		n, err := strconv.Atoi(s[1:])
		if err == nil && n >= 1 && n <= 24 {
			return uint32(0x70 + n - 1)
		}
	}
	switch s {
	case "SPACE", "ПРОБЕЛ":
		return 0x20
	case "ENTER":
		return 0x0D
	case "TAB":
		return 0x09
	case "ESC", "ESCAPE":
		return 0x1B
	case "BACKSPACE":
		return 0x08
	case "DELETE", "DEL":
		return 0x2E
	case "INSERT", "INS":
		return 0x2D
	case "HOME":
		return 0x24
	case "END":
		return 0x23
	case "PAGEUP":
		return 0x21
	case "PAGEDOWN":
		return 0x22
	case "UP":
		return 0x26
	case "DOWN":
		return 0x28
	case "LEFT":
		return 0x25
	case "RIGHT":
		return 0x27
	}
	return 0
}

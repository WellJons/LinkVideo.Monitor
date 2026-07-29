//go:build windows

package main

import (
	"context"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

const (
	wmPowerBroadcast         = 0x0218
	pbtPowerSettingChange    = 0x8013
	deviceNotifyWindowHandle = 0
)

var (
	procRegisterPowerSettingNotification   = user32.NewProc("RegisterPowerSettingNotification")
	procUnregisterPowerSettingNotification = user32.NewProc("UnregisterPowerSettingNotification")
	procPostMessageW                       = user32.NewProc("PostMessageW")

	guidConsoleDisplayState = winGUID{0x6fe69556, 0x704a, 0x47a0, [8]byte{0x8f, 0x24, 0xc2, 0x8d, 0x93, 0x6f, 0xda, 0x47}}
	guidMonitorPowerOn      = winGUID{0x02731015, 0x4510, 0x4526, [8]byte{0x99, 0xe6, 0xe5, 0xa1, 0x7e, 0xbd, 0x1a, 0xea}}

	displayPowerWindows sync.Map // map[uintptr]*windowsDisplayPowerStateWatcher
)

type powerBroadcastSetting struct {
	PowerSetting winGUID
	DataLength   uint32
	Data         [1]byte
}

type windowsDisplayPowerStateWatcher struct {
	mu           sync.Mutex
	changed      func(bool)
	lastKnown    bool
	lastOff      bool
	consoleKnown bool
}

func newDisplayPowerStateWatcher() displayPowerStateWatcher {
	return &windowsDisplayPowerStateWatcher{}
}

func (w *windowsDisplayPowerStateWatcher) Run(ctx context.Context, changed func(bool)) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	w.mu.Lock()
	w.changed = changed
	w.mu.Unlock()

	className, _ := syscall.UTF16PtrFromString("LinkVideoMonitorDisplayPowerState")
	windowName, _ := syscall.UTF16PtrFromString("LinkVideo Monitor display power state")
	hinst, _, _ := procGetModuleHandleW.Call(0)
	wc := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   syscall.NewCallback(displayPowerWndProc),
		Instance:  hinst,
		ClassName: className,
	}
	_, _, _ = procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		0,
		0, 0, 0, 0,
		0, 0, hinst, 0,
	)
	if hwnd == 0 {
		<-ctx.Done()
		return
	}
	displayPowerWindows.Store(hwnd, w)
	defer displayPowerWindows.Delete(hwnd)

	consoleNotify, _, _ := procRegisterPowerSettingNotification.Call(
		hwnd,
		uintptr(unsafe.Pointer(&guidConsoleDisplayState)),
		deviceNotifyWindowHandle,
	)
	monitorNotify, _, _ := procRegisterPowerSettingNotification.Call(
		hwnd,
		uintptr(unsafe.Pointer(&guidMonitorPowerOn)),
		deviceNotifyWindowHandle,
	)
	defer func() {
		if consoleNotify != 0 {
			procUnregisterPowerSettingNotification.Call(consoleNotify)
		}
		if monitorNotify != 0 {
			procUnregisterPowerSettingNotification.Call(monitorNotify)
		}
	}()

	go func() {
		<-ctx.Done()
		procPostMessageW.Call(hwnd, wmClose, 0, 0)
	}()

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func displayPowerWndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmPowerBroadcast:
		if wParam == pbtPowerSettingChange && lParam != 0 {
			if value, guid, ok := readDisplayPowerBroadcast(lParam); ok {
				if watcherValue, found := displayPowerWindows.Load(hwnd); found {
					watcherValue.(*windowsDisplayPowerStateWatcher).applyPowerSetting(guid, value)
				}
			}
		}
		return 1
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	result, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func readDisplayPowerBroadcast(ptr uintptr) (uint32, winGUID, bool) {
	setting := (*powerBroadcastSetting)(unsafe.Pointer(ptr))
	if setting == nil || setting.DataLength < 4 {
		return 0, winGUID{}, false
	}
	dataPtr := unsafe.Pointer(uintptr(ptr) + unsafe.Offsetof(setting.Data))
	return *(*uint32)(dataPtr), setting.PowerSetting, true
}

func (w *windowsDisplayPowerStateWatcher) applyPowerSetting(setting winGUID, value uint32) {
	w.mu.Lock()
	if setting == guidConsoleDisplayState {
		w.consoleKnown = true
	} else if setting == guidMonitorPowerOn {
		// On Windows 8+ the console display notification is more precise and can
		// report the dimmed state. Use the older Vista/Windows 7 notification only
		// until the console notification has been observed.
		if w.consoleKnown {
			w.mu.Unlock()
			return
		}
	} else {
		w.mu.Unlock()
		return
	}

	off := value == 0
	changed := !w.lastKnown || w.lastOff != off
	w.lastKnown = true
	w.lastOff = off
	callback := w.changed
	w.mu.Unlock()
	if changed && callback != nil {
		callback(off)
	}
}

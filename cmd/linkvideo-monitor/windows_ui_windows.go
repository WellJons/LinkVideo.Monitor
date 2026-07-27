//go:build windows

package main

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

const (
	csHRedraw = 0x0002
	csVRedraw = 0x0001

	wsPopup = 0x80000000

	wsExTopmost     = 0x00000008
	wsExTransparent = 0x00000020
	wsExToolWindow  = 0x00000080
	wsExLayered     = 0x00080000
	wsExNoActivate  = 0x08000000

	swShow = 5

	wmDestroy     = 0x0002
	wmPaint       = 0x000F
	wmClose       = 0x0010
	wmEraseBkgnd  = 0x0014
	wmKeyDown     = 0x0100
	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmNCHitTest   = 0x0084
	wmTimer       = 0x0113

	vkEscape = 0x1B

	htTransparent = -1

	colorWindow = 5
	nullBrush   = 5
	transparent = 1
	psSolid     = 0

	dtCenter     = 0x00000001
	dtVCenter    = 0x00000004
	dtSingleLine = 0x00000020
	dtNoPrefix   = 0x00000800

	lwaColorKey = 0x00000001
	lwaAlpha    = 0x00000002

	smCxScreen        = 0
	smCyScreen        = 1
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCxVirtualScreen = 78
	smCyVirtualScreen = 79

	spiGetWorkArea          = 0x0030
	monitorDefaultToNearest = 0x00000002

	idcArrow = 32512
	idcCross = 32515
)

type point struct{ X, Y int32 }
type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}
type rect32 struct{ Left, Top, Right, Bottom int32 }
type paintStruct struct {
	Hdc       uintptr
	Erase     int32
	Paint     rect32
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}
type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

var (
	gdi32 = syscall.NewLazyDLL("gdi32.dll")

	procGetModuleHandleW           = kernel32Platform.NewProc("GetModuleHandleW")
	procCreateMutexW               = kernel32Platform.NewProc("CreateMutexW")
	procRegisterClassExW           = user32.NewProc("RegisterClassExW")
	procCreateWindowExW            = user32.NewProc("CreateWindowExW")
	procShowWindow                 = user32.NewProc("ShowWindow")
	procUpdateWindow               = user32.NewProc("UpdateWindow")
	procGetMessageW                = user32.NewProc("GetMessageW")
	procTranslateMessage           = user32.NewProc("TranslateMessage")
	procDispatchMessageW           = user32.NewProc("DispatchMessageW")
	procDefWindowProcW             = user32.NewProc("DefWindowProcW")
	procPostQuitMessage            = user32.NewProc("PostQuitMessage")
	procDestroyWindow              = user32.NewProc("DestroyWindow")
	procLoadCursorW                = user32.NewProc("LoadCursorW")
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	procSetCapture                 = user32.NewProc("SetCapture")
	procReleaseCapture             = user32.NewProc("ReleaseCapture")
	procInvalidateRect             = user32.NewProc("InvalidateRect")
	procBeginPaint                 = user32.NewProc("BeginPaint")
	procEndPaint                   = user32.NewProc("EndPaint")
	procGetClientRect              = user32.NewProc("GetClientRect")
	procFillRect                   = user32.NewProc("FillRect")
	procGetSystemMetrics           = user32.NewProc("GetSystemMetrics")
	procSetWindowPos               = user32.NewProc("SetWindowPos")
	procSetFocus                   = user32.NewProc("SetFocus")
	procSetTimer                   = user32.NewProc("SetTimer")
	procKillTimer                  = user32.NewProc("KillTimer")
	procGetCursorPos               = user32.NewProc("GetCursorPos")
	procDrawTextW                  = user32.NewProc("DrawTextW")
	procSetWindowRgn               = user32.NewProc("SetWindowRgn")
	procSystemParametersInfoW      = user32.NewProc("SystemParametersInfoW")
	procMonitorFromRect            = user32.NewProc("MonitorFromRect")

	procCreatePen          = gdi32.NewProc("CreatePen")
	procCreateSolidBrush   = gdi32.NewProc("CreateSolidBrush")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procRectangle          = gdi32.NewProc("Rectangle")
	procSetBkMode          = gdi32.NewProc("SetBkMode")
	procSetTextColor       = gdi32.NewProc("SetTextColor")
	procTextOutW           = gdi32.NewProc("TextOutW")
	procGetStockObject     = gdi32.NewProc("GetStockObject")
	procCreateFontW        = gdi32.NewProc("CreateFontW")
	procEllipse            = gdi32.NewProc("Ellipse")
	procCreateRoundRectRgn = gdi32.NewProc("CreateRoundRectRgn")
)

func rgb(r, g, b byte) uintptr     { return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16 }
func signedWord(v uintptr) int     { return int(int16(v & 0xffff)) }
func signedHighWord(v uintptr) int { return int(int16((v >> 16) & 0xffff)) }

var regionState struct {
	dragging           bool
	startX, startY     int
	curX, curY         int
	virtualX, virtualY int
	result             Region
	cancelled          bool
}

func selectRegionInteractive() (Region, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	regionState = struct {
		dragging           bool
		startX, startY     int
		curX, curY         int
		virtualX, virtualY int
		result             Region
		cancelled          bool
	}{}
	regionState.virtualX = int(metric(smXVirtualScreen))
	regionState.virtualY = int(metric(smYVirtualScreen))
	width := int(metric(smCxVirtualScreen))
	height := int(metric(smCyVirtualScreen))
	if width <= 0 || height <= 0 {
		return Region{}, fmt.Errorf("не удалось определить размер рабочего стола")
	}

	className, _ := syscall.UTF16PtrFromString("LVScreenRegionSelector")
	title, _ := syscall.UTF16PtrFromString("Выбор области")
	hinst, _, _ := procGetModuleHandleW.Call(0)
	cursor, _, _ := procLoadCursorW.Call(0, idcCross)
	blackBrush, _, _ := procCreateSolidBrush.Call(rgb(0, 0, 0))
	wc := wndClassEx{
		Size:       uint32(unsafe.Sizeof(wndClassEx{})),
		Style:      csHRedraw | csVRedraw,
		WndProc:    syscall.NewCallback(regionWndProc),
		Instance:   hinst,
		Cursor:     cursor,
		Background: blackBrush,
		ClassName:  className,
	}
	atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		// Класс мог остаться зарегистрирован в процессе; создание окна всё равно может сработать.
		_ = err
	}
	hwnd, _, createErr := procCreateWindowExW.Call(
		wsExTopmost|wsExLayered|wsExToolWindow,
		uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)), wsPopup,
		uintptr(regionState.virtualX), uintptr(regionState.virtualY), uintptr(width), uintptr(height),
		0, 0, hinst, 0,
	)
	if hwnd == 0 {
		return Region{}, fmt.Errorf("не удалось открыть выбор области: %v", createErr)
	}
	_, _, _ = procSetLayeredWindowAttributes.Call(hwnd, 0, 95, lwaAlpha)
	_, _, _ = procShowWindow.Call(hwnd, swShow)
	_, _, _ = procUpdateWindow.Call(hwnd)
	_, _, _ = procSetFocus.Call(hwnd)

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	if regionState.cancelled || regionState.result.Width < 2 || regionState.result.Height < 2 {
		return Region{}, fmt.Errorf("выбор области отменён")
	}
	regionState.result.Width = even(regionState.result.Width)
	regionState.result.Height = even(regionState.result.Height)
	return regionState.result, nil
}

func regionWndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmLButtonDown:
		regionState.dragging = true
		regionState.startX, regionState.startY = signedWord(lParam), signedHighWord(lParam)
		regionState.curX, regionState.curY = regionState.startX, regionState.startY
		_, _, _ = procSetCapture.Call(hwnd)
		_, _, _ = procInvalidateRect.Call(hwnd, 0, 1)
		return 0
	case wmMouseMove:
		if regionState.dragging {
			regionState.curX, regionState.curY = signedWord(lParam), signedHighWord(lParam)
			_, _, _ = procInvalidateRect.Call(hwnd, 0, 1)
		}
		return 0
	case wmLButtonUp:
		if regionState.dragging {
			regionState.dragging = false
			regionState.curX, regionState.curY = signedWord(lParam), signedHighWord(lParam)
			_, _, _ = procReleaseCapture.Call()
			left, right := regionState.startX, regionState.curX
			top, bottom := regionState.startY, regionState.curY
			if left > right {
				left, right = right, left
			}
			if top > bottom {
				top, bottom = bottom, top
			}
			regionState.result = Region{X: regionState.virtualX + left, Y: regionState.virtualY + top, Width: right - left, Height: bottom - top}
			_, _, _ = procDestroyWindow.Call(hwnd)
		}
		return 0
	case wmKeyDown:
		if wParam == vkEscape {
			regionState.cancelled = true
			_, _, _ = procDestroyWindow.Call(hwnd)
			return 0
		}
	case wmPaint:
		var ps paintStruct
		hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		if regionState.dragging {
			left, right := regionState.startX, regionState.curX
			top, bottom := regionState.startY, regionState.curY
			if left > right {
				left, right = right, left
			}
			if top > bottom {
				top, bottom = bottom, top
			}
			pen, _, _ := procCreatePen.Call(psSolid, 3, rgb(255, 174, 26))
			oldPen, _, _ := procSelectObject.Call(hdc, pen)
			brush, _, _ := procGetStockObject.Call(nullBrush)
			oldBrush, _, _ := procSelectObject.Call(hdc, brush)
			_, _, _ = procRectangle.Call(hdc, uintptr(left), uintptr(top), uintptr(right), uintptr(bottom))
			_, _, _ = procSelectObject.Call(hdc, oldBrush)
			_, _, _ = procSelectObject.Call(hdc, oldPen)
			_, _, _ = procDeleteObject.Call(pen)

			label := fmt.Sprintf("%d × %d", right-left, bottom-top)
			txt, _ := syscall.UTF16FromString(label)
			_, _, _ = procSetBkMode.Call(hdc, transparent)
			_, _, _ = procSetTextColor.Call(hdc, rgb(255, 255, 255))
			_, _, _ = procTextOutW.Call(hdc, uintptr(left+8), uintptr(top+8), uintptr(unsafe.Pointer(&txt[0])), uintptr(len(txt)-1))
		} else {
			hint := "Зажмите левую кнопку мыши и выделите область. Esc — отмена"
			txt, _ := syscall.UTF16FromString(hint)
			_, _, _ = procSetBkMode.Call(hdc, transparent)
			_, _, _ = procSetTextColor.Call(hdc, rgb(255, 255, 255))
			_, _, _ = procTextOutW.Call(hdc, 30, 30, uintptr(unsafe.Pointer(&txt[0])), uintptr(len(txt)-1))
		}
		_, _, _ = procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmDestroy:
		_, _, _ = procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

var overlayState struct {
	started   time.Time
	placing   bool
	dragging  bool
	dragDX    int
	dragDY    int
	result    OverlayPosition
	cancelled bool
}

func primaryOverlayWorkArea() rect32 {
	var work rect32
	ok, _, _ := procSystemParametersInfoW.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&work)), 0)
	if ok != 0 && work.Right > work.Left && work.Bottom > work.Top {
		return work
	}
	return rect32{Left: 0, Top: 0, Right: int32(metric(smCxScreen)), Bottom: int32(metric(smCyScreen))}
}

func overlayWorkArea(x, y, width, height int) rect32 {
	r := rect32{Left: int32(x), Top: int32(y), Right: int32(x + width), Bottom: int32(y + height)}
	monitor, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), monitorDefaultToNearest)
	if monitor != 0 {
		info := monitorInfoEx{Size: uint32(unsafe.Sizeof(monitorInfoEx{}))}
		ok, _, _ := procGetMonitorInfoW.Call(monitor, uintptr(unsafe.Pointer(&info)))
		if ok != 0 && info.Work.Right > info.Work.Left && info.Work.Bottom > info.Work.Top {
			return rect32{Left: info.Work.Left, Top: info.Work.Top, Right: info.Work.Right, Bottom: info.Work.Bottom}
		}
	}
	return primaryOverlayWorkArea()
}

func defaultOverlayPosition(width, height int) (int, int) {
	work := primaryOverlayWorkArea()
	return overlayPositionInsideWorkArea(
		int(work.Right)-width-16, int(work.Bottom)-height-16, width, height,
		int(work.Left), int(work.Top), int(work.Right), int(work.Bottom), 12,
	)
}

func safeOverlayPosition(x, y, width, height int) (int, int) {
	work := overlayWorkArea(x, y, width, height)
	return overlayPositionInsideWorkArea(
		x, y, width, height, int(work.Left), int(work.Top), int(work.Right), int(work.Bottom), 12,
	)
}

func acquireOverlaySingleton() (uintptr, bool) {
	name, _ := syscall.UTF16PtrFromString("Local\\LinkVideo.Monitor.RecordingOverlay")
	h, _, callErr := procCreateMutexW.Call(0, 1, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return 0, false
	}
	if errno, ok := callErr.(syscall.Errno); ok && errno == syscall.Errno(183) {
		_, _, _ = procCloseHandle.Call(h)
		return 0, false
	}
	return h, true
}

func runOverlay(startedUnix int64, x, y int) error {
	mutex, ok := acquireOverlaySingleton()
	if !ok {
		return nil
	}
	defer func() { _, _, _ = procCloseHandle.Call(mutex) }()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if startedUnix <= 0 {
		startedUnix = time.Now().Unix()
	}
	overlayState = struct {
		started   time.Time
		placing   bool
		dragging  bool
		dragDX    int
		dragDY    int
		result    OverlayPosition
		cancelled bool
	}{started: time.Unix(startedUnix, 0)}
	return runOverlayWindow(x, y, false)
}

func placeOverlayInteractive(x, y int) (OverlayPosition, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	overlayState = struct {
		started   time.Time
		placing   bool
		dragging  bool
		dragDX    int
		dragDY    int
		result    OverlayPosition
		cancelled bool
	}{started: time.Now(), placing: true}
	if err := runOverlayWindow(x, y, true); err != nil {
		return OverlayPosition{}, err
	}
	if overlayState.cancelled {
		return OverlayPosition{}, fmt.Errorf("перемещение индикатора отменено")
	}
	return overlayState.result, nil
}

func runOverlayWindow(x, y int, placing bool) error {
	width, height := 214, 36
	if x < 0 || y < 0 {
		x, y = defaultOverlayPosition(width, height)
	} else {
		x, y = safeOverlayPosition(x, y, width, height)
	}
	className, _ := syscall.UTF16PtrFromString("LVScreenStatusOverlayV4")
	title, _ := syscall.UTF16PtrFromString("Screen recording indicator")
	hinst, _, _ := procGetModuleHandleW.Call(0)
	cursor := uintptr(0)
	exStyle := uintptr(wsExTopmost | wsExLayered | wsExToolWindow | wsExNoActivate)
	if placing {
		cursor, _, _ = procLoadCursorW.Call(0, idcArrow)
		exStyle = wsExTopmost | wsExLayered | wsExToolWindow
	} else {
		exStyle |= wsExTransparent
	}
	backgroundColor := rgb(38, 41, 45)
	backgroundBrush, _, _ := procCreateSolidBrush.Call(backgroundColor)
	wc := wndClassEx{Size: uint32(unsafe.Sizeof(wndClassEx{})), Style: csHRedraw | csVRedraw,
		WndProc: syscall.NewCallback(overlayWndProc), Instance: hinst, Cursor: cursor,
		Background: backgroundBrush, ClassName: className}
	_, _, _ = procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	hwnd, _, err := procCreateWindowExW.Call(exStyle,
		uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)), wsPopup,
		uintptr(x), uintptr(y), uintptr(width), uintptr(height), 0, 0, hinst, 0)
	if hwnd == 0 {
		return fmt.Errorf("не удалось создать индикатор: %v", err)
	}
	rgn, _, _ := procCreateRoundRectRgn.Call(0, 0, uintptr(width+1), uintptr(height+1), 10, 10)
	if rgn != 0 {
		_, _, _ = procSetWindowRgn.Call(hwnd, rgn, 1)
	}
	alpha := uintptr(218)
	if placing {
		alpha = 238
	}
	_, _, _ = procSetLayeredWindowAttributes.Call(hwnd, 0, alpha, lwaAlpha)
	_, _, _ = procSetWindowPos.Call(hwnd, ^uintptr(0), uintptr(x), uintptr(y), uintptr(width), uintptr(height), 0x0010|0x0040)
	_, _, _ = procShowWindow.Call(hwnd, swShow)
	_, _, _ = procUpdateWindow.Call(hwnd)

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}

func overlayWndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmNCHitTest:
		if !overlayState.placing {
			return uintptr(^uintptr(0))
		}
	case wmLButtonDown:
		if overlayState.placing {
			var cursor point
			_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
			var r rect32
			_, _, _ = procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
			overlayState.dragging = true
			overlayState.dragDX = int(cursor.X - r.Left)
			overlayState.dragDY = int(cursor.Y - r.Top)
			_, _, _ = procSetCapture.Call(hwnd)
			return 0
		}
	case wmMouseMove:
		if overlayState.placing && overlayState.dragging {
			var cursor point
			_, _, _ = procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
			x := int(cursor.X) - overlayState.dragDX
			y := int(cursor.Y) - overlayState.dragDY
			_, _, _ = procSetWindowPos.Call(hwnd, ^uintptr(0), uintptr(x), uintptr(y), 0, 0, 0x0001|0x0004|0x0010)
			return 0
		}
	case wmLButtonUp:
		if overlayState.placing && overlayState.dragging {
			overlayState.dragging = false
			_, _, _ = procReleaseCapture.Call()
			var r rect32
			_, _, _ = procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
			x, y := safeOverlayPosition(int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top))
			overlayState.result = OverlayPosition{X: x, Y: y}
			_, _, _ = procSetWindowPos.Call(hwnd, ^uintptr(0), uintptr(x), uintptr(y), 0, 0, 0x0001|0x0004|0x0010)
			_, _, _ = procDestroyWindow.Call(hwnd)
			return 0
		}
	case wmKeyDown:
		if overlayState.placing && wParam == vkEscape {
			overlayState.cancelled = true
			_, _, _ = procDestroyWindow.Call(hwnd)
			return 0
		}
	case wmTimer:
		return 0
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		var ps paintStruct
		hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		var client rect32
		_, _, _ = procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&client)))
		backgroundColor := rgb(38, 41, 45)
		if overlayState.placing {
			backgroundColor = rgb(47, 50, 54)
		}
		bg, _, _ := procCreateSolidBrush.Call(backgroundColor)
		_, _, _ = procFillRect.Call(hdc, uintptr(unsafe.Pointer(&client)), bg)
		_, _, _ = procDeleteObject.Call(bg)

		text := "Ведётся запись экрана"
		txt, _ := syscall.UTF16FromString(text)
		_, _, _ = procSetBkMode.Call(hdc, transparent)
		face, _ := syscall.UTF16PtrFromString("Segoe UI")
		// ANTIALIASED_QUALITY вместо ClearType: у полупрозрачного окна так нет
		// цветной ряби по краям букв.
		font, _, _ := procCreateFontW.Call(^uintptr(17-1), 0, 0, 0, 500, 0, 0, 0, 1, 0, 0, 4, 0, uintptr(unsafe.Pointer(face)))
		oldFont, _, _ := procSelectObject.Call(hdc, font)
		_, _, _ = procSetTextColor.Call(hdc, rgb(242, 244, 246))
		textRect := rect32{Left: 10, Top: 0, Right: client.Right - 10, Bottom: client.Bottom}
		_, _, _ = procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&txt[0])), uintptr(len(txt)-1), uintptr(unsafe.Pointer(&textRect)), dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
		_, _, _ = procSelectObject.Call(hdc, oldFont)
		_, _, _ = procDeleteObject.Call(font)
		_, _, _ = procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmClose:
		_, _, _ = procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		_, _, _ = procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

func metric(index int) int32 {
	r, _, _ := procGetSystemMetrics.Call(uintptr(index))
	return int32(r)
}

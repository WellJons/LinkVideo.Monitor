//go:build windows

package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	productName = "LinkVideo Monitor"
	version     = "0.8.11"

	clientWidth  = 900
	clientHeight = 580
	leftWidth    = 266

	modeInstall = iota
	modeUninstallConfirm
	modeUninstallElevated

	pageWelcome = iota
	pageLicense
	pageOptions
	pageProgress
	pageFinish

	idBack            = 1001
	idNext            = 1002
	idCancel          = 1003
	idAcceptLicense   = 1010
	idDesktopShortcut = 1011
	idRunAfter        = 1013
	idRemoveData      = 1014
	idLicenseText     = 1020
	idInstallPath     = 1021
	idProgress        = 1022
	idProgressStatus  = 1023
	idPageTitle       = 1030
	idPageDescription = 1031

	wmCreate         = 0x0001
	wmEraseBkgnd     = 0x0014
	wmDestroy        = 0x0002
	wmClose          = 0x0010
	wmPaint          = 0x000F
	wmCommand        = 0x0111
	wmDrawItem       = 0x002B
	wmSetIcon        = 0x0080
	wmTimer          = 0x0113
	wmCtlColorStatic = 0x0138
	wmCtlColorBtn    = 0x0135
	wmCtlColorEdit   = 0x0133
	wmSetFont        = 0x0030
	wmSetRedraw      = 0x000B
	wmGetText        = 0x000D
	wmApp            = 0x8000
	msgProgress      = wmApp + 1
	msgWorkDone      = wmApp + 2
	msgStartWork     = wmApp + 3

	wsOverlapped   = 0x00000000
	wsCaption      = 0x00C00000
	wsSysMenu      = 0x00080000
	wsMinimizeBox  = 0x00020000
	wsClipChildren = 0x02000000
	wsChild        = 0x40000000
	wsVisible      = 0x10000000
	wsTabStop      = 0x00010000
	wsBorder       = 0x00800000
	wsVScroll      = 0x00200000
	wsExClientEdge = 0x00000200

	ssLeft         = 0x00000000
	bsOwnerDraw    = 0x0000000B
	bsAutoCheckbox = 0x00000003
	esMultiline    = 0x0004
	esAutoVScroll  = 0x0040
	esReadOnly     = 0x0800
	esNoHideSel    = 0x0100
	swHide         = 0
	swShow         = 5
	swShowNormal   = 1
	bnClicked      = 0
	bstChecked     = 1
	odsDisabled    = 0x0004
	dtLeft         = 0x0000
	dtCenter       = 0x0001
	dtVCenter      = 0x0004
	dtSingleLine   = 0x0020
	dtWordBreak    = 0x0010
	transparent    = 1
	psSolid        = 0
	imageIcon      = 1
	lrDefaultSize  = 0x0040
	colorWindow    = 5
	idcArrow       = 32512
	idiApplication = 32512
	iconSmall      = 0
	iconBig        = 1
	timerPageEnter = 1

	pbmSetPos       = 0x0402
	pbmSetRange32   = 0x0406
	pbmSetBarColor  = 0x0409
	pbmSetBkColor   = 0x2001
	pbsSmooth       = 0x01
	emSetBkgndColor = 0x0443
	rdwInvalidate   = 0x0001
	rdwErase        = 0x0004
	rdwAllChildren  = 0x0080
	rdwUpdateNow    = 0x0100
)

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }
type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}
type paintStruct struct {
	Hdc       uintptr
	Erase     int32
	RcPaint   rect
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}
type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}
type initCommonControlsEx struct {
	DwSize uint32
	DwICC  uint32
}
type drawItemStruct struct {
	CtlType    uint32
	CtlID      uint32
	ItemID     uint32
	ItemAction uint32
	ItemState  uint32
	HwndItem   uintptr
	Hdc        uintptr
	RcItem     rect
	ItemData   uintptr
}

var (
	user32DLL   = syscall.NewLazyDLL("user32.dll")
	kernel32DLL = syscall.NewLazyDLL("kernel32.dll")
	gdi32DLL    = syscall.NewLazyDLL("gdi32.dll")
	shell32DLL  = syscall.NewLazyDLL("shell32.dll")
	comctl32DLL = syscall.NewLazyDLL("comctl32.dll")

	procRegisterClassExW   = user32DLL.NewProc("RegisterClassExW")
	procCreateWindowExW    = user32DLL.NewProc("CreateWindowExW")
	procDefWindowProcW     = user32DLL.NewProc("DefWindowProcW")
	procShowWindow         = user32DLL.NewProc("ShowWindow")
	procUpdateWindow       = user32DLL.NewProc("UpdateWindow")
	procGetMessageW        = user32DLL.NewProc("GetMessageW")
	procTranslateMessage   = user32DLL.NewProc("TranslateMessage")
	procDispatchMessageW   = user32DLL.NewProc("DispatchMessageW")
	procPostQuitMessage    = user32DLL.NewProc("PostQuitMessage")
	procDestroyWindow      = user32DLL.NewProc("DestroyWindow")
	procPostMessageW       = user32DLL.NewProc("PostMessageW")
	procSendMessageW       = user32DLL.NewProc("SendMessageW")
	procSetTimer           = user32DLL.NewProc("SetTimer")
	procKillTimer          = user32DLL.NewProc("KillTimer")
	procEnableWindow       = user32DLL.NewProc("EnableWindow")
	procSetWindowTextW     = user32DLL.NewProc("SetWindowTextW")
	procGetWindowTextW     = user32DLL.NewProc("GetWindowTextW")
	procCheckDlgButton     = user32DLL.NewProc("CheckDlgButton")
	procIsDlgButtonChecked = user32DLL.NewProc("IsDlgButtonChecked")
	procBeginPaint         = user32DLL.NewProc("BeginPaint")
	procEndPaint           = user32DLL.NewProc("EndPaint")
	procFillRect           = user32DLL.NewProc("FillRect")
	procInvalidateRect     = user32DLL.NewProc("InvalidateRect")
	procRedrawWindow       = user32DLL.NewProc("RedrawWindow")
	procGetClientRect      = user32DLL.NewProc("GetClientRect")
	procGetSystemMetrics   = user32DLL.NewProc("GetSystemMetrics")
	procSetWindowPos       = user32DLL.NewProc("SetWindowPos")
	procAdjustWindowRectEx = user32DLL.NewProc("AdjustWindowRectEx")
	procLoadCursorW        = user32DLL.NewProc("LoadCursorW")
	procLoadIconW          = user32DLL.NewProc("LoadIconW")
	procMessageBoxW        = user32DLL.NewProc("MessageBoxW")
	procSetProcessDPIAware = user32DLL.NewProc("SetProcessDPIAware")

	procGetModuleHandleW    = kernel32DLL.NewProc("GetModuleHandleW")
	procLoadLibraryW        = kernel32DLL.NewProc("LoadLibraryW")
	procCloseHandle         = kernel32DLL.NewProc("CloseHandle")
	procWaitForSingleObject = kernel32DLL.NewProc("WaitForSingleObject")
	procGetExitCodeProcess  = kernel32DLL.NewProc("GetExitCodeProcess")
	procMoveFileExW         = kernel32DLL.NewProc("MoveFileExW")

	procShellExecuteExW = shell32DLL.NewProc("ShellExecuteExW")

	procCreateSolidBrush = gdi32DLL.NewProc("CreateSolidBrush")
	procDeleteObject     = gdi32DLL.NewProc("DeleteObject")
	procCreateFontW      = gdi32DLL.NewProc("CreateFontW")
	procSelectObject     = gdi32DLL.NewProc("SelectObject")
	procSetBkMode        = gdi32DLL.NewProc("SetBkMode")
	procSetTextColor     = gdi32DLL.NewProc("SetTextColor")
	procDrawTextW        = user32DLL.NewProc("DrawTextW")
	procCreatePen        = gdi32DLL.NewProc("CreatePen")
	procRoundRect        = gdi32DLL.NewProc("RoundRect")
	procPolygon          = gdi32DLL.NewProc("Polygon")
	procPolyline         = gdi32DLL.NewProc("Polyline")

	procInitCommonControlsEx = comctl32DLL.NewProc("InitCommonControlsEx")
)

type wizard struct {
	mode     int
	elevated bool
	upgrade  bool
	hwnd     uintptr
	page     int

	pages map[int][]uintptr

	title       uintptr
	description uintptr
	back        uintptr
	next        uintptr
	cancel      uintptr

	acceptLicense   uintptr
	desktopShortcut uintptr
	runAfter        uintptr
	finishInfo      uintptr
	removeData      uintptr
	licenseText     uintptr
	installPath     uintptr
	progress        uintptr
	progressStatus  uintptr

	fontNormal uintptr
	fontSmall  uintptr
	fontTitle  uintptr
	fontBold   uintptr
	fontBrand  uintptr
	fontHero   uintptr

	whiteBrush uintptr
	darkBrush  uintptr
	lightBrush uintptr

	workMu       sync.Mutex
	workPercent  int
	workStatus   string
	workErr      error
	workWarnings []string
	workSuccess  bool
	working      bool
	installedApp string

	pageAnimStart    time.Time
	pageAnimProgress float64
	pageAnimRunning  bool
}

var currentWizard *wizard

func main() {
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "--installer-elevated":
			runWizard(modeInstall, true, false)
			return
		case "--prepare-upgrade":
			stopCaptureServiceForUpgrade()
			return
		case "--service-install":
			if len(os.Args) < 3 {
				os.Exit(20)
			}
			if err := installUACServiceWorker(os.Args[2]); err != nil {
				_, _ = fmt.Fprintln(os.Stderr, err)
				os.Exit(20)
			}
			return
		case "--uninstall-elevated":
			removeData := len(os.Args) >= 3 && boolArg(os.Args[2])
			runWizard(modeUninstallElevated, true, removeData)
			return
		}
	}
	if uninstallerBuild {
		runWizard(modeUninstallConfirm, false, false)
		return
	}
	if !isProcessElevated() {
		self, err := os.Executable()
		if err != nil {
			messageBox("Не удалось определить путь установщика: "+err.Error(), 0x10)
			return
		}
		if err := runElevatedAndWait(self, "--installer-elevated", 0xFFFFFFFF); err != nil {
			messageBox("Установка отменена: "+err.Error(), 0x10)
		}
		return
	}
	runWizard(modeInstall, true, false)
}

func runWizard(mode int, elevated bool, removeData bool) {
	// Win32 windows and their message queues are bound to the creating OS thread.
	// Without this lock the Go scheduler may move the goroutine after CreateWindowEx,
	// leaving the UI on a different thread and making Windows report “Not responding”.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	procSetProcessDPIAware.Call()
	// RichEdit paints long Unicode license text reliably and avoids the
	// duplicated glyphs produced by the classic EDIT control on some Windows builds.
	procLoadLibraryW.Call(uintptr(unsafe.Pointer(utf16("Msftedit.dll"))))
	init := initCommonControlsEx{DwSize: uint32(unsafe.Sizeof(initCommonControlsEx{})), DwICC: 0x00000020}
	procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&init)))

	w := &wizard{
		mode:     mode,
		elevated: elevated,
		upgrade:  existingInstallation(),
		pages:    make(map[int][]uintptr),
	}
	currentWizard = w

	instance, _, _ := procGetModuleHandleW.Call(0)
	className := utf16("LinkVideoMonitorInstallerWindow")
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	icon, _, _ := procLoadIconW.Call(instance, 1)
	if icon == 0 {
		icon, _, _ = procLoadIconW.Call(0, idiApplication)
	}
	wc := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		Style:         0x0003,
		LpfnWndProc:   syscall.NewCallback(windowProc),
		HInstance:     instance,
		HIcon:         icon,
		HCursor:       cursor,
		HbrBackground: uintptr(colorWindow + 1),
		LpszClassName: className,
		HIconSm:       icon,
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	style := uintptr(wsOverlapped | wsCaption | wsSysMenu | wsMinimizeBox | wsClipChildren)
	rc := rect{0, 0, clientWidth, clientHeight}
	procAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&rc)), style, 0, 0)
	width := int(rc.Right - rc.Left)
	height := int(rc.Bottom - rc.Top)
	screenW, _, _ := procGetSystemMetrics.Call(0)
	screenH, _, _ := procGetSystemMetrics.Call(1)
	x := (int(screenW) - width) / 2
	y := (int(screenH) - height) / 2

	caption := "Установка LinkVideo Monitor"
	if mode != modeInstall {
		caption = "Удаление LinkVideo Monitor"
	}
	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(utf16(caption))),
		style,
		uintptr(x), uintptr(y), uintptr(width), uintptr(height),
		0, 0, instance, 0,
	)
	if hwnd == 0 {
		messageBox("Не удалось открыть окно установщика: "+err.Error(), 0x10)
		return
	}
	w.hwnd = hwnd
	procSendMessageW.Call(hwnd, wmSetIcon, iconBig, icon)
	procSendMessageW.Call(hwnd, wmSetIcon, iconSmall, icon)
	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)

	if mode == modeUninstallElevated {
		setChecked(w.removeData, removeData)
		w.setPage(pageProgress)
		procPostMessageW.Call(hwnd, msgStartWork, 0, 0)
	}

	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func windowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	w := currentWizard
	switch message {
	case wmCreate:
		if w != nil {
			w.hwnd = hwnd
			w.createControls()
		}
		return 0
	case wmCommand:
		if w != nil {
			id := int(wParam & 0xFFFF)
			code := int((wParam >> 16) & 0xFFFF)
			if code == bnClicked || id == idBack || id == idNext || id == idCancel {
				w.handleCommand(id)
			}
		}
		return 0
	case wmDrawItem:
		if w != nil {
			return w.drawButton((*drawItemStruct)(unsafe.Pointer(lParam)))
		}
	case wmTimer:
		if w != nil && wParam == timerPageEnter {
			w.tickPageAnimation()
			return 0
		}
	case wmCtlColorStatic, wmCtlColorBtn, wmCtlColorEdit:
		if w != nil {
			procSetBkMode.Call(wParam, transparent)
			procSetTextColor.Call(wParam, rgb(32, 37, 43))
			return w.whiteBrush
		}
	case wmEraseBkgnd:
		// Background is painted in WM_PAINT. Returning non-zero prevents the
		// default erase pass and removes the strong page-transition flicker.
		return 1
	case wmPaint:
		if w != nil {
			w.paint()
			return 0
		}
	case msgProgress:
		if w != nil {
			w.applyProgress()
		}
		return 0
	case msgWorkDone:
		if w != nil {
			w.finishWork()
		}
		return 0
	case msgStartWork:
		if w != nil {
			w.startWork()
		}
		return 0
	case wmClose:
		if w != nil && w.working {
			messageBox("Дождитесь завершения текущей операции.", 0x40)
			return 0
		}
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		if w != nil {
			w.cleanup()
			if w.mode == modeUninstallElevated {
				scheduleSelfDelete()
			}
		}
		procPostQuitMessage.Call(0)
		return 0
	}
	result, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func (w *wizard) createControls() {
	w.whiteBrush, _, _ = procCreateSolidBrush.Call(rgb(255, 255, 255))
	w.darkBrush, _, _ = procCreateSolidBrush.Call(rgb(65, 67, 70))
	w.lightBrush, _, _ = procCreateSolidBrush.Call(rgb(244, 245, 247))
	w.fontNormal = createFont(16, 400)
	w.fontSmall = createFont(14, 400)
	w.fontTitle = createFont(28, 700)
	w.fontBold = createFont(16, 700)
	w.fontBrand = createFont(25, 700)
	w.fontHero = createFont(23, 700)

	w.title = w.newControl("STATIC", "", wsChild|wsVisible|ssLeft, 0, 310, 45, 535, 44, idPageTitle)
	w.description = w.newControl("STATIC", "", wsChild|wsVisible|ssLeft, 0, 310, 92, 525, 58, idPageDescription)
	setFont(w.title, w.fontTitle)
	setFont(w.description, w.fontNormal)

	w.back = w.newControl("BUTTON", "Назад", wsChild|wsVisible|wsTabStop|bsOwnerDraw, 0, 523, 524, 108, 38, idBack)
	w.cancel = w.newControl("BUTTON", "Отмена", wsChild|wsVisible|wsTabStop|bsOwnerDraw, 0, 642, 524, 108, 38, idCancel)
	w.next = w.newControl("BUTTON", "Далее", wsChild|wsVisible|wsTabStop|bsOwnerDraw, 0, 761, 524, 108, 38, idNext)
	setFont(w.back, w.fontBold)
	setFont(w.cancel, w.fontBold)
	setFont(w.next, w.fontBold)

	if w.mode == modeInstall {
		w.createInstallPages()
		w.setPage(pageWelcome)
	} else {
		w.createUninstallPages()
		if w.mode == modeUninstallElevated {
			w.setPage(pageProgress)
		} else {
			w.setPage(pageWelcome)
		}
	}
}

func (w *wizard) createInstallPages() {
	// The welcome page is painted as branded advantage cards instead of release notes.
	w.pages[pageWelcome] = []uintptr{}

	w.licenseText = w.newControl("RICHEDIT50W", loadLicenseText(), wsChild|wsVScroll|esMultiline|esAutoVScroll|esReadOnly|esNoHideSel, wsExClientEdge, 310, 156, 545, 285, idLicenseText)
	procSendMessageW.Call(w.licenseText, emSetBkgndColor, 0, rgb(255, 255, 255))
	w.acceptLicense = w.newControl("BUTTON", "Я принимаю условия пользовательского соглашения", wsChild|wsTabStop|bsAutoCheckbox, 0, 310, 457, 545, 30, idAcceptLicense)
	setFont(w.licenseText, w.fontSmall)
	setFont(w.acceptLicense, w.fontNormal)
	w.pages[pageLicense] = []uintptr{w.licenseText, w.acceptLicense}

	pathLabel := w.newControl("STATIC", "Папка установки", wsChild|ssLeft, 0, 310, 168, 300, 25, 0)
	w.installPath = w.newControl("EDIT", defaultInstallDir(), wsChild|wsBorder|esReadOnly, wsExClientEdge, 310, 197, 545, 38, idInstallPath)
	w.desktopShortcut = w.newControl("BUTTON", "Создать ярлык на рабочем столе", wsChild|wsTabStop|bsAutoCheckbox, 0, 310, 272, 520, 30, idDesktopShortcut)
	optionHint := w.newControl("STATIC", "LinkVideo Monitor будет запускаться вместе с Windows. Отключить автозапуск можно после установки в настройках программы. Фоновая служба защищённого захвата устанавливается автоматически.", wsChild|ssLeft, 0, 310, 326, 535, 88, 0)
	setFont(pathLabel, w.fontBold)
	setFont(w.installPath, w.fontNormal)
	setFont(w.desktopShortcut, w.fontNormal)
	setFont(optionHint, w.fontSmall)
	setChecked(w.desktopShortcut, true)
	w.pages[pageOptions] = []uintptr{pathLabel, w.installPath, w.desktopShortcut, optionHint}

	w.progress = w.newControl("msctls_progress32", "", wsChild|pbsSmooth, 0, 310, 222, 545, 22, idProgress)
	w.progressStatus = w.newControl("STATIC", "Подготовка…", wsChild|ssLeft, 0, 310, 263, 545, 48, idProgressStatus)
	setFont(w.progressStatus, w.fontNormal)
	procSendMessageW.Call(w.progress, pbmSetRange32, 0, 100)
	procSendMessageW.Call(w.progress, pbmSetBarColor, 0, rgb(255, 173, 25))
	procSendMessageW.Call(w.progress, pbmSetBkColor, 0, rgb(238, 240, 242))
	w.pages[pageProgress] = []uintptr{w.progress, w.progressStatus}

	w.finishInfo = w.newControl("STATIC", "Программа установлена и зарегистрирована в Windows. Удалить её можно через «Установленные приложения» обычным пошаговым мастером.", wsChild|ssLeft, 0, 310, 180, 535, 84, 0)
	w.runAfter = w.newControl("BUTTON", "Запустить LinkVideo Monitor", wsChild|wsTabStop|bsAutoCheckbox, 0, 310, 310, 500, 30, idRunAfter)
	setFont(w.finishInfo, w.fontNormal)
	setFont(w.runAfter, w.fontNormal)
	setChecked(w.runAfter, true)
	w.pages[pageFinish] = []uintptr{w.finishInfo, w.runAfter}
}

func (w *wizard) createUninstallPages() {
	confirm := w.newControl("STATIC", "LinkVideo Monitor, фоновая служба и ярлыки будут удалены с этого компьютера.", wsChild|ssLeft, 0, 310, 178, 535, 64, 0)
	w.removeData = w.newControl("BUTTON", "Удалить также настройки, журналы и сохранённую ссылку подключения", wsChild|wsTabStop|bsAutoCheckbox, 0, 310, 274, 545, 48, idRemoveData)
	hint := w.newControl("STATIC", "Снимите отметку, чтобы сохранить конфигурацию для последующей повторной установки.", wsChild|ssLeft, 0, 338, 330, 505, 48, 0)
	setFont(confirm, w.fontNormal)
	setFont(w.removeData, w.fontNormal)
	setFont(hint, w.fontSmall)
	setChecked(w.removeData, true)
	w.pages[pageWelcome] = []uintptr{confirm, w.removeData, hint}

	w.progress = w.newControl("msctls_progress32", "", wsChild|pbsSmooth, 0, 310, 222, 545, 22, idProgress)
	w.progressStatus = w.newControl("STATIC", "Подготовка…", wsChild|ssLeft, 0, 310, 263, 545, 48, idProgressStatus)
	setFont(w.progressStatus, w.fontNormal)
	procSendMessageW.Call(w.progress, pbmSetRange32, 0, 100)
	procSendMessageW.Call(w.progress, pbmSetBarColor, 0, rgb(255, 173, 25))
	procSendMessageW.Call(w.progress, pbmSetBkColor, 0, rgb(238, 240, 242))
	w.pages[pageProgress] = []uintptr{w.progress, w.progressStatus}

	finish := w.newControl("STATIC", "LinkVideo Monitor удалён. Вы можете закрыть это окно.", wsChild|ssLeft, 0, 310, 188, 535, 60, 0)
	setFont(finish, w.fontNormal)
	w.pages[pageFinish] = []uintptr{finish}
}

func (w *wizard) setPage(page int) {
	// Hide/show all page controls as one atomic redraw. This removes the visible
	// jump and prevents child controls from keeping stale pixels between pages.
	procSendMessageW.Call(w.hwnd, wmSetRedraw, 0, 0)
	defer func() {
		procSendMessageW.Call(w.hwnd, wmSetRedraw, 1, 0)
		procRedrawWindow.Call(w.hwnd, 0, 0, rdwInvalidate|rdwErase|rdwAllChildren|rdwUpdateNow)
	}()
	for _, controls := range w.pages {
		for _, hwnd := range controls {
			procShowWindow.Call(hwnd, swHide)
		}
	}
	w.page = page
	for _, hwnd := range w.pages[page] {
		procShowWindow.Call(hwnd, swShow)
	}

	if w.mode == modeInstall {
		switch page {
		case pageWelcome:
			if w.upgrade {
				setText(w.title, "Обновление LinkVideo Monitor")
				setText(w.description, "Обновление без потери ссылки подключения и пользовательских настроек.")
			} else {
				setText(w.title, "LinkVideo Monitor")
				setText(w.description, "Запись и передача экрана рабочего компьютера в облако LinkVideo.")
			}
			setText(w.next, "Далее")
			procShowWindow.Call(w.back, swHide)
			procShowWindow.Call(w.cancel, swShow)
			procShowWindow.Call(w.next, swShow)
			enable(w.next, true)
		case pageLicense:
			setText(w.title, "Пользовательское соглашение")
			setText(w.description, "Для продолжения ознакомьтесь с условиями и подтвердите согласие.")
			setText(w.next, "Далее")
			procShowWindow.Call(w.back, swShow)
			procShowWindow.Call(w.cancel, swShow)
			procShowWindow.Call(w.next, swShow)
			enable(w.next, isChecked(w.acceptLicense))
		case pageOptions:
			setText(w.title, "Параметры установки")
			setText(w.description, "Проверьте расположение программы и создание ярлыка.")
			setText(w.next, "Установить")
			procShowWindow.Call(w.back, swShow)
			procShowWindow.Call(w.cancel, swShow)
			procShowWindow.Call(w.next, swShow)
			enable(w.next, true)
		case pageProgress:
			title := "Установка LinkVideo Monitor"
			if w.upgrade {
				title = "Обновление LinkVideo Monitor"
			}
			setText(w.title, title)
			setText(w.description, "Не закрывайте окно до завершения процесса.")
			procShowWindow.Call(w.back, swHide)
			procShowWindow.Call(w.cancel, swHide)
			procShowWindow.Call(w.next, swHide)
		case pageFinish:
			if w.workSuccess {
				setText(w.title, "Готово")
				w.workMu.Lock()
				warnings := append([]string(nil), w.workWarnings...)
				w.workMu.Unlock()
				if len(warnings) > 0 {
					setText(w.description, "LinkVideo Monitor установлен. Дополнительные действия завершены с предупреждениями: "+strings.Join(warnings, "; "))
				} else {
					setText(w.description, "LinkVideo Monitor успешно установлен.")
				}
				setText(w.finishInfo, "Программа установлена в папку Program Files и зарегистрирована в Windows. Удалить её можно через «Установленные приложения» обычным пошаговым мастером.")
				procShowWindow.Call(w.runAfter, swShow)
			} else {
				setText(w.title, "Установка не завершена")
				setText(w.description, errorText(w.workErr))
				setText(w.finishInfo, "Установка была остановлена. Уже скопированные файлы можно удалить повторным запуском мастера удаления или установить программу ещё раз.")
				procShowWindow.Call(w.runAfter, swHide)
			}
			setText(w.next, "Готово")
			procShowWindow.Call(w.back, swHide)
			procShowWindow.Call(w.cancel, swHide)
			procShowWindow.Call(w.next, swShow)
			enable(w.next, true)
		}
	} else {
		switch page {
		case pageWelcome:
			setText(w.title, "Удаление LinkVideo Monitor")
			setText(w.description, "Подтвердите удаление программы с этого компьютера.")
			setText(w.next, "Удалить")
			procShowWindow.Call(w.back, swHide)
			procShowWindow.Call(w.cancel, swShow)
			procShowWindow.Call(w.next, swShow)
			enable(w.next, true)
		case pageProgress:
			setText(w.title, "Удаление LinkVideo Monitor")
			setText(w.description, "Не закрывайте окно до завершения процесса.")
			procShowWindow.Call(w.back, swHide)
			procShowWindow.Call(w.cancel, swHide)
			procShowWindow.Call(w.next, swHide)
		case pageFinish:
			if w.workSuccess {
				setText(w.title, "Удаление завершено")
				setText(w.description, "LinkVideo Monitor удалён с компьютера.")
			} else {
				setText(w.title, "Удаление завершено с ошибкой")
				setText(w.description, errorText(w.workErr))
			}
			setText(w.next, "Готово")
			procShowWindow.Call(w.back, swHide)
			procShowWindow.Call(w.cancel, swHide)
			procShowWindow.Call(w.next, swShow)
			enable(w.next, true)
		}
	}
	if w.mode == modeInstall && page == pageWelcome {
		w.startPageAnimation()
	} else {
		if w.pageAnimRunning {
			procKillTimer.Call(w.hwnd, timerPageEnter)
		}
		w.pageAnimRunning = false
		w.pageAnimProgress = 1
	}
	if page == pageLicense && w.licenseText != 0 {
		procRedrawWindow.Call(w.licenseText, 0, 0, rdwInvalidate|rdwErase|rdwUpdateNow)
	}
}

func (w *wizard) handleCommand(id int) {
	switch id {
	case idAcceptLicense:
		if w.mode == modeInstall && w.page == pageLicense {
			enable(w.next, isChecked(w.acceptLicense))
		}
	case idBack:
		if w.mode == modeInstall {
			if w.page == pageLicense {
				w.setPage(pageWelcome)
			} else if w.page == pageOptions {
				w.setPage(pageLicense)
			}
		}
	case idCancel:
		procDestroyWindow.Call(w.hwnd)
	case idNext:
		if w.mode == modeInstall {
			switch w.page {
			case pageWelcome:
				w.setPage(pageLicense)
			case pageLicense:
				if isChecked(w.acceptLicense) {
					w.setPage(pageOptions)
				}
			case pageOptions:
				w.setPage(pageProgress)
				procPostMessageW.Call(w.hwnd, msgStartWork, 0, 0)
			case pageFinish:
				if w.workSuccess && isChecked(w.runAfter) && w.installedApp != "" {
					_ = launchInstalledApplication(w.installedApp)
				}
				procDestroyWindow.Call(w.hwnd)
			}
		} else {
			switch w.page {
			case pageWelcome:
				if err := launchElevatedUninstaller(isChecked(w.removeData)); err != nil {
					messageBox(err.Error(), 0x10)
					return
				}
				procDestroyWindow.Call(w.hwnd)
			case pageFinish:
				procDestroyWindow.Call(w.hwnd)
			}
		}
	}
}

func (w *wizard) startWork() {
	if w.working {
		return
	}
	w.working = true

	// Read control state on the window thread before starting background work.
	opts := installOptions{}
	removeData := false
	if w.mode == modeInstall {
		opts = installOptions{DesktopShortcut: isChecked(w.desktopShortcut), AutoStart: true}
	} else {
		removeData = isChecked(w.removeData)
	}

	go func(opts installOptions, removeData bool) {
		var err error
		var installedApp string
		if w.mode == modeInstall {
			var warnings []string
			installedApp, warnings, err = installProduct(opts, w.postProgress)
			w.workMu.Lock()
			w.workWarnings = append([]string(nil), warnings...)
			w.workMu.Unlock()
		} else {
			err = uninstallProduct(removeData, w.postProgress)
		}
		w.workMu.Lock()
		w.installedApp = installedApp
		w.workErr = err
		w.workSuccess = err == nil
		w.workMu.Unlock()
		procPostMessageW.Call(w.hwnd, msgWorkDone, 0, 0)
	}(opts, removeData)
}

func (w *wizard) postProgress(percent int, status string) {
	w.workMu.Lock()
	w.workPercent = percent
	w.workStatus = status
	w.workMu.Unlock()
	procPostMessageW.Call(w.hwnd, msgProgress, 0, 0)
}

func (w *wizard) applyProgress() {
	w.workMu.Lock()
	percent := w.workPercent
	status := w.workStatus
	w.workMu.Unlock()
	procSendMessageW.Call(w.progress, pbmSetPos, uintptr(percent), 0)
	setText(w.progressStatus, status)
}

func (w *wizard) finishWork() {
	w.working = false
	w.applyProgress()
	w.setPage(pageFinish)
}

func (w *wizard) newControl(class, text string, style, exStyle uintptr, x, y, width, height, id int) uintptr {
	instance, _, _ := procGetModuleHandleW.Call(0)
	hwnd, _, _ := procCreateWindowExW.Call(
		exStyle,
		uintptr(unsafe.Pointer(utf16(class))),
		uintptr(unsafe.Pointer(utf16(text))),
		style,
		uintptr(x), uintptr(y), uintptr(width), uintptr(height),
		w.hwnd, uintptr(id), instance, 0,
	)
	if id != 0 {
		setFont(hwnd, w.fontNormal)
	}
	return hwnd
}

func (w *wizard) paint() {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(w.hwnd, uintptr(unsafe.Pointer(&ps)))
	defer procEndPaint.Call(w.hwnd, uintptr(unsafe.Pointer(&ps)))

	var client rect
	procGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&client)))
	right := rect{leftWidth, 0, client.Right, client.Bottom}
	left := rect{0, 0, leftWidth, client.Bottom}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&right)), w.whiteBrush)
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&left)), w.darkBrush)

	// LinkVideo chain mark.
	orangeBrush, _, _ := procCreateSolidBrush.Call(rgb(255, 173, 25))
	greyBrush, _, _ := procCreateSolidBrush.Call(rgb(201, 204, 207))
	oldBrush, _, _ := procSelectObject.Call(hdc, orangeBrush)
	p1 := [4]point{{34, 52}, {49, 37}, {64, 52}, {49, 67}}
	procPolygon.Call(hdc, uintptr(unsafe.Pointer(&p1[0])), 4)
	procSelectObject.Call(hdc, greyBrush)
	p2 := [4]point{{55, 40}, {66, 29}, {77, 40}, {66, 51}}
	procPolygon.Call(hdc, uintptr(unsafe.Pointer(&p2[0])), 4)
	procSelectObject.Call(hdc, oldBrush)
	procDeleteObject.Call(orangeBrush)
	procDeleteObject.Call(greyBrush)

	drawText(hdc, "LinkVideo", rect{89, 27, 235, 59}, w.fontBrand, rgb(255, 255, 255), dtLeft|dtVCenter|dtSingleLine)
	drawText(hdc, "MONITOR", rect{90, 57, 230, 81}, w.fontSmall, rgb(255, 173, 25), dtLeft|dtVCenter|dtSingleLine)
	drawText(hdc, "Запись экрана", rect{34, 122, 235, 157}, w.fontHero, rgb(255, 255, 255), dtLeft|dtVCenter|dtSingleLine)
	drawText(hdc, "для бизнеса", rect{34, 154, 235, 189}, w.fontHero, rgb(255, 255, 255), dtLeft|dtVCenter|dtSingleLine)

	steps := w.stepLabels()
	active := w.activeStep()
	y := int32(255)
	for i, label := range steps {
		color := rgb(174, 179, 184)
		bullet := rgb(113, 118, 123)
		if i == active {
			color = rgb(255, 255, 255)
			bullet = rgb(255, 173, 25)
		} else if i < active {
			color = rgb(220, 223, 226)
			bullet = rgb(49, 198, 109)
		}
		b, _, _ := procCreateSolidBrush.Call(bullet)
		dot := rect{35, y + 7, 44, y + 16}
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&dot)), b)
		procDeleteObject.Call(b)
		drawText(hdc, label, rect{58, y, 225, y + 28}, w.fontNormal, color, dtLeft|dtVCenter|dtSingleLine)
		y += 48
	}

	drawText(hdc, "Версия "+version+" Beta", rect{34, client.Bottom - 66, 225, client.Bottom - 38}, w.fontSmall, rgb(174, 179, 184), dtLeft|dtVCenter|dtSingleLine)
	drawText(hdc, "linkvideo.ru", rect{34, client.Bottom - 39, 225, client.Bottom - 15}, w.fontSmall, rgb(255, 173, 25), dtLeft|dtVCenter|dtSingleLine)

	if w.mode == modeInstall && w.page == pageWelcome {
		w.drawWelcomePage(hdc)
	}

	lineBrush, _, _ := procCreateSolidBrush.Call(rgb(223, 227, 232))
	line := rect{leftWidth, 506, client.Right, 507}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&line)), lineBrush)
	procDeleteObject.Call(lineBrush)
}

func (w *wizard) startPageAnimation() {
	w.pageAnimStart = time.Now()
	w.pageAnimProgress = 0
	w.pageAnimRunning = true
	procSetTimer.Call(w.hwnd, timerPageEnter, 16, 0)
}

func (w *wizard) tickPageAnimation() {
	if !w.pageAnimRunning {
		return
	}
	elapsed := time.Since(w.pageAnimStart)
	p := float64(elapsed) / float64(260*time.Millisecond)
	if p >= 1 {
		p = 1
		w.pageAnimRunning = false
		procKillTimer.Call(w.hwnd, timerPageEnter)
	}
	// Cubic ease-out keeps the motion subtle and quick.
	inv := 1 - p
	w.pageAnimProgress = 1 - inv*inv*inv
	procInvalidateRect.Call(w.hwnd, 0, 0)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func (w *wizard) drawWelcomePage(hdc uintptr) {
	progress := w.pageAnimProgress
	if !w.pageAnimRunning && progress == 0 {
		progress = 1
	}

	// Animated accent under the page introduction.
	accentWidth := int32(88 * progress)
	accentBrush, _, _ := procCreateSolidBrush.Call(rgb(255, 173, 25))
	accent := rect{310, 151, 310 + accentWidth, 155}
	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&accent)), accentBrush)
	procDeleteObject.Call(accentBrush)

	cards := []struct {
		title string
		body  string
	}{
		{"Контроль рабочего места", "Прямая трансляция и архив экрана сотрудника."},
		{"Работа в фоне", "Автозапуск и восстановление после сна и перезагрузки."},
		{"Без камеры", "Для записи достаточно установить программу на компьютер."},
	}
	baseX := int32(310)
	const cardW int32 = 168
	const gap int32 = 14
	for i, card := range cards {
		local := clamp01(progress*1.45 - float64(i)*0.18)
		inv := 1 - local
		eased := 1 - inv*inv*inv
		x := baseX + int32(i)*(cardW+gap) + int32((1-eased)*24)
		w.drawBenefitCard(hdc, rect{x, 164, x + cardW, 312}, card.title, card.body)
	}

	local := clamp01(progress*1.2 - 0.25)
	yOffset := int32((1 - local) * 12)
	w.drawRequirementsCard(hdc, rect{310, 342 + yOffset, 842, 448 + yOffset})
}

func (w *wizard) drawBenefitCard(hdc uintptr, rc rect, title, body string) {
	brush, _, _ := procCreateSolidBrush.Call(rgb(248, 249, 250))
	pen, _, _ := procCreatePen.Call(psSolid, 1, rgb(226, 229, 233))
	oldBrush, _, _ := procSelectObject.Call(hdc, brush)
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	procRoundRect.Call(hdc, uintptr(rc.Left), uintptr(rc.Top), uintptr(rc.Right), uintptr(rc.Bottom), 16, 16)
	procSelectObject.Call(hdc, oldBrush)
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(brush)
	procDeleteObject.Call(pen)

	// Compact orange check mark.
	checkPen, _, _ := procCreatePen.Call(psSolid, 5, rgb(255, 126, 61))
	oldCheckPen, _, _ := procSelectObject.Call(hdc, checkPen)
	points := [3]point{{rc.Left + 18, rc.Top + 29}, {rc.Left + 27, rc.Top + 38}, {rc.Left + 43, rc.Top + 20}}
	procPolyline.Call(hdc, uintptr(unsafe.Pointer(&points[0])), 3)
	procSelectObject.Call(hdc, oldCheckPen)
	procDeleteObject.Call(checkPen)

	drawText(hdc, title, rect{rc.Left + 16, rc.Top + 54, rc.Right - 14, rc.Top + 94}, w.fontBold, rgb(36, 40, 44), dtLeft|dtWordBreak)
	drawText(hdc, body, rect{rc.Left + 16, rc.Top + 96, rc.Right - 14, rc.Bottom - 12}, w.fontSmall, rgb(78, 83, 89), dtLeft|dtWordBreak)
}

func (w *wizard) drawRequirementsCard(hdc uintptr, rc rect) {
	brush, _, _ := procCreateSolidBrush.Call(rgb(255, 247, 230))
	pen, _, _ := procCreatePen.Call(psSolid, 1, rgb(255, 221, 155))
	oldBrush, _, _ := procSelectObject.Call(hdc, brush)
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	procRoundRect.Call(hdc, uintptr(rc.Left), uintptr(rc.Top), uintptr(rc.Right), uintptr(rc.Bottom), 14, 14)
	procSelectObject.Call(hdc, oldBrush)
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(brush)
	procDeleteObject.Call(pen)

	drawText(hdc, "Минимальные системные требования", rect{rc.Left + 16, rc.Top + 12, rc.Right - 14, rc.Top + 39}, w.fontBold, rgb(44, 46, 49), dtLeft|dtVCenter|dtSingleLine)
	drawText(hdc, "Windows 7 и выше · только x64", rect{rc.Left + 16, rc.Top + 46, rc.Left + 260, rc.Top + 73}, w.fontSmall, rgb(69, 72, 76), dtLeft|dtVCenter|dtSingleLine)
	drawText(hdc, "CPU от 2 ГГц", rect{rc.Left + 276, rc.Top + 46, rc.Left + 390, rc.Top + 73}, w.fontSmall, rgb(69, 72, 76), dtLeft|dtVCenter|dtSingleLine)
	drawText(hdc, "ОЗУ от 4 ГБ", rect{rc.Left + 402, rc.Top + 46, rc.Right - 12, rc.Top + 73}, w.fontSmall, rgb(69, 72, 76), dtLeft|dtVCenter|dtSingleLine)
}

func (w *wizard) stepLabels() []string {
	if w.mode == modeInstall {
		return []string{"Начало", "Соглашение", "Параметры", "Установка"}
	}
	return []string{"Подтверждение", "Удаление", "Готово"}
}

func (w *wizard) activeStep() int {
	if w.mode == modeInstall {
		switch w.page {
		case pageWelcome:
			return 0
		case pageLicense:
			return 1
		case pageOptions:
			return 2
		default:
			return 3
		}
	}
	switch w.page {
	case pageWelcome:
		return 0
	case pageProgress:
		return 1
	default:
		return 2
	}
}

func (w *wizard) drawButton(dis *drawItemStruct) uintptr {
	if dis == nil {
		return 0
	}
	primary := int(dis.CtlID) == idNext
	disabled := dis.ItemState&odsDisabled != 0
	fill := rgb(255, 255, 255)
	border := rgb(205, 211, 217)
	textColor := rgb(45, 50, 55)
	if primary {
		fill = rgb(255, 173, 25)
		border = rgb(235, 150, 0)
		textColor = rgb(43, 39, 31)
	}
	if disabled {
		fill = rgb(238, 240, 242)
		border = rgb(222, 225, 228)
		textColor = rgb(145, 150, 155)
	}
	brush, _, _ := procCreateSolidBrush.Call(fill)
	pen, _, _ := procCreatePen.Call(psSolid, 1, border)
	oldBrush, _, _ := procSelectObject.Call(dis.Hdc, brush)
	oldPen, _, _ := procSelectObject.Call(dis.Hdc, pen)
	procRoundRect.Call(dis.Hdc, uintptr(dis.RcItem.Left), uintptr(dis.RcItem.Top), uintptr(dis.RcItem.Right), uintptr(dis.RcItem.Bottom), 10, 10)
	procSelectObject.Call(dis.Hdc, oldBrush)
	procSelectObject.Call(dis.Hdc, oldPen)
	procDeleteObject.Call(brush)
	procDeleteObject.Call(pen)

	text := getText(dis.HwndItem)
	drawText(dis.Hdc, text, dis.RcItem, w.fontBold, textColor, dtCenter|dtVCenter|dtSingleLine)
	return 1
}

func (w *wizard) cleanup() {
	if w.pageAnimRunning {
		procKillTimer.Call(w.hwnd, timerPageEnter)
	}
	for _, object := range []uintptr{w.fontNormal, w.fontSmall, w.fontTitle, w.fontBold, w.fontBrand, w.fontHero, w.whiteBrush, w.darkBrush, w.lightBrush} {
		if object != 0 {
			procDeleteObject.Call(object)
		}
	}
}

func loadLicenseText() string {
	text := strings.TrimSpace(embeddedLicenseText)
	if text == "" {
		return "Пользовательское соглашение временно недоступно."
	}
	return text
}

func createFont(size int32, weight int32) uintptr {
	face := utf16("Segoe UI")
	font, _, _ := procCreateFontW.Call(
		uintptr(-size), 0, 0, 0, uintptr(weight), 0, 0, 0,
		1, 0, 0, 5, 0, uintptr(unsafe.Pointer(face)),
	)
	return font
}

func setFont(hwnd, font uintptr) {
	if hwnd != 0 && font != 0 {
		procSendMessageW.Call(hwnd, wmSetFont, font, 1)
	}
}

func setText(hwnd uintptr, text string) {
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(utf16(text))))
}

func getText(hwnd uintptr) string {
	buf := make([]uint16, 256)
	n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

func setChecked(hwnd uintptr, checked bool) {
	state := uintptr(0)
	if checked {
		state = bstChecked
	}
	procCheckDlgButton.Call(currentWizard.hwnd, uintptr(controlID(hwnd)), state)
}

func isChecked(hwnd uintptr) bool {
	id := controlID(hwnd)
	state, _, _ := procIsDlgButtonChecked.Call(currentWizard.hwnd, uintptr(id))
	return state == bstChecked
}

func controlID(hwnd uintptr) int {
	// GetDlgCtrlID is exported by user32; resolve lazily to keep declarations compact.
	id, _, _ := user32DLL.NewProc("GetDlgCtrlID").Call(hwnd)
	return int(id)
}

func enable(hwnd uintptr, enabled bool) {
	value := uintptr(0)
	if enabled {
		value = 1
	}
	procEnableWindow.Call(hwnd, value)
	procInvalidateRect.Call(hwnd, 0, 1)
}

func drawText(hdc uintptr, text string, rc rect, font uintptr, color uintptr, flags uintptr) {
	oldFont, _, _ := procSelectObject.Call(hdc, font)
	procSetBkMode.Call(hdc, transparent)
	procSetTextColor.Call(hdc, color)
	p := utf16(text)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(p)), uintptr(^uint32(0)), uintptr(unsafe.Pointer(&rc)), flags)
	procSelectObject.Call(hdc, oldFont)
}

func utf16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func rgb(r, g, b byte) uintptr {
	return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16
}

func messageBox(text string, flags uintptr) {
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(utf16(text))), uintptr(unsafe.Pointer(utf16(productName))), flags)
}

func errorText(err error) string {
	if err == nil {
		return "Операция завершена."
	}
	text := strings.TrimSpace(err.Error())
	if len([]rune(text)) > 280 {
		text = string([]rune(text)[:280]) + "…"
	}
	return text
}

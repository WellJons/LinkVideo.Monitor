//go:build windows

package main

import (
	"bytes"
	"context"
	_ "embed"
	"image/color"
	"image/png"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	wtsCurrentServerHandle = 0
	wtsCurrentSession      = 0xffffffff
	wtsSessionInfoEx       = 25
	wtsSessionStateLock    = 0
	wtsSessionStateUnlock  = 1
)

type wtsInfoExLevel1W struct {
	SessionID      uint32
	SessionState   int32
	SessionFlags   int32
	WinStationName [33]uint16
	UserName       [21]uint16
	DomainName     [18]uint16
	LogonTime      int64
	ConnectTime    int64
	DisconnectTime int64
	LastInputTime  int64
	CurrentTime    int64
	IncomingBytes  uint32
	OutgoingBytes  uint32
	IncomingFrames uint32
	OutgoingFrames uint32
	IncomingComp   uint32
	OutgoingComp   uint32
}

type wtsInfoExW struct {
	Level uint32
	_     uint32 // align the union to 8 bytes on Windows amd64
	Data  wtsInfoExLevel1W
}

type windowsSessionStateWatcher struct {
	locked atomic.Bool
}

var (
	wtsapi32                 = syscall.NewLazyDLL("wtsapi32.dll")
	procWTSQuerySessionInfoW = wtsapi32.NewProc("WTSQuerySessionInformationW")
	procWTSFreeMemory        = wtsapi32.NewProc("WTSFreeMemory")
)

func newSessionStateWatcher() sessionStateWatcher {
	return &windowsSessionStateWatcher{}
}

func (w *windowsSessionStateWatcher) Run(ctx context.Context, changed func(bool)) {
	check := func() {
		locked, known := queryCurrentSessionLocked()
		if !known {
			return
		}
		old := w.locked.Swap(locked)
		if old != locked {
			changed(locked)
		}
	}
	check()
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}

func queryCurrentSessionLocked() (bool, bool) {
	var buffer uintptr
	var bytesReturned uint32
	ok, _, _ := procWTSQuerySessionInfoW.Call(
		wtsCurrentServerHandle,
		wtsCurrentSession,
		wtsSessionInfoEx,
		uintptr(unsafe.Pointer(&buffer)),
		uintptr(unsafe.Pointer(&bytesReturned)),
	)
	if ok == 0 || buffer == 0 {
		return false, false
	}
	defer procWTSFreeMemory.Call(buffer)
	if bytesReturned < uint32(unsafe.Sizeof(wtsInfoExW{})) {
		return false, false
	}
	info := (*wtsInfoExW)(unsafe.Pointer(buffer))
	if info.Level != 1 {
		return false, false
	}
	flags := info.Data.SessionFlags
	if flags != wtsSessionStateLock && flags != wtsSessionStateUnlock {
		return false, false
	}
	locked := flags == wtsSessionStateLock
	major, minor, _ := windowsVersion()
	// Windows 7 / Server 2008 R2 report the two values in reverse.
	if major == 6 && minor == 1 {
		locked = !locked
	}
	return locked, true
}

//go:embed linkvideo_lock_logo.png
var linkVideoLockLogoPNG []byte

//go:embed linkvideo_display_off.png
var linkVideoDisplayOffPNG []byte

func makeSessionLockedFrame(width, height int) []byte {
	if frame := makeEmbeddedPNGFrame(width, height, linkVideoDisplayOffPNG); len(frame) == width*height*4 {
		return frame
	}
	return makeWindowsStatusFrameStyled(
		width,
		height,
		"Экран заблокирован",
		"Трансляция восстановится при входе в систему",
		true,
	)
}

// makeEmbeddedPNGFrame scales and center-crops a branded PNG into a raw BGRA
// frame. Each physical monitor receives its own complete image when the capture
// plan spans multiple displays.
func makeEmbeddedPNGFrame(width, height int, pngData []byte) []byte {
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

	// Scale to fill the destination and crop only the excess dimension. The
	// source asset is 16:9, so normal desktop displays retain the full design.
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

func makeProtectedDesktopFrame(width, height int) []byte {
	return makeWindowsStatusFrame(width, height, "Защищённый экран Windows", "Ожидание доступного кадра")
}

func makeWindowsStatusFrame(width, height int, title, subtitle string) []byte {
	return makeWindowsStatusFrameStyled(width, height, title, subtitle, false)
}

func makeWindowsStatusFrameStyled(width, height int, title, subtitle string, branded bool) []byte {
	if width < 2 || height < 2 {
		return makeFallbackStatusFrame(width, height)
	}
	screenDC, _, _ := captureGetDC.Call(0)
	if screenDC == 0 {
		return makeFallbackStatusFrame(width, height)
	}
	defer captureReleaseDC.Call(0, screenDC)
	memoryDC, _, _ := captureCreateCompatibleDC.Call(screenDC)
	if memoryDC == 0 {
		return makeFallbackStatusFrame(width, height)
	}
	defer captureDeleteDC.Call(memoryDC)

	info := gdiBitmapInfo{}
	info.Header.Size = uint32(unsafe.Sizeof(info.Header))
	info.Header.Width = int32(width)
	info.Header.Height = -int32(height)
	info.Header.Planes = 1
	info.Header.BitCount = 32
	info.Header.Compression = gdiBI_RGB
	info.Header.SizeImage = uint32(width * height * 4)
	var bits unsafe.Pointer
	bitmap, _, _ := captureCreateDIBSection.Call(
		screenDC,
		uintptr(unsafe.Pointer(&info)),
		gdiDIB_RGB_COLORS,
		uintptr(unsafe.Pointer(&bits)),
		0,
		0,
	)
	if bitmap == 0 || bits == nil {
		return makeFallbackStatusFrame(width, height)
	}
	defer captureDeleteObject.Call(bitmap)
	oldBitmap, _, _ := captureSelectObject.Call(memoryDC, bitmap)
	if oldBitmap == 0 {
		return makeFallbackStatusFrame(width, height)
	}
	defer captureSelectObject.Call(memoryDC, oldBitmap)

	client := rect32{Right: int32(width), Bottom: int32(height)}
	brush, _, _ := procCreateSolidBrush.Call(rgb(31, 35, 40))
	if brush != 0 {
		procFillRect.Call(memoryDC, uintptr(unsafe.Pointer(&client)), brush)
		procDeleteObject.Call(brush)
	}
	procSetBkMode.Call(memoryDC, transparent)
	face, _ := syscall.UTF16PtrFromString("Segoe UI")

	titleSize := clampStatusSize(height/18, 26, 64)
	titleTop := height/2 - titleSize
	titleBottom := height / 2
	logoSize := 0
	logoTop := 0

	if branded {
		logoSize = clampStatusSize(height/7, 64, 140)
		logoTop = height/2 - logoSize - clampStatusSize(height/13, 34, 88)

		// A high-contrast LinkVideo accent makes the status readable even when
		// the player scales a Full HD frame down to a small preview tile.
		accentWidth := clampStatusSize(width/7, 150, 360)
		accentHeight := clampStatusSize(height/180, 4, 8)
		accentLeft := (width - accentWidth) / 2
		accentTop := logoTop - clampStatusSize(height/32, 14, 30)
		accentRect := rect32{Left: int32(accentLeft), Top: int32(accentTop), Right: int32(accentLeft + accentWidth), Bottom: int32(accentTop + accentHeight)}
		accentBrush, _, _ := procCreateSolidBrush.Call(rgb(244, 115, 33))
		if accentBrush != 0 {
			procFillRect.Call(memoryDC, uintptr(unsafe.Pointer(&accentRect)), accentBrush)
			procDeleteObject.Call(accentBrush)
		}

		brandSize := clampStatusSize(height/30, 24, 44)
		brandTop := logoTop + logoSize + 6
		drawStatusText(memoryDC, face, "LinkVideo", brandSize, 600, rgb(242, 244, 246), width, brandTop, brandTop+brandSize*2)

		titleSize = clampStatusSize(height/24, 26, 54)
		titleTop = brandTop + brandSize*2 + 8
		titleBottom = titleTop + titleSize*2
	}

	drawStatusText(memoryDC, face, title, titleSize, 600, rgb(242, 244, 246), width, titleTop, titleBottom)

	subSize := titleSize / 2
	if subSize < 16 {
		subSize = 16
	}
	subTop := titleBottom + 8
	if !branded {
		subTop = height/2 + 8
	}
	subtitleColor := rgb(174, 181, 189)
	if branded {
		subtitleColor = rgb(232, 235, 238)
		subSize = clampStatusSize(height/34, 18, 34)
	}
	drawStatusText(memoryDC, face, subtitle, subSize, 500, subtitleColor, width, subTop, subTop+subSize*3)

	frame := make([]byte, width*height*4)
	copy(frame, unsafe.Slice((*byte)(bits), len(frame)))
	if branded {
		overlayLinkVideoLogo(frame, width, height, logoSize, logoTop)
	}
	return frame
}

func drawStatusText(memoryDC uintptr, face *uint16, textValue string, fontSize, weight int, textColor uintptr, width, top, bottom int) {
	font, _, _ := procCreateFontW.Call(
		^uintptr(fontSize-1), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 4, 0,
		uintptr(unsafe.Pointer(face)),
	)
	if font == 0 {
		return
	}
	oldFont, _, _ := procSelectObject.Call(memoryDC, font)
	procSetTextColor.Call(memoryDC, textColor)
	text, _ := syscall.UTF16FromString(textValue)
	r := rect32{
		Left: int32(width / 12), Top: int32(top),
		Right: int32(width - width/12), Bottom: int32(bottom),
	}
	procDrawTextW.Call(
		memoryDC,
		uintptr(unsafe.Pointer(&text[0])),
		uintptr(len(text)-1),
		uintptr(unsafe.Pointer(&r)),
		dtCenter|dtVCenter|dtSingleLine|dtNoPrefix,
	)
	procSelectObject.Call(memoryDC, oldFont)
	procDeleteObject.Call(font)
}

func overlayLinkVideoLogo(frame []byte, width, height, targetSize, top int) {
	if targetSize <= 0 || width <= 0 || height <= 0 || len(frame) < width*height*4 {
		return
	}
	logo, err := png.Decode(bytes.NewReader(linkVideoLockLogoPNG))
	if err != nil {
		return
	}
	bounds := logo.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return
	}
	left := (width - targetSize) / 2
	for dy := 0; dy < targetSize; dy++ {
		y := top + dy
		if y < 0 || y >= height {
			continue
		}
		sy := bounds.Min.Y + dy*bounds.Dy()/targetSize
		for dx := 0; dx < targetSize; dx++ {
			x := left + dx
			if x < 0 || x >= width {
				continue
			}
			sx := bounds.Min.X + dx*bounds.Dx()/targetSize
			pixel := color.NRGBAModel.Convert(logo.At(sx, sy)).(color.NRGBA)
			if pixel.A == 0 {
				continue
			}
			offset := (y*width + x) * 4
			alpha := uint32(pixel.A)
			inverse := uint32(255 - pixel.A)
			frame[offset+0] = byte((uint32(pixel.B)*alpha + uint32(frame[offset+0])*inverse + 127) / 255)
			frame[offset+1] = byte((uint32(pixel.G)*alpha + uint32(frame[offset+1])*inverse + 127) / 255)
			frame[offset+2] = byte((uint32(pixel.R)*alpha + uint32(frame[offset+2])*inverse + 127) / 255)
			frame[offset+3] = 255
		}
	}
}

func clampStatusSize(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

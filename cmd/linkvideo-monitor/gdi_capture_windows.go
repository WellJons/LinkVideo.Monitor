//go:build windows

package main

import (
	"fmt"
	"io"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

type gdiPoint struct {
	X int32
	Y int32
}

type gdiCursorInfo struct {
	Size   uint32
	Flags  uint32
	Cursor uintptr
	Screen gdiPoint
}

type gdiIconInfo struct {
	Icon     int32
	HotspotX uint32
	HotspotY uint32
	Mask     uintptr
	Color    uintptr
}

type gdiBitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type gdiRGBQuad struct {
	Blue     byte
	Green    byte
	Red      byte
	Reserved byte
}

type gdiBitmapInfo struct {
	Header gdiBitmapInfoHeader
	Colors [1]gdiRGBQuad
}

const (
	gdiBI_RGB         = 0
	gdiDIB_RGB_COLORS = 0
	gdiSRCCOPY        = 0x00CC0020
	gdiCursorShowing  = 0x00000001
	gdiDINormal       = 0x0003
	gdiHalfTone       = 4
)

var (
	captureGDI32              = syscall.NewLazyDLL("gdi32.dll")
	captureCreateCompatibleDC = captureGDI32.NewProc("CreateCompatibleDC")
	captureDeleteDC           = captureGDI32.NewProc("DeleteDC")
	captureCreateDIBSection   = captureGDI32.NewProc("CreateDIBSection")
	captureSelectObject       = captureGDI32.NewProc("SelectObject")
	captureDeleteObject       = captureGDI32.NewProc("DeleteObject")
	captureBitBlt             = captureGDI32.NewProc("BitBlt")
	captureStretchBlt         = captureGDI32.NewProc("StretchBlt")
	captureSetStretchBltMode  = captureGDI32.NewProc("SetStretchBltMode")
	captureGetDC              = user32.NewProc("GetDC")
	captureReleaseDC          = user32.NewProc("ReleaseDC")
	captureGetCursorInfo      = user32.NewProc("GetCursorInfo")
	captureGetIconInfo        = user32.NewProc("GetIconInfo")
	captureDrawIconEx         = user32.NewProc("DrawIconEx")
	captureGetSystemMetrics   = user32.NewProc("GetSystemMetrics")
	captureSetProcessDPIAware = user32.NewProc("SetProcessDPIAware")
)

// runGDICapture writes top-down BGRA frames to out. It is intentionally kept
// inside the existing application binary, so the compatibility fallback does
// not add another runtime or a large dependency to the installer.
func runGDICapture(out io.Writer, x, y, width, height, outputWidth, outputHeight, fps int, drawCursor bool) error {
	return runGDICaptureInternal(out, x, y, width, height, outputWidth, outputHeight, fps, drawCursor, false)
}

func runGDICaptureInternal(out io.Writer, x, y, width, height, outputWidth, outputHeight, fps int, drawCursor, reacquireEveryFrame bool) error {
	if width < 2 || height < 2 || outputWidth < 2 || outputHeight < 2 {
		return fmt.Errorf("invalid capture size %dx%d -> %dx%d", width, height, outputWidth, outputHeight)
	}
	if fps < 1 || fps > 60 {
		return fmt.Errorf("invalid capture frame rate %d", fps)
	}
	width = even(width)
	height = even(height)
	outputWidth = even(outputWidth)
	outputHeight = even(outputHeight)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	_, _, _ = captureSetProcessDPIAware.Call()

	screenDC, _, callErr := captureGetDC.Call(0)
	if screenDC == 0 {
		return fmt.Errorf("GetDC failed: %v", callErr)
	}
	defer func() {
		if screenDC != 0 {
			_, _, _ = captureReleaseDC.Call(0, screenDC)
		}
	}()

	memoryDC, _, callErr := captureCreateCompatibleDC.Call(screenDC)
	if memoryDC == 0 {
		return fmt.Errorf("CreateCompatibleDC failed: %v", callErr)
	}
	defer captureDeleteDC.Call(memoryDC)

	info := gdiBitmapInfo{}
	info.Header.Size = uint32(unsafe.Sizeof(info.Header))
	info.Header.Width = int32(outputWidth)
	// A negative height creates a top-down DIB, matching FFmpeg rawvideo.
	info.Header.Height = -int32(outputHeight)
	info.Header.Planes = 1
	info.Header.BitCount = 32
	info.Header.Compression = gdiBI_RGB
	info.Header.SizeImage = uint32(outputWidth * outputHeight * 4)

	var bits unsafe.Pointer
	bitmap, _, callErr := captureCreateDIBSection.Call(
		screenDC,
		uintptr(unsafe.Pointer(&info)),
		gdiDIB_RGB_COLORS,
		uintptr(unsafe.Pointer(&bits)),
		0,
		0,
	)
	if bitmap == 0 || bits == nil {
		return fmt.Errorf("CreateDIBSection failed: %v", callErr)
	}
	defer captureDeleteObject.Call(bitmap)
	old, _, _ := captureSelectObject.Call(memoryDC, bitmap)
	if old == 0 {
		return fmt.Errorf("SelectObject failed")
	}
	defer captureSelectObject.Call(memoryDC, old)

	frameSize := outputWidth * outputHeight * 4
	frame := unsafe.Slice((*byte)(bits), frameSize)
	period := time.Second / time.Duration(fps)
	next := time.Now()

	for {
		// Do not add CAPTUREBLT here. On some Windows/GPU combinations it
		// temporarily removes and restores the software/animated system cursor
		// for every BitBlt call. That makes the physical pointer flicker while
		// the stream itself remains stable because we draw its cursor manually.
		// SRCCOPY is sufficient for the composed desktop on supported Windows.
		//
		// When Windows switches to the secure UAC desktop, BitBlt can fail or
		// return no new pixels. We intentionally keep the previous DIB contents
		// and still emit a frame, preserving the encoder and RTSP connection.
		// The Winlogon helper reacquires its desktop DC every frame. A DC opened
		// while Winlogon is inactive can remain stale after Win+L on some systems.
		if reacquireEveryFrame {
			if screenDC != 0 {
				_, _, _ = captureReleaseDC.Call(0, screenDC)
			}
			screenDC, _, _ = captureGetDC.Call(0)
			if screenDC == 0 {
				time.Sleep(period)
				continue
			}
		}
		var ok uintptr
		if outputWidth == width && outputHeight == height {
			ok, _, _ = captureBitBlt.Call(
				memoryDC, 0, 0, uintptr(outputWidth), uintptr(outputHeight),
				screenDC, uintptr(int64(x)), uintptr(int64(y)),
				gdiSRCCOPY,
			)
		} else {
			_, _, _ = captureSetStretchBltMode.Call(memoryDC, gdiHalfTone)
			ok, _, _ = captureStretchBlt.Call(
				memoryDC, 0, 0, uintptr(outputWidth), uintptr(outputHeight),
				screenDC, uintptr(int64(x)), uintptr(int64(y)), uintptr(width), uintptr(height),
				gdiSRCCOPY,
			)
		}
		if ok != 0 && drawCursor {
			drawGDICursor(memoryDC, x, y, width, height, outputWidth, outputHeight)
		} else if ok == 0 {
			// A desktop switch can invalidate a screen DC. Reacquire it so capture
			// resumes after the UAC desktop closes, while the previous frame keeps
			// flowing to the encoder in the meantime.
			_, _, _ = captureReleaseDC.Call(0, screenDC)
			screenDC, _, _ = captureGetDC.Call(0)
		}

		if err := writeFull(out, frame); err != nil {
			if err == syscall.ERROR_BROKEN_PIPE {
				return nil
			}
			return err
		}

		next = next.Add(period)
		now := time.Now()
		if now.Sub(next) > period {
			// Do not generate a burst after a scheduler pause.
			next = now
		}
		if wait := time.Until(next); wait > 0 {
			time.Sleep(wait)
		}
	}
}

func drawGDICursor(memoryDC uintptr, captureX, captureY, sourceWidth, sourceHeight, outputWidth, outputHeight int) {
	ci := gdiCursorInfo{Size: uint32(unsafe.Sizeof(gdiCursorInfo{}))}
	ok, _, _ := captureGetCursorInfo.Call(uintptr(unsafe.Pointer(&ci)))
	if ok == 0 || ci.Flags&gdiCursorShowing == 0 || ci.Cursor == 0 {
		return
	}
	var ii gdiIconInfo
	ok, _, _ = captureGetIconInfo.Call(ci.Cursor, uintptr(unsafe.Pointer(&ii)))
	if ok == 0 {
		return
	}
	if ii.Mask != 0 {
		defer captureDeleteObject.Call(ii.Mask)
	}
	if ii.Color != 0 {
		defer captureDeleteObject.Call(ii.Color)
	}

	scaleX := float64(outputWidth) / float64(sourceWidth)
	scaleY := float64(outputHeight) / float64(sourceHeight)
	cursorW, _, _ := captureGetSystemMetrics.Call(13) // SM_CXCURSOR
	cursorH, _, _ := captureGetSystemMetrics.Call(14) // SM_CYCURSOR
	drawW := int(float64(cursorW) * scaleX)
	drawH := int(float64(cursorH) * scaleY)
	if drawW < 8 {
		drawW = 8
	}
	if drawH < 8 {
		drawH = 8
	}
	drawX := int(float64(int(ci.Screen.X)-captureX)*scaleX - float64(ii.HotspotX)*scaleX)
	drawY := int(float64(int(ci.Screen.Y)-captureY)*scaleY - float64(ii.HotspotY)*scaleY)
	// Skip cursors that are completely outside the captured rectangle.
	if drawX >= outputWidth || drawY >= outputHeight || drawX < -drawW || drawY < -drawH {
		return
	}
	_, _, _ = captureDrawIconEx.Call(
		memoryDC,
		uintptr(int64(drawX)),
		uintptr(int64(drawY)),
		ci.Cursor,
		uintptr(drawW),
		uintptr(drawH),
		0,
		0,
		gdiDINormal,
	)
}

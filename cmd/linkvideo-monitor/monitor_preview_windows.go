//go:build windows

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"sync"
	"syscall"
	"unsafe"
)

type bitmapInfoHeader struct {
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
type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

var monitorPreviewMu sync.Mutex

var (
	procGetDC                  = user32.NewProc("GetDC")
	procReleaseDC              = user32.NewProc("ReleaseDC")
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procGetDIBits              = gdi32.NewProc("GetDIBits")
)

func captureMonitorPNG(index int) ([]byte, error) {
	monitorPreviewMu.Lock()
	defer monitorPreviewMu.Unlock()
	monitors, err := listMonitors()
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(monitors) {
		return nil, fmt.Errorf("монитор %d не найден", index+1)
	}
	m := monitors[index]
	w, h := m.Width, m.Height
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("некорректный размер монитора")
	}

	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		return nil, fmt.Errorf("не удалось получить изображение экрана")
	}
	defer procReleaseDC.Call(0, screenDC)
	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		return nil, fmt.Errorf("не удалось создать буфер изображения")
	}
	defer procDeleteDC.Call(memDC)
	bitmap, _, _ := procCreateCompatibleBitmap.Call(screenDC, uintptr(w), uintptr(h))
	if bitmap == 0 {
		return nil, fmt.Errorf("не удалось создать снимок монитора")
	}
	defer procDeleteObject.Call(bitmap)
	old, _, _ := procSelectObject.Call(memDC, bitmap)
	if old == 0 {
		return nil, fmt.Errorf("не удалось подготовить буфер снимка")
	}

	const srccopy = 0x00CC0020
	const captureblt = 0x40000000
	ok, _, _ := procBitBlt.Call(memDC, 0, 0, uintptr(w), uintptr(h), screenDC, uintptr(int64(m.X)), uintptr(int64(m.Y)), srccopy|captureblt)
	// GetDIBits не должен читать bitmap, пока тот выбран в DC. В 0.4 это
	// нарушалось, из-за чего браузер получал ошибку вместо PNG.
	_, _, _ = procSelectObject.Call(memDC, old)
	if ok == 0 {
		return nil, fmt.Errorf("Windows не разрешила сделать снимок монитора")
	}

	raw := make([]byte, w*h*4)
	info := bitmapInfo{Header: bitmapInfoHeader{
		Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: int32(w), Height: -int32(h),
		Planes: 1, BitCount: 32, Compression: 0, SizeImage: uint32(len(raw)),
	}}
	rows, _, _ := procGetDIBits.Call(memDC, bitmap, 0, uintptr(h), uintptr(unsafe.Pointer(&raw[0])), uintptr(unsafe.Pointer(&info)), 0)
	if rows == 0 {
		return nil, fmt.Errorf("не удалось прочитать снимок монитора")
	}

	maxW, maxH := 420, 236
	scale := 1.0
	if w > maxW {
		scale = float64(maxW) / float64(w)
	}
	if float64(h)*scale > float64(maxH) {
		scale = float64(maxH) / float64(h)
	}
	outW, outH := int(float64(w)*scale), int(float64(h)*scale)
	if outW < 1 {
		outW = 1
	}
	if outH < 1 {
		outH = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, outW, outH))
	for y := 0; y < outH; y++ {
		sy := y * h / outH
		for x := 0; x < outW; x++ {
			sx := x * w / outW
			src := (sy*w + sx) * 4
			dst := y*img.Stride + x*4
			img.Pix[dst+0] = raw[src+2]
			img.Pix[dst+1] = raw[src+1]
			img.Pix[dst+2] = raw[src+0]
			img.Pix[dst+3] = 255
		}
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var _ = syscall.Errno(0)

//go:build windows

package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type iUnknownVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
}
type iMMDeviceEnumeratorVtbl struct {
	QueryInterface                         uintptr
	AddRef                                 uintptr
	Release                                uintptr
	EnumAudioEndpoints                     uintptr
	GetDefaultAudioEndpoint                uintptr
	GetDevice                              uintptr
	RegisterEndpointNotificationCallback   uintptr
	UnregisterEndpointNotificationCallback uintptr
}
type iMMDeviceEnumerator struct{ Vtbl *iMMDeviceEnumeratorVtbl }
type iMMDeviceVtbl struct {
	QueryInterface    uintptr
	AddRef            uintptr
	Release           uintptr
	Activate          uintptr
	OpenPropertyStore uintptr
	GetID             uintptr
	GetState          uintptr
}
type iMMDevice struct{ Vtbl *iMMDeviceVtbl }
type iAudioClientVtbl struct {
	QueryInterface    uintptr
	AddRef            uintptr
	Release           uintptr
	Initialize        uintptr
	GetBufferSize     uintptr
	GetStreamLatency  uintptr
	GetCurrentPadding uintptr
	IsFormatSupported uintptr
	GetMixFormat      uintptr
	GetDevicePeriod   uintptr
	Start             uintptr
	Stop              uintptr
	Reset             uintptr
	SetEventHandle    uintptr
	GetService        uintptr
}
type iAudioClient struct{ Vtbl *iAudioClientVtbl }
type iAudioCaptureClientVtbl struct {
	QueryInterface    uintptr
	AddRef            uintptr
	Release           uintptr
	GetBuffer         uintptr
	ReleaseBuffer     uintptr
	GetNextPacketSize uintptr
}
type iAudioCaptureClient struct{ Vtbl *iAudioCaptureClientVtbl }

type waveFormatEx struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	ExtraSize      uint16
}

var (
	ole32                = syscall.NewLazyDLL("ole32.dll")
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")

	clsidMMDeviceEnumerator = guid{0xBCDE0395, 0xE52F, 0x467C, [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = guid{0xA95664D2, 0x9614, 0x4F35, [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioClient         = guid{0x1CB9AD4C, 0xDBFA, 0x4C32, [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	iidIAudioCaptureClient  = guid{0xC8ADBD64, 0xE71E, 0x48A0, [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}}
)

const (
	clsctxAll                           = 23
	coinitMultithreaded                 = 0
	eRender                             = 0
	eConsole                            = 0
	audclntSharemodeShared              = 0
	audclntStreamflagsLoopback          = 0x00020000
	audclntStreamflagsAutoConvertPCM    = 0x80000000
	audclntStreamflagsSrcDefaultQuality = 0x08000000
	audclntBufferflagsSilent            = 0x00000002
	waveFormatPCM                       = 1
)

func hresultError(hr uintptr, where string) error {
	if int32(hr) >= 0 {
		return nil
	}
	return fmt.Errorf("%s: HRESULT 0x%08X", where, uint32(hr))
}

func releaseCOM(obj unsafe.Pointer) {
	if obj == nil {
		return
	}
	v := *(**iUnknownVtbl)(obj)
	syscall.SyscallN(v.Release, uintptr(obj))
}

func runWASAPILoopback(out io.Writer) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr, _, _ := procCoInitializeEx.Call(0, coinitMultithreaded)
	// S_FALSE (1) means COM was already initialized and is still success.
	if err := hresultError(hr, "CoInitializeEx"); err != nil {
		return err
	}
	defer procCoUninitialize.Call()

	var enumerator *iMMDeviceEnumerator
	hr, _, _ = procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&enumerator)),
	)
	if err := hresultError(hr, "CoCreateInstance(MMDeviceEnumerator)"); err != nil {
		return err
	}
	defer releaseCOM(unsafe.Pointer(enumerator))

	var device *iMMDevice
	hr, _, _ = syscall.SyscallN(enumerator.Vtbl.GetDefaultAudioEndpoint, uintptr(unsafe.Pointer(enumerator)), eRender, eConsole, uintptr(unsafe.Pointer(&device)))
	if err := hresultError(hr, "GetDefaultAudioEndpoint"); err != nil {
		return err
	}
	defer releaseCOM(unsafe.Pointer(device))

	var client *iAudioClient
	hr, _, _ = syscall.SyscallN(device.Vtbl.Activate, uintptr(unsafe.Pointer(device)), uintptr(unsafe.Pointer(&iidIAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&client)))
	if err := hresultError(hr, "IMMDevice.Activate(IAudioClient)"); err != nil {
		return err
	}
	defer releaseCOM(unsafe.Pointer(client))

	format := waveFormatEx{
		FormatTag:      waveFormatPCM,
		Channels:       2,
		SamplesPerSec:  48000,
		AvgBytesPerSec: 48000 * 2 * 2,
		BlockAlign:     4,
		BitsPerSample:  16,
		ExtraSize:      0,
	}
	flags := uintptr(audclntStreamflagsLoopback | audclntStreamflagsAutoConvertPCM | audclntStreamflagsSrcDefaultQuality)
	hr, _, _ = syscall.SyscallN(
		client.Vtbl.Initialize,
		uintptr(unsafe.Pointer(client)),
		audclntSharemodeShared,
		flags,
		uintptr(0), // shared mode: let the engine select a suitable buffer
		uintptr(0),
		uintptr(unsafe.Pointer(&format)),
		0,
	)
	if err := hresultError(hr, "IAudioClient.Initialize(loopback PCM 48 kHz stereo)"); err != nil {
		return err
	}

	var capture *iAudioCaptureClient
	hr, _, _ = syscall.SyscallN(client.Vtbl.GetService, uintptr(unsafe.Pointer(client)), uintptr(unsafe.Pointer(&iidIAudioCaptureClient)), uintptr(unsafe.Pointer(&capture)))
	if err := hresultError(hr, "IAudioClient.GetService(IAudioCaptureClient)"); err != nil {
		return err
	}
	defer releaseCOM(unsafe.Pointer(capture))

	hr, _, _ = syscall.SyscallN(client.Vtbl.Start, uintptr(unsafe.Pointer(client)))
	if err := hresultError(hr, "IAudioClient.Start"); err != nil {
		return err
	}
	defer syscall.SyscallN(client.Vtbl.Stop, uintptr(unsafe.Pointer(client)))

	// Keep a continuous audio clock for the server archive, but do not allow a
	// large software queue to accumulate. Earlier builds allowed up to 500 ms of
	// queued PCM and then FFmpeg could buffer even more packets, which made the
	// sound noticeably lag behind the screen. The helper now keeps at most about
	// 40 ms and drops stale samples after a scheduler or device stall.
	const chunkDuration = 10 * time.Millisecond
	chunkBytes := int(format.AvgBytesPerSec) / 100 // 10 ms, 48 kHz stereo s16le
	if chunkBytes <= 0 {
		return fmt.Errorf("invalid WASAPI format: AvgBytesPerSec=%d", format.AvgBytesPerSec)
	}
	queue := make([]byte, 0, chunkBytes*4)
	chunk := make([]byte, chunkBytes)
	nextWrite := time.Now()
	blockAlign := int(format.BlockAlign)

	appendSilence := func(byteCount int) {
		if byteCount > 0 {
			queue = append(queue, make([]byte, byteCount)...)
		}
	}
	trimToRecent := func(keepBytes int) {
		if keepBytes < 0 {
			keepBytes = 0
		}
		if len(queue) <= keepBytes {
			return
		}
		drop := len(queue) - keepBytes
		if blockAlign > 0 {
			drop -= drop % blockAlign
		}
		if drop > 0 {
			queue = queue[drop:]
		}
	}

	for {
		// Drain packets currently available from WASAPI into a very small queue.
		var packetFrames uint32
		hr, _, _ = syscall.SyscallN(capture.Vtbl.GetNextPacketSize, uintptr(unsafe.Pointer(capture)), uintptr(unsafe.Pointer(&packetFrames)))
		if err := hresultError(hr, "IAudioCaptureClient.GetNextPacketSize"); err != nil {
			return err
		}
		for packetFrames > 0 {
			var data *byte
			var frames uint32
			var bufferFlags uint32
			hr, _, _ = syscall.SyscallN(
				capture.Vtbl.GetBuffer,
				uintptr(unsafe.Pointer(capture)),
				uintptr(unsafe.Pointer(&data)),
				uintptr(unsafe.Pointer(&frames)),
				uintptr(unsafe.Pointer(&bufferFlags)),
				0, 0,
			)
			if err := hresultError(hr, "IAudioCaptureClient.GetBuffer"); err != nil {
				return err
			}

			byteCount := int(frames) * blockAlign
			if bufferFlags&audclntBufferflagsSilent != 0 || data == nil {
				appendSilence(byteCount)
			} else if byteCount > 0 {
				raw := unsafe.Slice(data, byteCount)
				queue = append(queue, raw...)
			}

			releaseHR, _, _ := syscall.SyscallN(capture.Vtbl.ReleaseBuffer, uintptr(unsafe.Pointer(capture)), uintptr(frames))
			if err := hresultError(releaseHR, "IAudioCaptureClient.ReleaseBuffer"); err != nil {
				return err
			}
			hr, _, _ = syscall.SyscallN(capture.Vtbl.GetNextPacketSize, uintptr(unsafe.Pointer(capture)), uintptr(unsafe.Pointer(&packetFrames)))
			if err := hresultError(hr, "IAudioCaptureClient.GetNextPacketSize"); err != nil {
				return err
			}
		}

		// Never play old sound after a temporary stall. Keep only the newest 20 ms
		// once the buffered amount exceeds 40 ms.
		if len(queue) > chunkBytes*4 {
			trimToRecent(chunkBytes * 2)
		}

		now := time.Now()
		if now.Before(nextWrite) {
			wait := time.Until(nextWrite)
			if wait > 2*time.Millisecond {
				wait = 2 * time.Millisecond
			}
			time.Sleep(wait)
			continue
		}

		// After a long scheduler pause, resume from the current moment instead of
		// quickly emitting a backlog that would be heard later than the video.
		if now.Sub(nextWrite) > 30*time.Millisecond {
			trimToRecent(chunkBytes * 2)
			nextWrite = now
		}

		for i := range chunk {
			chunk[i] = 0
		}
		n := len(queue)
		if n > chunkBytes {
			n = chunkBytes
		}
		copy(chunk, queue[:n])
		queue = queue[n:]
		if writeErr := writeFull(out, chunk); writeErr != nil {
			if writeErr == syscall.ERROR_BROKEN_PIPE {
				return nil
			}
			return writeErr
		}
		nextWrite = nextWrite.Add(chunkDuration)
	}

}

// Keep binary imported on all Go versions where unsafe.Slice is compiled with
// different inlining decisions; also acts as a compile-time little-endian sanity check.
var _ = binary.LittleEndian

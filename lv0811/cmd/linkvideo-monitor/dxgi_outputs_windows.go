//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

type winGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type dxgiOutputDesc struct {
	DeviceName         [32]uint16
	DesktopCoordinates winRect
	AttachedToDesktop  int32
	Rotation           uint32
	Monitor            uintptr
}

type dxgiOutputIdentity struct {
	AdapterIndex int
	OutputIndex  int
	DeviceName   string
	Monitor      uintptr
	X            int
	Y            int
	Width        int
	Height       int
}

var (
	dxgiDLL                = syscall.NewLazyDLL("dxgi.dll")
	procCreateDXGIFactory1 = dxgiDLL.NewProc("CreateDXGIFactory1")
	iidIDXGIFactory1       = winGUID{0x770aae78, 0xf26f, 0x4dba, [8]byte{0xa8, 0x29, 0x25, 0x3c, 0x83, 0xd1, 0xb3, 0x87}}
)

const dxgiErrorNotFound = uintptr(0x887A0002)

func comVTableMethod(obj uintptr, index int) uintptr {
	if obj == 0 {
		return 0
	}
	vtbl := *(*uintptr)(unsafe.Pointer(obj))
	return *(*uintptr)(unsafe.Pointer(vtbl + uintptr(index)*unsafe.Sizeof(uintptr(0))))
}

func comRelease(obj uintptr) {
	if obj == 0 {
		return
	}
	method := comVTableMethod(obj, 2)
	if method != 0 {
		_, _, _ = syscall.SyscallN(method, obj)
	}
}

func enumerateDXGIOutputs() ([]dxgiOutputIdentity, error) {
	var factory uintptr
	hr, _, _ := procCreateDXGIFactory1.Call(
		uintptr(unsafe.Pointer(&iidIDXGIFactory1)),
		uintptr(unsafe.Pointer(&factory)),
	)
	if int32(hr) < 0 || factory == 0 {
		return nil, fmt.Errorf("CreateDXGIFactory1: 0x%08X", uint32(hr))
	}
	defer comRelease(factory)

	enumAdapters1 := comVTableMethod(factory, 12)
	if enumAdapters1 == 0 {
		return nil, fmt.Errorf("IDXGIFactory1::EnumAdapters1 недоступен")
	}

	var result []dxgiOutputIdentity
	for adapterIndex := 0; ; adapterIndex++ {
		var adapter uintptr
		hr, _, _ = syscall.SyscallN(enumAdapters1, factory, uintptr(adapterIndex), uintptr(unsafe.Pointer(&adapter)))
		if hr == dxgiErrorNotFound || uint32(hr) == uint32(dxgiErrorNotFound) {
			break
		}
		if int32(hr) < 0 || adapter == 0 {
			break
		}

		enumOutputs := comVTableMethod(adapter, 7)
		for outputIndex := 0; enumOutputs != 0; outputIndex++ {
			var output uintptr
			outHR, _, _ := syscall.SyscallN(enumOutputs, adapter, uintptr(outputIndex), uintptr(unsafe.Pointer(&output)))
			if outHR == dxgiErrorNotFound || uint32(outHR) == uint32(dxgiErrorNotFound) {
				break
			}
			if int32(outHR) < 0 || output == 0 {
				break
			}
			getDesc := comVTableMethod(output, 7)
			var desc dxgiOutputDesc
			descHR, _, _ := syscall.SyscallN(getDesc, output, uintptr(unsafe.Pointer(&desc)))
			if int32(descHR) >= 0 && desc.AttachedToDesktop != 0 {
				result = append(result, dxgiOutputIdentity{
					AdapterIndex: adapterIndex,
					OutputIndex:  outputIndex,
					DeviceName:   syscall.UTF16ToString(desc.DeviceName[:]),
					Monitor:      desc.Monitor,
					X:            int(desc.DesktopCoordinates.Left),
					Y:            int(desc.DesktopCoordinates.Top),
					Width:        int(desc.DesktopCoordinates.Right - desc.DesktopCoordinates.Left),
					Height:       int(desc.DesktopCoordinates.Bottom - desc.DesktopCoordinates.Top),
				})
			}
			comRelease(output)
		}
		comRelease(adapter)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("DXGI не вернул подключённые выходы")
	}
	return result, nil
}

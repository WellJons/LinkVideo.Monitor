//go:build windows

package main

import (
	"strings"
	"syscall"
	"unsafe"
)

func videoAdapterNames() []string {
	result := make([]string, 0, 4)
	for index := uint32(0); index < 16; index++ {
		var dd displayDeviceW
		dd.Size = uint32(unsafe.Sizeof(dd))
		ok, _, _ := procEnumDisplayDevicesW.Call(0, uintptr(index), uintptr(unsafe.Pointer(&dd)), 0)
		if ok == 0 {
			break
		}
		name := strings.TrimSpace(syscall.UTF16ToString(dd.DeviceString[:]))
		if name != "" {
			result = append(result, name)
		}
	}
	return result
}

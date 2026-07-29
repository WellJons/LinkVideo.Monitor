//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

type rtlOSVersionInfo struct {
	Size        uint32
	Major       uint32
	Minor       uint32
	Build       uint32
	PlatformID  uint32
	ServicePack [128]uint16
}

var (
	ntdll             = syscall.NewLazyDLL("ntdll.dll")
	procRtlGetVersion = ntdll.NewProc("RtlGetVersion")
)

func windowsVersion() (major, minor, build uint32) {
	info := rtlOSVersionInfo{Size: uint32(unsafe.Sizeof(rtlOSVersionInfo{}))}
	status, _, _ := procRtlGetVersion.Call(uintptr(unsafe.Pointer(&info)))
	if int32(status) < 0 {
		return 0, 0, 0
	}
	return info.Major, info.Minor, info.Build
}

func supportsDesktopDuplication() bool {
	major, minor, _ := windowsVersion()
	return major > 6 || (major == 6 && minor >= 2) // Windows 8 / Server 2012 and newer.
}

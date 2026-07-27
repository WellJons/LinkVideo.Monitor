//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	moveFileReplaceExisting = 0x00000001
	moveFileWriteThrough    = 0x00000008
)

var procMoveFileExW = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

func replaceFileAtomically(tempPath, targetPath string) error {
	tempPtr, err := syscall.UTF16PtrFromString(tempPath)
	if err != nil {
		return err
	}
	targetPtr, err := syscall.UTF16PtrFromString(targetPath)
	if err != nil {
		return err
	}
	ok, _, callErr := procMoveFileExW.Call(
		uintptr(unsafe.Pointer(tempPtr)),
		uintptr(unsafe.Pointer(targetPtr)),
		moveFileReplaceExisting|moveFileWriteThrough,
	)
	if ok == 0 {
		return fmt.Errorf("MoveFileExW: %w", callErr)
	}
	return nil
}

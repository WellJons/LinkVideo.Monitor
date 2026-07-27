//go:build !windows

package main

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

func enumerateDXGIOutputs() ([]dxgiOutputIdentity, error) { return nil, nil }

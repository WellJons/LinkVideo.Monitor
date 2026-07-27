//go:build !windows

package main

import (
	"errors"
	"io"
)

func runGDICapture(out io.Writer, x, y, width, height, outputWidth, outputHeight, fps int, drawCursor bool) error {
	return errors.New("GDI capture is supported only on Windows")
}

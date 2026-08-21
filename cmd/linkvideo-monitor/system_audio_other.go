//go:build !windows && !darwin

package main

import (
	"errors"
	"io"
)

func runWASAPILoopback(out io.Writer) error {
	return errors.New("захват системного звука пока поддерживается только в Windows и macOS")
}

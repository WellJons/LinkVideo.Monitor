//go:build darwin

package main

import (
	"fmt"
	"os"
	"strconv"
)

func init() {
	if len(os.Args) <= 1 || os.Args[1] != "--set-startup" {
		return
	}
	if len(os.Args) <= 2 {
		_, _ = fmt.Fprintln(os.Stderr, "usage: --set-startup true|false")
		os.Exit(2)
	}
	enabled, err := strconv.ParseBool(os.Args[2])
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "некорректное значение --set-startup:", os.Args[2])
		os.Exit(2)
	}
	if err := syncStartupRegistration(enabled); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(9)
	}
	status, err := startupRegistrationStatus()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(9)
	}
	_, _ = fmt.Fprintln(os.Stdout, status)
	os.Exit(0)
}

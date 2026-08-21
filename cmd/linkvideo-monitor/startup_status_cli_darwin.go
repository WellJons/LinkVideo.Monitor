//go:build darwin

package main

import (
	"fmt"
	"os"
)

func init() {
	if len(os.Args) <= 1 || os.Args[1] != "--startup-status" {
		return
	}
	status, err := startupRegistrationStatus()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(9)
	}
	_, _ = fmt.Fprintln(os.Stdout, status)
	os.Exit(0)
}

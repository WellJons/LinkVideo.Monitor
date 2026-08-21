//go:build darwin

package main

import (
	"fmt"
	"os"
)

func init() {
	if len(os.Args) != 2 || os.Args[1] != "--url-handler-status" {
		return
	}
	if err := syncURLProtocolRegistration(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	status, err := urlProtocolRegistrationStatus()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
	_, _ = fmt.Fprintln(os.Stdout, status)
	os.Exit(0)
}

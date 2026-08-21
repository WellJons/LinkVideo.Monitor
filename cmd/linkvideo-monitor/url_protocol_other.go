//go:build !windows && !darwin

package main

func syncURLProtocolRegistration() error { return nil }

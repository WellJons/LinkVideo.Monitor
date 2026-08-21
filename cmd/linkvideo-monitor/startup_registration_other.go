//go:build !windows && !darwin

package main

func syncStartupRegistration(enabled bool) error { return nil }

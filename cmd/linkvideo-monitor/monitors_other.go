//go:build !windows && !darwin

package main

import "os/exec"

func listMonitors() ([]Monitor, error) {
	return []Monitor{{Index: 0, Name: "Desktop", X: 0, Y: 0, Width: 1920, Height: 1080, Primary: true}}, nil
}
func hideChildWindow(cmd *exec.Cmd) {}

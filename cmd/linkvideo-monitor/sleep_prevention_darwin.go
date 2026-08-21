//go:build darwin

package main

import (
	"os"
	"os/exec"
	"strconv"
	"sync"
)

var (
	macOSSleepMu  sync.Mutex
	macOSSleepCmd *exec.Cmd
)

func setSleepPrevention(enabled, keepDisplayOn bool) {
	macOSSleepMu.Lock()
	if macOSSleepCmd != nil && macOSSleepCmd.Process != nil {
		_ = macOSSleepCmd.Process.Kill()
		macOSSleepCmd = nil
	}
	if !enabled {
		macOSSleepMu.Unlock()
		return
	}

	cmd := exec.Command("/usr/bin/caffeinate", macOSCaffeinateArgs(keepDisplayOn, os.Getpid())...)
	if err := cmd.Start(); err != nil {
		macOSSleepMu.Unlock()
		return
	}
	macOSSleepCmd = cmd
	macOSSleepMu.Unlock()

	go func(current *exec.Cmd) {
		_ = current.Wait()
		macOSSleepMu.Lock()
		if macOSSleepCmd == current {
			macOSSleepCmd = nil
		}
		macOSSleepMu.Unlock()
	}(cmd)
}

func macOSCaffeinateArgs(keepDisplayOn bool, pid int) []string {
	args := []string{"-i"}
	if keepDisplayOn {
		args = append(args, "-d")
	}
	if pid > 0 {
		args = append(args, "-w", strconv.Itoa(pid))
	}
	return args
}

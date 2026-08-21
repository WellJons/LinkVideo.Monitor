//go:build !darwin

package main

import "time"

func startResumeWatcher(a *app) {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		last := time.Now()
		for now := range ticker.C {
			gap := now.Sub(last)
			last = now
			if gap < 20*time.Second {
				continue
			}
			handleResumeRecovery(a, "Обнаружено возобновление системы после сна")
		}
	}()
}

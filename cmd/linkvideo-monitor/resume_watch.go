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
			a.mu.Lock()
			shouldRestart := a.cfg.RestartAfterResume && a.desired
			a.mu.Unlock()
			a.requestRemoteSync()
			if shouldRestart {
				a.appendLog("Обнаружено возобновление Windows после сна; поток будет перезапущен")
				go func() {
					time.Sleep(2 * time.Second) // дать сети и аудиоустройствам восстановиться
					if err := a.restart(); err != nil {
						a.appendLog("Не удалось перезапустить поток после сна: " + err.Error())
					}
				}()
			}
		}
	}()
}

package main

import "time"

func handleResumeRecovery(a *app, message string) {
	a.mu.Lock()
	shouldRestart := a.cfg.RestartAfterResume && a.desired
	a.mu.Unlock()
	a.requestRemoteSync()
	if !shouldRestart {
		return
	}
	if message != "" {
		a.appendLog(message + "; поток будет перезапущен")
	}
	go func() {
		time.Sleep(2 * time.Second) // дать сети, дисплеям и аудиоустройствам восстановиться
		// The user may stop the stream during this delay. Do not turn it
		// back on from an obsolete resume notification.
		a.mu.Lock()
		stillDesired := a.cfg.RestartAfterResume && a.desired
		a.mu.Unlock()
		if !stillDesired {
			return
		}
		if err := a.restart(); err != nil {
			a.appendLog("Не удалось перезапустить поток после возобновления системы: " + err.Error())
		}
	}()
}

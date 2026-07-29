package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const maxReconnectHistory = 200

func normalizeProcessExitCode(code int) int {
	// Windows exposes process exit codes as an unsigned DWORD. FFmpeg often
	// returns negative AVERROR values, so -32 (EPIPE / Broken pipe) arrives as
	// 4294967264. Convert that wrapped value back to a readable signed code.
	if code > 2147483647 && uint64(code) <= 4294967295 {
		return int(int32(uint32(code)))
	}
	return code
}

type RestartEvent struct {
	At       string `json:"at"`
	Reason   string `json:"reason"`
	ExitCode int    `json:"exit_code"`
}

func (a *app) loadReconnectHistory() {
	b, err := os.ReadFile(a.restartHistoryPath)
	if err != nil {
		return
	}
	var items []RestartEvent
	if json.Unmarshal(b, &items) != nil {
		return
	}
	for i := range items {
		items[i].ExitCode = normalizeProcessExitCode(items[i].ExitCode)
	}
	if len(items) > maxReconnectHistory {
		items = items[len(items)-maxReconnectHistory:]
	}
	a.mu.Lock()
	a.restartHistory = append([]RestartEvent(nil), items...)
	if len(items) > 0 {
		last := items[len(items)-1]
		a.lastRestartReason = last.Reason
		if parsed, err := time.Parse(time.RFC3339, last.At); err == nil {
			a.lastRestartAt = parsed
		}
	}
	a.mu.Unlock()
}

func (a *app) appendReconnectEventLocked(reason string, exitCode int, at time.Time) []RestartEvent {
	event := RestartEvent{At: at.Format(time.RFC3339), Reason: reason, ExitCode: normalizeProcessExitCode(exitCode)}
	a.restartHistory = append(a.restartHistory, event)
	if len(a.restartHistory) > maxReconnectHistory {
		a.restartHistory = append([]RestartEvent(nil), a.restartHistory[len(a.restartHistory)-maxReconnectHistory:]...)
	}
	return append([]RestartEvent(nil), a.restartHistory...)
}

func (a *app) saveReconnectHistory(items []RestartEvent) {
	if len(items) == 0 || a.restartHistoryPath == "" {
		return
	}
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(a.restartHistoryPath), 0o700)
	tmp := a.restartHistoryPath + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		if replaceFileAtomically(tmp, a.restartHistoryPath) != nil {
			_ = os.Remove(tmp)
		}
	}
}

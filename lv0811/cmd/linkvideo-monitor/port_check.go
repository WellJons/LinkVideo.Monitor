package main

import (
	"fmt"
	"net"
	"time"
)

type PortCheckResult struct {
	Port      int    `json:"port"`
	Available bool   `json:"available"`
	UsedByApp bool   `json:"used_by_app"`
	Message   string `json:"message"`
}

func (a *app) checkLocalRTSPPort(port int) PortCheckResult {
	result := PortCheckResult{Port: port}
	if port < 1 || port > 65535 {
		result.Message = "Укажите порт от 1 до 65535"
		return result
	}
	a.mu.Lock()
	ownPort := a.cfg.LocalRTSPPort
	ownRunning := a.mediaRunning || a.mediaCmd != nil
	a.mu.Unlock()
	if ownRunning && ownPort == port {
		result.Available = true
		result.UsedByApp = true
		result.Message = fmt.Sprintf("Порт %d используется локальным RTSP-сервером LinkVideo Monitor", port)
		return result
	}
	ln, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		result.Message = fmt.Sprintf("Порт %d уже занят другой программой. Укажите другой порт", port)
		return result
	}
	_ = ln.Close()
	// Give Windows a moment to release the probe socket before MediaMTX starts.
	time.Sleep(15 * time.Millisecond)
	result.Available = true
	result.Message = fmt.Sprintf("Порт %d свободен", port)
	return result
}

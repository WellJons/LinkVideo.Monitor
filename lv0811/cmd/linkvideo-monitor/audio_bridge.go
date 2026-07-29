package main

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

type systemAudioBridge struct {
	app      *app
	listener net.Listener
	cancel   context.CancelFunc
	done     chan struct{}
	mu       sync.Mutex
	conn     net.Conn
}

func startSystemAudioBridge(parent context.Context, a *app) (*systemAudioBridge, string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", fmt.Errorf("не удалось открыть локальный канал системного звука: %w", err)
	}
	ctx, cancel := context.WithCancel(parent)
	bridge := &systemAudioBridge{app: a, listener: listener, cancel: cancel, done: make(chan struct{})}
	go bridge.run(ctx)
	return bridge, "tcp://" + listener.Addr().String(), nil
}

func (b *systemAudioBridge) run(ctx context.Context) {
	defer close(b.done)
	conn, err := b.listener.Accept()
	if err != nil {
		if ctx.Err() == nil {
			b.app.appendLog("Канал системного звука: " + err.Error())
		}
		return
	}
	_ = b.listener.Close()
	b.mu.Lock()
	b.conn = conn
	b.mu.Unlock()
	defer conn.Close()

	for ctx.Err() == nil {
		err = runWASAPILoopback(conn)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			b.app.appendLog("Системный звук временно недоступен: " + err.Error())
		}
		// Keep the audio input alive while Windows changes the default playback
		// device. A short silence prevents FFmpeg from closing the stream.
		if !writeAudioSilence(ctx, conn, time.Second) {
			return
		}
	}
}

func writeAudioSilence(ctx context.Context, conn net.Conn, duration time.Duration) bool {
	const sampleRate = 48000
	const channels = 2
	const bytesPerSample = 2
	const chunk = 10 * time.Millisecond
	data := make([]byte, sampleRate*channels*bytesPerSample/100)
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		default:
		}
		if err := writeFull(conn, data); err != nil {
			return false
		}
		time.Sleep(chunk)
	}
	return true
}

func (b *systemAudioBridge) Close() {
	if b == nil {
		return
	}
	b.cancel()
	_ = b.listener.Close()
	b.mu.Lock()
	if b.conn != nil {
		_ = b.conn.Close()
	}
	b.mu.Unlock()
	select {
	case <-b.done:
	case <-time.After(2 * time.Second):
	}
}

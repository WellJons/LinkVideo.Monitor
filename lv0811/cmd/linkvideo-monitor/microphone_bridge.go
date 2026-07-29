package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	microphoneSampleRate = 48000
	microphoneChannels   = 2
)

type microphoneBridge struct {
	app        *app
	cfg        Config
	listener   net.Listener
	cancel     context.CancelFunc
	done       chan struct{}
	mu         sync.Mutex
	conn       net.Conn
	cmd        *exec.Cmd
	voiceUntil time.Time
}

func startMicrophoneBridge(parent context.Context, a *app, cfg Config) (*microphoneBridge, string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", fmt.Errorf("не удалось открыть локальный канал микрофона: %w", err)
	}
	ctx, cancel := context.WithCancel(parent)
	bridge := &microphoneBridge{app: a, cfg: cfg, listener: listener, cancel: cancel, done: make(chan struct{})}
	go bridge.run(ctx)
	return bridge, "tcp://" + listener.Addr().String(), nil
}

func (b *microphoneBridge) run(ctx context.Context) {
	defer close(b.done)
	conn, err := b.listener.Accept()
	if err != nil {
		if ctx.Err() == nil {
			b.app.appendLog("Канал микрофона: " + err.Error())
		}
		return
	}
	_ = b.listener.Close()
	b.mu.Lock()
	b.conn = conn
	b.mu.Unlock()
	defer conn.Close()

	for ctx.Err() == nil {
		err = b.captureOnce(ctx, conn)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			b.app.appendLog("Микрофон временно недоступен: " + err.Error())
		}
		b.app.updateMicrophoneRuntime(false, 0)
		if !writeAudioSilence(ctx, conn, time.Second) {
			return
		}
	}
}

func (b *microphoneBridge) captureOnce(ctx context.Context, conn net.Conn) error {
	device := strings.TrimSpace(b.cfg.MicrophoneDevice)
	if device == "" {
		return fmt.Errorf("устройство ввода не выбрано")
	}
	args := []string{
		"-hide_banner", "-loglevel", "warning",
		"-f", "dshow", "-audio_buffer_size", "50",
		"-i", "audio=" + device,
		"-ac", fmt.Sprint(microphoneChannels), "-ar", fmt.Sprint(microphoneSampleRate),
		"-f", "s16le", "pipe:1",
	}
	cmd := exec.CommandContext(ctx, resolveExecutable(b.cfg.FFmpegPath), args...)
	hideChildWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	b.mu.Lock()
	b.cmd = cmd
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.cmd = nil
		b.mu.Unlock()
	}()

	go func() {
		s := bufio.NewScanner(stderr)
		for s.Scan() {
			line := strings.TrimSpace(s.Text())
			if line != "" {
				b.app.appendLog("microphone: " + line)
			}
		}
	}()

	buf := make([]byte, microphoneSampleRate*microphoneChannels*2/50) // ~20 ms
	for ctx.Err() == nil {
		n, readErr := stdout.Read(buf)
		if n > 0 {
			chunk := buf[:n-n%2]
			levelDB, levelPct := pcmLevel(chunk)
			pass := b.shouldPass(levelDB)
			if !pass {
				for i := range chunk {
					chunk[i] = 0
				}
			}
			b.app.mu.Lock()
			mode := b.app.cfg.MicrophoneMode
			b.app.mu.Unlock()
			active := pass && (mode == "push_to_talk" || levelDB > -55)
			b.app.updateMicrophoneRuntime(active, levelPct)
			if err := writeFull(conn, chunk); err != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return err
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				readErr = cmd.Wait()
			} else {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if readErr == nil {
				return fmt.Errorf("захват микрофона завершён")
			}
			return readErr
		}
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	return ctx.Err()
}

func (b *microphoneBridge) shouldPass(levelDB float64) bool {
	b.app.mu.Lock()
	muted := b.app.microphoneMuted
	ptt := b.app.microphonePTTActive
	mode := b.app.cfg.MicrophoneMode
	voiceDB := b.app.cfg.MicrophoneVoiceDB
	b.app.mu.Unlock()
	if muted {
		return false
	}
	switch mode {
	case "voice":
		if levelDB >= float64(voiceDB) {
			b.voiceUntil = time.Now().Add(450 * time.Millisecond)
		}
		return time.Now().Before(b.voiceUntil)
	case "push_to_talk":
		return ptt
	default:
		return true
	}
}

func pcmLevel(data []byte) (float64, int) {
	if len(data) < 2 {
		return -96, 0
	}
	var sum float64
	count := 0
	for i := 0; i+1 < len(data); i += 2 {
		s := int16(binary.LittleEndian.Uint16(data[i : i+2]))
		v := float64(s) / 32768.0
		sum += v * v
		count++
	}
	if count == 0 || sum == 0 {
		return -96, 0
	}
	rms := math.Sqrt(sum / float64(count))
	db := 20 * math.Log10(rms)
	pct := int(math.Round((db + 60) / 60 * 100))
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return db, pct
}

func (a *app) updateMicrophoneRuntime(active bool, level int) {
	a.mu.Lock()
	a.microphoneActive = active
	a.microphoneLevel = level
	a.mu.Unlock()
}

func (b *microphoneBridge) Close() {
	if b == nil {
		return
	}
	b.cancel()
	_ = b.listener.Close()
	b.mu.Lock()
	if b.conn != nil {
		_ = b.conn.Close()
	}
	if b.cmd != nil && b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
	}
	b.mu.Unlock()
	select {
	case <-b.done:
	case <-time.After(2 * time.Second):
	}
	b.app.updateMicrophoneRuntime(false, 0)
}

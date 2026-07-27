package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const remoteAPIVersion = 1

// Developers can embed the production endpoint at build time with:
// -ldflags="-X main.defaultRemoteAPIURL=https://admin.example/api/monitor/sync"
var defaultRemoteAPIURL string

type remoteSyncRequest struct {
	APIVersion     int                   `json:"api_version"`
	ConnectionLink string                `json:"connection_link"`
	Client         remoteClientInfo      `json:"client"`
	State          remoteRuntimeState    `json:"state"`
	Settings       remoteCurrentSettings `json:"settings"`
}

type remoteClientInfo struct {
	Product      string `json:"product"`
	Version      string `json:"version"`
	ComputerName string `json:"computer_name"`
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
}

type remoteRuntimeState struct {
	ProcessRunning  bool   `json:"process_running"`
	ProcessPID      int    `json:"process_pid"`
	StreamDesired   bool   `json:"stream_desired"`
	Streaming       bool   `json:"streaming"`
	FFmpegPID       int    `json:"ffmpeg_pid,omitempty"`
	StartedAt       string `json:"started_at,omitempty"`
	Restarts        int    `json:"restarts"`
	LastError       string `json:"last_error,omitempty"`
	AppliedRevision int64  `json:"applied_revision"`
	LastCommandID   string `json:"last_command_id,omitempty"`
}

type remoteCurrentSettings struct {
	Protocol     string `json:"protocol"`
	FPS          int    `json:"fps"`
	BitrateKbps  int    `json:"bitrate_kbps"`
	AudioEnabled bool   `json:"audio_enabled"`
}

type remoteSyncResponse struct {
	Success  *bool           `json:"success,omitempty"`
	Revision int64           `json:"revision,omitempty"`
	Settings *remoteSettings `json:"settings,omitempty"`
	Command  *remoteCommand  `json:"command,omitempty"`
	Message  string          `json:"message,omitempty"`
}

type remoteCommand struct {
	ID     json.RawMessage `json:"id"`
	Action string          `json:"action"`
}

type remoteSettings struct {
	Protocol     *string `json:"protocol,omitempty"`
	FPS          *int    `json:"fps,omitempty"`
	BitrateKbps  *int    `json:"bitrate_kbps,omitempty"`
	AudioEnabled *bool   `json:"audio_enabled,omitempty"`
}

func startRemoteControl(a *app) {
	go a.remoteControlLoop()
}

func (a *app) requestRemoteSync() {
	if a.remoteWake == nil {
		return
	}
	select {
	case a.remoteWake <- struct{}{}:
	default:
	}
}

func (a *app) remoteControlLoop() {
	for {
		a.mu.Lock()
		cfg := a.cfg
		a.mu.Unlock()

		interval := time.Duration(cfg.RemoteSyncIntervalMin) * time.Minute
		if interval < time.Minute {
			interval = 5 * time.Minute
		}

		if cfg.RemoteControlEnabled && strings.TrimSpace(cfg.RemoteAPIURL) != "" {
			_ = a.syncRemoteOnce()
		}

		timer := time.NewTimer(interval)
		select {
		case <-timer.C:
		case <-a.remoteWake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
	}
}

func (a *app) syncRemoteOnce() error {
	a.mu.Lock()
	if a.remoteSyncing {
		a.mu.Unlock()
		return errors.New("синхронизация уже выполняется")
	}
	cfg := a.cfg
	if !cfg.RemoteControlEnabled {
		a.mu.Unlock()
		return errors.New("дистанционное управление выключено")
	}
	endpoint := strings.TrimSpace(cfg.RemoteAPIURL)
	if endpoint == "" {
		a.mu.Unlock()
		return errors.New("не указан адрес API")
	}
	if strings.TrimSpace(cfg.Link) == "" {
		a.remoteLastError = "Не указана ссылка подключения LinkVideo"
		a.mu.Unlock()
		return errors.New("не указана ссылка подключения LinkVideo")
	}
	a.remoteSyncing = true
	request := a.makeRemoteSyncRequestLocked(cfg)
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.remoteSyncing = false
		a.mu.Unlock()
	}()

	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		if err == nil {
			err = errors.New("адрес должен начинаться с http:// или https://")
		}
		a.setRemoteError(err)
		return err
	}

	body, err := json.Marshal(request)
	if err != nil {
		a.setRemoteError(err)
		return err
	}
	httpReq, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		a.setRemoteError(err)
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", appName+"/"+appVersion)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		a.setRemoteError(err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err = fmt.Errorf("API вернул HTTP %d", resp.StatusCode)
		a.setRemoteError(err)
		return err
	}
	var result remoteSyncResponse
	dec := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := dec.Decode(&result); err != nil {
		a.setRemoteError(fmt.Errorf("не удалось прочитать ответ API: %w", err))
		return err
	}
	if result.Success != nil && !*result.Success {
		err = errors.New(strings.TrimSpace(result.Message))
		if err.Error() == "" {
			err = errors.New("сервер отклонил синхронизацию")
		}
		a.setRemoteError(err)
		return err
	}

	if err := a.applyRemoteResponse(result); err != nil {
		a.setRemoteError(err)
		return err
	}
	a.mu.Lock()
	a.remoteLastSyncAt = time.Now()
	a.remoteLastError = ""
	a.mu.Unlock()
	a.appendLog("Дистанционные настройки: синхронизация выполнена")
	return nil
}

func (a *app) makeRemoteSyncRequestLocked(cfg Config) remoteSyncRequest {
	host, _ := os.Hostname()
	startedAt := ""
	if !a.startedAt.IsZero() {
		startedAt = a.startedAt.Format(time.RFC3339)
	}
	return remoteSyncRequest{
		APIVersion:     remoteAPIVersion,
		ConnectionLink: cfg.Link,
		Client: remoteClientInfo{
			Product:      appName,
			Version:      appVersion,
			ComputerName: host,
			OS:           runtime.GOOS,
			Architecture: runtime.GOARCH,
		},
		State: remoteRuntimeState{
			ProcessRunning:  true,
			ProcessPID:      os.Getpid(),
			StreamDesired:   a.desired,
			Streaming:       a.running,
			FFmpegPID:       a.pid,
			StartedAt:       startedAt,
			Restarts:        a.restarts,
			LastError:       a.lastError,
			AppliedRevision: cfg.RemoteRevision,
			LastCommandID:   cfg.RemoteLastCommandID,
		},
		Settings: remoteCurrentSettings{
			Protocol:     cfg.Protocol,
			FPS:          cfg.FPS,
			BitrateKbps:  cfg.BitrateKbps,
			AudioEnabled: cfg.AudioEnabled,
		},
	}
}

func (a *app) applyRemoteResponse(resp remoteSyncResponse) error {
	var commandID, action string
	if resp.Command != nil {
		commandID = commandIDString(resp.Command.ID)
		action = strings.ToLower(strings.TrimSpace(resp.Command.Action))
		if action != "" && commandID == "" {
			return errors.New("у дистанционной команды отсутствует обязательный id")
		}
		switch action {
		case "", "restart_stream":
		default:
			return fmt.Errorf("неподдерживаемая дистанционная команда %q", action)
		}
	}

	a.mu.Lock()
	before := a.cfg
	after := before
	if resp.Settings != nil {
		applyRemoteSettings(&after, *resp.Settings)
	}
	if resp.Revision > after.RemoteRevision {
		after.RemoteRevision = resp.Revision
	}
	newCommand := action != "" && commandID != before.RemoteLastCommandID
	if newCommand {
		after.RemoteLastCommandID = commandID
	}
	normalizeConfig(&after)
	configChanged := !reflect.DeepEqual(streamConfigView(before), streamConfigView(after))
	metadataChanged := !reflect.DeepEqual(before, after)
	a.cfg = after
	if metadataChanged {
		if err := a.saveConfigLocked(); err != nil {
			a.cfg = before
			a.mu.Unlock()
			return err
		}
	}
	wasDesired := a.desired
	wasRunning := a.running
	a.mu.Unlock()

	if before.LaunchWithWindows != after.LaunchWithWindows {
		if err := syncStartupRegistration(after.LaunchWithWindows); err != nil {
			a.appendLog("Автозапуск Windows: " + err.Error())
		}
	}
	if wasDesired {
		setSleepPrevention(after.PreventSleep, after.KeepDisplayOn)
	}
	if before.OverlayEnabled != after.OverlayEnabled || before.OverlayX != after.OverlayX || before.OverlayY != after.OverlayY {
		a.setOverlayStatus(wasRunning, "")
	}

	if newCommand && action == "restart_stream" {
		return a.restart()
	}
	if configChanged && wasDesired {
		return a.restart()
	}
	return nil
}

func applyRemoteSettings(cfg *Config, s remoteSettings) {
	if s.Protocol != nil {
		cfg.Protocol = *s.Protocol
	}
	if s.FPS != nil {
		cfg.FPS = *s.FPS
	}
	if s.BitrateKbps != nil {
		switch *s.BitrateKbps {
		case 256:
			cfg.QualityProfile = "economy"
		case 512:
			cfg.QualityProfile = "fast"
		case 1024:
			cfg.QualityProfile = "medium"
		}
		cfg.BitrateKbps = *s.BitrateKbps
	}
	if s.AudioEnabled != nil {
		cfg.AudioEnabled = *s.AudioEnabled
	}
}

type streamConfigSnapshot struct {
	OutputMode         string
	Protocol           string
	LocalRTSPPort      int
	LocalRTSPPath      string
	CaptureMode        string
	MonitorIndex       int
	MonitorNumber      int
	OffsetX            int
	OffsetY            int
	Width              int
	Height             int
	QualityProfile     string
	FPS                int
	BitrateKbps        int
	Encoder            string
	Preset             string
	Cursor             bool
	AudioEnabled       bool
	AudioQuality       string
	OverlayEnabled     bool
	OverlayX           int
	OverlayY           int
	RestartAfterResume bool
	PreventSleep       bool
	KeepDisplayOn      bool
}

func streamConfigView(c Config) streamConfigSnapshot {
	return streamConfigSnapshot{
		OutputMode: c.OutputMode, Protocol: c.Protocol, LocalRTSPPort: c.LocalRTSPPort, LocalRTSPPath: c.LocalRTSPPath,
		CaptureMode: c.CaptureMode, MonitorIndex: c.MonitorIndex, MonitorNumber: c.MonitorNumber, OffsetX: c.OffsetX, OffsetY: c.OffsetY,
		Width: c.Width, Height: c.Height, QualityProfile: c.QualityProfile, FPS: c.FPS, BitrateKbps: c.BitrateKbps,
		Encoder: c.Encoder, Preset: c.Preset, Cursor: c.Cursor, AudioEnabled: c.AudioEnabled,
		AudioQuality: c.AudioQuality, OverlayEnabled: c.OverlayEnabled, OverlayX: c.OverlayX, OverlayY: c.OverlayY,
		RestartAfterResume: c.RestartAfterResume, PreventSleep: c.PreventSleep, KeepDisplayOn: c.KeepDisplayOn,
	}
}

func commandIDString(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return trimmed
}

func (a *app) setRemoteError(err error) {
	a.mu.Lock()
	a.remoteLastError = err.Error()
	a.remoteLastSyncAt = time.Now()
	a.mu.Unlock()
	a.appendLog("Дистанционные настройки: " + err.Error())
}

func (a *app) scheduleProcessRestart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "--restart-wait", strconv.Itoa(os.Getpid()))
	cmd.Dir = filepath.Dir(exe)
	hideChildWindow(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("не удалось подготовить перезапуск процесса: %w", err)
	}
	a.appendLog("Получена команда перезапуска процесса LinkVideo Monitor")
	go func() {
		time.Sleep(700 * time.Millisecond)
		os.Exit(0)
	}()
	return nil
}

func runRestartWait() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := netDialLocal()
		if err != nil {
			break
		}
		_ = conn.Close()
		time.Sleep(300 * time.Millisecond)
	}
	cmd := exec.Command(exe, "--background")
	cmd.Dir = filepath.Dir(exe)
	hideChildWindow(cmd)
	return cmd.Start()
}

func netDialLocal() (net.Conn, error) {
	return net.DialTimeout("tcp", listenAddr, 250*time.Millisecond)
}

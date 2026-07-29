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

const remoteAPIVersion = 2

// Production values are embedded at build time and are not shown in the UI:
// -ldflags="-X main.defaultRemoteAPIURL=https://.../api/monitor/sync -X main.defaultRemoteAPIKey=..."
var defaultRemoteAPIURL string
var defaultRemoteAPIKey string

type remoteSyncRequest struct {
	APIVersion     int                   `json:"api_version"`
	ConnectionLink string                `json:"connection_link"`
	Client         remoteClientInfo      `json:"client"`
	State          remoteRuntimeState    `json:"state"`
	Settings       remoteCurrentSettings `json:"settings"`
	Capabilities   remoteCapabilities    `json:"capabilities"`
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
	SessionLocked   bool   `json:"session_locked"`
	CaptureBackend  string `json:"capture_backend,omitempty"`
	AppliedRevision int64  `json:"applied_revision"`
	LastCommandID   string `json:"last_command_id,omitempty"`
}

type remoteCurrentSettings struct {
	Protocol               string `json:"protocol"`
	ResolutionProfile      string `json:"resolution_profile"`
	CaptureMode            string `json:"capture_mode"`
	MonitorNumber          int    `json:"monitor_number"`
	FPS                    int    `json:"fps"`
	BitrateKbps            int    `json:"bitrate_kbps"`
	Codec                  string `json:"codec"`
	Encoder                string `json:"encoder"`
	Cursor                 bool   `json:"cursor"`
	PrivacyProtection      bool   `json:"privacy_protection"`
	AudioEnabled           bool   `json:"audio_enabled"`
	MicrophoneEnabled      bool   `json:"microphone_enabled"`
	MicrophoneDevice       string `json:"microphone_device,omitempty"`
	MicrophoneMode         string `json:"microphone_mode,omitempty"`
	MicrophoneVoiceDB      int    `json:"microphone_voice_db,omitempty"`
	MicrophonePTTHotkey    string `json:"microphone_ptt_hotkey,omitempty"`
	MicrophoneToggleHotkey string `json:"microphone_toggle_hotkey,omitempty"`
	OverlayEnabled         bool   `json:"overlay_enabled"`
	LaunchWithWindows      bool   `json:"launch_with_windows"`
	PreventSleep           bool   `json:"prevent_sleep"`
	KeepDisplayOn          bool   `json:"keep_display_on"`
}

type remoteCapabilities struct {
	Protocols          []string            `json:"protocols"`
	FPS                []int               `json:"fps"`
	BitratesKbps       []int               `json:"bitrates_kbps"`
	ResolutionProfiles []string            `json:"resolution_profiles"`
	CaptureModes       []string            `json:"capture_modes"`
	Encoders           []EncoderCapability `json:"encoders"`
	Microphone         bool                `json:"microphone"`
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
	Protocol               *string `json:"protocol,omitempty"`
	ResolutionProfile      *string `json:"resolution_profile,omitempty"`
	CaptureMode            *string `json:"capture_mode,omitempty"`
	MonitorNumber          *int    `json:"monitor_number,omitempty"`
	FPS                    *int    `json:"fps,omitempty"`
	BitrateKbps            *int    `json:"bitrate_kbps,omitempty"`
	Codec                  *string `json:"codec,omitempty"`
	Encoder                *string `json:"encoder,omitempty"`
	Cursor                 *bool   `json:"cursor,omitempty"`
	PrivacyProtection      *bool   `json:"privacy_protection,omitempty"`
	AudioEnabled           *bool   `json:"audio_enabled,omitempty"`
	MicrophoneEnabled      *bool   `json:"microphone_enabled,omitempty"`
	MicrophoneDevice       *string `json:"microphone_device,omitempty"`
	MicrophoneMode         *string `json:"microphone_mode,omitempty"`
	MicrophoneVoiceDB      *int    `json:"microphone_voice_db,omitempty"`
	MicrophonePTTHotkey    *string `json:"microphone_ptt_hotkey,omitempty"`
	MicrophoneToggleHotkey *string `json:"microphone_toggle_hotkey,omitempty"`
	OverlayEnabled         *bool   `json:"overlay_enabled,omitempty"`
	LaunchWithWindows      *bool   `json:"launch_with_windows,omitempty"`
	PreventSleep           *bool   `json:"prevent_sleep,omitempty"`
	KeepDisplayOn          *bool   `json:"keep_display_on,omitempty"`
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
	if key := strings.TrimSpace(defaultRemoteAPIKey); key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}

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
	encoders := append([]EncoderCapability(nil), a.encoderCapabilities...)
	if len(encoders) == 0 {
		// Capability probing is started by the UI and on startup. Do not block a
		// sync for hardware probing; an empty list tells the server to wait for
		// the next heartbeat before changing the encoder.
		encoders = []EncoderCapability{}
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
			SessionLocked:   a.sessionLocked,
			CaptureBackend:  a.captureBackend,
			AppliedRevision: cfg.RemoteRevision,
			LastCommandID:   cfg.RemoteLastCommandID,
		},
		Settings: remoteCurrentSettings{
			Protocol: cfg.Protocol, ResolutionProfile: cfg.ResolutionProfile,
			CaptureMode: cfg.CaptureMode, MonitorNumber: cfg.MonitorNumber,
			FPS: cfg.FPS, BitrateKbps: cfg.BitrateKbps, Codec: cfg.Codec, Encoder: cfg.Encoder,
			Cursor: cfg.Cursor, PrivacyProtection: cfg.PrivacyProtection,
			AudioEnabled: cfg.AudioEnabled, MicrophoneEnabled: cfg.MicrophoneEnabled,
			MicrophoneDevice: cfg.MicrophoneDevice, MicrophoneMode: cfg.MicrophoneMode, MicrophoneVoiceDB: cfg.MicrophoneVoiceDB, MicrophonePTTHotkey: cfg.MicrophonePTTHotkey, MicrophoneToggleHotkey: cfg.MicrophoneToggleHotkey, OverlayEnabled: cfg.OverlayEnabled,
			LaunchWithWindows: cfg.LaunchWithWindows, PreventSleep: cfg.PreventSleep,
			KeepDisplayOn: cfg.KeepDisplayOn,
		},
		Capabilities: remoteCapabilities{
			Protocols: []string{"rtsp", "rtmp"}, FPS: []int{15, 25, 30},
			BitratesKbps:       []int{256, 512, 1024},
			ResolutionProfiles: []string{"original", "full_hd", "hd"},
			CaptureModes:       []string{"full", "monitor"}, Encoders: encoders, Microphone: true,
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
		case "", "start_stream", "stop_stream", "restart_stream", "restart_application":
		default:
			return fmt.Errorf("неподдерживаемая дистанционная команда %q", action)
		}
	}

	a.mu.Lock()
	before := a.cfg
	a.mu.Unlock()
	after := before
	if resp.Settings != nil {
		if err := a.applyRemoteSettingsValidated(&after, *resp.Settings); err != nil {
			return err
		}
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

	a.mu.Lock()
	// Rebase on the latest hidden metadata in case another sync/save completed.
	current := a.cfg
	after.RemoteAPIURL = current.RemoteAPIURL
	after.RemoteControlEnabled = current.RemoteControlEnabled
	after.RemoteSyncIntervalMin = current.RemoteSyncIntervalMin
	a.cfg = after
	if metadataChanged {
		if err := a.saveConfigLocked(); err != nil {
			a.cfg = current
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

	if newCommand {
		switch action {
		case "start_stream":
			return a.start()
		case "stop_stream":
			a.stop()
			return nil
		case "restart_stream":
			return a.restart()
		case "restart_application":
			return a.scheduleProcessRestart()
		}
	}
	if configChanged && wasDesired {
		return a.restart()
	}
	return nil
}

func (a *app) applyRemoteSettingsValidated(cfg *Config, s remoteSettings) error {
	if s.Protocol != nil {
		v := strings.ToLower(strings.TrimSpace(*s.Protocol))
		if v != "rtsp" && v != "rtmp" {
			return fmt.Errorf("API передал неподдерживаемый протокол %q", *s.Protocol)
		}
		cfg.Protocol = v
	}
	if s.ResolutionProfile != nil {
		v := strings.TrimSpace(*s.ResolutionProfile)
		if v != "original" && v != "full_hd" && v != "hd" {
			return fmt.Errorf("API передал неподдерживаемое разрешение %q", v)
		}
		cfg.ResolutionProfile = v
	}
	if s.CaptureMode != nil {
		v := strings.TrimSpace(*s.CaptureMode)
		if v != "full" && v != "monitor" {
			return fmt.Errorf("API передал неподдерживаемый режим захвата %q", v)
		}
		cfg.CaptureMode = v
	}
	if s.MonitorNumber != nil {
		if *s.MonitorNumber < 0 {
			return errors.New("API передал неверный номер монитора")
		}
		cfg.MonitorNumber = *s.MonitorNumber
	}
	if s.FPS != nil {
		switch *s.FPS {
		case 15, 25, 30:
			cfg.FPS = *s.FPS
		default:
			return fmt.Errorf("API передал неподдерживаемый FPS %d", *s.FPS)
		}
	}
	if s.BitrateKbps != nil {
		switch *s.BitrateKbps {
		case 256:
			cfg.QualityProfile = "economy"
		case 512:
			cfg.QualityProfile = "fast"
		case 1024:
			cfg.QualityProfile = "medium"
		default:
			return fmt.Errorf("API передал неподдерживаемый битрейт %d Кбит/с", *s.BitrateKbps)
		}
		cfg.BitrateKbps = *s.BitrateKbps
	}
	if s.Codec != nil {
		v := strings.ToLower(strings.TrimSpace(*s.Codec))
		if v != "h264" && v != "h265" {
			return fmt.Errorf("API передал неподдерживаемый кодек %q", v)
		}
		cfg.Codec = v
	}
	if s.Encoder != nil {
		v := strings.TrimSpace(*s.Encoder)
		if v == "" {
			return errors.New("API передал пустой кодировщик")
		}
		available := false
		capabilities := a.getEncoderCapabilities(false)
		for _, capability := range capabilities {
			if capability.Name == v {
				if !capability.Available {
					return fmt.Errorf("кодировщик %s недоступен на компьютере: %s", capability.Label, capability.Reason)
				}
				available = true
				break
			}
		}
		if !available {
			return fmt.Errorf("API передал неизвестный кодировщик %q", v)
		}
		cfg.Encoder = v
		cfg.Codec = encoderCodec(v)
	}
	if s.Cursor != nil {
		cfg.Cursor = *s.Cursor
	}
	if s.PrivacyProtection != nil {
		cfg.PrivacyProtection = *s.PrivacyProtection
	}
	if s.AudioEnabled != nil {
		cfg.AudioEnabled = *s.AudioEnabled
	}
	if s.MicrophoneEnabled != nil {
		cfg.MicrophoneEnabled = *s.MicrophoneEnabled
	}
	if s.MicrophoneDevice != nil {
		cfg.MicrophoneDevice = strings.TrimSpace(*s.MicrophoneDevice)
	}
	if s.MicrophoneMode != nil {
		v := strings.TrimSpace(*s.MicrophoneMode)
		if v != "always" && v != "voice" && v != "push_to_talk" {
			return fmt.Errorf("API передал неподдерживаемый режим микрофона %q", v)
		}
		cfg.MicrophoneMode = v
	}
	if s.MicrophoneVoiceDB != nil {
		if *s.MicrophoneVoiceDB < -70 || *s.MicrophoneVoiceDB > -10 {
			return errors.New("API передал неверный порог активации микрофона")
		}
		cfg.MicrophoneVoiceDB = *s.MicrophoneVoiceDB
	}
	if s.MicrophonePTTHotkey != nil {
		cfg.MicrophonePTTHotkey = strings.TrimSpace(*s.MicrophonePTTHotkey)
	}
	if s.MicrophoneToggleHotkey != nil {
		cfg.MicrophoneToggleHotkey = strings.TrimSpace(*s.MicrophoneToggleHotkey)
	}
	if s.OverlayEnabled != nil {
		cfg.OverlayEnabled = *s.OverlayEnabled
	}
	if s.LaunchWithWindows != nil {
		cfg.LaunchWithWindows = *s.LaunchWithWindows
	}
	if s.PreventSleep != nil {
		cfg.PreventSleep = *s.PreventSleep
	}
	if s.KeepDisplayOn != nil {
		cfg.KeepDisplayOn = *s.KeepDisplayOn
	}
	return nil
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
	ResolutionProfile  string
	QualityProfile     string
	FPS                int
	BitrateKbps        int
	Codec              string
	Encoder            string
	Preset             string
	Cursor             bool
	PrivacyProtection  bool
	AudioEnabled       bool
	MicrophoneEnabled  bool
	MicrophoneDevice   string
	AudioQuality       string
	OverlayEnabled     bool
	OverlayX           int
	OverlayY           int
	LaunchWithWindows  bool
	RestartAfterResume bool
	PreventSleep       bool
	KeepDisplayOn      bool
}

func streamConfigView(c Config) streamConfigSnapshot {
	return streamConfigSnapshot{
		OutputMode: c.OutputMode, Protocol: c.Protocol, LocalRTSPPort: c.LocalRTSPPort, LocalRTSPPath: c.LocalRTSPPath,
		CaptureMode: c.CaptureMode, MonitorIndex: c.MonitorIndex, MonitorNumber: c.MonitorNumber, OffsetX: c.OffsetX, OffsetY: c.OffsetY,
		Width: c.Width, Height: c.Height, ResolutionProfile: c.ResolutionProfile, QualityProfile: c.QualityProfile,
		FPS: c.FPS, BitrateKbps: c.BitrateKbps, Codec: c.Codec, Encoder: c.Encoder, Preset: c.Preset,
		Cursor: c.Cursor, PrivacyProtection: c.PrivacyProtection, AudioEnabled: c.AudioEnabled,
		MicrophoneEnabled: c.MicrophoneEnabled, MicrophoneDevice: c.MicrophoneDevice,
		AudioQuality: c.AudioQuality, OverlayEnabled: c.OverlayEnabled, OverlayX: c.OverlayX, OverlayY: c.OverlayY,
		LaunchWithWindows: c.LaunchWithWindows, RestartAfterResume: c.RestartAfterResume,
		PreventSleep: c.PreventSleep, KeepDisplayOn: c.KeepDisplayOn,
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

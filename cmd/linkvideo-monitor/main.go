package main

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	appName    = "LinkVideo Monitor"
	appVersion = "0.8.12-beta"
	listenAddr = "127.0.0.1:8098"
)

//go:embed favicon.ico
var faviconICO []byte

type Config struct {
	DefaultsRevision       int    `json:"defaults_revision"`
	PrivacyProtection      bool   `json:"privacy_protection"`
	ResolutionProfile      string `json:"resolution_profile"` // original, full_hd, hd
	Link                   string `json:"link"`
	OutputMode             string `json:"output_mode"` // remote or local
	Protocol               string `json:"protocol"`    // rtsp or rtmp
	FFmpegPath             string `json:"ffmpeg_path"`
	MediaMTXPath           string `json:"mediamtx_path"`
	LocalRTSPPort          int    `json:"local_rtsp_port"`
	LocalRTSPPath          string `json:"local_rtsp_path"`
	CaptureMode            string `json:"capture_mode"` // full, monitor, custom
	MonitorIndex           int    `json:"monitor_index"`
	MonitorNumber          int    `json:"monitor_number"` // номер монитора в настройках Windows
	WindowTitle            string `json:"window_title"`
	WindowHandle           string `json:"window_handle"`
	WindowProcess          string `json:"window_process"`
	OffsetX                int    `json:"offset_x"`
	OffsetY                int    `json:"offset_y"`
	Width                  int    `json:"width"`
	Height                 int    `json:"height"`
	QualityProfile         string `json:"quality_profile"` // economy, fast, medium
	FPS                    int    `json:"fps"`
	BitrateKbps            int    `json:"bitrate_kbps"`
	RateControl            string `json:"rate_control"` // cbr or vbr
	MaxrateKbps            int    `json:"maxrate_kbps"`
	BufsizeKbps            int    `json:"bufsize_kbps"`
	Codec                  string `json:"codec"`           // h264 or h265
	Encoder                string `json:"encoder"`         // software, NVENC, AMF or QSV encoder
	CaptureBackend         string `json:"capture_backend"` // auto, dxgi, gdi
	Preset                 string `json:"preset"`
	Cursor                 bool   `json:"cursor"`
	AudioEnabled           bool   `json:"audio_enabled"`
	MicrophoneEnabled      bool   `json:"microphone_enabled"`
	MicrophoneDevice       string `json:"microphone_device"`
	MicrophoneMode         string `json:"microphone_mode"` // always, voice, push_to_talk
	MicrophoneVoiceDB      int    `json:"microphone_voice_db"`
	MicrophonePTTHotkey    string `json:"microphone_ptt_hotkey"`
	MicrophoneToggleHotkey string `json:"microphone_toggle_hotkey"`
	AudioQuality           string `json:"audio_quality"` // low, medium, high
	AudioSource            string `json:"audio_source"`  // system or dshow
	AudioDevice            string `json:"audio_device"`
	AudioBitrateKbps       int    `json:"audio_bitrate_kbps"`
	AudioSampleRate        int    `json:"audio_sample_rate"`
	AudioChannels          int    `json:"audio_channels"`
	AudioAdvanceMs         int    `json:"audio_advance_ms"`
	LaunchWithWindows      bool   `json:"launch_with_windows"`
	AutoStart              bool   `json:"auto_start"`
	RestartAfterResume     bool   `json:"restart_after_resume"`
	PreventSleep           bool   `json:"prevent_sleep"`
	KeepDisplayOn          bool   `json:"keep_display_on"`
	OverlayEnabled         bool   `json:"overlay_enabled"`
	OverlayText            string `json:"overlay_text"` // legacy compatibility
	OverlayX               int    `json:"overlay_x"`
	OverlayY               int    `json:"overlay_y"`
	DeveloperMode          bool   `json:"developer_mode"`
	RestartDelayS          int    `json:"restart_delay_s"`
	RemoteControlEnabled   bool   `json:"remote_control_enabled"`
	RemoteAPIURL           string `json:"remote_api_url"`
	RemoteSyncIntervalMin  int    `json:"remote_sync_interval_min"`
	RemoteRevision         int64  `json:"remote_revision"`
	RemoteLastCommandID    string `json:"remote_last_command_id"`
}

type Monitor struct {
	Index         int     `json:"index"`          // внутренний индекс захвата
	DisplayNumber int     `json:"display_number"` // номер в параметрах дисплея Windows
	Name          string  `json:"name"`
	Model         string  `json:"model,omitempty"`
	Manufacturer  string  `json:"manufacturer,omitempty"`
	DeviceID      string  `json:"device_id,omitempty"`
	X             int     `json:"x"`
	Y             int     `json:"y"`
	Width         int     `json:"width"`
	Height        int     `json:"height"`
	Primary       bool    `json:"primary"`
	AdapterIndex  int     `json:"adapter_index,omitempty"`
	OutputIndex   int     `json:"output_index,omitempty"`
	HMonitor      uintptr `json:"-"`
}

type WindowInfo struct {
	Handle      string `json:"handle"`
	Title       string `json:"title"`
	PID         uint32 `json:"pid"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	ProcessName string `json:"process_name"`
	DisplayName string `json:"display_name"`
}

type Status struct {
	Version             string         `json:"version"`
	Desired             bool           `json:"desired"`
	Running             bool           `json:"running"`
	PID                 int            `json:"pid"`
	MediaMTXRunning     bool           `json:"mediamtx_running"`
	StartedAt           string         `json:"started_at,omitempty"`
	Restarts            int            `json:"restarts"`
	LastExitCode        int            `json:"last_exit_code"`
	LastError           string         `json:"last_error,omitempty"`
	ResolvedURL         string         `json:"resolved_url,omitempty"`
	LocalRTSPURL        string         `json:"local_rtsp_url,omitempty"`
	LocalRTSPLANURL     string         `json:"local_rtsp_lan_url,omitempty"`
	Command             string         `json:"command,omitempty"`
	RecentLog           []string       `json:"recent_log"`
	ConfigPath          string         `json:"config_path"`
	LogPath             string         `json:"log_path"`
	LastRestartReason   string         `json:"last_restart_reason,omitempty"`
	LastRestartAt       string         `json:"last_restart_at,omitempty"`
	RestartHistory      []RestartEvent `json:"restart_history,omitempty"`
	CaptureStatus       string         `json:"capture_status,omitempty"`
	CaptureBackend      string         `json:"capture_backend,omitempty"`
	VideoEncoder        string         `json:"video_encoder,omitempty"`
	VideoFPS            float64        `json:"video_fps,omitempty"`
	VideoSpeed          float64        `json:"video_speed,omitempty"`
	VideoDup            int            `json:"video_dup,omitempty"`
	VideoDrop           int            `json:"video_drop,omitempty"`
	RemoteEnabled       bool           `json:"remote_enabled"`
	RemoteSyncing       bool           `json:"remote_syncing"`
	RemoteLastSyncAt    string         `json:"remote_last_sync_at,omitempty"`
	RemoteLastError     string         `json:"remote_last_error,omitempty"`
	RemoteRevision      int64          `json:"remote_revision"`
	RemoteLastCommandID string         `json:"remote_last_command_id,omitempty"`
	MicrophoneMode      string         `json:"microphone_mode,omitempty"`
	MicrophoneMuted     bool           `json:"microphone_muted"`
	MicrophoneActive    bool           `json:"microphone_active"`
	MicrophoneLevel     int            `json:"microphone_level"`
}

type app struct {
	mu                      sync.Mutex
	cfg                     Config
	cfgPath                 string
	logPath                 string
	logsDir                 string
	desired                 bool
	running                 bool
	cmd                     *exec.Cmd
	audioCmd                *exec.Cmd
	overlayCmd              *exec.Cmd
	overlayDone             chan struct{}
	overlayRetryPending     bool
	mediaCmd                *exec.Cmd
	mediaRunning            bool
	generation              int64
	pid                     int
	startedAt               time.Time
	restarts                int
	lastExitCode            int
	lastError               string
	resolvedURL             string
	command                 string
	recent                  []string
	lastRestartReason       string
	lastRestartAt           time.Time
	captureStatus           string
	captureBackend          string
	videoEncoder            string
	encoderFailureDetected  bool
	encoderFailureReason    string
	encoderStartupConfirmed bool
	encoderFailures         map[string]encoderFailureState
	videoFPS                float64
	videoSpeed              float64
	videoDup                int
	videoDrop               int
	fatalCaptureReason      string
	remoteWake              chan struct{}
	remoteSyncing           bool
	remoteLastSyncAt        time.Time
	remoteLastError         string
	configLoadError         bool
	sessionLocked           bool
	authPath                string
	restartHistoryPath      string
	adminTokens             map[string]time.Time
	adminFailures           int
	adminBlockedUntil       time.Time
	restartHistory          []RestartEvent
	encoderCapabilities     []EncoderCapability
	encoderCapabilitiesAt   time.Time
	microphoneMuted         bool
	microphoneActive        bool
	microphoneLevel         int
	microphonePTTActive     bool
}

func defaultConfig() Config {
	return Config{
		DefaultsRevision:       11,
		PrivacyProtection:      false,
		ResolutionProfile:      "original",
		OutputMode:             "remote",
		Protocol:               "rtsp",
		FFmpegPath:             "ffmpeg.exe",
		MediaMTXPath:           "mediamtx.exe",
		LocalRTSPPort:          8554,
		LocalRTSPPath:          "screen",
		CaptureMode:            "full",
		MonitorIndex:           0,
		MonitorNumber:          0,
		QualityProfile:         "medium",
		Width:                  1920,
		Height:                 1080,
		FPS:                    15,
		BitrateKbps:            1024,
		RateControl:            "cbr",
		MaxrateKbps:            1024,
		BufsizeKbps:            2048,
		Codec:                  "h264",
		Encoder:                "libx264",
		CaptureBackend:         "auto",
		Preset:                 "veryfast",
		Cursor:                 true,
		AudioEnabled:           false,
		MicrophoneMode:         "always",
		MicrophoneVoiceDB:      -42,
		MicrophonePTTHotkey:    "Ctrl+Alt+Space",
		MicrophoneToggleHotkey: "Ctrl+Alt+M",
		AudioQuality:           "medium",
		AudioSource:            "system",
		AudioBitrateKbps:       128,
		AudioSampleRate:        48000,
		AudioChannels:          2,
		AudioAdvanceMs:         800,
		LaunchWithWindows:      true,
		AutoStart:              true,
		RestartAfterResume:     true,
		OverlayEnabled:         true,
		OverlayX:               -1,
		OverlayY:               -1,
		RestartDelayS:          3,
		RemoteControlEnabled:   strings.TrimSpace(defaultRemoteAPIURL) != "",
		RemoteAPIURL:           strings.TrimSpace(defaultRemoteAPIURL),
		RemoteSyncIntervalMin:  5,
	}
}

func appDataPaths() (string, string, string) {
	configBase, err := os.UserConfigDir()
	if err != nil || configBase == "" {
		configBase = "."
	}
	localBase, err := os.UserCacheDir()
	if err != nil || localBase == "" {
		localBase = configBase
	}
	configDir := filepath.Join(configBase, "LinkVideo.Monitor")
	logsDir := filepath.Join(localBase, "LinkVideo.Monitor", "Logs")
	return filepath.Join(configDir, "config.json"), logsDir, filepath.Join(logsDir, "sender-"+time.Now().Format("2006-01-02")+".log")
}

func newApp() *app {
	cfgPath, logsDir, logPath := appDataPaths()
	a := &app{cfg: defaultConfig(), cfgPath: cfgPath, logsDir: logsDir, logPath: logPath, lastExitCode: -1, remoteWake: make(chan struct{}, 1), encoderFailures: make(map[string]encoderFailureState), adminTokens: make(map[string]time.Time)}
	a.authPath = filepath.Join(filepath.Dir(cfgPath), "admin-auth.json")
	// 0.8.11 uses a LinkVideo-managed password embedded in the application.
	// Remove the obsolete client-created password file from 0.8.0.
	_ = os.Remove(a.authPath)
	a.restartHistoryPath = filepath.Join(filepath.Dir(cfgPath), "reconnect-history.json")
	a.cleanupOldLogs()
	a.loadReconnectHistory()
	if err := migrateLegacyConfig(cfgPath); err != nil {
		a.appendLog("Не удалось перенести настройки предыдущей версии: " + err.Error())
	}
	if err := a.loadConfig(); err != nil {
		a.configLoadError = true
		a.lastError = "Не удалось загрузить настройки: " + err.Error()
		a.appendLog(a.lastError + "; автозапуск потока отключён до успешного сохранения")
	}
	startMicrophoneHotkeys(a)
	return a
}

func migrateLegacyConfig(newPath string) error {
	if _, err := os.Stat(newPath); err == nil {
		return nil
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		return nil
	}
	legacy := filepath.Join(base, "LinkVideo.ScreenSender", "config.json")
	b, err := os.ReadFile(legacy)
	if err != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(newPath, b, 0o600)
}

func (a *app) loadConfig() error {
	b, err := os.ReadFile(a.cfgPath)
	if errors.Is(err, os.ErrNotExist) {
		return a.saveConfigLocked()
	}
	if err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	cfg := defaultConfig()
	if err := json.Unmarshal(b, &cfg); err != nil {
		return err
	}
	migrated := false
	if _, ok := raw["defaults_revision"]; !ok {
		// Однократное обновление самых ранних beta-конфигов.
		cfg.DefaultsRevision = 11
		cfg.Codec = "h264"
		cfg.Encoder = "libx264"
		cfg.CaptureBackend = "auto"
		cfg.PrivacyProtection = false
		cfg.Preset = "veryfast"
		cfg.ResolutionProfile = "original"
		cfg.Cursor = true
		cfg.OverlayEnabled = true
		cfg.AutoStart = true
		cfg.RestartAfterResume = true
		cfg.FPS = 15
		cfg.QualityProfile = "medium"
		cfg.BitrateKbps = 1024
		cfg.MaxrateKbps = 1024
		cfg.BufsizeKbps = 2048
		if cfg.CaptureMode == "window" {
			cfg.CaptureMode = "full"
		}
		migrated = true
	} else if cfg.DefaultsRevision < 11 {
		// Версия 0.5.4 закрепляет обязательный автозапуск потока и восстановление после сна,
		// сохраняя безопасные значения предыдущих версий.
		// Версия 0.4.4 меняла безопасные начальные значения, исправляла
		// захват курсора и удаляет профиль 2000 Кбит/с. Миграция выполняется один раз.
		cfg.DefaultsRevision = 11
		cfg.Codec = "h264"
		cfg.Encoder = "libx264"
		cfg.CaptureBackend = "auto"
		// Защита полей остаётся доступной, но после обновления выключается: реактивное
		// распознавание UI Automation не должно создавать ложное чувство гарантии.
		cfg.PrivacyProtection = false
		cfg.Preset = "veryfast"
		if cfg.ResolutionProfile == "" {
			cfg.ResolutionProfile = "original"
		}
		cfg.Cursor = true
		cfg.OverlayEnabled = true
		cfg.AutoStart = true
		cfg.RestartAfterResume = true
		if cfg.FPS != 15 && cfg.FPS != 25 && cfg.FPS != 30 {
			cfg.FPS = 15
		}
		if cfg.QualityProfile == "high" || cfg.BitrateKbps > 1024 {
			cfg.QualityProfile = "medium"
			cfg.BitrateKbps = 1024
			cfg.MaxrateKbps = 1024
			cfg.BufsizeKbps = 2048
		}
		migrated = true
	}
	normalizeConfig(&cfg)
	a.cfg = cfg
	if migrated {
		return a.saveConfigLocked()
	}
	return nil
}

func normalizeConfig(c *Config) {
	// Эти два режима являются обязательным поведением продукта и не выводятся
	// в пользовательский интерфейс: поток запускается вместе с Monitor и
	// автоматически восстанавливается после выхода Windows из сна.
	c.AutoStart = true
	c.RestartAfterResume = true
	if c.OutputMode != "local" {
		c.OutputMode = "remote"
	}
	if c.Protocol != "rtmp" {
		c.Protocol = "rtsp"
	}
	if c.MediaMTXPath == "" {
		c.MediaMTXPath = "mediamtx.exe"
	}
	if c.LocalRTSPPort < 1 || c.LocalRTSPPort > 65535 {
		c.LocalRTSPPort = 8554
	}
	c.LocalRTSPPath = "screen"
	switch c.CaptureMode {
	case "monitor", "full":
	default:
		c.CaptureMode = "full"
	}
	switch c.ResolutionProfile {
	case "original", "full_hd", "hd":
	default:
		c.ResolutionProfile = "original"
	}
	if c.FFmpegPath == "" {
		c.FFmpegPath = "ffmpeg.exe"
	}
	switch c.FPS {
	case 15, 25, 30:
	default:
		c.FPS = 15
	}
	profiles := map[string]int{"economy": 256, "fast": 512, "medium": 1024}
	if v, ok := profiles[c.QualityProfile]; ok {
		c.BitrateKbps = v
	} else {
		// Старые ручные значения переводятся в ближайший поддерживаемый профиль.
		switch {
		case c.BitrateKbps <= 384:
			c.QualityProfile, c.BitrateKbps = "economy", 256
		case c.BitrateKbps <= 768:
			c.QualityProfile, c.BitrateKbps = "fast", 512
		default:
			c.QualityProfile, c.BitrateKbps = "medium", 1024
		}
	}
	if c.RateControl != "vbr" {
		c.RateControl = "cbr"
	}
	c.MaxrateKbps = c.BitrateKbps
	c.BufsizeKbps = c.BitrateKbps * 2
	if c.Codec != "h265" {
		c.Codec = "h264"
	}
	if encoderCodec(c.Encoder) != c.Codec {
		c.Encoder = softwareEncoderForCodec(c.Codec)
	}
	validEncoders := map[string]bool{
		"libx264": true, "h264_nvenc": true, "h264_amf": true, "h264_qsv": true,
		"libx265": true, "hevc_nvenc": true, "hevc_amf": true, "hevc_qsv": true,
	}
	if !validEncoders[c.Encoder] {
		c.Encoder = softwareEncoderForCodec(c.Codec)
	}
	switch c.CaptureBackend {
	case "auto", "dxgi", "gdi":
	default:
		c.CaptureBackend = "auto"
	}
	allowedPresets := map[string]bool{"ultrafast": true, "superfast": true, "veryfast": true, "faster": true, "fast": true}
	if !allowedPresets[c.Preset] {
		c.Preset = "veryfast"
	}
	if c.RestartDelayS < 1 {
		c.RestartDelayS = 1
	}
	if c.RestartDelayS > 60 {
		c.RestartDelayS = 60
	}
	// Дистанционная синхронизация является служебной функцией и не управляется
	// пользователем из интерфейса. В производственной сборке адрес API
	// встраивается через defaultRemoteAPIURL. Для совместимости также
	// сохраняется адрес, заданный в предыдущих beta-версиях.
	if embedded := strings.TrimSpace(defaultRemoteAPIURL); embedded != "" {
		c.RemoteAPIURL = embedded
	}
	c.RemoteAPIURL = strings.TrimSpace(c.RemoteAPIURL)
	c.RemoteControlEnabled = c.RemoteAPIURL != ""
	c.RemoteSyncIntervalMin = 5
	// DirectShow was previously an alternative to system audio. It is now a
	// separate microphone channel that can be mixed with system sound.
	if c.AudioSource == "dshow" && c.AudioEnabled && !c.MicrophoneEnabled {
		c.MicrophoneEnabled = true
		c.MicrophoneDevice = c.AudioDevice
		c.AudioEnabled = false
	}
	c.AudioSource = "system"
	c.AudioDevice = ""
	switch c.MicrophoneMode {
	case "always", "voice", "push_to_talk":
	default:
		c.MicrophoneMode = "always"
	}
	if c.MicrophoneVoiceDB < -70 || c.MicrophoneVoiceDB > -10 {
		c.MicrophoneVoiceDB = -42
	}
	if strings.TrimSpace(c.MicrophonePTTHotkey) == "" {
		c.MicrophonePTTHotkey = "Ctrl+Alt+Space"
	}
	if strings.TrimSpace(c.MicrophoneToggleHotkey) == "" {
		c.MicrophoneToggleHotkey = "Ctrl+Alt+M"
	}
	audioProfiles := map[string]int{"low": 64, "medium": 128, "high": 192}
	if v, ok := audioProfiles[c.AudioQuality]; ok {
		c.AudioBitrateKbps = v
	} else {
		// Старые конфиги могли содержать только числовой битрейт.
		switch {
		case c.AudioBitrateKbps <= 80:
			c.AudioQuality = "low"
			c.AudioBitrateKbps = 64
		case c.AudioBitrateKbps >= 160:
			c.AudioQuality = "high"
			c.AudioBitrateKbps = 192
		default:
			c.AudioQuality = "medium"
			c.AudioBitrateKbps = 128
		}
	}
	switch c.AudioSampleRate {
	case 32000, 44100, 48000:
	default:
		c.AudioSampleRate = 48000
	}
	if c.AudioChannels != 1 {
		c.AudioChannels = 2
	}
	if c.AudioAdvanceMs < 0 {
		c.AudioAdvanceMs = 0
	}
	if c.AudioAdvanceMs > 2000 {
		c.AudioAdvanceMs = 2000
	}
	// OverlayText is kept only to load configs from 0.3.x. The current
	// indicator text is fixed in the helper; legacy custom text is ignored.
	c.OverlayText = ""
	if c.OverlayX < -1 {
		c.OverlayX = -1
	}
	if c.OverlayY < -1 {
		c.OverlayY = -1
	}
	if c.Width < 2 {
		c.Width = 1920
	}
	if c.Height < 2 {
		c.Height = 1080
	}
	c.Width -= c.Width % 2
	c.Height -= c.Height % 2
}

func (a *app) saveConfigLocked() error {
	dir := filepath.Dir(a.cfgPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(a.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "config-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := writeFull(tmp, b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := replaceFileAtomically(tmpPath, a.cfgPath); err != nil {
		return err
	}
	return nil
}

func (a *app) failStart(gen int64, err error) bool {
	a.mu.Lock()
	if a.generation != gen {
		a.mu.Unlock()
		return false
	}
	a.desired = false
	a.lastError = err.Error()
	a.mu.Unlock()
	setSleepPrevention(false, false)
	a.setOverlayStatus(false, "Запись экрана не ведётся")
	return true
}

func (a *app) start() error {
	a.mu.Lock()
	if a.desired {
		a.mu.Unlock()
		return nil
	}
	if a.configLoadError {
		err := errors.New("настройки не загружены; откройте интерфейс и сохраните их повторно")
		a.lastError = err.Error()
		a.mu.Unlock()
		return err
	}
	previousConfig := a.cfg
	cfg := a.cfg
	normalizeConfig(&cfg)
	a.cfg = cfg
	if err := a.saveConfigLocked(); err != nil {
		a.cfg = previousConfig
		a.mu.Unlock()
		return err
	}
	a.generation++
	gen := a.generation
	a.desired = true
	a.lastError = ""
	a.mu.Unlock()

	if err := syncStartupRegistration(cfg.LaunchWithWindows); err != nil {
		a.appendLog("Автозапуск Windows: " + err.Error())
	}
	setSleepPrevention(cfg.PreventSleep, cfg.KeepDisplayOn)
	a.setOverlayStatus(false, "Подключение…")

	if _, _, err := buildFFmpeg(cfg); err != nil {
		a.failStart(gen, err)
		return err
	}
	if cfg.OutputMode == "local" {
		portStatus := a.checkLocalRTSPPort(cfg.LocalRTSPPort)
		if !portStatus.Available {
			err := errors.New(portStatus.Message)
			a.failStart(gen, err)
			return err
		}
		if err := a.ensureMediaMTX(cfg); err != nil {
			a.failStart(gen, err)
			return err
		}
	}
	go a.runLoop(gen)
	return nil
}

func (a *app) stop() {
	a.mu.Lock()
	a.desired = false
	a.generation++
	cmd := a.cmd
	audioCmd := a.audioCmd
	mediaCmd := a.mediaCmd
	mediaPort := a.cfg.LocalRTSPPort
	mediaWasRunning := a.mediaRunning || mediaCmd != nil
	// Состояние локального RTSP-сервера сбрасывается сразу. Иначе быстрый
	// перезапуск после смены порта может принять завершающийся старый процесс
	// за уже готовый новый сервер.
	a.mediaCmd = nil
	a.mediaRunning = false
	a.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if audioCmd != nil && audioCmd.Process != nil {
		_ = audioCmd.Process.Kill()
	}
	if mediaCmd != nil && mediaCmd.Process != nil {
		_ = mediaCmd.Process.Kill()
	}
	if mediaWasRunning && mediaPort > 0 {
		address := fmt.Sprintf("127.0.0.1:%d", mediaPort)
		deadline := time.Now().Add(2500 * time.Millisecond)
		for time.Now().Before(deadline) {
			conn, err := net.DialTimeout("tcp", address, 120*time.Millisecond)
			if err != nil {
				break
			}
			_ = conn.Close()
			time.Sleep(100 * time.Millisecond)
		}
	}
	setSleepPrevention(false, false)
	a.setOverlayStatus(false, "Запись экрана не ведётся")
}

func (a *app) restart() error {
	a.stop()
	time.Sleep(350 * time.Millisecond)
	return a.start()
}

func (a *app) markPendingRestart(reason string, terminate bool) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return
	}
	a.mu.Lock()
	if a.fatalCaptureReason == "" {
		a.fatalCaptureReason = reason
	}
	cmd := a.cmd
	active := a.desired && a.running
	a.mu.Unlock()
	if terminate && active && cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func (a *app) markFatalCapture(reason string) {
	a.markPendingRestart(reason, true)
}

func (a *app) watchCaptureTarget(_ *exec.Cmd, _ Config, _ capturePlan) {
	// Захват отдельного приложения удалён из пользовательского продукта.
}

func (a *app) runLoop(gen int64) {
	first := true
	for {
		a.mu.Lock()
		if !a.desired || a.generation != gen {
			a.mu.Unlock()
			return
		}
		cfg := a.cfg
		// Do not restart the encoder when Windows locks or resumes. A static
		// lock screen naturally consumes little bandwidth even with the normal
		// profile, while preserving the same RTSP publishing connection.
		a.fatalCaptureReason = ""
		a.encoderFailureDetected = false
		a.encoderFailureReason = ""
		a.encoderStartupConfirmed = false
		a.mu.Unlock()

		if cfg.OutputMode == "local" {
			portStatus := a.checkLocalRTSPPort(cfg.LocalRTSPPort)
			if !portStatus.Available {
				a.recordLaunchError(gen, errors.New(portStatus.Message))
				return
			}
			if err := a.ensureMediaMTX(cfg); err != nil {
				a.recordLaunchError(gen, err)
				return
			}
		}

		plan, err := resolveCapturePlan(cfg)
		if err != nil {
			a.recordLaunchError(gen, err)
			return
		}
		encoder := a.selectVideoEncoder(cfg, plan)
		runtimeCfg, runtimeOptimized := a.runtimeConfigForPerformance(cfg, plan, encoder)
		a.mu.Lock()
		currentGeneration := a.desired && a.generation == gen
		a.mu.Unlock()
		if !currentGeneration {
			return
		}

		streamCtx, cancelStream := context.WithCancel(context.Background())
		var audioBridge *systemAudioBridge
		var microphoneBridge *microphoneBridge
		audioURL := ""
		microphoneURL := ""
		if runtimeCfg.AudioEnabled && runtimeCfg.AudioSource == "system" {
			audioBridge, audioURL, err = startSystemAudioBridge(streamCtx, a)
			if err != nil {
				cancelStream()
				a.recordLaunchError(gen, err)
				return
			}
		}
		if runtimeCfg.MicrophoneEnabled {
			microphoneBridge, microphoneURL, err = startMicrophoneBridge(streamCtx, a, runtimeCfg)
			if err != nil {
				if audioBridge != nil {
					audioBridge.Close()
				}
				cancelStream()
				a.recordLaunchError(gen, err)
				return
			}
		}

		resolved, args, _, err := buildEncoderFFmpegDetailed(runtimeCfg, plan, encoder, audioURL, microphoneURL)
		if err != nil {
			if audioBridge != nil {
				audioBridge.Close()
			}
			if microphoneBridge != nil {
				microphoneBridge.Close()
			}
			cancelStream()
			a.recordLaunchError(gen, err)
			return
		}

		cmd := exec.Command(resolveExecutable(runtimeCfg.FFmpegPath), args...)
		videoIn, err := cmd.StdinPipe()
		if err != nil {
			if audioBridge != nil {
				audioBridge.Close()
			}
			if microphoneBridge != nil {
				microphoneBridge.Close()
			}
			cancelStream()
			a.recordLaunchError(gen, err)
			return
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			if audioBridge != nil {
				audioBridge.Close()
			}
			if microphoneBridge != nil {
				microphoneBridge.Close()
			}
			cancelStream()
			a.recordLaunchError(gen, err)
			return
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			if audioBridge != nil {
				audioBridge.Close()
			}
			if microphoneBridge != nil {
				microphoneBridge.Close()
			}
			cancelStream()
			a.recordLaunchError(gen, err)
			return
		}
		hideChildWindow(cmd)

		shownArgs := args
		shownDestination := resolved
		if runtimeCfg.OutputMode != "local" {
			shownArgs = redactArgs(args)
			shownDestination = redactURL(resolved)
		}
		commandText := quoteCommand(append([]string{resolveExecutable(runtimeCfg.FFmpegPath)}, shownArgs...))
		if err := cmd.Start(); err != nil {
			if audioBridge != nil {
				audioBridge.Close()
			}
			if microphoneBridge != nil {
				microphoneBridge.Close()
			}
			cancelStream()
			a.recordLaunchError(gen, err)
			return
		}
		if !runtimeOptimized {
			lowerProcessPriority(cmd.Process.Pid)
		}

		a.mu.Lock()
		if !a.desired || a.generation != gen {
			a.mu.Unlock()
			_ = cmd.Process.Kill()
			if audioBridge != nil {
				audioBridge.Close()
			}
			if microphoneBridge != nil {
				microphoneBridge.Close()
			}
			cancelStream()
			return
		}
		a.cmd = cmd
		a.running = true
		a.pid = cmd.Process.Pid
		a.startedAt = time.Now()
		a.resolvedURL = shownDestination
		a.command = commandText
		a.lastError = ""
		a.captureStatus = plan.Description
		a.videoEncoder = encoder
		a.videoFPS, a.videoSpeed, a.videoDup, a.videoDrop = 0, 0, 0, 0
		if !first {
			a.restarts++
		}
		a.mu.Unlock()
		first = false
		a.setOverlayStatus(true, "")
		a.appendLog(fmt.Sprintf("Поток запущен, PID=%d, источник=%s, кодировщик=%s, назначение=%s", cmd.Process.Pid, plan.Description, encoderLabel(encoder), shownDestination))

		capture := newCaptureSupervisor(a, runtimeCfg, plan, runtimeOptimized)
		go capture.run(streamCtx)
		writerDone := make(chan error, 1)
		go func() { writerDone <- capture.writeFrames(streamCtx, videoIn) }()

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); a.scanPipe("ffmpeg", stderr) }()
		go func() { defer wg.Done(); a.scanPipe("ffmpeg", stdout) }()

		err = cmd.Wait()
		cancelStream()
		if audioBridge != nil {
			audioBridge.Close()
		}
		if microphoneBridge != nil {
			microphoneBridge.Close()
		}
		select {
		case <-writerDone:
		case <-time.After(2 * time.Second):
		}
		wg.Wait()

		exitCode := -1
		if cmd.ProcessState != nil {
			exitCode = normalizeProcessExitCode(cmd.ProcessState.ExitCode())
		}
		var reconnectSnapshot []RestartEvent
		a.mu.Lock()
		// A previous FFmpeg process may finish after a manual/remote restart has
		// already installed a newer process in app state. Never let the stale
		// generation erase the PID/Running/cmd fields of that new stream.
		if a.generation != gen || a.cmd != cmd {
			a.mu.Unlock()
			return
		}
		a.running = false
		a.pid = 0
		a.cmd = nil
		a.lastExitCode = exitCode
		stillDesired := a.desired
		delay := time.Duration(a.cfg.RestartDelayS) * time.Second
		reason := a.fatalCaptureReason
		encoderFailed := a.encoderFailureDetected && isHardwareEncoder(encoder)
		streamDuration := time.Since(a.startedAt)
		if encoderFailed {
			if a.encoderFailures == nil {
				a.encoderFailures = make(map[string]encoderFailureState)
			}
			state := a.encoderFailures[encoder]
			state.Count++
			state.LastFailure = time.Now()
			state.DisabledUntil = time.Now().Add(30 * time.Minute)
			if state.Count >= 2 {
				state.DisabledUntil = time.Now().Add(2 * time.Hour)
			}
			state.Reason = a.encoderFailureReason
			a.encoderFailures[encoder] = state
			a.videoEncoder = ""
			reason = "Аппаратный кодировщик " + encoderLabel(encoder) + " завершился с ошибкой; временно используется " + encoderLabel(softwareEncoderForCodec(cfg.Codec))
			delay = 700 * time.Millisecond
		} else if isHardwareEncoder(encoder) && streamDuration >= time.Minute {
			delete(a.encoderFailures, encoder)
		}
		if reason == "" && err != nil && stillDesired {
			reason = "RTSP-соединение было прервано"
		}
		if reason != "" && stillDesired {
			now := time.Now()
			a.lastRestartReason = reason
			a.lastRestartAt = now
			a.lastError = reason
			reconnectSnapshot = a.appendReconnectEventLocked(reason, exitCode, now)
		}
		a.mu.Unlock()
		if len(reconnectSnapshot) > 0 {
			a.saveReconnectHistory(reconnectSnapshot)
		}
		a.setOverlayStatus(false, "")
		if reason != "" {
			a.appendLog(fmt.Sprintf("Поток остановлен: %s (код %d)", reason, exitCode))
		} else {
			a.appendLog(fmt.Sprintf("Поток остановлен, код=%d", exitCode))
		}
		if !stillDesired {
			return
		}
		time.Sleep(delay)
	}
}

func (a *app) scanPipe(prefix string, r io.Reader) {
	s := bufio.NewScanner(r)
	s.Split(splitCRLF)
	buf := make([]byte, 64*1024)
	s.Buffer(buf, 2*1024*1024)
	for s.Scan() {
		line := s.Text()
		if prefix == "ffmpeg" && a.processFFmpegLine(prefix, line) {
			continue
		}
		a.appendLog(prefix + ": " + line)
	}
}

func (a *app) recordLaunchError(gen int64, err error) {
	a.mu.Lock()
	// Ignore a launch error produced by an obsolete restart generation. Without
	// this guard an old goroutine can stop a newer, already running stream.
	if a.generation != gen || !a.desired {
		a.mu.Unlock()
		return
	}
	a.running = false
	a.desired = false
	a.lastError = err.Error()
	mediaCmd := a.mediaCmd
	audioCmd := a.audioCmd
	a.audioCmd = nil
	a.mu.Unlock()
	if mediaCmd != nil && mediaCmd.Process != nil {
		_ = mediaCmd.Process.Kill()
	}
	if audioCmd != nil && audioCmd.Process != nil {
		_ = audioCmd.Process.Kill()
	}
	setSleepPrevention(false, false)
	a.setOverlayStatus(false, "Запись экрана не ведётся")
	a.appendLog("ERROR starting FFmpeg: " + err.Error())
}

func (a *app) status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	localURL, lanURL := localStreamLinks(a.cfg)
	s := Status{Version: appVersion, Desired: a.desired, Running: a.running, PID: a.pid, MediaMTXRunning: a.mediaRunning, Restarts: a.restarts,
		LastExitCode: a.lastExitCode, LastError: a.lastError, ResolvedURL: a.resolvedURL, LocalRTSPURL: localURL, LocalRTSPLANURL: lanURL, Command: a.command,
		RecentLog: append([]string(nil), a.recent...), ConfigPath: a.cfgPath, LogPath: a.logPath,
		LastRestartReason: a.lastRestartReason, RestartHistory: append([]RestartEvent(nil), a.restartHistory...), CaptureStatus: a.captureStatus, CaptureBackend: a.captureBackend, VideoEncoder: a.videoEncoder, VideoFPS: a.videoFPS, VideoSpeed: a.videoSpeed, VideoDup: a.videoDup, VideoDrop: a.videoDrop,
		RemoteEnabled: a.cfg.RemoteControlEnabled, RemoteSyncing: a.remoteSyncing, RemoteLastError: a.remoteLastError,
		RemoteRevision: a.cfg.RemoteRevision, RemoteLastCommandID: a.cfg.RemoteLastCommandID, MicrophoneMode: a.cfg.MicrophoneMode, MicrophoneMuted: a.microphoneMuted, MicrophoneActive: a.microphoneActive, MicrophoneLevel: a.microphoneLevel}
	if !a.startedAt.IsZero() {
		s.StartedAt = a.startedAt.Format(time.RFC3339)
	}
	if !a.lastRestartAt.IsZero() {
		s.LastRestartAt = a.lastRestartAt.Format(time.RFC3339)
	}
	if !a.remoteLastSyncAt.IsZero() {
		s.RemoteLastSyncAt = a.remoteLastSyncAt.Format(time.RFC3339)
	}
	return s
}

func helperExecutable(_ string) (string, error) {
	// Индикатор, окно перемещения и WASAPI-loopback запускаются специальными
	// режимами того же исполняемого файла. Это убирает две полные копии EXE из
	// установщика и не добавляет отдельный runtime.
	return os.Executable()
}

func resolveExecutable(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		local := filepath.Join(filepath.Dir(exe), p)
		if _, err := os.Stat(local); err == nil {
			return local
		}
	}
	return p
}

type capturePlan struct {
	Mode         string
	Input        string
	Window       *WindowInfo
	Fallback     bool
	Description  string
	X            int
	Y            int
	Width        int
	Height       int
	OutputWidth  int
	OutputHeight int
}

// normalizeCapturePlanDimensions is the final guard before any raw-video pipe
// or H.264 encoder is created. YUV 4:2:0 and all supported H.264 encoders need
// even frame dimensions. Multi-monitor layouts can produce an odd virtual
// desktop size when screens are shifted by one pixel in Windows settings.
func normalizeCapturePlanDimensions(plan capturePlan) capturePlan {
	plan.Width = even(plan.Width)
	plan.Height = even(plan.Height)
	if plan.OutputWidth < 2 {
		plan.OutputWidth = plan.Width
	}
	if plan.OutputHeight < 2 {
		plan.OutputHeight = plan.Height
	}
	plan.OutputWidth = even(plan.OutputWidth)
	plan.OutputHeight = even(plan.OutputHeight)
	return plan
}

func validateCapturePlanDimensions(plan capturePlan) error {
	if plan.Width < 2 || plan.Height < 2 || plan.OutputWidth < 2 || plan.OutputHeight < 2 {
		return fmt.Errorf("некорректный размер кадра %dx%d -> %dx%d", plan.Width, plan.Height, plan.OutputWidth, plan.OutputHeight)
	}
	if plan.OutputWidth%2 != 0 || plan.OutputHeight%2 != 0 {
		return fmt.Errorf("размер H.264 должен быть чётным: %dx%d", plan.OutputWidth, plan.OutputHeight)
	}
	return nil
}

func findWindowForConfig(cfg Config) *WindowInfo {
	items, err := listWindows()
	if err != nil {
		return nil
	}
	for i := range items {
		if cfg.WindowHandle != "" && strings.EqualFold(items[i].Handle, cfg.WindowHandle) {
			return &items[i]
		}
	}
	for i := range items {
		if cfg.WindowProcess != "" && strings.EqualFold(items[i].ProcessName, cfg.WindowProcess) {
			return &items[i]
		}
	}
	for i := range items {
		if cfg.WindowTitle != "" && items[i].Title == cfg.WindowTitle {
			return &items[i]
		}
	}
	return nil
}

func selectedMonitor(cfg Config, monitors []Monitor) (Monitor, bool) {
	if cfg.MonitorNumber > 0 {
		for _, m := range monitors {
			if m.DisplayNumber == cfg.MonitorNumber {
				return m, true
			}
		}
	}
	if cfg.MonitorIndex >= 0 && cfg.MonitorIndex < len(monitors) {
		return monitors[cfg.MonitorIndex], true
	}
	return Monitor{}, false
}

func resolveCapturePlan(cfg Config) (capturePlan, error) {
	plan := capturePlan{Mode: cfg.CaptureMode, Input: "desktop", Description: "Все экраны"}
	monitors, err := listMonitors()
	if err != nil {
		return plan, fmt.Errorf("не удалось получить список мониторов: %w", err)
	}
	if len(monitors) == 0 {
		return plan, errors.New("Windows не обнаружила подключённые мониторы")
	}
	if cfg.CaptureMode == "monitor" {
		m, ok := selectedMonitor(cfg, monitors)
		if !ok {
			return plan, errors.New("выбранный монитор не найден")
		}
		number := m.DisplayNumber
		if number <= 0 {
			number = m.Index + 1
		}
		plan.Description = fmt.Sprintf("Монитор %d · %dx%d", number, m.Width, m.Height)
		plan.X, plan.Y, plan.Width, plan.Height = m.X, m.Y, even(m.Width), even(m.Height)
		plan.OutputWidth, plan.OutputHeight = scaledCaptureDimensions(plan.Width, plan.Height, cfg.ResolutionProfile)
		plan = normalizeCapturePlanDimensions(plan)
		return plan, validateCapturePlanDimensions(plan)
	}

	plan.Mode = "full"
	minX, minY := monitors[0].X, monitors[0].Y
	maxX, maxY := monitors[0].X+monitors[0].Width, monitors[0].Y+monitors[0].Height
	for _, m := range monitors[1:] {
		if m.X < minX {
			minX = m.X
		}
		if m.Y < minY {
			minY = m.Y
		}
		if m.X+m.Width > maxX {
			maxX = m.X + m.Width
		}
		if m.Y+m.Height > maxY {
			maxY = m.Y + m.Height
		}
	}
	plan.X, plan.Y = minX, minY
	plan.Width, plan.Height = even(maxX-minX), even(maxY-minY)
	plan.OutputWidth, plan.OutputHeight = scaledCaptureDimensions(plan.Width, plan.Height, cfg.ResolutionProfile)
	plan = normalizeCapturePlanDimensions(plan)
	return plan, validateCapturePlanDimensions(plan)
}

func scaledCaptureDimensions(width, height int, profile string) (int, int) {
	maxW, maxH := width, height
	switch profile {
	case "full_hd":
		maxW, maxH = 1920, 1080
	case "hd":
		maxW, maxH = 1280, 720
	default:
		return even(width), even(height)
	}
	if width <= maxW && height <= maxH {
		return even(width), even(height)
	}
	ratioW := float64(maxW) / float64(width)
	ratioH := float64(maxH) / float64(height)
	ratio := ratioW
	if ratioH < ratio {
		ratio = ratioH
	}
	return even(int(float64(width) * ratio)), even(int(float64(height) * ratio))
}

func resolutionScaleFilter(profile string) string {
	switch profile {
	case "full_hd":
		return "scale=w='min(iw,1920)':h='min(ih,1080)':force_original_aspect_ratio=decrease:force_divisible_by=2,setsar=1"
	case "hd":
		return "scale=w='min(iw,1280)':h='min(ih,720)':force_original_aspect_ratio=decrease:force_divisible_by=2,setsar=1"
	default:
		return ""
	}
}

func appendResolutionFilter(chain, profile, outputLabel string) string {
	scale := resolutionScaleFilter(profile)
	if scale == "" {
		return chain + "[" + outputLabel + "]"
	}
	return chain + "," + scale + "[" + outputLabel + "]"
}

func buildDesktopCaptureFilter(cfg Config, plan capturePlan) (string, error) {
	monitors, err := listMonitors()
	if err != nil {
		return "", fmt.Errorf("не удалось получить список мониторов: %w", err)
	}
	if len(monitors) == 0 {
		return "", errors.New("Windows не обнаружила подключённые мониторы")
	}

	source := func(outputIndex, offsetX, offsetY, width, height int, label string, applyScale bool) string {
		opts := []string{
			fmt.Sprintf("output_idx=%d", outputIndex),
			fmt.Sprintf("framerate=%d", cfg.FPS),
			fmt.Sprintf("draw_mouse=%s", bool01(cfg.Cursor)),
			"output_fmt=bgra",
			"allow_fallback=1",
		}
		if width > 0 && height > 0 {
			opts = append(opts,
				fmt.Sprintf("video_size=%dx%d", even(width), even(height)),
				fmt.Sprintf("offset_x=%d", offsetX),
				fmt.Sprintf("offset_y=%d", offsetY))
		}
		chain := "ddagrab=" + strings.Join(opts, ":") + ",hwdownload,format=bgra"
		if applyScale {
			return appendResolutionFilter(chain, cfg.ResolutionProfile, label)
		}
		return chain + "[" + label + "]"
	}

	switch plan.Mode {
	case "monitor":
		m, ok := selectedMonitor(cfg, monitors)
		if !ok {
			return "", errors.New("выбранный монитор не найден")
		}
		return source(m.Index, 0, 0, 0, 0, "vout", true), nil

	default:
		if len(monitors) == 1 {
			return source(0, 0, 0, 0, 0, "vout", true), nil
		}
		minX, minY := monitors[0].X, monitors[0].Y
		for _, m := range monitors[1:] {
			if m.X < minX {
				minX = m.X
			}
			if m.Y < minY {
				minY = m.Y
			}
		}
		parts := make([]string, 0, len(monitors)+1)
		labels := strings.Builder{}
		layout := make([]string, 0, len(monitors))
		for i, m := range monitors {
			label := fmt.Sprintf("v%d", i)
			parts = append(parts, source(i, 0, 0, 0, 0, label, false))
			labels.WriteString("[" + label + "]")
			layout = append(layout, fmt.Sprintf("%d_%d", m.X-minX, m.Y-minY))
		}
		stackChain := fmt.Sprintf("%sxstack=inputs=%d:layout=%s:fill=black", labels.String(), len(monitors), strings.Join(layout, "|"))
		parts = append(parts, appendResolutionFilter(stackChain, cfg.ResolutionProfile, "vout"))
		return strings.Join(parts, ";"), nil
	}
}

func buildEncoderFFmpegDetailed(cfg Config, plan capturePlan, encoder, systemAudioURL, microphoneAudioURL string) (string, []string, capturePlan, error) {
	runtimeFPS := cfg.FPS
	normalizeConfig(&cfg)
	// 10/12 FPS are internal emergency modes selected only by the adaptive
	// performance controller. Keep normalizeConfig strict for persisted/UI
	// settings, but allow those two runtime-only values in the encoder pipeline.
	if runtimeFPS == 10 || runtimeFPS == 12 {
		cfg.FPS = runtimeFPS
	}
	if encoder == "" || encoder == "auto" {
		encoder = softwareEncoderForCodec(cfg.Codec)
	}
	cfg.Codec = encoderCodec(encoder)

	var dest string
	if cfg.OutputMode == "local" {
		dest = localRTSPURL(cfg, "127.0.0.1")
	} else {
		var err error
		dest, _, err = resolvePublishURL(cfg.Link, cfg.Protocol)
		if err != nil {
			return "", nil, capturePlan{}, err
		}
	}
	if plan.Width < 2 || plan.Height < 2 || plan.OutputWidth < 2 || plan.OutputHeight < 2 {
		var err error
		plan, err = resolveCapturePlan(cfg)
		if err != nil {
			return "", nil, capturePlan{}, err
		}
	}
	plan = normalizeCapturePlanDimensions(plan)
	if err := validateCapturePlanDimensions(plan); err != nil {
		return "", nil, capturePlan{}, err
	}

	args := []string{"-hide_banner", "-loglevel", "info", "-fflags", "+genpts"}
	nextInput := 0
	systemInput := -1
	microphoneInput := -1
	if cfg.AudioEnabled {
		if strings.TrimSpace(systemAudioURL) == "" {
			return "", nil, plan, errors.New("не создан локальный канал системного звука")
		}
		systemInput = nextInput
		nextInput++
		args = append(args,
			"-thread_queue_size", "8",
			"-f", "s16le",
			"-ar", "48000",
			"-ac", "2",
			"-i", systemAudioURL)
	}
	if cfg.MicrophoneEnabled {
		if strings.TrimSpace(cfg.MicrophoneDevice) == "" {
			return "", nil, plan, errors.New("выберите устройство микрофона")
		}
		if strings.TrimSpace(microphoneAudioURL) == "" {
			return "", nil, plan, errors.New("не создан локальный канал микрофона")
		}
		microphoneInput = nextInput
		nextInput++
		args = append(args,
			"-thread_queue_size", "16",
			"-f", "s16le",
			"-ar", "48000",
			"-ac", "2",
			"-i", microphoneAudioURL)
	}

	videoInput := nextInput
	args = append(args,
		"-thread_queue_size", "2",
		"-f", "rawvideo",
		"-pixel_format", "bgra",
		"-video_size", fmt.Sprintf("%dx%d", plan.OutputWidth, plan.OutputHeight),
		"-framerate", strconv.Itoa(cfg.FPS),
		"-i", "pipe:0")

	encoderPixelFormat := "yuv420p"
	if isHardwareEncoder(encoder) {
		encoderPixelFormat = "nv12"
	}
	filters := []string{fmt.Sprintf("[%d:v]setpts=N/(%d*TB),scale=trunc(iw/2)*2:trunc(ih/2)*2:flags=bicubic+accurate_rnd+full_chroma_int,format=%s[vout]", videoInput, cfg.FPS, encoderPixelFormat)}
	hasAudio := systemInput >= 0 || microphoneInput >= 0
	if systemInput >= 0 {
		systemFilter := "aresample=async=1:first_pts=0,asetpts=N/SR/TB"
		if cfg.AudioAdvanceMs > 0 {
			systemFilter = fmt.Sprintf("atrim=start=%.3f,aresample=async=1:first_pts=0,asetpts=N/SR/TB", float64(cfg.AudioAdvanceMs)/1000)
		}
		filters = append(filters, fmt.Sprintf("[%d:a:0]%s[asystem]", systemInput, systemFilter))
	}
	if microphoneInput >= 0 {
		filters = append(filters, fmt.Sprintf("[%d:a:0]aresample=async=1:first_pts=0,asetpts=N/SR/TB[amicrophone]", microphoneInput))
	}
	if systemInput >= 0 && microphoneInput >= 0 {
		filters = append(filters, "[asystem][amicrophone]amix=inputs=2:duration=longest:dropout_transition=0:normalize=0,alimiter=limit=0.95[aout]")
	} else if systemInput >= 0 {
		filters = append(filters, "[asystem]anull[aout]")
	} else if microphoneInput >= 0 {
		filters = append(filters, "[amicrophone]anull[aout]")
	}
	args = append(args, "-filter_complex", strings.Join(filters, ";"), "-map", "[vout]")
	if hasAudio {
		args = append(args, "-map", "[aout]")
	}

	gopFrames := cfg.FPS * 2
	switch encoder {
	case "h264_nvenc", "hevc_nvenc":
		args = append(args,
			"-c:v", encoder,
			"-preset", "p4",
			"-tune", "ll",
			"-rc", map[bool]string{true: "cbr", false: "vbr"}[cfg.RateControl == "cbr"],
			"-multipass", "qres",
			"-spatial_aq", "1",
			"-bf", "0")
	case "h264_amf", "hevc_amf":
		args = append(args,
			"-c:v", encoder,
			"-usage", "lowlatency",
			"-quality", "balanced",
			"-rc", map[bool]string{true: "cbr", false: "vbr_peak"}[cfg.RateControl == "cbr"],
			"-bf", "0")
	case "h264_qsv", "hevc_qsv":
		args = append(args,
			"-c:v", encoder,
			"-preset", "medium",
			"-look_ahead", "0",
			"-bf", "0")
	case "libx265":
		x265Params := fmt.Sprintf("keyint=%d:min-keyint=%d:scenecut=0:open-gop=0:repeat-headers=1", gopFrames, gopFrames)
		if cfg.RateControl == "cbr" {
			x265Params += fmt.Sprintf(":vbv-maxrate=%d:vbv-bufsize=%d:strict-cbr=1", cfg.MaxrateKbps, cfg.BufsizeKbps)
		}
		args = append(args,
			"-c:v", "libx265",
			"-preset", cfg.Preset,
			"-tune", "zerolatency",
			"-x265-params", x265Params,
			"-bf", "0")
	default:
		x264Params := fmt.Sprintf(
			"nal-hrd=%s:keyint=%d:min-keyint=%d:scenecut=0:repeat-headers=1:aud=1",
			map[bool]string{true: "cbr", false: "vbr"}[cfg.RateControl == "cbr"], gopFrames, gopFrames,
		)
		args = append(args,
			"-c:v", "libx264",
			"-preset", cfg.Preset,
			"-tune", "zerolatency",
			"-x264-params", x264Params,
			"-bf", "0")
	}

	args = append(args, "-b:v", fmt.Sprintf("%dk", cfg.BitrateKbps))
	if cfg.RateControl == "cbr" {
		args = append(args, "-minrate", fmt.Sprintf("%dk", cfg.BitrateKbps))
	}
	args = append(args,
		"-maxrate", fmt.Sprintf("%dk", cfg.MaxrateKbps),
		"-bufsize", fmt.Sprintf("%dk", cfg.BufsizeKbps),
		"-r", strconv.Itoa(cfg.FPS),
		"-fps_mode", "cfr",
		"-g", strconv.Itoa(gopFrames),
		"-keyint_min", strconv.Itoa(gopFrames),
		"-sc_threshold", "0",
		"-pix_fmt", encoderPixelFormat)
	if isHardwareEncoder(encoder) {
		args = append(args, "-bsf:v", "dump_extra=freq=keyframe")
	}

	if hasAudio {
		args = append(args,
			"-c:a", "aac",
			"-profile:a", "aac_low",
			"-b:a", fmt.Sprintf("%dk", cfg.AudioBitrateKbps),
			"-ar", strconv.Itoa(cfg.AudioSampleRate),
			"-ac", strconv.Itoa(cfg.AudioChannels))
	} else {
		args = append(args, "-an")
	}

	if cfg.OutputMode == "local" || cfg.Protocol == "rtsp" {
		args = append(args, "-f", "rtsp", "-rtsp_transport", "tcp", dest)
	} else {
		args = append(args, "-f", "flv", dest)
	}
	return dest, args, plan, nil
}

func buildFFmpegDetailed(cfg Config) (string, []string, capturePlan, error) {
	plan, err := resolveCapturePlan(cfg)
	if err != nil {
		return "", nil, capturePlan{}, err
	}
	audioURL := ""
	if cfg.AudioEnabled && cfg.AudioSource == "system" {
		audioURL = "tcp://127.0.0.1:12345"
	}
	encoder := cfg.Encoder
	if encoder == "" || encoder == "auto" {
		encoder = "libx264"
	}
	microphoneURL := ""
	if cfg.MicrophoneEnabled {
		microphoneURL = "tcp://127.0.0.1:12346"
	}
	return buildEncoderFFmpegDetailed(cfg, plan, encoder, audioURL, microphoneURL)
}

func buildFFmpeg(cfg Config) (string, []string, error) {
	dest, args, _, err := buildFFmpegDetailed(cfg)
	return dest, args, err
}

func even(v int) int {
	if v < 2 {
		return 2
	}
	return v - v%2
}
func bool01(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// LinkVideo stores a Base58-encoded JSON object. The hash field is treated as
// an opaque server-generated value: its format can vary between link versions.
type encodedLink struct {
	RtmpURL      string          `json:"rtmp_url"`
	SerialNumber string          `json:"serial_number"`
	Hash         json.RawMessage `json:"hash,omitempty"`
}

func resolvePublishURL(input, protocol string) (string, string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", errors.New("ссылка LinkVideo не указана")
	}

	var raw string
	serial := ""
	if strings.HasPrefix(strings.ToLower(input), "rtmp://") || strings.HasPrefix(strings.ToLower(input), "rtsp://") {
		raw = input
	} else {
		token, err := extractEncodedToken(input)
		if err != nil {
			return "", "", err
		}
		decoded, err := decodeBase58(token)
		if err != nil {
			return "", "", fmt.Errorf("не удалось декодировать Base58: %w", err)
		}
		var p encodedLink
		if err := json.Unmarshal(decoded, &p); err != nil {
			return "", "", fmt.Errorf("неверный формат ссылки: %w", err)
		}
		if p.RtmpURL == "" {
			return "", "", errors.New("в ссылке отсутствует rtmp_url")
		}
		// Hash is intentionally not validated here. Older LinkVideo.Monitor builds
		// used a decimal CRC32 for one link format, while newer links can contain
		// another server-generated signature. It is not needed to publish because
		// the actual authorization data is already part of rtmp_url.
		raw, serial = p.RtmpURL, p.SerialNumber
		if err := validateLinkVideoHost(raw); err != nil {
			return "", "", err
		}
	}

	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "", "", errors.New("адрес сервера в ссылке некорректен")
	}
	switch protocol {
	case "rtmp":
		raw = makeRTMP(raw)
	default:
		raw = makeRTSP(raw)
	}
	return raw, serial, nil
}

func validateLinkVideoHost(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	h := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(h, ".video.goodline.info") {
		return fmt.Errorf("домен %q не соответствует штатному домену LinkVideo", h)
	}
	return nil
}

func makeRTSP(s string) string {
	if strings.HasPrefix(strings.ToLower(s), "rtsp://") {
		return s
	}
	s = replacePrefixFold(s, "rtmp://", "rtsp://")
	return strings.Replace(s, ":1935/", ":554/", 1)
}
func makeRTMP(s string) string {
	if strings.HasPrefix(strings.ToLower(s), "rtmp://") {
		return s
	}
	s = replacePrefixFold(s, "rtsp://", "rtmp://")
	return strings.Replace(s, ":554/", ":1935/", 1)
}
func replacePrefixFold(s, old, new string) string {
	if len(s) >= len(old) && strings.EqualFold(s[:len(old)], old) {
		return new + s[len(old):]
	}
	return s
}

func extractEncodedToken(input string) (string, error) {
	token := strings.TrimSpace(input)
	lower := strings.ToLower(token)

	// Сайт и разные браузеры могут передавать ссылку несколькими формами:
	// linkvideomonitor:<Base58>
	// linkvideomonitor://<Base58>
	// linkvideomonitor://open/<Base58>
	// linkvideomonitor://open/<Base58>/
	// Оригинальная программа фактически брала последний непустой сегмент пути.
	const scheme = "linkvideomonitor:"
	if strings.HasPrefix(lower, scheme) {
		token = token[len(scheme):]
	}

	// Браузер иногда экранирует отдельные символы URI. Для Base58 это обычно
	// не требуется, но декодирование делает обработчик совместимее с сайтом.
	if decoded, err := url.PathUnescape(token); err == nil {
		token = decoded
	}

	// Не включаем query/fragment и завершающие разделители.
	if i := strings.IndexAny(token, "?#"); i >= 0 {
		token = token[:i]
	}
	token = strings.TrimSpace(strings.Trim(token, "/\\"))

	// Если браузер или сайт добавил host/action перед полезной частью,
	// используем последний непустой сегмент — так же, как штатный Monitor.
	if i := strings.LastIndexAny(token, "/\\"); i >= 0 {
		token = token[i+1:]
	}
	token = strings.TrimSpace(strings.Trim(token, "/\\"))
	if token == "" {
		return "", errors.New("в ссылке отсутствует Base58-код после linkvideomonitor:")
	}
	return token, nil
}

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func decodeBase58(s string) ([]byte, error) {
	if s == "" {
		return nil, errors.New("пустая строка")
	}
	out := []byte{0}
	for _, r := range s {
		idx := strings.IndexRune(base58Alphabet, r)
		if idx < 0 {
			return nil, fmt.Errorf("недопустимый символ %q", r)
		}
		carry := idx
		for i := len(out) - 1; i >= 0; i-- {
			n := int(out[i])*58 + carry
			out[i] = byte(n & 0xff)
			carry = n >> 8
		}
		for carry > 0 {
			out = append([]byte{byte(carry & 0xff)}, out...)
			carry >>= 8
		}
	}
	zeros := 0
	for zeros < len(s) && s[zeros] == '1' {
		zeros++
	}
	i := 0
	for i < len(out)-1 && out[i] == 0 {
		i++
	}
	result := append(make([]byte, zeros), out[i:]...)
	return result, nil
}

func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<скрыто>"
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) > 0 && parts[len(parts)-1] != "" {
		parts[len(parts)-1] = "***"
	}
	u.Path = "/" + strings.Join(parts, "/")
	u.RawQuery = ""
	return u.String()
}
func redactArgs(args []string) []string {
	r := append([]string(nil), args...)
	for i, v := range r {
		if strings.HasPrefix(strings.ToLower(v), "rtsp://") || strings.HasPrefix(strings.ToLower(v), "rtmp://") {
			r[i] = redactURL(v)
		}
	}
	return r
}
func quoteCommand(args []string) string {
	q := make([]string, len(args))
	for i, s := range args {
		if strings.ContainsAny(s, " \t\"") {
			q[i] = strconv.Quote(s)
		} else {
			q[i] = s
		}
	}
	return strings.Join(q, " ")
}

func testDestination(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "rtmp" {
			port = "1935"
		} else if u.Scheme == "rtsp" {
			port = "554"
		} else {
			return errors.New("неподдерживаемая схема")
		}
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort(u.Hostname(), port), 5*time.Second)
	if err != nil {
		return err
	}
	return c.Close()
}

func protectedConfigChange(previous, next Config) bool {
	if previous.FPS != next.FPS ||
		previous.BitrateKbps != next.BitrateKbps ||
		previous.MaxrateKbps != next.MaxrateKbps ||
		previous.BufsizeKbps != next.BufsizeKbps ||
		previous.QualityProfile != next.QualityProfile ||
		previous.LaunchWithWindows != next.LaunchWithWindows {
		return true
	}
	// The user may enable the visible recording indicator without a password,
	// but hiding it completely is an administrator-only action.
	return previous.OverlayEnabled && !next.OverlayEnabled
}

// configChangeRequiresRestart contains only settings that are baked into the
// active capture/encoder process. A restart caused by saving an allowed local
// setting is an internal application action and must not require the separate
// administrator permission used by the manual Start/Stop/Restart buttons.
func configChangeRequiresRestart(previous, next Config) bool {
	return previous.PrivacyProtection != next.PrivacyProtection ||
		previous.ResolutionProfile != next.ResolutionProfile ||
		previous.Link != next.Link ||
		previous.OutputMode != next.OutputMode ||
		previous.Protocol != next.Protocol ||
		previous.FFmpegPath != next.FFmpegPath ||
		previous.MediaMTXPath != next.MediaMTXPath ||
		previous.LocalRTSPPort != next.LocalRTSPPort ||
		previous.LocalRTSPPath != next.LocalRTSPPath ||
		previous.CaptureMode != next.CaptureMode ||
		previous.MonitorIndex != next.MonitorIndex ||
		previous.MonitorNumber != next.MonitorNumber ||
		previous.QualityProfile != next.QualityProfile ||
		previous.FPS != next.FPS ||
		previous.BitrateKbps != next.BitrateKbps ||
		previous.RateControl != next.RateControl ||
		previous.MaxrateKbps != next.MaxrateKbps ||
		previous.BufsizeKbps != next.BufsizeKbps ||
		previous.Codec != next.Codec ||
		previous.Encoder != next.Encoder ||
		previous.Preset != next.Preset ||
		previous.Cursor != next.Cursor ||
		previous.AudioEnabled != next.AudioEnabled ||
		previous.MicrophoneEnabled != next.MicrophoneEnabled ||
		previous.MicrophoneDevice != next.MicrophoneDevice ||
		previous.AudioQuality != next.AudioQuality ||
		previous.AudioSource != next.AudioSource ||
		previous.AudioDevice != next.AudioDevice ||
		previous.AudioBitrateKbps != next.AudioBitrateKbps ||
		previous.AudioSampleRate != next.AudioSampleRate ||
		previous.AudioChannels != next.AudioChannels ||
		previous.AudioAdvanceMs != next.AudioAdvanceMs
}

// hasUsableRemoteTarget reports whether the remote output already contains a
// complete publish target. A fresh installation intentionally starts without a
// LinkVideo connection link, so AutoStart cannot begin until this becomes true.
func hasUsableRemoteTarget(cfg Config) bool {
	if cfg.OutputMode != "remote" {
		return false
	}
	_, _, err := resolvePublishURL(cfg.Link, cfg.Protocol)
	return err == nil
}

// shouldAutoStartAfterFirstTarget allows the first LinkVideo link to activate
// the stream without asking for the administrator password. It is deliberately
// limited to the transition from no valid target to a valid one, so a stream
// that was manually stopped does not start again after an unrelated save.
func shouldAutoStartAfterFirstTarget(previous, next Config, desired bool) bool {
	return !desired && !hasUsableRemoteTarget(previous) && hasUsableRemoteTarget(next)
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, indexHTML)
	})
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/x-icon")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(faviconICO)
	})
	mux.HandleFunc("/api/auth/status", a.handleAuthStatus)
	mux.HandleFunc("/api/auth/setup", a.handleAuthSetup)
	mux.HandleFunc("/api/auth/verify", a.handleAuthVerify)
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			a.mu.Lock()
			cfg := a.cfg
			a.mu.Unlock()
			writeJSON(w, 200, cfg)
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		var cfg Config
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&cfg); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		a.mu.Lock()
		previousConfig := a.cfg
		a.mu.Unlock()
		// Внутренние параметры отсутствуют в веб-форме и должны сохраняться.
		cfg.DefaultsRevision = previousConfig.DefaultsRevision
		cfg.CaptureBackend = previousConfig.CaptureBackend
		cfg.AutoStart = previousConfig.AutoStart
		cfg.RestartAfterResume = previousConfig.RestartAfterResume
		cfg.DeveloperMode = previousConfig.DeveloperMode
		cfg.WindowTitle = previousConfig.WindowTitle
		cfg.WindowHandle = previousConfig.WindowHandle
		cfg.WindowProcess = previousConfig.WindowProcess
		cfg.OffsetX = previousConfig.OffsetX
		cfg.OffsetY = previousConfig.OffsetY
		cfg.Width = previousConfig.Width
		cfg.Height = previousConfig.Height
		// Тип битрейта управляется программой и не выводится в интерфейс.
		cfg.RateControl = "cbr"
		// Параметры синхронизации являются служебными и скрыты от пользователя.
		cfg.RemoteAPIURL = previousConfig.RemoteAPIURL
		cfg.RemoteControlEnabled = previousConfig.RemoteControlEnabled
		cfg.RemoteSyncIntervalMin = previousConfig.RemoteSyncIntervalMin
		cfg.RemoteRevision = previousConfig.RemoteRevision
		cfg.RemoteLastCommandID = previousConfig.RemoteLastCommandID
		normalizeConfig(&cfg)
		restartRequired := configChangeRequiresRestart(previousConfig, cfg)
		if protectedConfigChange(previousConfig, cfg) && !a.requireAdmin(w, r) {
			return
		}
		if cfg.OutputMode == "local" {
			portStatus := a.checkLocalRTSPPort(cfg.LocalRTSPPort)
			if !portStatus.Available {
				writeJSON(w, http.StatusConflict, map[string]string{"error": portStatus.Message})
				return
			}
		}
		a.mu.Lock()
		// Re-read in case a remote sync changed hidden metadata while the port was checked.
		current := a.cfg
		cfg.RemoteRevision = current.RemoteRevision
		cfg.RemoteLastCommandID = current.RemoteLastCommandID
		a.cfg = cfg
		if !current.MicrophoneEnabled && cfg.MicrophoneEnabled {
			a.microphoneMuted = false
		}
		if !cfg.MicrophoneEnabled {
			a.microphonePTTActive = false
			a.microphoneActive = false
			a.microphoneLevel = 0
		}
		err := a.saveConfigLocked()
		if err != nil {
			a.cfg = current
		} else {
			a.configLoadError = false
		}
		a.mu.Unlock()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if err := syncStartupRegistration(cfg.LaunchWithWindows); err != nil {
			a.appendLog("Автозапуск Windows: " + err.Error())
		}
		a.mu.Lock()
		desired := a.desired
		running := a.running
		a.mu.Unlock()
		autoStartAfterLink := shouldAutoStartAfterFirstTarget(previousConfig, cfg, desired)
		if desired {
			setSleepPrevention(cfg.PreventSleep, cfg.KeepDisplayOn)
		}
		a.setOverlayStatus(running, func() string {
			if running {
				return cfg.OverlayText
			}
			if desired {
				return "Подключение…"
			}
			return "Запись экрана не ведётся"
		}())
		if autoStartAfterLink {
			a.appendLog("Ссылка подключения добавлена; поток запускается автоматически")
			go func() {
				if startErr := a.start(); startErr != nil {
					a.appendLog("Не удалось автоматически запустить поток: " + startErr.Error())
				}
			}()
		} else if desired && restartRequired {
			a.appendLog("Настройки источника изменены; поток автоматически перезапускается")
			go func() {
				if restartErr := a.restart(); restartErr != nil {
					a.appendLog("Не удалось применить настройки потока: " + restartErr.Error())
				}
			}()
		}
		a.requestRemoteSync()
		writeJSON(w, 200, cfg)
	})
	mux.HandleFunc("/api/apply-link", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		var body struct {
			Link string `json:"link"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "не удалось прочитать ссылку"})
			return
		}
		if _, err := extractEncodedToken(body.Link); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		a.mu.Lock()
		previousConfig := a.cfg
		a.cfg.Link = strings.TrimSpace(body.Link)
		a.cfg.OutputMode = "remote"
		nextConfig := a.cfg
		err := a.saveConfigLocked()
		if err != nil {
			a.cfg = previousConfig
		} else {
			a.configLoadError = false
		}
		desired := a.desired
		autoStartAfterLink := shouldAutoStartAfterFirstTarget(previousConfig, nextConfig, desired)
		a.mu.Unlock()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if desired {
			go func() {
				if restartErr := a.restart(); restartErr != nil {
					a.appendLog("Не удалось применить новую ссылку подключения: " + restartErr.Error())
				}
			}()
		} else if autoStartAfterLink {
			a.appendLog("Ссылка подключения получена; поток запускается автоматически")
			go func() {
				if startErr := a.start(); startErr != nil {
					a.appendLog("Не удалось автоматически запустить поток: " + startErr.Error())
				}
			}()
		}
		a.requestRemoteSync()
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/api/check-updates", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		result, err := checkForUpdates(r.Context())
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, result)
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, a.status()) })
	mux.HandleFunc("/api/monitors", func(w http.ResponseWriter, r *http.Request) {
		ms, err := listMonitors()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, ms)
	})
	mux.HandleFunc("/api/place-overlay", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		// Перемещение индикатора не изменяет параметры трансляции и разрешено
		// пользователю без пароля. Отключение индикатора по-прежнему проходит
		// через защищённое сохранение /api/config.
		if !overlayPlacementMu.TryLock() {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "перемещение надписи уже открыто"})
			return
		}
		defer overlayPlacementMu.Unlock()
		a.mu.Lock()
		x, y := a.cfg.OverlayX, a.cfg.OverlayY
		wasRunning := a.running
		a.mu.Unlock()
		a.stopOverlay()
		// Завершаем возможные зависшие копии индикатора от ранних beta-версий.
		killOverlay := exec.Command("taskkill.exe", "/IM", "LinkVideo.ScreenOverlay.exe", "/T", "/F")
		hideChildWindow(killOverlay)
		_ = killOverlay.Run()
		time.Sleep(180 * time.Millisecond)
		exe, err := helperExecutable("LinkVideo.ScreenOverlay.exe")
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		cmd := exec.Command(exe, "--place-overlay", strconv.Itoa(x), strconv.Itoa(y))
		hideChildWindow(cmd)
		out, err := cmd.Output()
		if err != nil {
			if wasRunning {
				a.setOverlayStatus(true, "")
			}
			writeJSON(w, 400, map[string]string{"error": "перемещение индикатора отменено"})
			return
		}
		var pos OverlayPosition
		if err := json.Unmarshal(out, &pos); err != nil {
			writeJSON(w, 500, map[string]string{"error": "не удалось сохранить положение индикатора"})
			return
		}
		a.mu.Lock()
		previousX, previousY := a.cfg.OverlayX, a.cfg.OverlayY
		a.cfg.OverlayX, a.cfg.OverlayY = pos.X, pos.Y
		err = a.saveConfigLocked()
		if err != nil {
			a.cfg.OverlayX, a.cfg.OverlayY = previousX, previousY
		} else {
			a.configLoadError = false
		}
		a.mu.Unlock()
		if wasRunning {
			a.setOverlayStatus(true, "")
		}
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, pos)
	})
	mux.HandleFunc("/api/logs/text", func(w http.ResponseWriter, r *http.Request) {
		b, _, err := a.exportLogs()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/api/logs/download", func(w http.ResponseWriter, r *http.Request) {
		b, filename, err := a.exportLogs()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/api/logs/open-folder", func(w http.ResponseWriter, r *http.Request) {
		_ = os.MkdirAll(a.logsDir, 0755)
		if err := openFolder(a.logsDir); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("/api/audio-devices", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		ffmpegPath := a.cfg.FFmpegPath
		a.mu.Unlock()
		devices, err := listAudioDevices(ffmpegPath)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, devices)
	})
	mux.HandleFunc("/api/encoder-capabilities", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, a.getEncoderCapabilities(r.URL.Query().Get("refresh") == "1"))
	})
	mux.HandleFunc("/api/check-port", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		var body struct {
			Port int `json:"port"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "не удалось прочитать порт"})
			return
		}
		result := a.checkLocalRTSPPort(body.Port)
		status := http.StatusOK
		if !result.Available {
			status = http.StatusConflict
		}
		writeJSON(w, status, result)
	})
	mux.HandleFunc("/api/local-links", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		cfg := a.cfg
		a.mu.Unlock()
		localURL, lanURL := localStreamLinks(cfg)
		_, statErr := os.Stat(resolveExecutable(cfg.MediaMTXPath))
		writeJSON(w, 200, map[string]any{"local": localURL, "lan": lanURL, "mediamtx_found": statErr == nil})
	})
	mux.HandleFunc("/api/remote-sync", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		if err := a.syncRemoteOnce(); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, a.status())
	})
	mux.HandleFunc("/api/start", func(w http.ResponseWriter, r *http.Request) {
		if err := a.start(); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, a.status())
	})
	mux.HandleFunc("/api/stop", func(w http.ResponseWriter, r *http.Request) {
		if !a.requireAdmin(w, r) {
			return
		}
		a.stop()
		writeJSON(w, 200, a.status())
	})
	mux.HandleFunc("/api/restart", func(w http.ResponseWriter, r *http.Request) {
		if err := a.restart(); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, a.status())
	})
	mux.HandleFunc("/api/exit", func(w http.ResponseWriter, r *http.Request) {
		a.stop()
		writeJSON(w, 200, map[string]string{"status": "closing"})
		go func() { time.Sleep(300 * time.Millisecond); os.Exit(0) }()
	})
	mux.HandleFunc("/api/test", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		cfg := a.cfg
		a.mu.Unlock()
		var body struct {
			Link          string `json:"link"`
			Protocol      string `json:"protocol"`
			OutputMode    string `json:"output_mode"`
			LocalRTSPPort int    `json:"local_rtsp_port"`
		}
		if r.Body != nil {
			b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if len(strings.TrimSpace(string(b))) > 0 {
				if err := json.Unmarshal(b, &body); err != nil {
					writeJSON(w, 400, map[string]string{"error": "не удалось прочитать параметры проверки"})
					return
				}
				if strings.TrimSpace(body.Link) != "" {
					cfg.Link = body.Link
				}
				if body.Protocol != "" {
					cfg.Protocol = body.Protocol
				}
				if body.OutputMode != "" {
					cfg.OutputMode = body.OutputMode
				}
				if body.LocalRTSPPort > 0 {
					cfg.LocalRTSPPort = body.LocalRTSPPort
				}
			}
		}
		normalizeConfig(&cfg)
		if cfg.OutputMode == "local" {
			portStatus := a.checkLocalRTSPPort(cfg.LocalRTSPPort)
			if !portStatus.Available {
				writeJSON(w, http.StatusConflict, map[string]string{"error": portStatus.Message})
				return
			}
			localURL, lanURL := localStreamLinks(cfg)
			writeJSON(w, 200, map[string]string{"status": "ok", "destination": localURL, "lan": lanURL, "server": fmt.Sprintf("локальный порт %d", cfg.LocalRTSPPort)})
			return
		}
		dest, serial, err := resolvePublishURL(cfg.Link, cfg.Protocol)
		if err == nil {
			err = testDestination(dest)
		}
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		server := ""
		if u, parseErr := url.Parse(dest); parseErr == nil {
			server = u.Hostname()
		}
		writeJSON(w, 200, map[string]string{"status": "ok", "destination": redactURL(dest), "server": server, "serial_number": serial})
	})

	mux.HandleFunc("/api/preview-command", func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		cfg := a.cfg
		a.mu.Unlock()
		dest, args, err := buildFFmpeg(cfg)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		shownArgs := args
		shownDest := dest
		if cfg.OutputMode != "local" {
			shownArgs = redactArgs(args)
			shownDest = redactURL(dest)
		}
		writeJSON(w, 200, map[string]string{"destination": shownDest, "command": quoteCommand(append([]string{resolveExecutable(cfg.FFmpegPath)}, shownArgs...))})
	})
	return localOnly(mux)
}

var postOnlyLocalPaths = map[string]bool{
	"/api/auth/setup":       true,
	"/api/auth/verify":      true,
	"/api/check-port":       true,
	"/api/apply-link":       true,
	"/api/check-updates":    true,
	"/api/place-overlay":    true,
	"/api/logs/open-folder": true,
	"/api/remote-sync":      true,
	"/api/start":            true,
	"/api/stop":             true,
	"/api/restart":          true,
	"/api/exit":             true,
	"/api/test":             true,
}

func isLocalWebHost(value string) bool {
	host := strings.TrimSpace(value)
	port := ""
	if parsedHost, parsedPort, err := net.SplitHostPort(host); err == nil {
		host, port = parsedHost, parsedPort
	} else {
		host = strings.Trim(host, "[]")
	}
	if port != "" && port != "8098" {
		return false
	}
	return strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "::1"
}

func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || (remoteHost != "127.0.0.1" && remoteHost != "::1") || !isLocalWebHost(r.Host) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if postOnlyLocalPaths[r.URL.Path] && r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}
		// Browsers cannot attach this non-simple header from another origin without
		// a successful CORS preflight. We never enable CORS, so a random website
		// cannot stop, restart or reconfigure the local Monitor process.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get("X-LinkVideo-Request") != "1" {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden local request"})
				return
			}
		}
		w.Header().Set("X-LinkVideo-Monitor", appVersion)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func openFolder(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

func openBrowser(raw string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", raw)
	case "darwin":
		cmd = exec.Command("open", raw)
	default:
		cmd = exec.Command("xdg-open", raw)
	}
	_ = cmd.Start()
}

func protocolLinkArg(args []string) string {
	for _, arg := range args[1:] {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(arg)), "linkvideomonitor:") {
			return strings.TrimSpace(arg)
		}
	}
	return ""
}

func runningInstanceAvailable() bool {
	req, err := http.NewRequest(http.MethodGet, "http://"+listenAddr+"/api/status", nil)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 750 * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(resp.Header.Get("X-LinkVideo-Monitor")) == "" {
		return false
	}
	var status Status
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&status); err != nil {
		return false
	}
	return strings.TrimSpace(status.Version) != ""
}

func sendLinkToRunning(link string) error {
	body, _ := json.Marshal(map[string]string{"link": link})
	req, err := http.NewRequest(http.MethodPost, "http://"+listenAddr+"/api/apply-link", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-LinkVideo-Request", "1")
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("локальный интерфейс вернул HTTP %d", resp.StatusCode)
	}
	return nil
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--uac-service":
			if err := runUACService(); err != nil {
				_, _ = fmt.Fprintln(os.Stderr, err)
				os.Exit(11)
			}
			return
		case "--secure-desktop-capture", "--secure-gdi-capture":
			if err := runSecureGDICapture(os.Args[2:]); err != nil {
				_, _ = fmt.Fprintln(os.Stderr, err)
				os.Exit(12)
			}
			return
		case "--restart-wait":
			if err := runRestartWait(); err != nil {
				_, _ = fmt.Fprintln(os.Stderr, err)
				os.Exit(7)
			}
			return
		case "--uninstall":
			runUninstaller()
			return
		case "--uninstall-worker":
			parentPID := 0
			if len(os.Args) > 2 {
				parentPID, _ = strconv.Atoi(os.Args[2])
			}
			runUninstallWorker(parentPID)
			return
		case "--wasapi-loopback":
			if err := runWASAPILoopback(os.Stdout); err != nil {
				_, _ = fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
			return
		case "--gdi-capture":
			if len(os.Args) < 10 {
				_, _ = fmt.Fprintln(os.Stderr, "usage: --gdi-capture x y width height output_width output_height fps cursor")
				os.Exit(8)
			}
			x, _ := strconv.Atoi(os.Args[2])
			y, _ := strconv.Atoi(os.Args[3])
			width, _ := strconv.Atoi(os.Args[4])
			height, _ := strconv.Atoi(os.Args[5])
			outputWidth, _ := strconv.Atoi(os.Args[6])
			outputHeight, _ := strconv.Atoi(os.Args[7])
			fps, _ := strconv.Atoi(os.Args[8])
			drawCursor := os.Args[9] == "1" || strings.EqualFold(os.Args[9], "true")
			if err := runGDICapture(os.Stdout, x, y, width, height, outputWidth, outputHeight, fps, drawCursor); err != nil {
				_, _ = fmt.Fprintln(os.Stderr, err)
				os.Exit(8)
			}
			return
		case "--select-region":
			r, err := selectRegionInteractive()
			if err != nil {
				_, _ = fmt.Fprintln(os.Stderr, err)
				os.Exit(3)
			}
			if err := printRegionJSON(r); err != nil {
				os.Exit(4)
			}
			return
		case "--overlay":
			started := time.Now().Unix()
			x, y := -1, -1
			if len(os.Args) > 2 {
				started, _ = strconv.ParseInt(os.Args[2], 10, 64)
			}
			if len(os.Args) > 3 {
				x, _ = strconv.Atoi(os.Args[3])
			}
			if len(os.Args) > 4 {
				y, _ = strconv.Atoi(os.Args[4])
			}
			_ = runOverlay(started, x, y)
			return
		case "--place-overlay":
			x, y := -1, -1
			if len(os.Args) > 2 {
				x, _ = strconv.Atoi(os.Args[2])
			}
			if len(os.Args) > 3 {
				y, _ = strconv.Atoi(os.Args[3])
			}
			pos, err := placeOverlayInteractive(x, y)
			if err != nil {
				os.Exit(5)
			}
			if err := printOverlayPositionJSON(pos); err != nil {
				os.Exit(6)
			}
			return
		}
	}

	uriLink := protocolLinkArg(os.Args)
	background := len(os.Args) > 1 && os.Args[1] == "--background"
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		// Do not assume that every process listening on 8098 is LinkVideo Monitor:
		// otherwise a port collision could receive the private connection link.
		if !runningInstanceAvailable() {
			log.Printf("локальный порт %s занят другой программой: %v", listenAddr, err)
			return
		}
		if uriLink != "" {
			_ = sendLinkToRunning(uriLink)
		}
		if !background {
			openBrowser("http://" + listenAddr)
		}
		return
	}
	defer listener.Close()

	a := newApp()
	if uriLink != "" {
		if _, err := extractEncodedToken(uriLink); err == nil {
			a.mu.Lock()
			previousConfig := a.cfg
			a.cfg.Link = uriLink
			a.cfg.OutputMode = "remote"
			saveErr := a.saveConfigLocked()
			if saveErr != nil {
				a.cfg = previousConfig
			} else {
				a.configLoadError = false
			}
			a.mu.Unlock()
			if saveErr != nil {
				a.appendLog("Не удалось сохранить ссылку подключения: " + saveErr.Error())
			}
		}
	}
	log.Printf("%s %s", appName, appVersion)
	if err := syncStartupRegistration(a.cfg.LaunchWithWindows); err != nil {
		a.appendLog("Автозапуск Windows: " + err.Error())
	}
	if err := syncURLProtocolRegistration(); err != nil {
		a.appendLog("Ссылка запуска с сайта: " + err.Error())
	}
	startResumeWatcher(a)
	if a.cfg.OverlayEnabled {
		a.setOverlayStatus(false, "Запись экрана не ведётся")
	}
	if !background {
		go func() { time.Sleep(350 * time.Millisecond); openBrowser("http://" + listenAddr) }()
	}
	if a.cfg.AutoStart && !a.configLoadError {
		_ = a.start()
	}
	go a.getEncoderCapabilities(false)
	startRemoteControl(a)

	srv := &http.Server{Handler: a.routes(), ReadHeaderTimeout: 5 * time.Second}
	log.Printf("Web UI: http://%s", listenAddr)
	if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

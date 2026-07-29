package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEmbeddedAdminPassword(t *testing.T) {
	a := &app{adminTokens: make(map[string]time.Time)}
	if !a.adminPasswordConfigured() {
		t.Fatal("embedded administrator password is not configured")
	}
	if _, err := a.verifyAdminPassword("wrong-password"); err == nil {
		t.Fatal("wrong password must be rejected")
	}
	token, err := a.verifyAdminPassword("LinkVideoM2.0")
	if err != nil {
		t.Fatalf("embedded password was rejected: %v", err)
	}
	if token == "" {
		t.Fatal("empty administrator token")
	}
	if _, err := a.setupAdminPassword("another-password"); err == nil {
		t.Fatal("client must not be able to replace the managed password")
	}
}

func TestStartAndRestartDoNotRequireAdminPassword(t *testing.T) {
	tmp := t.TempDir()
	a := &app{
		cfg:          defaultConfig(),
		cfgPath:      filepath.Join(tmp, "config.json"),
		logsDir:      tmp,
		logPath:      filepath.Join(tmp, "monitor.log"),
		adminTokens:  make(map[string]time.Time),
		lastExitCode: -1,
	}
	h := a.routes()
	for _, path := range []string{"/api/start", "/api/restart"} {
		req := httptest.NewRequest(http.MethodPost, "http://localhost:8098"+path, nil)
		req.RemoteAddr = "127.0.0.1:34000"
		req.Host = "localhost:8098"
		req.Header.Set("X-LinkVideo-Request", "1")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code == http.StatusUnauthorized {
			t.Fatalf("%s unexpectedly requires the administrator password: %s", path, rr.Body.String())
		}
	}
}

func TestStopStillRequiresAdminPassword(t *testing.T) {
	a := &app{cfg: defaultConfig(), adminTokens: make(map[string]time.Time)}
	req := httptest.NewRequest(http.MethodPost, "http://localhost:8098/api/stop", nil)
	req.RemoteAddr = "127.0.0.1:34000"
	req.Host = "localhost:8098"
	req.Header.Set("X-LinkVideo-Request", "1")
	rr := httptest.NewRecorder()
	a.routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("stop status=%d, want %d; body=%s", rr.Code, http.StatusUnauthorized, rr.Body.String())
	}
}

func TestProtectedConfigChangesMatchRequestedPolicy(t *testing.T) {
	base := defaultConfig()

	ordinary := []struct {
		name string
		edit func(*Config)
	}{
		{"monitor", func(c *Config) { c.MonitorNumber = 2 }},
		{"cursor", func(c *Config) { c.Cursor = !c.Cursor }},
		{"privacy", func(c *Config) { c.PrivacyProtection = !c.PrivacyProtection }},
		{"system audio", func(c *Config) { c.AudioEnabled = !c.AudioEnabled }},
		{"microphone", func(c *Config) { c.MicrophoneEnabled = !c.MicrophoneEnabled }},
		{"microphone device", func(c *Config) { c.MicrophoneDevice = "Test microphone" }},
		{"enable indicator", func(c *Config) { c.OverlayEnabled = true }},
		{"codec", func(c *Config) { c.Codec = "h265"; c.Encoder = "libx265" }},
	}
	for _, tc := range ordinary {
		next := base
		if tc.name == "enable indicator" {
			next.OverlayEnabled = false
		}
		tc.edit(&next)
		if protectedConfigChange(base, next) {
			t.Fatalf("%s must not require administrator password", tc.name)
		}
	}

	protected := []struct {
		name string
		edit func(*Config)
	}{
		{"fps", func(c *Config) { c.FPS = 25 }},
		{"bitrate", func(c *Config) { c.BitrateKbps = 512; c.MaxrateKbps = 512; c.BufsizeKbps = 1024 }},
		{"quality profile", func(c *Config) { c.QualityProfile = "fast" }},
		{"windows autostart", func(c *Config) { c.LaunchWithWindows = !c.LaunchWithWindows }},
		{"disable indicator", func(c *Config) { c.OverlayEnabled = false }},
	}
	for _, tc := range protected {
		next := base
		tc.edit(&next)
		if !protectedConfigChange(base, next) {
			t.Fatalf("%s must require administrator password", tc.name)
		}
	}
}

func TestSelectivePasswordUIWording(t *testing.T) {
	for _, want := range []string{
		"hasProtectedChanges()",
		"Изменение настроек видео, автозапуска Windows и отключение индикатора",
		"passwordInput.value=''",
	} {
		if !strings.Contains(indexHTML, want) {
			t.Fatalf("selective password UI is missing %q", want)
		}
	}
	for _, forbidden := range []string{"Для '+codecLabel+' доступно: '", "автоматически смешиваются", "Системный звук и микрофон передаются в одной аудиодорожке", "Порт 8554 свободен"} {
		if strings.Contains(indexHTML, forbidden) {
			t.Fatalf("unnecessary permanent UI hint is still present: %q", forbidden)
		}
	}
}

func TestLocalPortCheckRejectsOccupiedPort(t *testing.T) {
	ln, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	a := &app{cfg: defaultConfig()}
	result := a.checkLocalRTSPPort(port)
	if result.Available || !strings.Contains(result.Message, "уже занят") {
		t.Fatalf("occupied port was not rejected: %+v", result)
	}
}

func TestReconnectHistoryPersistsAllReasons(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reconnect-history.json")
	a := &app{restartHistoryPath: path}

	a.mu.Lock()
	items := a.appendReconnectEventLocked("сервер закрыл RTSP-соединение", 32, time.Date(2026, 7, 28, 12, 0, 0, 0, time.Local))
	items = a.appendReconnectEventLocked("сеть временно недоступна", 1, time.Date(2026, 7, 28, 12, 5, 0, 0, time.Local))
	a.mu.Unlock()
	a.saveReconnectHistory(items)

	loaded := &app{restartHistoryPath: path}
	loaded.loadReconnectHistory()
	if len(loaded.restartHistory) != 2 {
		t.Fatalf("history length=%d", len(loaded.restartHistory))
	}
	if loaded.restartHistory[0].Reason != "сервер закрыл RTSP-соединение" || loaded.lastRestartReason != "сеть временно недоступна" {
		t.Fatalf("unexpected history: %+v", loaded.restartHistory)
	}
}

func TestWindowsWrappedFFmpegExitCodeIsReadable(t *testing.T) {
	if got := normalizeProcessExitCode(4294967264); got != -32 {
		t.Fatalf("wrapped FFmpeg exit code=%d, want -32", got)
	}
	if got := normalizeProcessExitCode(1); got != 1 {
		t.Fatalf("ordinary exit code changed to %d", got)
	}
}

func TestH265SoftwareEncoderCommand(t *testing.T) {
	cfg := defaultConfig()
	cfg.OutputMode = "local"
	cfg.Codec = "h265"
	cfg.Encoder = "libx265"
	_, args, _, err := buildEncoderFFmpegDetailed(cfg, capturePlan{OutputWidth: 1920, OutputHeight: 1080}, "libx265", "", "")
	if err != nil {
		t.Fatal(err)
	}
	joined := quoteCommand(args)
	for _, want := range []string{"-c:v libx265", "keyint=30", "min-keyint=30", "repeat-headers=1"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("H.265 command does not contain %q: %s", want, joined)
		}
	}
}

func TestSystemAudioAndMicrophoneAreMixed(t *testing.T) {
	cfg := defaultConfig()
	cfg.OutputMode = "local"
	cfg.AudioEnabled = true
	cfg.MicrophoneEnabled = true
	cfg.MicrophoneDevice = "Test Microphone"
	_, args, _, err := buildEncoderFFmpegDetailed(cfg, capturePlan{OutputWidth: 1280, OutputHeight: 720}, "libx264", "tcp://127.0.0.1:12345", "tcp://127.0.0.1:12346")
	if err != nil {
		t.Fatal(err)
	}
	joined := quoteCommand(args)
	for _, want := range []string{"tcp://127.0.0.1:12346", "amix=inputs=2", "-c:a aac", "-map [aout]"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("mixed audio command does not contain %q: %s", want, joined)
		}
	}
}

func TestMicrophoneWorksWithoutSystemAudio(t *testing.T) {
	cfg := defaultConfig()
	cfg.OutputMode = "local"
	cfg.AudioEnabled = false
	cfg.MicrophoneEnabled = true
	cfg.MicrophoneDevice = "Test Microphone"
	_, args, _, err := buildEncoderFFmpegDetailed(cfg, capturePlan{OutputWidth: 1280, OutputHeight: 720}, "libx264", "", "tcp://127.0.0.1:12346")
	if err != nil {
		t.Fatal(err)
	}
	joined := quoteCommand(args)
	for _, want := range []string{"tcp://127.0.0.1:12346", "[amicrophone]anull[aout]", "-c:a aac", "-map [aout]"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("microphone-only command does not contain %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "amix=inputs=2") || strings.Contains(joined, "tcp://127.0.0.1:12345") {
		t.Fatalf("microphone-only command unexpectedly contains system audio mixing: %s", joined)
	}
}

func TestBusinessProductUIControls(t *testing.T) {
	for _, want := range []string{
		`<option value="rtsp">RTSP</option>`,
		`<option value="30">30 кадров/с</option>`,
		`<option value="h265">H.265</option>`,
		`id="microphone_enabled"`,
		`id="microphone_device"`,
		`data-microphone-mode="voice"`,
		`data-microphone-mode="push_to_talk"`,
		`id="microphone_ptt_hotkey"`,
		`id="microphone_toggle_hotkey"`,
		`Микрофон активен`,
		`id="startStreamButton"`,
		`Для изменения положения зажмите надпись и перетащите её мышью.`,
		`restoreProtectedFields`,
		`Скопировать журнал`,
		`История переподключений`,
		`Изменение настроек видео, автозапуска Windows и отключение индикатора`,
		`Закрепите IP-адрес этого компьютера`,
		`соединение закрыто сервером`,
		`Сервер '+server+' доступен`,
		`id="saveAllButton" class="btn" type="button" disabled`,
	} {
		if !strings.Contains(indexHTML, want) {
			t.Fatalf("business UI element is missing: %s", want)
		}
	}
	for _, forbidden := range []string{"RTSP / TCP", "Сохранить журнал в файл", "видеоплеера", "автоматически смешиваются", "Системный звук и микрофон передаются в одной аудиодорожке", "Для '+codecLabel+' доступно: '"} {
		if strings.Contains(indexHTML, forbidden) {
			t.Fatalf("obsolete UI text is still visible: %s", forbidden)
		}
	}
}

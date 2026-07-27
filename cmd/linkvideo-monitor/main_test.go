package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func encodeBase58ForTest(data []byte) string {
	zeros := 0
	for zeros < len(data) && data[zeros] == 0 {
		zeros++
	}
	digits := []byte{0}
	for _, b := range data {
		carry := int(b)
		for i := len(digits) - 1; i >= 0; i-- {
			n := int(digits[i])*256 + carry
			digits[i] = byte(n % 58)
			carry = n / 58
		}
		for carry > 0 {
			digits = append([]byte{byte(carry % 58)}, digits...)
			carry /= 58
		}
	}
	i := 0
	for i < len(digits)-1 && digits[i] == 0 {
		i++
	}
	out := make([]byte, zeros+len(digits)-i)
	for j := 0; j < zeros; j++ {
		out[j] = '1'
	}
	for j, d := range digits[i:] {
		out[zeros+j] = base58Alphabet[d]
	}
	return string(out)
}

func makeEncodedLink(t *testing.T) string {
	t.Helper()
	p := encodedLink{
		RtmpURL:      "rtmp://node.video.goodline.info:1935/live/secret-key",
		SerialNumber: "TEST-001",
		Hash:         json.RawMessage(`"opaque-server-signature"`),
	}
	full, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return "https://example/link/" + encodeBase58ForTest(full)
}

func TestResolveLinkVideoMonitorScheme(t *testing.T) {
	encodedURL := makeEncodedLink(t)
	token := encodedURL[strings.LastIndex(encodedURL, "/")+1:]

	for _, input := range []string{
		"linkvideomonitor:" + token,
		"LINKVIDEOMONITOR:" + token,
		"linkvideomonitor://" + token,
		"linkvideomonitor:///" + token,
		"linkvideomonitor://open/" + token,
		"linkvideomonitor://open/" + token + "/",
		"linkvideomonitor://camera/connect/" + token + "?source=web",
		"  linkvideomonitor:" + token + "  ",
	} {
		got, serial, err := resolvePublishURL(input, "rtsp")
		if err != nil {
			t.Fatalf("input %q: %v", input, err)
		}
		if got != "rtsp://node.video.goodline.info:554/live/secret-key" {
			t.Fatalf("input %q: got %q", input, got)
		}
		if serial != "TEST-001" {
			t.Fatalf("input %q: serial=%q", input, serial)
		}
	}
}

func TestResolveEncodedLinkRTSP(t *testing.T) {
	got, serial, err := resolvePublishURL(makeEncodedLink(t), "rtsp")
	if err != nil {
		t.Fatal(err)
	}
	want := "rtsp://node.video.goodline.info:554/live/secret-key"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if serial != "TEST-001" {
		t.Fatalf("serial=%q", serial)
	}
}

func TestOpaqueHashAccepted(t *testing.T) {
	p := encodedLink{
		RtmpURL:      "rtmp://node.video.goodline.info:1935/live/key",
		SerialNumber: "X",
		Hash:         json.RawMessage(`"not-a-crc32"`),
	}
	b, _ := json.Marshal(p)
	got, serial, err := resolvePublishURL(encodeBase58ForTest(b), "rtsp")
	if err != nil {
		t.Fatal(err)
	}
	if got != "rtsp://node.video.goodline.info:554/live/key" || serial != "X" {
		t.Fatalf("got=%q serial=%q", got, serial)
	}
}

func TestDirectURLConversion(t *testing.T) {
	got, _, err := resolvePublishURL("rtsp://host.local:554/live/key", "rtmp")
	if err != nil {
		t.Fatal(err)
	}
	if got != "rtmp://host.local:1935/live/key" {
		t.Fatalf("got %q", got)
	}
}

func TestRedaction(t *testing.T) {
	got := redactURL("rtsp://node.video.goodline.info:554/live/secret-key?token=x")
	if got != "rtsp://node.video.goodline.info:554/live/%2A%2A%2A" && got != "rtsp://node.video.goodline.info:554/live/***" {
		t.Fatalf("redaction=%q", got)
	}
}

func TestConfigAcceptsNumericStrings(t *testing.T) {
	var cfg Config
	err := json.Unmarshal([]byte(`{"monitor_index":"2","fps":"25","local_rtsp_port":"8554","audio_channels":"1"}`), &cfg)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MonitorIndex != 2 || cfg.FPS != 25 || cfg.LocalRTSPPort != 8554 || cfg.AudioChannels != 1 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestBuildLocalRTSPWithAAC(t *testing.T) {
	cfg := defaultConfig()
	cfg.OutputMode = "local"
	cfg.LocalRTSPPath = "office-screen"
	cfg.AudioEnabled = true
	cfg.AudioSource = "system"
	dest, args, err := buildFFmpeg(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if dest != "rtsp://127.0.0.1:8554/screen" {
		t.Fatalf("dest=%q", dest)
	}
	joined := quoteCommand(args)
	for _, part := range []string{"-thread_queue_size 8", "-f s16le", "-i tcp://127.0.0.1:12345", "-f rawvideo", "-pixel_format bgra", "-i pipe:0", "-c:a aac", "-profile:a aac_low", "asetpts=N/SR/TB", "-r 15", "-fps_mode cfr", "-f rtsp"} {
		if !strings.Contains(joined, part) {
			t.Fatalf("command does not contain %q: %s", part, joined)
		}
	}
	if strings.Contains(joined, "-shortest") || strings.Contains(joined, "use_wallclock_as_timestamps") || strings.Contains(joined, "Non-monotonic") {
		t.Fatalf("continuous system audio must use a sample-count clock: %s", joined)
	}
}

func TestCursorAndProfilesAffectCommand(t *testing.T) {
	cfg := defaultConfig()
	cfg.OutputMode = "local"
	cfg.Cursor = false
	cfg.QualityProfile = "medium"
	cfg.FPS = 25
	plan, err := resolveCapturePlan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	captureArgs, err := buildDXGICaptureArgs(cfg, plan)
	if err != nil {
		t.Fatal(err)
	}
	captureCommand := quoteCommand(captureArgs)
	for _, part := range []string{"ddagrab=", "draw_mouse=0", "framerate=25", "-f rawvideo", "pipe:1"} {
		if !strings.Contains(captureCommand, part) {
			t.Fatalf("capture command does not contain %q: %s", part, captureCommand)
		}
	}
	if strings.Contains(captureCommand, "dup_frames") || strings.Contains(captureCommand, "gdigrab") {
		t.Fatalf("unexpected legacy capture option: %s", captureCommand)
	}

	_, args, err := buildFFmpeg(cfg)
	if err != nil {
		t.Fatal(err)
	}
	joined := quoteCommand(args)
	for _, part := range []string{"-f rawvideo", "-b:v 1024k", "-minrate 1024k", "-maxrate 1024k"} {
		if !strings.Contains(joined, part) {
			t.Fatalf("encoder command does not contain %q: %s", part, joined)
		}
	}
}

func TestFixedTwoSecondGOP(t *testing.T) {
	for _, fps := range []int{10, 15, 20, 25} {
		cfg := defaultConfig()
		cfg.OutputMode = "local"
		cfg.FPS = fps
		_, args, err := buildFFmpeg(cfg)
		if err != nil {
			t.Fatal(err)
		}
		joined := quoteCommand(args)
		gop := fps * 2
		for _, want := range []string{
			"-g " + strconv.Itoa(gop),
			"keyint=" + strconv.Itoa(gop),
			"min-keyint=" + strconv.Itoa(gop),
			"scenecut=0",
			"repeat-headers=1",
			"aud=1",
		} {
			if !strings.Contains(joined, want) {
				t.Fatalf("fps=%d command does not contain %q: %s", fps, want, joined)
			}
		}
	}
}

func TestGOPIsNotExposedAsUserSetting(t *testing.T) {
	lower := strings.ToLower(indexHTML)
	if strings.Contains(lower, `id="gop`) || strings.Contains(lower, `name="gop`) || strings.Contains(lower, `интервал ключевых кадров`) {
		t.Fatal("GOP must remain an internal fixed setting")
	}
}

func TestSanitizeLogTextRemovesPublishKey(t *testing.T) {
	input := "Output #0, rtsp, to 'rtsp://node.video.goodline.info:554/vcrf?publishsign=secret/linkvideo_123_main':"
	got := sanitizeLogText(input)
	if strings.Contains(got, "publishsign=secret") || strings.Contains(got, "linkvideo_123_main") {
		t.Fatalf("publish key leaked: %s", got)
	}
	if !strings.Contains(got, "rtsp://node.video.goodline.info:554/") {
		t.Fatalf("host was unexpectedly removed: %s", got)
	}
}

func TestDefaultCursorIsEnabled(t *testing.T) {
	if !defaultConfig().Cursor {
		t.Fatal("cursor must be enabled by default")
	}
}

func TestDefaultFPSAndVideoProfiles(t *testing.T) {
	cfg := defaultConfig()
	if cfg.FPS != 15 {
		t.Fatalf("default fps=%d", cfg.FPS)
	}
	for _, fps := range []int{5, 30, 60} {
		c := cfg
		c.FPS = fps
		normalizeConfig(&c)
		if c.FPS != 15 {
			t.Fatalf("unsupported fps %d normalized to %d", fps, c.FPS)
		}
	}
	c := cfg
	c.QualityProfile = "high"
	c.BitrateKbps = 2000
	normalizeConfig(&c)
	if c.QualityProfile != "medium" || c.BitrateKbps != 1024 {
		t.Fatalf("removed high profile was not migrated: %+v", c)
	}
}

func TestDefaultAudioIsDisabled(t *testing.T) {
	if defaultConfig().AudioEnabled {
		t.Fatal("audio must be disabled by default")
	}
	if defaultConfig().AudioQuality != "medium" || defaultConfig().AudioBitrateKbps != 128 {
		t.Fatalf("unexpected default audio profile: %+v", defaultConfig())
	}
}

func TestAudioQualityProfilesAffectCommand(t *testing.T) {
	for name, bitrate := range map[string]int{"low": 64, "medium": 128, "high": 192} {
		cfg := defaultConfig()
		cfg.OutputMode = "local"
		cfg.AudioEnabled = true
		cfg.AudioQuality = name
		_, args, err := buildFFmpeg(cfg)
		if err != nil {
			t.Fatal(err)
		}
		joined := quoteCommand(args)
		want := "-b:a " + strconv.Itoa(bitrate) + "k"
		if !strings.Contains(joined, want) {
			t.Fatalf("profile %s does not contain %q: %s", name, want, joined)
		}
	}
}

func TestOldConfigTurnsAudioOffOnMigration(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{"audio_enabled":true,"audio_bitrate_kbps":192}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.AudioEnabled {
		t.Fatal("old beta config must migrate to audio disabled")
	}
	if cfg.AudioQuality != "high" {
		t.Fatalf("audio quality=%q", cfg.AudioQuality)
	}
}

func TestDefaultProductOptions(t *testing.T) {
	cfg := defaultConfig()
	if !cfg.Cursor || !cfg.OverlayEnabled || !cfg.PrivacyProtection {
		t.Fatalf("cursor and overlay must be enabled by default: %+v", cfg)
	}
	if cfg.QualityProfile != "medium" || cfg.BitrateKbps != 1024 {
		t.Fatalf("unexpected default video quality: %+v", cfg)
	}
	if cfg.ResolutionProfile != "original" {
		t.Fatalf("unexpected default resolution: %+v", cfg)
	}
}

func TestLegacyDefaultsMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	old := `{"capture_mode":"window","cursor":false,"overlay_enabled":false,"quality_profile":"economy","bitrate_kbps":256}`
	if err := os.WriteFile(path, []byte(old), 0644); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: defaultConfig(), cfgPath: path, logsDir: dir, logPath: filepath.Join(dir, "test.log")}
	if err := a.loadConfig(); err != nil {
		t.Fatal(err)
	}
	if a.cfg.CaptureMode != "full" || !a.cfg.Cursor || !a.cfg.OverlayEnabled {
		t.Fatalf("legacy migration failed: %+v", a.cfg)
	}
	if a.cfg.QualityProfile != "medium" || a.cfg.BitrateKbps != 1024 || a.cfg.DefaultsRevision != 7 || a.cfg.Encoder != "auto" || a.cfg.CaptureBackend != "auto" {
		t.Fatalf("legacy defaults were not updated: %+v", a.cfg)
	}
}

func TestWindowCaptureIsNoLongerAccepted(t *testing.T) {
	cfg := defaultConfig()
	cfg.CaptureMode = "window"
	normalizeConfig(&cfg)
	if cfg.CaptureMode != "full" {
		t.Fatalf("capture mode=%q", cfg.CaptureMode)
	}
}

func TestMediaMTXConfigUsesSelectedPortAndPath(t *testing.T) {
	cfg := defaultConfig()
	cfg.LocalRTSPPort = 9554
	cfg.LocalRTSPPath = "office screen"
	got := string(mediaMTXConfig(cfg))
	for _, want := range []string{"rtspAddress: :9554", `"office-screen":`, "source: publisher"} {
		if !strings.Contains(got, want) {
			t.Fatalf("MediaMTX config does not contain %q:\n%s", want, got)
		}
	}
}

func TestLocalRTSPPathIsAlwaysScreen(t *testing.T) {
	cfg := defaultConfig()
	cfg.LocalRTSPPath = "custom-name"
	normalizeConfig(&cfg)
	if cfg.LocalRTSPPath != "screen" {
		t.Fatalf("local RTSP path=%q", cfg.LocalRTSPPath)
	}
}

func TestInterfaceUsesSingleGlobalSaveButton(t *testing.T) {
	if count := strings.Count(indexHTML, `id="saveAllButton"`); count != 1 {
		t.Fatalf("global save button count=%d", count)
	}
	if strings.Contains(indexHTML, `onclick="saveSection(`) || strings.Contains(indexHTML, `class="save"`) {
		t.Fatal("section save buttons must not be present")
	}
	if !strings.Contains(indexHTML, `onclick="saveAllSettings()"`) {
		t.Fatal("global save handler is missing")
	}
}

func TestInterfaceHasOnlySupportedFPSAndBitrates(t *testing.T) {
	for _, want := range []string{`<option value="10">10 кадров/с</option>`, `<option value="15">15 кадров/с</option>`, `<option value="20">20 кадров/с</option>`, `<option value="25">25 кадров/с</option>`} {
		if !strings.Contains(indexHTML, want) {
			t.Fatalf("FPS option is missing: %s", want)
		}
	}
	if strings.Contains(indexHTML, `value="30"`) || strings.Contains(indexHTML, `2000 Кбит/с`) || strings.Contains(indexHTML, `data-profile="high"`) {
		t.Fatal("removed FPS or bitrate profile is still visible")
	}
}

func TestInterfaceWordingAndFavicon(t *testing.T) {
	for _, want := range []string{`href="/favicon.ico"`, `Передача трансляции в облако.`, `Готовая ссылка для подключения в LinkVideo Server.`, `<small>1920×1080</small>`, `<small>1280×720</small>`} {
		if !strings.Contains(indexHTML, want) {
			t.Fatalf("interface text is missing: %s", want)
		}
	}
	for _, forbidden := range []string{`VLC`, `Ссылка на этом компьютере`, `Ссылка в локальной сети`, `Имя потока`, `Передача изображения и звука компьютера в LinkVideo.`} {
		if strings.Contains(indexHTML, forbidden) {
			t.Fatalf("obsolete interface text is still present: %s", forbidden)
		}
	}
}

func TestBrandUsesSameTypographyForMonitor(t *testing.T) {
	if !strings.Contains(indexHTML, `<span>LinkVideo</span> <span class="brand-product">Monitor</span>`) {
		t.Fatal("brand text is not rendered with the unified typography")
	}
	if strings.Contains(indexHTML, `<small>MONITOR</small>`) {
		t.Fatal("legacy small MONITOR label is still present")
	}
}

func TestSaveAndRestartButtonsAreInFooterAndRTSPLinkIsSingle(t *testing.T) {
	if !strings.Contains(indexHTML, `<div class="bottomactions"><button id="saveAllButton"`) {
		t.Fatal("save button must be in the fixed footer")
	}
	if !strings.Contains(indexHTML, `onclick="restartStream()">Перезапустить поток</button>`) {
		t.Fatal("restart button must be in the fixed footer")
	}
	if strings.Contains(indexHTML, `sidebarRTSPURL`) || strings.Contains(indexHTML, `localURL`) || strings.Contains(indexHTML, `lanURL`) {
		t.Fatal("duplicate RTSP link fields must not be present")
	}
	if !strings.Contains(indexHTML, `id="localRTSPURL"`) {
		t.Fatal("single local RTSP link field is missing")
	}
}

func TestStandardProfileName(t *testing.T) {
	if !strings.Contains(indexHTML, `<b>Стандартный</b><small>1024 Кбит/с</small>`) {
		t.Fatal("1024 Kbit profile must be named Standard")
	}
	if strings.Contains(indexHTML, `<b>Средний</b><small>1024 Кбит/с</small>`) {
		t.Fatal("legacy Medium name is still visible for 1024 Kbit profile")
	}
}

func TestRemoteSyncUsesConnectionLinkAndAppliesSettings(t *testing.T) {
	var received remoteSyncRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method=%s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"revision":7,"settings":{"fps":15,"bitrate_kbps":512,"protocol":"rtmp","audio_enabled":true}}`)
	}))
	defer server.Close()

	tmp := t.TempDir()
	cfg := defaultConfig()
	cfg.Link = "linkvideomonitor:TEST-LINK"
	cfg.RemoteControlEnabled = true
	cfg.RemoteAPIURL = server.URL
	a := &app{
		cfg:          cfg,
		cfgPath:      filepath.Join(tmp, "config.json"),
		logsDir:      tmp,
		logPath:      filepath.Join(tmp, "test.log"),
		lastExitCode: -1,
		remoteWake:   make(chan struct{}, 1),
	}
	if err := a.syncRemoteOnce(); err != nil {
		t.Fatal(err)
	}
	if received.ConnectionLink != cfg.Link {
		t.Fatalf("connection_link=%q", received.ConnectionLink)
	}
	if received.APIVersion != remoteAPIVersion || !received.State.ProcessRunning {
		t.Fatalf("unexpected request: %+v", received)
	}
	if a.cfg.FPS != 15 || a.cfg.BitrateKbps != 512 || a.cfg.QualityProfile != "fast" {
		t.Fatalf("video settings not applied: %+v", a.cfg)
	}
	if !a.cfg.Cursor || !a.cfg.AudioEnabled || a.cfg.Protocol != "rtmp" {
		t.Fatalf("protocol/audio settings not applied or unrelated settings changed: %+v", a.cfg)
	}
	if a.cfg.RemoteRevision != 7 {
		t.Fatalf("revision=%d", a.cfg.RemoteRevision)
	}
}

func TestRemoteSyncDoesNotChangeConnectionLink(t *testing.T) {
	cfg := defaultConfig()
	cfg.Link = "linkvideomonitor:ORIGINAL"
	a := &app{cfg: cfg, cfgPath: filepath.Join(t.TempDir(), "config.json")}
	fps := 20
	resp := remoteSyncResponse{Revision: 2, Settings: &remoteSettings{FPS: &fps}}
	if err := a.applyRemoteResponse(resp); err != nil {
		t.Fatal(err)
	}
	if a.cfg.Link != "linkvideomonitor:ORIGINAL" {
		t.Fatalf("link was changed: %q", a.cfg.Link)
	}
}

func TestRemoteCommandIDIsRequiredAndAcceptedAsNumber(t *testing.T) {
	if got := commandIDString(json.RawMessage(`125`)); got != "125" {
		t.Fatalf("numeric command id=%q", got)
	}
	cfg := defaultConfig()
	a := &app{cfg: cfg, cfgPath: filepath.Join(t.TempDir(), "config.json")}
	resp := remoteSyncResponse{Command: &remoteCommand{Action: "restart_stream"}}
	if err := a.applyRemoteResponse(resp); err == nil {
		t.Fatal("command without id must be rejected")
	}
}

func TestAutomaticStreamAndResumeAreMandatoryAndHidden(t *testing.T) {
	cfg := defaultConfig()
	cfg.AutoStart = false
	cfg.RestartAfterResume = false
	normalizeConfig(&cfg)
	if !cfg.AutoStart || !cfg.RestartAfterResume {
		t.Fatalf("mandatory automatic behavior is disabled: %+v", cfg)
	}
	for _, forbidden := range []string{`id="auto_start"`, `id="restart_after_resume"`, "Автоматически начинать трансляцию", "Восстанавливать трансляцию после сна", "фактическая частота кадров"} {
		if strings.Contains(indexHTML, forbidden) {
			t.Fatalf("removed UI element is still visible: %q", forbidden)
		}
	}
	if !strings.Contains(indexHTML, "Время работы") {
		t.Fatal("continuous stream uptime is not shown in status")
	}
}

func TestRemoteAPIIsLimitedToRequiredSettings(t *testing.T) {
	encoded, err := json.Marshal(remoteCurrentSettings{Protocol: "rtsp", FPS: 10, BitrateKbps: 1024, AudioEnabled: false})
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, want := range []string{`"protocol"`, `"fps"`, `"bitrate_kbps"`, `"audio_enabled"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("required API field is missing: %s", want)
		}
	}
	for _, forbidden := range []string{`monitor_index`, `show_cursor`, `overlay_enabled`, `auto_start`, `restart_after_resume`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("unnecessary API field is still sent: %s", forbidden)
		}
	}
}

func TestRemoteControlIsAutomaticAndHiddenFromUI(t *testing.T) {
	for _, forbidden := range []string{
		"Дистанционное управление",
		"Получать настройки из админки",
		`id="remote_api_url"`,
		`id="remote_control_enabled"`,
		"Проверить синхронизацию",
	} {
		if strings.Contains(indexHTML, forbidden) {
			t.Fatalf("remote control UI must be hidden: %q", forbidden)
		}
	}
	if !strings.Contains(indexHTML, `<h2>Журнал работы</h2>`) {
		t.Fatal("work log section is missing")
	}

	old := defaultRemoteAPIURL
	defaultRemoteAPIURL = "https://admin.example/api/monitor/sync"
	defer func() { defaultRemoteAPIURL = old }()
	cfg := defaultConfig()
	normalizeConfig(&cfg)
	if !cfg.RemoteControlEnabled || cfg.RemoteAPIURL == "" || cfg.RemoteSyncIntervalMin != 5 {
		t.Fatalf("automatic remote control is not configured: %+v", cfg)
	}
}

func TestResolutionProfilesAreVisible(t *testing.T) {
	for _, want := range []string{
		`data-resolution="original"`,
		`data-resolution="full_hd"`,
		`data-resolution="hd"`,
		`<small>1920×1080</small>`,
		`<small>1280×720</small>`,
	} {
		if !strings.Contains(indexHTML, want) {
			t.Fatalf("resolution option is missing: %s", want)
		}
	}
}

func TestResolutionScalingFilter(t *testing.T) {
	cfg := defaultConfig()
	cfg.CaptureMode = "monitor"
	plan, err := resolveCapturePlan(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for profile, want := range map[string]string{
		"original": "",
		"full_hd":  "min(iw,1920)",
		"hd":       "min(iw,1280)",
	} {
		cfg.ResolutionProfile = profile
		filter, err := buildDesktopCaptureFilter(cfg, plan)
		if err != nil {
			t.Fatal(err)
		}
		if want == "" {
			if strings.Contains(filter, "scale=") {
				t.Fatalf("original resolution unexpectedly scales: %s", filter)
			}
		} else if !strings.Contains(filter, want) || !strings.Contains(filter, "force_divisible_by=2") {
			t.Fatalf("profile %s has wrong filter: %s", profile, filter)
		}
	}
}

func TestInvalidResolutionFallsBackToOriginal(t *testing.T) {
	cfg := defaultConfig()
	cfg.ResolutionProfile = "4k"
	normalizeConfig(&cfg)
	if cfg.ResolutionProfile != "original" {
		t.Fatalf("resolution profile=%q", cfg.ResolutionProfile)
	}
}

func TestOddMultiMonitorDimensionsAreNormalizedForH264(t *testing.T) {
	plan := normalizeCapturePlanDimensions(capturePlan{
		Mode: "full", Width: 3840, Height: 1081,
		OutputWidth: 3840, OutputHeight: 1081,
	})
	if plan.OutputWidth != 3840 || plan.OutputHeight != 1080 {
		t.Fatalf("odd frame was not normalized: %+v", plan)
	}
	if err := validateCapturePlanDimensions(plan); err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig()
	cfg.OutputMode = "local"
	_, args, _, err := buildEncoderFFmpegDetailed(cfg, plan, "libx264", "")
	if err != nil {
		t.Fatal(err)
	}
	joined := quoteCommand(args)
	if !strings.Contains(joined, "-video_size 3840x1080") {
		t.Fatalf("encoder did not receive an even frame size: %s", joined)
	}
	if !strings.Contains(joined, "scale=trunc(iw/2)*2:trunc(ih/2)*2") {
		t.Fatalf("final even-dimension guard is missing: %s", joined)
	}
}

func TestVideoEncoderCanBeSelectedManuallyInUI(t *testing.T) {
	for _, want := range []string{
		`<select id="encoder">`,
		`value="auto">Автоматически`,
		`value="h264_qsv">Intel Quick Sync`,
		`value="h264_nvenc">NVIDIA NVENC`,
		`value="h264_amf">AMD AMF`,
		`value="libx264">Программный H.264`,
	} {
		if !strings.Contains(indexHTML, want) {
			t.Fatalf("manual encoder UI is missing %q", want)
		}
	}
	if strings.Contains(indexHTML, `<input id="encoder" type="hidden">`) {
		t.Fatal("encoder must not remain hidden")
	}
}

func TestPrivacyPixelationChangesOnlyProtectedArea(t *testing.T) {
	plan := capturePlan{X: 0, Y: 0, Width: 100, Height: 100, OutputWidth: 100, OutputHeight: 100}
	frame := make([]byte, 100*100*4)
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			i := (y*100 + x) * 4
			frame[i], frame[i+1], frame[i+2], frame[i+3] = byte(x), byte(y), byte(x+y), 255
		}
	}
	beforeOutside := append([]byte(nil), frame[(5*100+5)*4:(5*100+5)*4+4]...)
	beforeInside := append([]byte(nil), frame[(50*100+50)*4:(50*100+50)*4+4]...)
	applyPrivacyPixelation(frame, plan, []privacyScreenRect{{Left: 40, Top: 40, Right: 60, Bottom: 60}})
	if got := frame[(5*100+5)*4 : (5*100+5)*4+4]; !bytes.Equal(got, beforeOutside) {
		t.Fatalf("outside pixel changed: %v -> %v", beforeOutside, got)
	}
	if got := frame[(50*100+50)*4 : (50*100+50)*4+4]; bytes.Equal(got, beforeInside) {
		t.Fatalf("protected pixel was not changed: %v", got)
	}
}

func TestLocalOnlyProtectsMutatingEndpoints(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := localOnly(next)

	tests := []struct {
		name       string
		method     string
		path       string
		host       string
		remoteAddr string
		header     string
		want       int
	}{
		{name: "safe local get", method: http.MethodGet, path: "/api/status", host: "127.0.0.1:8098", remoteAddr: "127.0.0.1:50000", want: http.StatusNoContent},
		{name: "mutating get rejected", method: http.MethodGet, path: "/api/stop", host: "127.0.0.1:8098", remoteAddr: "127.0.0.1:50000", want: http.StatusMethodNotAllowed},
		{name: "post without local header rejected", method: http.MethodPost, path: "/api/stop", host: "127.0.0.1:8098", remoteAddr: "127.0.0.1:50000", want: http.StatusForbidden},
		{name: "authorized local post", method: http.MethodPost, path: "/api/stop", host: "localhost:8098", remoteAddr: "127.0.0.1:50000", header: "1", want: http.StatusNoContent},
		{name: "dns rebinding host rejected", method: http.MethodGet, path: "/api/status", host: "attacker.example", remoteAddr: "127.0.0.1:50000", want: http.StatusForbidden},
		{name: "non loopback rejected", method: http.MethodGet, path: "/api/status", host: "127.0.0.1:8098", remoteAddr: "192.0.2.10:50000", want: http.StatusForbidden},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "http://"+tc.host+tc.path, nil)
			req.Host = tc.host
			req.RemoteAddr = tc.remoteAddr
			if tc.header != "" {
				req.Header.Set("X-LinkVideo-Request", tc.header)
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("status=%d, want %d, body=%q", rr.Code, tc.want, rr.Body.String())
			}
			if tc.want == http.StatusNoContent && rr.Header().Get("X-LinkVideo-Monitor") != appVersion {
				t.Fatalf("missing instance identity header")
			}
		})
	}
}

func TestRecordLaunchErrorIgnoresStaleGeneration(t *testing.T) {
	a := &app{
		desired:    true,
		running:    true,
		generation: 8,
		lastError:  "new stream is healthy",
	}
	a.recordLaunchError(7, errors.New("obsolete launch failed"))
	if !a.desired || !a.running {
		t.Fatalf("stale generation changed active stream state: desired=%v running=%v", a.desired, a.running)
	}
	if a.lastError != "new stream is healthy" {
		t.Fatalf("stale generation overwrote last error: %q", a.lastError)
	}
}

func TestFailStartIgnoresStaleGeneration(t *testing.T) {
	a := &app{desired: true, generation: 12, lastError: "new start"}
	if a.failStart(11, errors.New("obsolete preflight failed")) {
		t.Fatal("stale generation was incorrectly applied")
	}
	if !a.desired || a.lastError != "new start" {
		t.Fatalf("stale preflight changed state: desired=%v lastError=%q", a.desired, a.lastError)
	}
}

type shortWriter struct {
	buf bytes.Buffer
	max int
}

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) > w.max {
		p = p[:w.max]
	}
	return w.buf.Write(p)
}

func TestWriteFullHandlesPartialWrites(t *testing.T) {
	w := &shortWriter{max: 3}
	want := []byte("complete raw frame")
	if err := writeFull(w, want); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(w.buf.Bytes(), want) {
		t.Fatalf("got %q want %q", w.buf.Bytes(), want)
	}
}

func TestSaveConfigAtomicallyReplacesExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	a := &app{cfg: defaultConfig(), cfgPath: path}
	if err := a.saveConfigLocked(); err != nil {
		t.Fatal(err)
	}
	a.cfg.FPS = 25
	a.cfg.Link = "linkvideomonitor:test"
	if err := a.saveConfigLocked(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got Config
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("saved config is invalid JSON: %v", err)
	}
	if got.FPS != 25 || got.Link != a.cfg.Link {
		t.Fatalf("saved config is stale: %+v", got)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "config-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary config files remain: %v", matches)
	}
}

func TestUnsupportedRemoteCommandIsNotMarkedProcessed(t *testing.T) {
	dir := t.TempDir()
	a := &app{cfg: defaultConfig(), cfgPath: filepath.Join(dir, "config.json")}
	a.cfg.RemoteLastCommandID = "previous"
	before := a.cfg
	err := a.applyRemoteResponse(remoteSyncResponse{
		Command: &remoteCommand{ID: json.RawMessage(`"bad-1"`), Action: "delete_everything"},
	})
	if err == nil {
		t.Fatal("unsupported remote command was accepted")
	}
	if a.cfg.RemoteLastCommandID != before.RemoteLastCommandID {
		t.Fatalf("unsupported command was marked as processed: %q", a.cfg.RemoteLastCommandID)
	}
}

func TestStartDoesNotOverwriteUnreadableConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	original := []byte(`{"broken":`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: defaultConfig(), cfgPath: path, configLoadError: true}
	if err := a.start(); err == nil {
		t.Fatal("start unexpectedly accepted an unreadable configuration")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("unreadable config was overwritten: %q", got)
	}
}

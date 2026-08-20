from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text(encoding="utf-8")
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected exactly one match, got {count}")
    p.write_text(text.replace(old, new, 1), encoding="utf-8")


# Serialize expensive realtime encoder benchmarks and re-check the cache after
# waiting, so UI probing and stream startup never benchmark the same machine at
# the same time.
replace_once(
    "cmd/linkvideo-monitor/runtime_encoder_fallback.go",
    '''var encoderBenchmarkCache = struct {
\tsync.Mutex
\titems map[string]encoderBenchmarkCacheEntry
}{items: make(map[string]encoderBenchmarkCacheEntry)}
''',
    '''var encoderBenchmarkCache = struct {
\tsync.Mutex
\titems map[string]encoderBenchmarkCacheEntry
}{items: make(map[string]encoderBenchmarkCacheEntry)}

var encoderBenchmarkRunMu sync.Mutex
''',
)
replace_once(
    "cmd/linkvideo-monitor/runtime_encoder_fallback.go",
    '''\tencoderBenchmarkCache.Unlock()

\tactualFPS, err := benchmarkEncoderRealtime(cfg, plan, encoder, candidate)
''',
    '''\tencoderBenchmarkCache.Unlock()

\tencoderBenchmarkRunMu.Lock()
\tdefer encoderBenchmarkRunMu.Unlock()

\t// A second caller can reach this point while another benchmark is running.
\t// Re-check after acquiring the global gate so the same expensive probe is
\t// never executed twice back-to-back.
\tencoderBenchmarkCache.Lock()
\tif item, ok := encoderBenchmarkCache.items[key]; ok && time.Since(item.At) < runtimeBenchmarkCacheTTL {
\t\tencoderBenchmarkCache.Unlock()
\t\tif item.ErrText != "" {
\t\t\treturn item.ActualFPS, fmt.Errorf("%s", item.ErrText)
\t\t}
\t\treturn item.ActualFPS, nil
\t}
\tencoderBenchmarkCache.Unlock()

\tactualFPS, err := benchmarkEncoderRealtime(cfg, plan, encoder, candidate)
''',
)
replace_once(
    "cmd/linkvideo-monitor/runtime_encoder_fallback.go",
    '''\tif isHardwareEncoder(encoder) {
\t\t_, err := cachedEncoderBenchmark(cfg, plan, encoder, runtimePerformanceCandidate{FPS: fps})
\t\treturn err
\t}
''',
    '''\tif isHardwareEncoder(encoder) {
\t\t// Hardware encoders only need a short functional probe in the settings UI.
\t\t// The selected encoder still receives the full realtime benchmark before
\t\t// a stream starts, so capability discovery cannot overload the machine.
\t\treturn probeVideoEncoder(cfg, encoder, plan)
\t}
''',
)

# Prevent a delayed background restart from undoing an explicit Stop pressed by
# the user while the restart goroutine sleeps.
replace_once(
    "cmd/linkvideo-monitor/main.go",
    '''func (a *app) restart() error {
\ta.stop()
\ttime.Sleep(350 * time.Millisecond)
\treturn a.start()
}
''',
    '''func (a *app) restart() error {
\ta.mu.Lock()
\twasDesired := a.desired
\ta.mu.Unlock()
\tif !wasDesired {
\t\treturn nil
\t}
\ta.stop()
\ta.mu.Lock()
\trestartGeneration := a.generation
\ta.mu.Unlock()
\ttime.Sleep(350 * time.Millisecond)
\ta.mu.Lock()
\tinterrupted := a.generation != restartGeneration || a.desired
\ta.mu.Unlock()
\tif interrupted {
\t\treturn nil
\t}
\treturn a.start()
}
''',
)

# Fully validate a LinkVideo URI before persisting it.
replace_once(
    "cmd/linkvideo-monitor/main.go",
    '''\t\tif _, err := extractEncodedToken(body.Link); err != nil {
\t\t\twriteJSON(w, 400, map[string]string{"error": err.Error()})
\t\t\treturn
\t\t}
\t\ta.mu.Lock()
\t\tpreviousConfig := a.cfg
''',
    '''\t\ta.mu.Lock()
\t\tprotocol := a.cfg.Protocol
\t\ta.mu.Unlock()
\t\tif _, _, err := resolvePublishURL(body.Link, protocol); err != nil {
\t\t\twriteJSON(w, 400, map[string]string{"error": err.Error()})
\t\t\treturn
\t\t}
\t\ta.mu.Lock()
\t\tpreviousConfig := a.cfg
''',
)

# Do not expose RTSP/RTMP userinfo in exported logs/commands.
replace_once(
    "cmd/linkvideo-monitor/main.go",
    '''\tu.Path = "/" + strings.Join(parts, "/")
\tu.RawQuery = ""
\treturn u.String()
''',
    '''\tu.Path = "/" + strings.Join(parts, "/")
\tu.RawQuery = ""
\tu.Fragment = ""
\tu.User = nil
\treturn u.String()
''',
)

# Validate protocol-handler input before saving it, and do not start a second
# full encoder capability sweep automatically at process startup.
replace_once(
    "cmd/linkvideo-monitor/main.go",
    '''\tif uriLink != "" {
\t\tif _, err := extractEncodedToken(uriLink); err == nil {
\t\t\ta.mu.Lock()
''',
    '''\tif uriLink != "" {
\t\tif _, _, err := resolvePublishURL(uriLink, a.cfg.Protocol); err == nil {
\t\t\ta.mu.Lock()
''',
)
replace_once(
    "cmd/linkvideo-monitor/main.go",
    '''\tgo a.getEncoderCapabilities(false)
\tstartRemoteControl(a)
''',
    '''\tstartRemoteControl(a)
''',
)

# Forced capability refresh is a side-effecting expensive operation. Keep the
# normal GET used by the UI, but require POST for refresh=1.
replace_once(
    "cmd/linkvideo-monitor/main.go",
    '''\tmux.HandleFunc("/api/encoder-capabilities", func(w http.ResponseWriter, r *http.Request) {
\t\twriteJSON(w, 200, a.getEncoderCapabilities(r.URL.Query().Get("refresh") == "1"))
\t})
''',
    '''\tmux.HandleFunc("/api/encoder-capabilities", func(w http.ResponseWriter, r *http.Request) {
\t\tforce := r.URL.Query().Get("refresh") == "1"
\t\tif force && r.Method != http.MethodPost {
\t\t\twriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "forced refresh requires POST"})
\t\t\treturn
\t\t}
\t\twriteJSON(w, 200, a.getEncoderCapabilities(force))
\t})
''',
)

# Require the custom same-origin header for encoder capability probing even on
# GET; the web UI sends it for all API calls below.
replace_once(
    "cmd/linkvideo-monitor/main.go",
    '''\t\tif r.Method != http.MethodGet && r.Method != http.MethodHead {
\t\t\tif r.Header.Get("X-LinkVideo-Request") != "1" {
\t\t\t\twriteJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden local request"})
\t\t\t\treturn
\t\t\t}
\t\t}
''',
    '''\t\trequireLocalHeader := r.Method != http.MethodGet && r.Method != http.MethodHead
\t\tif r.URL.Path == "/api/encoder-capabilities" {
\t\t\trequireLocalHeader = true
\t\t}
\t\tif requireLocalHeader && r.Header.Get("X-LinkVideo-Request") != "1" {
\t\t\twriteJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden local request"})
\t\t\treturn
\t\t}
''',
)
replace_once(
    "cmd/linkvideo-monitor/webui.go",
    '''async function api(path,opt={}){opt={...opt};const method=(opt.method||'GET').toUpperCase();opt.headers={...(opt.headers||{})};if(method!=='GET'&&method!=='HEAD')opt.headers['X-LinkVideo-Request']='1';if(adminToken)opt.headers['X-LinkVideo-Admin-Token']=adminToken;const r=await fetch(path,opt);const type=r.headers.get('content-type')||'';const j=type.includes('json')?await r.json().catch(()=>({})):{};if(!r.ok)throw Error(j.error||('HTTP '+r.status));return j}
''',
    '''async function api(path,opt={}){opt={...opt};const method=(opt.method||'GET').toUpperCase();opt.headers={...(opt.headers||{})};opt.headers['X-LinkVideo-Request']='1';if(adminToken)opt.headers['X-LinkVideo-Admin-Token']=adminToken;const r=await fetch(path,opt);const type=r.headers.get('content-type')||'';const j=type.includes('json')?await r.json().catch(()=>({})):{};if(!r.ok)throw Error(j.error||('HTTP '+r.status));return j}
''',
)

# Remote control may send the connection link and build-time bearer key; never
# allow that over cleartext except for an explicit loopback development server.
replace_once(
    "cmd/linkvideo-monitor/remote_control.go",
    '\t"net/url"\n',
    '',
)
replace_once(
    "cmd/linkvideo-monitor/remote_control.go",
    '''\tu, err := url.Parse(endpoint)
\tif err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
\t\tif err == nil {
\t\t\terr = errors.New("адрес должен начинаться с http:// или https://")
\t\t}
\t\ta.setRemoteError(err)
\t\treturn err
\t}

\tbody, err := json.Marshal(request)
''',
    '''\tif err := validateRemoteAPIEndpoint(endpoint); err != nil {
\t\ta.setRemoteError(err)
\t\treturn err
\t}

\tbody, err := json.Marshal(request)
''',
)

# A tracked privacy element can temporarily lose its COM reference. Reattach
# the live focused element when it is recognised again instead of letting the
# privacy mask expire after focus changes.
replace_once(
    "cmd/linkvideo-monitor/privacy_windows.go",
    '''\t\tif rectDistance(item.Rect, rect) <= 3 {
\t\t\trect = item.Rect
\t\t}
\t\titem.Rect = rect
\t\titem.Title = title
''',
    '''\t\tif rectDistance(item.Rect, rect) <= 3 {
\t\t\trect = item.Rect
\t\t}
\t\tif item.Element == 0 && element != 0 {
\t\t\tcomAddRef(element)
\t\t\titem.Element = element
\t\t}
\t\titem.Rect = rect
\t\titem.Title = title
''',
)

# Service requests must originate from the exact installed executable, not
# merely from any process that copied the LinkVideo.Monitor.exe basename.
replace_once(
    "cmd/linkvideo-monitor/uac_service_windows.go",
    '''func isLinkVideoMonitorProcessName(name string) bool {
\tname = strings.ToLower(strings.TrimSpace(name))
\tif name == "linkvideo.monitor.exe" {
\t\treturn true
\t}
\treturn strings.HasPrefix(name, "linkvideo.monitor_") && strings.HasSuffix(name, ".exe")
}

func launchSecureAgent(req secureCaptureRequest) (*secureAgentProcess, error) {
''',
    '''func launchSecureAgent(req secureCaptureRequest) (*secureAgentProcess, error) {
''',
)
replace_once(
    "cmd/linkvideo-monitor/uac_service_windows.go",
    '''\tif !isLinkVideoMonitorProcessName(processImageName(req.ClientPID)) {
\t\treturn nil, errors.New("secure capture request was not created by LinkVideo Monitor")
\t}

\t// Launch the installed application rather than the service copy. FFmpeg is
\t// installed next to this executable and is used by the DXGI secure helper.
\texe, err := loadInstalledAppPath()
\tif err != nil {
\t\texe, err = os.Executable()
\t\tif err != nil {
\t\t\treturn nil, err
\t\t}
\t}
''',
    '''\t// Launch the installed application rather than the service copy. FFmpeg is
\t// installed next to this executable and is used by the DXGI secure helper.
\texe, err := loadInstalledAppPath()
\tif err != nil {
\t\treturn nil, fmt.Errorf("installed LinkVideo Monitor path is unavailable: %w", err)
\t}
\trequesterPath := processExecutablePath(req.ClientPID)
\tif !sameWindowsExecutablePath(requesterPath, exe) {
\t\treturn nil, errors.New("secure capture request was not created by the installed LinkVideo Monitor")
\t}
''',
)
replace_once(
    "cmd/linkvideo-monitor/uac_service_windows.go",
    '''func validSecureCaptureRequest(req secureCaptureRequest) bool {
\texpectedMapping := fmt.Sprintf(`Local\\LinkVideoMonitorSecure_%d`, req.SessionID)
\tif req.SessionID == 0xffffffff || req.ClientPID == 0 || !strings.EqualFold(req.MappingName, expectedMapping) {
\t\treturn false
\t}
\tif req.Width < 2 || req.Height < 2 || req.OutputWidth < 2 || req.OutputHeight < 2 {
\t\treturn false
\t}
\tif req.Width > 32768 || req.Height > 32768 || req.OutputWidth > 8192 || req.OutputHeight > 8192 {
\t\treturn false
\t}
\treturn req.FPS >= 1 && req.FPS <= 30
}
''',
    '''func validSecureCaptureRequest(req secureCaptureRequest) bool {
\texpectedMapping := fmt.Sprintf(`Local\\LinkVideoMonitorSecure_%d`, req.SessionID)
\tif req.SessionID == 0xffffffff || req.ClientPID == 0 || !strings.EqualFold(req.MappingName, expectedMapping) {
\t\treturn false
\t}
\treturn validSecureCaptureDimensions(req.Width, req.Height, req.OutputWidth, req.OutputHeight, req.FPS)
}
''',
)

# Validate frame dimensions before allocating/mapping potentially enormous
# buffers in the interactive process as well as in the service.
replace_once(
    "cmd/linkvideo-monitor/secure_capture_windows.go",
    '''\tframeBytes := plan.OutputWidth * plan.OutputHeight * 4
\tif frameBytes <= 0 {
\t\treturn nil, errors.New("invalid secure frame size")
\t}
''',
    '''\tif !validSecureCaptureDimensions(plan.Width, plan.Height, plan.OutputWidth, plan.OutputHeight, cfg.FPS) {
\t\treturn nil, errors.New("invalid secure capture dimensions")
\t}
\tframeBytes := plan.OutputWidth * plan.OutputHeight * 4
''',
)

# Session request files are writable by the interactive user, but must not be
# readable/replaceable by every other local user. Give each creator ownership
# of its own inherited file while SYSTEM/Admins retain full access.
replace_once(
    "installer/backend.go",
    '''\tif err := os.MkdirAll(sessionsDir, 0o777); err != nil {
\t\treturn err
\t}
\tservicePath := filepath.Join(serviceDir, "LinkVideo.Monitor.Service.exe")
''',
    '''\t// Service installation happens with the Monitor stopped, so stale request
\t// files can be discarded and the directory ACL rebuilt from a known state.
\t_ = os.RemoveAll(sessionsDir)
\tif err := os.MkdirAll(sessionsDir, 0o700); err != nil {
\t\treturn err
\t}
\tservicePath := filepath.Join(serviceDir, "LinkVideo.Monitor.Service.exe")
''',
)
replace_once(
    "installer/backend.go",
    '''\t_ = runHidden("icacls.exe", sessionsDir, "/grant", "*S-1-5-32-545:(OI)(CI)M", "/T", "/C")
''',
    '''\t_ = runHidden("icacls.exe", sessionsDir, "/inheritance:r")
\t_ = runHidden("icacls.exe", sessionsDir, "/grant:r", "*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F", "*S-1-3-0:(OI)(CI)(IO)F", "*S-1-5-32-545:(RX,WD,AD)")
''',
)

# Prevent a LAN client from publishing over the local camera path. Anonymous
# reads stay available; publish permission is restricted to loopback only.
replace_once(
    "cmd/linkvideo-monitor/local_rtsp.go",
    '''func mediaMTXConfig(cfg Config) []byte {
\tpath := sanitizeStreamPath(cfg.LocalRTSPPath)
\treturn []byte(fmt.Sprintf("logLevel: info\\nrtspAddress: :%d\\npaths:\\n  \\"%s\\":\\n    source: publisher\\n", cfg.LocalRTSPPort, path))
}
''',
    '''func mediaMTXConfig(cfg Config) []byte {
\tpath := sanitizeStreamPath(cfg.LocalRTSPPath)
\treturn []byte(fmt.Sprintf(`authMethod: internal
authInternalUsers:
  - user: any
    ips: ["127.0.0.1", "::1"]
    permissions:
      - action: publish
        path: "%s"
      - action: read
        path: "%s"
  - user: any
    ips: []
    permissions:
      - action: read
        path: "%s"
logLevel: info
rtspAddress: :%d
paths:
  "%s":
    source: publisher
`, path, path, path, cfg.LocalRTSPPort, path))
}
''',
)

# Never guess a DXGI output index from \\.\\DISPLAYn. If DXGI identity cannot
# be matched, the capture supervisor must use the reliable GDI fallback.
replace_once(
    "cmd/linkvideo-monitor/monitors_windows.go",
    '''\tfor i := range result {
\t\tif result[i].AdapterIndex < 0 {
\t\t\tresult[i].AdapterIndex = 0
\t\t}
\t\tif result[i].OutputIndex < 0 {
\t\t\tif result[i].DisplayNumber > 0 {
\t\t\t\tresult[i].OutputIndex = result[i].DisplayNumber - 1
\t\t\t} else {
\t\t\t\tresult[i].OutputIndex = result[i].Index
\t\t\t}
\t\t}
\t}
''',
    '''\t// Leave AdapterIndex/OutputIndex at -1 when DXGI could not prove the
\t// mapping. Guessing from DISPLAYn is unsafe on multi-GPU systems because
\t// Windows display numbers are not DXGI output indexes.
''',
)
replace_once(
    "cmd/linkvideo-monitor/capture_pipeline.go",
    '''\tif s.plan.Mode == "monitor" {
\t\tm, ok := selectedMonitor(s.cfg, monitors)
\t\treturn ok && m.AdapterIndex == 0
\t}
\tfor _, m := range monitors {
\t\tif m.AdapterIndex != 0 {
\t\t\treturn false
\t\t}
\t}
''',
    '''\tif s.plan.Mode == "monitor" {
\t\tm, ok := selectedMonitor(s.cfg, monitors)
\t\treturn ok && m.AdapterIndex == 0 && m.OutputIndex >= 0
\t}
\tfor _, m := range monitors {
\t\tif m.AdapterIndex != 0 || m.OutputIndex < 0 {
\t\t\treturn false
\t\t}
\t}
''',
)

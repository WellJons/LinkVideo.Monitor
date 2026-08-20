from pathlib import Path


def read(path):
    return Path(path).read_text(encoding="utf-8")


def write(path, text):
    Path(path).write_text(text, encoding="utf-8")


def replace_once(path, old, new):
    text = read(path)
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{path}: expected 1 match, got {count}")
    write(path, text.replace(old, new, 1))


def replace_between(path, start, end, new):
    text = read(path)
    i = text.find(start)
    if i < 0:
        raise SystemExit(f"{path}: start marker missing: {start!r}")
    j = text.find(end, i)
    if j < 0:
        raise SystemExit(f"{path}: end marker missing: {end!r}")
    write(path, text[:i] + new + text[j:])


# Preserve legitimate negative virtual-screen coordinates. (-1,-1) is the only
# unset sentinel; a monitor can validly live at x=-1920 or y=-1080.
replace_once(
    "cmd/linkvideo-monitor/main.go",
    '''\tc.OverlayText = ""
\tif c.OverlayX < -1 {
\t\tc.OverlayX = -1
\t}
\tif c.OverlayY < -1 {
\t\tc.OverlayY = -1
\t}
''',
    '''\tc.OverlayText = ""
''',
)
replace_once(
    "cmd/linkvideo-monitor/windows_ui_windows.go",
    '''\tif x < 0 || y < 0 {
\t\tx, y = defaultOverlayPosition(width, height)
\t} else {
''',
    '''\tif x == -1 && y == -1 {
\t\tx, y = defaultOverlayPosition(width, height)
\t} else {
''',
)
replace_once(
    "cmd/linkvideo-monitor/overlay_common.go",
    '''\tinside := x >= 0 || y >= 0
\tinside = inside && cx >= monitorX && cx < monitorX+monitorWidth && cy >= monitorY && cy < monitorY+monitorHeight
''',
    '''\tinside := !(x == -1 && y == -1)
\tinside = inside && cx >= monitorX && cx < monitorX+monitorWidth && cy >= monitorY && cy < monitorY+monitorHeight
''',
)

# Remote settings are revisioned and commands are acknowledged only after a
# successful action. Also fail instead of overwriting a local edit that happened
# while the HTTP request was in flight.
remote_impl = r'''func (a *app) applyRemoteResponse(resp remoteSyncResponse) error {
	var commandID, action string
	if resp.Command != nil {
		commandID = commandIDString(resp.Command.ID)
		action = strings.ToLower(strings.TrimSpace(resp.Command.Action))
		if action != "" && commandID == "" {
			return errors.New("у дистанционной команды отсутствует корректный обязательный id")
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

	// Settings are monotonic. An old/equal revision can be replayed by a proxy or
	// delayed request and must never roll a client back.
	settingsApplied := resp.Settings != nil && resp.Revision > before.RemoteRevision
	if settingsApplied {
		if err := a.applyRemoteSettingsValidated(&after, *resp.Settings); err != nil {
			return err
		}
		after.RemoteRevision = resp.Revision
	} else if resp.Revision > after.RemoteRevision {
		// A revision can advance without settings (for example command-only server
		// state). Preserve monotonic acknowledgement of that server revision.
		after.RemoteRevision = resp.Revision
	}

	newCommand := action != "" && commandID != before.RemoteLastCommandID
	normalizeConfig(&after)
	configChanged := !reflect.DeepEqual(streamConfigView(before), streamConfigView(after))
	metadataChanged := !reflect.DeepEqual(before, after)

	a.mu.Lock()
	current := a.cfg
	if !reflect.DeepEqual(current, before) {
		a.mu.Unlock()
		return errors.New("локальные настройки изменились во время синхронизации; ответ API будет применён при следующей попытке")
	}
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
		var err error
		switch action {
		case "start_stream":
			err = a.start()
		case "stop_stream":
			a.stop()
		case "restart_stream":
			err = a.restart()
		case "restart_application":
			err = a.scheduleProcessRestart()
		}
		if err != nil {
			return err
		}

		// ACK only after the action has been accepted successfully. If persisting
		// the ACK fails, the server may retry an idempotent command rather than
		// losing a command that never actually ran.
		a.mu.Lock()
		previousID := a.cfg.RemoteLastCommandID
		a.cfg.RemoteLastCommandID = commandID
		if err := a.saveConfigLocked(); err != nil {
			a.cfg.RemoteLastCommandID = previousID
			a.mu.Unlock()
			return fmt.Errorf("не удалось сохранить подтверждение дистанционной команды: %w", err)
		}
		a.mu.Unlock()
		return nil
	}
	if configChanged && wasDesired {
		return a.restart()
	}
	return nil
}

'''
replace_between(
    "cmd/linkvideo-monitor/remote_control.go",
    "func (a *app) applyRemoteResponse(resp remoteSyncResponse) error {",
    "func (a *app) applyRemoteSettingsValidated",
    remote_impl,
)

replace_between(
    "cmd/linkvideo-monitor/remote_control.go",
    "func commandIDString(raw json.RawMessage) string {",
    "func (a *app) setRemoteError",
    r'''func commandIDString(raw json.RawMessage) string {
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
	return ""
}

''',
)

# Strong ZIP containment independent of path-cleaning quirks or drive syntax.
replace_once(
    "installer/backend.go",
    '''\tfor _, f := range zr.File {
\t\tclean := filepath.Clean(f.Name)
\t\tif clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
\t\t\treturn fmt.Errorf("недопустимый путь в пакете: %s", f.Name)
\t\t}
\t\ttarget := filepath.Join(dest, clean)
''',
    '''\tfor _, f := range zr.File {
\t\tclean, target, err := payloadTargetPath(dest, f.Name)
\t\tif err != nil {
\t\t\treturn err
\t\t}
''',
)
replace_once(
    "installer/backend.go",
    "func extractPayload(dest string, onFile func(done, total int, name string)) error {\n",
    r'''func payloadTargetPath(dest, archiveName string) (string, string, error) {
	clean := filepath.Clean(strings.ReplaceAll(archiveName, "/", string(filepath.Separator)))
	if clean == "." || filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" {
		return "", "", fmt.Errorf("недопустимый путь в пакете: %s", archiveName)
	}
	target := filepath.Join(dest, clean)
	rel, err := filepath.Rel(filepath.Clean(dest), filepath.Clean(target))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", "", fmt.Errorf("недопустимый путь в пакете: %s", archiveName)
	}
	return clean, target, nil
}

func extractPayload(dest string, onFile func(done, total int, name string)) error {
''',
)

# Add targeted regression tests.
overlay_test = Path("cmd/linkvideo-monitor/overlay_position_test.go")
text = overlay_test.read_text(encoding="utf-8")
if "TestOverlayKeepsPositionOnMonitorAboveAndLeft" not in text:
    text += r'''
func TestOverlayKeepsPositionOnMonitorAboveAndLeft(t *testing.T) {
	x, y := overlayPositionForCaptureMonitor(-1500, -700, -1920, -1080, 1920, 1080, 214, 36)
	if x != -1500 || y != -700 {
		t.Fatalf("negative monitor position unexpectedly changed: %d,%d", x, y)
	}
}
'''
    overlay_test.write_text(text, encoding="utf-8")

remote_test = Path("cmd/linkvideo-monitor/remote_control_audit_test.go")
remote_test.write_text(r'''package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestCommandIDStringRejectsCompositeJSON(t *testing.T) {
	if got := commandIDString(json.RawMessage(`{"x":1}`)); got != "" {
		t.Fatalf("object accepted as command id: %q", got)
	}
	if got := commandIDString(json.RawMessage(`[1,2]`)); got != "" {
		t.Fatalf("array accepted as command id: %q", got)
	}
	if got := commandIDString(json.RawMessage(`"cmd-1"`)); got != "cmd-1" {
		t.Fatalf("string id mismatch: %q", got)
	}
	if got := commandIDString(json.RawMessage(`42`)); got != "42" {
		t.Fatalf("numeric id mismatch: %q", got)
	}
}

func newRemoteAuditApp(t *testing.T) *app {
	t.Helper()
	a := &app{
		cfg: defaultConfig(),
		cfgPath: filepath.Join(t.TempDir(), "config.json"),
		encoderFailures: make(map[string]encoderFailureState),
		adminTokens: make(map[string]time.Time),
	}
	return a
}

func TestRemoteStaleSettingsDoNotRollback(t *testing.T) {
	a := newRemoteAuditApp(t)
	a.cfg.RemoteRevision = 5
	a.cfg.Cursor = true
	v := false
	if err := a.applyRemoteResponse(remoteSyncResponse{Revision: 4, Settings: &remoteSettings{Cursor: &v}}); err != nil {
		t.Fatal(err)
	}
	if !a.cfg.Cursor || a.cfg.RemoteRevision != 5 {
		t.Fatalf("stale revision changed config: cursor=%v revision=%d", a.cfg.Cursor, a.cfg.RemoteRevision)
	}
}

func TestFailedRemoteCommandIsNotAcknowledged(t *testing.T) {
	a := newRemoteAuditApp(t)
	a.cfg.Link = ""
	cmd := remoteCommand{ID: json.RawMessage(`"cmd-fail"`), Action: "start_stream"}
	if err := a.applyRemoteResponse(remoteSyncResponse{Command: &cmd}); err == nil {
		t.Fatal("expected start_stream failure")
	}
	if a.cfg.RemoteLastCommandID != "" {
		t.Fatalf("failed command was acknowledged: %q", a.cfg.RemoteLastCommandID)
	}
}
'''.replace('"testing"\n)', '"testing"\n\t"time"\n)'), encoding="utf-8")

installer_test = Path("installer/backend_audit_test.go")
installer_test.write_text(r'''//go:build windows

package main

import (
	"path/filepath"
	"testing"
)

func TestPayloadTargetPathRejectsTraversal(t *testing.T) {
	dest := filepath.Join(`C:\Program Files`, "LinkVideo.Monitor")
	bad := []string{"../evil.exe", "../../evil.exe", `C:\evil.exe`, `/evil.exe`}
	for _, name := range bad {
		if _, _, err := payloadTargetPath(dest, name); err == nil {
			t.Fatalf("unsafe archive path accepted: %q", name)
		}
	}
	if _, target, err := payloadTargetPath(dest, "bin/ffmpeg.exe"); err != nil {
		t.Fatal(err)
	} else if filepath.Dir(target) != filepath.Join(dest, "bin") {
		t.Fatalf("unexpected safe target: %q", target)
	}
}
''', encoding="utf-8")

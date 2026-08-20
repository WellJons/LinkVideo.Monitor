package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
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

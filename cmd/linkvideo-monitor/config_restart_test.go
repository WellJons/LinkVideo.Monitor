package main

import (
	"strings"
	"testing"
)

func TestMonitorSelectionIsUnprotectedButRestartsCapture(t *testing.T) {
	previous := defaultConfig()
	next := previous
	next.CaptureMode = "monitor"
	next.MonitorIndex = 1
	next.MonitorNumber = 2
	if protectedConfigChange(previous, next) {
		t.Fatal("monitor selection must not require the LinkVideo administrator password")
	}
	if !configChangeRequiresRestart(previous, next) {
		t.Fatal("monitor selection must recreate the active capture pipeline")
	}
}

func TestAllowedCaptureSettingsRestartWithoutManualRestartEndpoint(t *testing.T) {
	previous := defaultConfig()
	next := previous
	next.Cursor = !previous.Cursor
	if protectedConfigChange(previous, next) {
		t.Fatal("cursor visibility must remain available without the administrator password")
	}
	if !configChangeRequiresRestart(previous, next) {
		t.Fatal("cursor visibility changes need a capture restart")
	}
	start := strings.Index(indexHTML, "async function persistSettings")
	end := strings.Index(indexHTML[start:], "async function saveAllSettings")
	if start < 0 || end < 0 {
		t.Fatal("persistSettings JavaScript function was not found")
	}
	body := indexHTML[start : start+end]
	if strings.Contains(body, "/api/restart") {
		t.Fatal("saving allowed settings must not invoke the password-protected manual restart endpoint")
	}
}

func TestOverlayPositionDoesNotRestartCapture(t *testing.T) {
	previous := defaultConfig()
	next := previous
	next.OverlayX = 100
	next.OverlayY = 200
	if protectedConfigChange(previous, next) {
		t.Fatal("moving the indicator must not require a password")
	}
	if configChangeRequiresRestart(previous, next) {
		t.Fatal("moving the indicator must not restart the video stream")
	}
}

func TestMicrophoneModesRemainUserConfigurable(t *testing.T) {
	previous := defaultConfig()
	next := previous
	next.MicrophoneMode = "voice"
	next.MicrophoneVoiceDB = -38
	next.MicrophonePTTHotkey = "F8"
	if protectedConfigChange(previous, next) {
		t.Fatal("microphone modes and hotkeys must remain available without the administrator password")
	}
	if configChangeRequiresRestart(previous, next) {
		t.Fatal("microphone gate and hotkey changes must not interrupt the active stream")
	}

	next.MicrophoneEnabled = true
	next.MicrophoneDevice = "Office microphone"
	if !configChangeRequiresRestart(previous, next) {
		t.Fatal("enabling a microphone input must rebuild the active audio pipeline")
	}
}

func TestFirstValidTargetRequestsAutomaticStart(t *testing.T) {
	previous := defaultConfig()
	previous.Link = ""
	next := previous
	next.Link = "rtmp://b2o-proxy62.video.goodline.info:1935/live/linkvideo_1_main"
	if !shouldAutoStartAfterFirstTarget(previous, next, false) {
		t.Fatal("the first valid LinkVideo target must start the stream automatically")
	}
	if shouldAutoStartAfterFirstTarget(previous, next, true) {
		t.Fatal("an already desired stream must not be started a second time")
	}
}

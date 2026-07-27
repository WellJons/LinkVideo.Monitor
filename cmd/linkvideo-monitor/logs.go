package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var streamURLPattern = regexp.MustCompile(`(?i)(?:rtsp|rtmp)://[^\s'\"]+`)
var progressKVPattern = regexp.MustCompile(`([a-zA-Z]+)=\s*([^\s]+)`)

func sanitizeLogText(line string) string {
	return streamURLPattern.ReplaceAllStringFunc(line, func(raw string) string {
		trailing := ""
		for len(raw) > 0 {
			last := raw[len(raw)-1]
			if strings.ContainsRune(",;)]}", rune(last)) {
				trailing = string(last) + trailing
				raw = raw[:len(raw)-1]
				continue
			}
			break
		}
		return redactURL(raw) + trailing
	})
}

func (a *app) currentLogPath() string {
	path := filepath.Join(a.logsDir, "sender-"+time.Now().Format("2006-01-02")+".log")
	a.mu.Lock()
	a.logPath = path
	a.mu.Unlock()
	return path
}

func (a *app) appendLog(line string) {
	line = strings.TrimSpace(strings.ReplaceAll(line, "\x00", ""))
	if line == "" {
		return
	}
	line = sanitizeLogText(line)
	stamp := time.Now().Format("2006-01-02 15:04:05") + " " + line
	a.mu.Lock()
	a.recent = append(a.recent, stamp)
	if len(a.recent) > 220 {
		a.recent = append([]string(nil), a.recent[len(a.recent)-220:]...)
	}
	a.mu.Unlock()

	logPath := a.currentLogPath()
	_ = os.MkdirAll(filepath.Dir(logPath), 0755)
	if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
		_, _ = fmt.Fprintln(f, stamp)
		_ = f.Close()
	}
}

func (a *app) cleanupOldLogs() {
	_ = os.MkdirAll(a.logsDir, 0755)
	entries, err := os.ReadDir(a.logsDir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -14)
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "sender-") || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(a.logsDir, e.Name()))
		}
	}
}

func (a *app) exportLogs() ([]byte, string, error) {
	entries, err := os.ReadDir(a.logsDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, "", err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "sender-") && strings.HasSuffix(e.Name(), ".log") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var out bytes.Buffer
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(a.logsDir, name))
		if err != nil {
			continue
		}
		if out.Len() > 0 {
			out.WriteString("\r\n\r\n")
		}
		out.WriteString("===== " + name + " =====\r\n")
		scanner := bufio.NewScanner(bytes.NewReader(b))
		scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
		for scanner.Scan() {
			out.WriteString(sanitizeLogText(scanner.Text()))
			out.WriteString("\r\n")
		}
	}
	if out.Len() == 0 {
		out.WriteString("Журнал пока пуст.\r\n")
	}
	filename := "LinkVideo-Monitor-logs-" + time.Now().Format("20060102-150405") + ".txt"
	return out.Bytes(), filename, nil
}

func splitCRLF(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i, b := range data {
		if b == '\n' || b == '\r' {
			advance = i + 1
			if b == '\r' && len(data) > i+1 && data[i+1] == '\n' {
				advance++
			}
			if i == 0 {
				return advance, nil, nil
			}
			return advance, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func (a *app) processFFmpegLine(prefix, line string) bool {
	clean := strings.TrimSpace(line)
	if clean == "" {
		return false
	}
	if strings.HasPrefix(clean, "frame=") {
		fields := progressKVPattern.FindAllStringSubmatch(clean, -1)
		a.mu.Lock()
		a.encoderStartupConfirmed = true
		for _, m := range fields {
			if len(m) != 3 {
				continue
			}
			switch m[1] {
			case "fps":
				a.videoFPS, _ = strconv.ParseFloat(m[2], 64)
			case "speed":
				a.videoSpeed, _ = strconv.ParseFloat(strings.TrimSuffix(m[2], "x"), 64)
			case "dup":
				a.videoDup, _ = strconv.Atoi(m[2])
			case "drop":
				a.videoDrop, _ = strconv.Atoi(m[2])
			}
		}
		a.mu.Unlock()
		return true
	}
	lower := strings.ToLower(clean)
	if strings.HasPrefix(lower, "output #0") || strings.Contains(lower, "stream #0:0: video: h264") {
		a.mu.Lock()
		a.encoderStartupConfirmed = true
		a.mu.Unlock()
	}

	a.mu.Lock()
	activeEncoder := strings.ToLower(a.videoEncoder)
	a.mu.Unlock()

	// Do not blacklist a hardware encoder because of an unrelated FFmpeg error.
	// The old broad check treated every "Error while opening encoder" (including
	// odd dimensions or filter failures) as a GPU failure and permanently jumped
	// from Intel/NVIDIA/AMD to libx264. Only hardware-specific diagnostics are
	// accepted here.
	hardwareFailureMarkers := []string{
		"no capable devices found",
		"cannot load nvcuda",
		"openencodesession",
		"failed to create nvenc",
		"nvenc api version",
		"no nvenc capable devices",
		"error initializing an internal mfx session",
		"failed to create a qsv device",
		"no device available for encoder",
		"mfx session",
		"unsupported device",
		"amf failed",
		"failed to initialise amf",
		"failed to initialize amf",
		"device creation failed",
		"d3d11 device creation failed",
	}
	encoderFailure := false
	for _, marker := range hardwareFailureMarkers {
		if strings.Contains(lower, marker) {
			encoderFailure = true
			break
		}
	}
	if !encoderFailure && activeEncoder != "" && activeEncoder != "libx264" && strings.Contains(lower, activeEncoder) &&
		(strings.Contains(lower, "failed") || strings.Contains(lower, "unsupported") || strings.Contains(lower, "no device")) {
		encoderFailure = true
	}
	if encoderFailure {
		a.mu.Lock()
		a.encoderFailureDetected = true
		a.encoderFailureReason = clean
		a.mu.Unlock()
	}
	if strings.Contains(lower, "failed to capture image") || strings.Contains(lower, "error during demuxing") {
		a.markFatalCapture("Windows временно заблокировала захват экрана")
	} else if strings.Contains(lower, "failed reading rtsp data") || strings.Contains(lower, "broken pipe") {
		a.markPendingRestart("Сервер закрыл RTSP-соединение", false)
	}
	return false
}

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type mediaMTXRelease struct {
	Version      string
	URL          string
	SHA256       string
	LegacyConfig bool
}

var (
	mediaMTXCurrentRelease  = mediaMTXRelease{Version: "1.19.3"}
	mediaMTXWindows7Release = mediaMTXRelease{
		Version:      "1.0.3",
		LegacyConfig: true,
	}
)

var invalidStreamPath = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func sanitizeStreamPath(v string) string {
	v = strings.Trim(strings.TrimSpace(v), "/")
	v = invalidStreamPath.ReplaceAllString(v, "-")
	v = strings.Trim(v, "-.")
	if v == "" {
		return "screen"
	}
	if len(v) > 80 {
		v = v[:80]
	}
	return v
}

func localRTSPURL(cfg Config, host string) string {
	return fmt.Sprintf("rtsp://%s:%d/%s", host, cfg.LocalRTSPPort, sanitizeStreamPath(cfg.LocalRTSPPath))
}

func firstLANIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip4 := ip.To4()
			if ip4 == nil || ip4[0] == 169 && ip4[1] == 254 {
				continue
			}
			return ip4.String()
		}
	}
	return ""
}

func localStreamLinks(cfg Config) (string, string) {
	local := localRTSPURL(cfg, "127.0.0.1")
	lan := ""
	if ip := firstLANIPv4(); ip != "" {
		lan = localRTSPURL(cfg, ip)
	}
	return local, lan
}

func mediaMTXReleaseForWindows(major, minor uint32) mediaMTXRelease {
	// Go 1.20 is the last Go release that supports Windows 7. MediaMTX 1.0.3
	// was built with Go 1.20, while current MediaMTX releases are built with a
	// newer Go runtime and cannot start on Windows 7 / Server 2008 R2.
	if major == 6 && minor == 1 {
		release := mediaMTXWindows7Release
		release.URL = "https://github.com/bluenviron/mediamtx/releases/download/v1.0.3/mediamtx_v1.0.3_windows_amd64.zip"
		release.SHA256 = "f3cffd7ec6113895e8742346644cd5856bd007e6535797ef41e4303cf4bc0d6c"
		return release
	}
	release := mediaMTXCurrentRelease
	release.URL = "https://github.com/bluenviron/mediamtx/releases/download/v1.19.3/mediamtx_v1.19.3_windows_amd64.zip"
	release.SHA256 = "5d82148d1032a6a190d9909a2997d9989457aaadf49af87dd02cd4512d31bebe"
	return release
}

func mediaMTXConfig(cfg Config) []byte {
	return mediaMTXConfigForRelease(cfg, selectedMediaMTXRelease())
}

func mediaMTXConfigForRelease(cfg Config, release mediaMTXRelease) []byte {
	path := sanitizeStreamPath(cfg.LocalRTSPPath)
	if release.LegacyConfig {
		return []byte(fmt.Sprintf(`logLevel: info
logDestinations: [stdout]
api: no
metrics: no
pprof: no
rtsp: yes
protocols: [tcp]
rtspAddress: :%d
rtmp: no
hls: no
webrtc: no
srt: no
paths:
  "%s":
    source: publisher
    publishIPs: [127.0.0.1, "::1"]
    readIPs: []
`, cfg.LocalRTSPPort, path))
	}

	return []byte(fmt.Sprintf(`logLevel: info
logDestinations: [stdout]
api: false
metrics: false
pprof: false
playback: false
rtsp: true
rtspTransports: [tcp]
rtspAddress: :%d
rtmp: false
hls: false
webrtc: false
srt: false
moq: false
authMethod: internal
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
paths:
  "%s":
    source: publisher
`, cfg.LocalRTSPPort, path, path, path, path))
}

func probeRTSP(address, streamURL string, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := fmt.Fprintf(conn, "OPTIONS %s RTSP/1.0\r\nCSeq: 1\r\nUser-Agent: LinkVideo-Monitor/%s\r\n\r\n", streamURL, appVersion); err != nil {
		return err
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return err
	}
	if !strings.HasPrefix(strings.TrimSpace(line), "RTSP/1.0") {
		return fmt.Errorf("порт отвечает не как RTSP-сервер: %s", strings.TrimSpace(line))
	}
	return nil
}

func (a *app) ensureMediaMTX(cfg Config) error {
	address := fmt.Sprintf("127.0.0.1:%d", cfg.LocalRTSPPort)
	streamURL := localRTSPURL(cfg, "127.0.0.1")
	release := selectedMediaMTXRelease()

	a.mu.Lock()
	runningCmd := a.mediaCmd
	running := a.mediaRunning && runningCmd != nil && runningCmd.Process != nil
	a.mu.Unlock()
	if running {
		// Не полагаемся только на флаг процесса: после смены порта старый
		// MediaMTX ещё может завершаться. Проверяем именно требуемый адрес.
		if err := probeRTSP(address, streamURL, 500*time.Millisecond); err == nil {
			return nil
		}
		_ = runningCmd.Process.Kill()
		a.mu.Lock()
		if a.mediaCmd == runningCmd {
			a.mediaCmd = nil
			a.mediaRunning = false
		}
		a.mu.Unlock()
	}
	if err := probeRTSP(address, streamURL, 500*time.Millisecond); err == nil {
		a.appendLog(fmt.Sprintf("Локальный RTSP-сервер уже работает на порту %d", cfg.LocalRTSPPort))
		return nil
	} else if conn, dialErr := net.DialTimeout("tcp", address, 250*time.Millisecond); dialErr == nil {
		_ = conn.Close()
		return fmt.Errorf("порт %d занят другой программой и не является RTSP-сервером", cfg.LocalRTSPPort)
	}

	managedDefault := isManagedMediaMTXPath(cfg.MediaMTXPath)
	var exe string
	if managedDefault {
		var err error
		exe, err = ensureManagedMediaMTX(a, release)
		if err != nil {
			return err
		}
	} else {
		exe = resolveExecutable(cfg.MediaMTXPath)
		if _, err := os.Stat(exe); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("локальный RTSP-сервер не найден: %s", exe)
			}
			return err
		}
	}

	cfgFile := filepath.Join(filepath.Dir(a.cfgPath), "mediamtx.yml")
	content := mediaMTXConfigForRelease(cfg, release)
	if err := os.MkdirAll(filepath.Dir(cfgFile), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(cfgFile, content, 0o644); err != nil {
		return err
	}

	cmd := exec.Command(exe, cfgFile)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	hideChildWindow(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("не удалось запустить локальную RTSP-камеру: %w", err)
	}

	a.mu.Lock()
	a.mediaCmd = cmd
	a.mediaRunning = true
	a.mu.Unlock()
	a.appendLog(fmt.Sprintf("Локальный RTSP-сервер запущен, PID=%d, порт=%d", cmd.Process.Pid, cfg.LocalRTSPPort))

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); scanExternalPipe(a, "mediamtx", stderr) }()
	go func() { defer wg.Done(); scanExternalPipe(a, "mediamtx", stdout) }()
	go func() {
		err := cmd.Wait()
		wg.Wait()
		a.mu.Lock()
		if a.mediaCmd == cmd {
			a.mediaCmd = nil
			a.mediaRunning = false
		}
		desired := a.desired
		a.mu.Unlock()
		if err != nil && desired {
			a.appendLog("Локальный RTSP-сервер остановился: " + err.Error())
		} else {
			a.appendLog("Локальный RTSP-сервер остановлен")
		}
	}()

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if err := probeRTSP(address, streamURL, 400*time.Millisecond); err == nil {
			a.appendLog("Локальная RTSP-трансляция готова: " + streamURL)
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	return fmt.Errorf("локальная RTSP-трансляция запущена, но порт %d не открылся", cfg.LocalRTSPPort)
}

func scanExternalPipe(a *app, prefix string, r io.Reader) {
	s := bufio.NewScanner(r)
	buf := make([]byte, 64*1024)
	s.Buffer(buf, 1024*1024)
	for s.Scan() {
		a.appendLog(prefix + ": " + s.Text())
	}
}

package main

import (
	"archive/zip"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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
	mediaMTXCurrentRelease = mediaMTXRelease{
		Version: "1.19.3",
		URL:     "https://github.com/bluenviron/mediamtx/releases/download/v1.19.3/mediamtx_v1.19.3_windows_amd64.zip",
		SHA256:  "5d82148d1032a6a190d9909a2997d9989457aaadf49af87dd02cd4512d31bebe",
	}
	mediaMTXWindows7Release = mediaMTXRelease{
		Version:      "1.0.3",
		URL:          "https://github.com/bluenviron/mediamtx/releases/download/v1.0.3/mediamtx_v1.0.3_windows_amd64.zip",
		SHA256:       "f3cffd7ec6113895e8742346644cd5856bd007e6535797ef41e4303cf4bc0d6c",
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

func mediaMTXDefaultPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "mediamtx.exe"), nil
}

func mediaMTXReleaseForWindows(major, minor uint32) mediaMTXRelease {
	// Go 1.20 is the last Go release that supports Windows 7. MediaMTX 1.0.3
	// was built with Go 1.20, while current MediaMTX releases are built with a
	// newer Go runtime and cannot start on Windows 7 / Server 2008 R2.
	if major == 6 && minor == 1 {
		return mediaMTXWindows7Release
	}
	return mediaMTXCurrentRelease
}

func selectedMediaMTXRelease() mediaMTXRelease {
	major, minor, _ := windowsVersion()
	return mediaMTXReleaseForWindows(major, minor)
}

func mediaMTXVersionMarker(dest string) string {
	return dest + ".linkvideo-version"
}

func mediaMTXMarkerVersion(dest string) string {
	data, err := os.ReadFile(mediaMTXVersionMarker(dest))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func managedMediaMTXNeedsInstall(dest string, release mediaMTXRelease) (bool, error) {
	info, err := os.Stat(dest)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	if info.IsDir() {
		return false, fmt.Errorf("путь локального RTSP-сервера является папкой: %s", dest)
	}

	installed := mediaMTXMarkerVersion(dest)
	if installed == release.Version {
		return false, nil
	}
	if installed != "" {
		return true, nil
	}

	// Releases before 0.8.12 did not create a version marker. On modern
	// Windows that unmarked binary is the existing 1.19.3 component and can be
	// kept. On Windows 7 it is exactly the incompatible binary that must be
	// replaced once with the Go 1.20-compatible release.
	return release.LegacyConfig, nil
}

func downloadMediaMTX(dest string, release mediaMTXRelease) error {
	client := &http.Client{Timeout: 3 * time.Minute}
	req, err := http.NewRequest(http.MethodGet, release.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "LinkVideo-Monitor/"+appVersion)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("не удалось загрузить компонент локальной трансляции: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("сервер загрузки вернул HTTP %d", resp.StatusCode)
	}

	tmpZip, err := os.CreateTemp("", "linkvideo-mediamtx-*.zip")
	if err != nil {
		return err
	}
	zipPath := tmpZip.Name()
	defer os.Remove(zipPath)
	hash := sha256.New()
	limited := io.LimitReader(resp.Body, 120<<20)
	if _, err := io.Copy(io.MultiWriter(tmpZip, hash), limited); err != nil {
		tmpZip.Close()
		return err
	}
	if err := tmpZip.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(got, release.SHA256) {
		return fmt.Errorf("контрольная сумма компонента локальной трансляции не совпала: %s", got)
	}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("загруженный архив повреждён: %w", err)
	}
	defer zr.Close()

	var source *zip.File
	for _, f := range zr.File {
		if strings.EqualFold(filepath.Base(f.Name), "mediamtx.exe") {
			source = f
			break
		}
	}
	if source == nil {
		return errors.New("в архиве не найден mediamtx.exe")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	in, err := source.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	tmpDest := dest + ".new"
	out, err := os.OpenFile(tmpDest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmpDest)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpDest)
		return closeErr
	}
	if err := replaceFileAtomically(tmpDest, dest); err != nil {
		_ = os.Remove(tmpDest)
		return err
	}
	if err := os.WriteFile(mediaMTXVersionMarker(dest), []byte(release.Version+"\n"), 0o644); err != nil {
		return fmt.Errorf("компонент установлен, но не удалось сохранить его версию: %w", err)
	}
	return nil
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

	managedDefault := strings.EqualFold(strings.TrimSpace(cfg.MediaMTXPath), "mediamtx.exe")
	var exe string
	if managedDefault {
		var err error
		exe, err = mediaMTXDefaultPath()
		if err != nil {
			return err
		}
		needsInstall, err := managedMediaMTXNeedsInstall(exe, release)
		if err != nil {
			return err
		}
		if needsInstall {
			a.appendLog(fmt.Sprintf("Установка компонента локальной RTSP-трансляции MediaMTX %s…", release.Version))
			if err := downloadMediaMTX(exe, release); err != nil {
				return fmt.Errorf("не удалось автоматически установить локальную RTSP-камеру: %w", err)
			}
			a.appendLog(fmt.Sprintf("Компонент локальной RTSP-трансляции MediaMTX %s установлен", release.Version))
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

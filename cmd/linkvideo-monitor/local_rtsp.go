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

const (
	mediaMTXVersion = "1.19.3"
	mediaMTXURL     = "https://github.com/bluenviron/mediamtx/releases/download/v" + mediaMTXVersion + "/mediamtx_v" + mediaMTXVersion + "_windows_amd64.zip"
	mediaMTXSHA256  = "5d82148d1032a6a190d9909a2997d9989457aaadf49af87dd02cd4512d31bebe"
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

func downloadMediaMTX(dest string) error {
	client := &http.Client{Timeout: 3 * time.Minute}
	req, err := http.NewRequest(http.MethodGet, mediaMTXURL, nil)
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
	if got := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(got, mediaMTXSHA256) {
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
	return nil
}

func mediaMTXConfig(cfg Config) []byte {
	path := sanitizeStreamPath(cfg.LocalRTSPPath)
	return []byte(fmt.Sprintf(`authMethod: internal
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

	exe := resolveExecutable(cfg.MediaMTXPath)
	if _, err := os.Stat(exe); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		// Стандартный компонент устанавливается автоматически при первом
		// использовании локальной RTSP-трансляции.
		if filepath.IsAbs(cfg.MediaMTXPath) || !strings.EqualFold(filepath.Base(cfg.MediaMTXPath), "mediamtx.exe") {
			return fmt.Errorf("локальный RTSP-сервер не найден: %s", exe)
		}
		var pathErr error
		exe, pathErr = mediaMTXDefaultPath()
		if pathErr != nil {
			return pathErr
		}
		a.appendLog("Установка компонента локальной RTSP-трансляции…")
		if err := downloadMediaMTX(exe); err != nil {
			return fmt.Errorf("не удалось автоматически установить локальную RTSP-камеру: %w", err)
		}
		a.appendLog("Компонент локальной RTSP-трансляции установлен")
	}

	cfgFile := filepath.Join(filepath.Dir(a.cfgPath), "mediamtx.yml")
	content := mediaMTXConfig(cfg)
	if err := os.MkdirAll(filepath.Dir(cfgFile), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(cfgFile, content, 0644); err != nil {
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

//go:build windows

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// runSecureDesktopCapture waits until Winlogon becomes the input desktop and
// then captures it through DXGI Desktop Duplication. The helper is launched by
// the Windows service as LOCAL_SYSTEM in the interactive session, which is the
// security context required by DuplicateOutput for the secure desktop.
func runSecureDesktopCapture(writer *secureMapWriter, req secureCaptureRequest) error {
	for {
		for !secureDesktopIsInput() {
			writer.SetInactive()
			time.Sleep(50 * time.Millisecond)
		}

		err := runSecureDesktopDXGIOnce(writer, req)
		writer.SetInactive()
		if err != nil {
			secureAgentLog("DXGI secure capture restart: " + err.Error())
		}
		// Desktop switches invalidate a duplication object. Recreate it while
		// Winlogon is active instead of keeping a stale frame indefinitely.
		if secureDesktopIsInput() {
			time.Sleep(120 * time.Millisecond)
		}
	}
}

func runSecureDesktopDXGIOnce(writer io.Writer, req secureCaptureRequest) error {
	args, err := buildSecureDXGICaptureArgs(req)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	ffmpeg := filepath.Join(filepath.Dir(exe), "ffmpeg.exe")
	if info, statErr := os.Stat(ffmpeg); statErr != nil || info.IsDir() {
		if statErr != nil {
			return fmt.Errorf("ffmpeg для защищённого захвата: %w", statErr)
		}
		return errors.New("ffmpeg для защищённого захвата является каталогом")
	}

	cmd := exec.Command(ffmpeg, args...)
	hideChildWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	var stderrTail atomic.Value
	stderrTail.Store("")
	go func() {
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 32*1024), 512*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				stderrTail.Store(line)
			}
		}
	}()

	frameBytes := req.OutputWidth * req.OutputHeight * 4
	framesDone := make(chan error, 1)
	var lastFrameUnix atomic.Int64
	go func() {
		frame := make([]byte, frameBytes)
		for {
			if _, err := io.ReadFull(stdout, frame); err != nil {
				framesDone <- err
				return
			}
			if _, err := writer.Write(frame); err != nil {
				framesDone <- err
				return
			}
			lastFrameUnix.Store(time.Now().UnixMilli())
		}
	}()

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	started := time.Now()
	var readErr error
	for readErr == nil {
		select {
		case readErr = <-framesDone:
		case <-ticker.C:
			last := lastFrameUnix.Load()
			if secureDesktopIsInput() {
				if last == 0 && time.Since(started) > 3*time.Second {
					readErr = errors.New("DXGI не передал первый кадр Winlogon за 3 секунды")
				} else if last != 0 && time.Since(time.UnixMilli(last)) > 3*time.Second {
					readErr = errors.New("DXGI перестал обновлять кадры Winlogon")
				}
			}
		}
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if tail, _ := stderrTail.Load().(string); tail != "" {
		return fmt.Errorf("%v; ffmpeg: %s", readErr, tail)
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return readErr
	}
	return waitErr
}

func buildSecureDXGICaptureArgs(req secureCaptureRequest) ([]string, error) {
	outputs, err := enumerateDXGIOutputs()
	if err != nil {
		return nil, fmt.Errorf("DXGI-выходы защищённого рабочего стола: %w", err)
	}
	monitors := make([]Monitor, 0, len(outputs))
	for i, output := range outputs {
		monitors = append(monitors, Monitor{
			Index: i, Name: output.DeviceName, X: output.X, Y: output.Y,
			Width: output.Width, Height: output.Height, HMonitor: output.Monitor,
			AdapterIndex: output.AdapterIndex, OutputIndex: output.OutputIndex,
		})
	}
	if len(monitors) == 0 {
		return nil, errors.New("Winlogon не вернул подключённые DXGI-выходы")
	}

	source := func(m Monitor, label string) string {
		return fmt.Sprintf(
			"ddagrab=output_idx=%d:framerate=%d:draw_mouse=%s:output_fmt=bgra:allow_fallback=1,hwdownload,format=bgra[%s]",
			m.OutputIndex, req.FPS, boolText(req.Cursor), label,
		)
	}
	finish := func(input string, cropX, cropY int) string {
		chain := fmt.Sprintf("[%s]crop=%d:%d:%d:%d", input, req.Width, req.Height, cropX, cropY)
		if req.OutputWidth != req.Width || req.OutputHeight != req.Height {
			chain += fmt.Sprintf(",scale=%d:%d:flags=fast_bilinear", req.OutputWidth, req.OutputHeight)
		}
		return chain + "[capture]"
	}

	for _, m := range monitors {
		if req.X == m.X && req.Y == m.Y && req.Width == even(m.Width) && req.Height == even(m.Height) {
			if m.AdapterIndex != 0 {
				return nil, errors.New("выбранный монитор Winlogon подключён к дополнительному видеоадаптеру")
			}
			filter := source(m, "raw") + ";" + finish("raw", 0, 0)
			return secureRawVideoArgs(filter), nil
		}
	}

	minX, minY := monitors[0].X, monitors[0].Y
	for _, m := range monitors {
		if m.AdapterIndex != 0 {
			return nil, errors.New("захват всех экранов Winlogon на нескольких видеоадаптерах пока недоступен")
		}
		if m.X < minX {
			minX = m.X
		}
		if m.Y < minY {
			minY = m.Y
		}
	}
	parts := make([]string, 0, len(monitors)+2)
	var labels strings.Builder
	layout := make([]string, 0, len(monitors))
	for i, m := range monitors {
		label := fmt.Sprintf("v%d", i)
		parts = append(parts, source(m, label))
		labels.WriteString("[" + label + "]")
		layout = append(layout, fmt.Sprintf("%d_%d", m.X-minX, m.Y-minY))
	}
	parts = append(parts, fmt.Sprintf("%sxstack=inputs=%d:layout=%s:fill=black[stacked]", labels.String(), len(monitors), strings.Join(layout, "|")))
	parts = append(parts, finish("stacked", req.X-minX, req.Y-minY))
	return secureRawVideoArgs(strings.Join(parts, ";")), nil
}

func secureRawVideoArgs(filter string) []string {
	return []string{
		"-hide_banner", "-loglevel", "warning", "-nostdin",
		"-filter_complex", filter,
		"-map", "[capture]",
		"-an", "-sn", "-dn",
		"-pix_fmt", "bgra", "-fps_mode", "cfr",
		"-f", "rawvideo", "pipe:1",
	}
}

func secureAgentLog(line string) {
	root := filepath.Join(os.Getenv("PROGRAMDATA"), "LinkVideo.Monitor")
	_ = os.MkdirAll(root, 0o755)
	f, err := os.OpenFile(filepath.Join(root, "secure-capture.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s %s\r\n", time.Now().Format("2006-01-02 15:04:05"), line)
}

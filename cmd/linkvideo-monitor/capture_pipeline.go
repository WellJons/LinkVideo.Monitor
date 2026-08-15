package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	captureBackendAuto = "auto"
	captureBackendDXGI = "dxgi"
	captureBackendGDI  = "gdi"
)

type captureSupervisor struct {
	app      *app
	cfg      Config
	plan     capturePlan
	frameLen int

	mu             sync.RWMutex
	latest         []byte
	scratch        []byte
	hasFrame       bool
	backend        string
	privacy        privacyTracker
	secure         secureDesktopBridge
	secureActive   bool
	secureHasFrame bool
	secureFrame    []byte
	session        sessionStateWatcher
	display        displayPowerStateWatcher
	sessionLocked  bool
	displayOff     bool
	lockedFrame    []byte
	protectedFrame []byte
	normalPriority bool
}

func newCaptureSupervisor(a *app, cfg Config, plan capturePlan, normalPriority bool) *captureSupervisor {
	frameLen := plan.OutputWidth * plan.OutputHeight * 4
	s := &captureSupervisor{
		app: a, cfg: cfg, plan: plan, frameLen: frameLen,
		latest: make([]byte, frameLen), scratch: make([]byte, frameLen), secureFrame: make([]byte, frameLen),
		session:        newSessionStateWatcher(),
		display:        newDisplayPowerStateWatcher(),
		lockedFrame:    makeSessionLockedFrame(plan.OutputWidth, plan.OutputHeight),
		protectedFrame: makeProtectedDesktopFrame(plan.OutputWidth, plan.OutputHeight),
		normalPriority: normalPriority,
	}
	// A single centered message can fall directly on the seam between two
	// physical displays. Build the lock fallback per captured monitor so the
	// LinkVideo logo and explanation remain clearly visible on every screen.
	if monitors, err := listMonitors(); err == nil && len(monitors) > 0 {
		s.lockedFrame = makeSessionLockedCaptureFrame(plan, monitors)
	}
	if cfg.PrivacyProtection {
		s.privacy = newPrivacyTracker()
	}
	return s
}

func (s *captureSupervisor) initialBackend() string {
	dxgiAvailable := supportsDesktopDuplication() && s.planSupportsDXGI()
	switch s.cfg.CaptureBackend {
	case captureBackendDXGI:
		if dxgiAvailable {
			return captureBackendDXGI
		}
		return captureBackendGDI
	case captureBackendGDI:
		return captureBackendGDI
	default:
		if dxgiAvailable {
			return captureBackendDXGI
		}
		return captureBackendGDI
	}
}

func (s *captureSupervisor) planSupportsDXGI() bool {
	monitors, err := listMonitors()
	if err != nil || len(monitors) == 0 {
		return false
	}
	if s.plan.Mode == "monitor" {
		m, ok := selectedMonitor(s.cfg, monitors)
		return ok && m.AdapterIndex == 0
	}
	for _, m := range monitors {
		if m.AdapterIndex != 0 {
			return false
		}
	}
	return true
}

func captureBackendLabel(backend string) string {
	switch backend {
	case captureBackendDXGI:
		return "Desktop Duplication"
	case captureBackendGDI:
		return "Совместимый захват Windows"
	default:
		return backend
	}
}

func (s *captureSupervisor) setBackend(backend string) {
	s.mu.Lock()
	changed := s.backend != backend
	s.backend = backend
	s.mu.Unlock()
	if changed {
		s.app.setCaptureBackend(backend)
		s.app.appendLog("Метод захвата: " + captureBackendLabel(backend))
	}
}

// run keeps a capture producer alive independently from the encoder. If DXGI
// loses access during UAC, display reset or an installer transition, the latest
// frame stays in memory and the producer is replaced by the GDI fallback. The
// FFmpeg encoder and RTSP connection are not restarted.
func (s *captureSupervisor) run(ctx context.Context) {
	if s.session != nil {
		go s.session.Run(ctx, s.handleSessionState)
	}
	if s.display != nil {
		go s.display.Run(ctx, s.handleDisplayPowerState)
	}
	if s.privacy != nil {
		go s.privacy.Run(ctx)
		s.app.appendLog("Защита конфиденциальных полей включена")
	}
	if secure, err := newSecureDesktopBridge(s.plan, s.cfg); err == nil {
		s.mu.Lock()
		s.secure = secure
		locked := s.sessionLocked
		s.mu.Unlock()
		secure.SetSessionLocked(locked)
		go secure.Run(ctx, s.handleSecureFrame)
		defer secure.Close()
		s.app.appendLog("Служба защищённого рабочего стола подключена")
	} else {
		s.app.appendLog("Захват UAC недоступен: " + err.Error())
	}
	backend := s.initialBackend()
	for {
		if ctx.Err() != nil {
			return
		}
		s.setBackend(backend)

		producerCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func(selected string) {
			done <- s.runProducer(producerCtx, selected)
		}(backend)

		// Once the secure desktop has gone away, retry the low-overhead DXGI path
		// without interrupting the output. The writer repeats the latest frame for
		// the short handover interval.
		var retry <-chan time.Time
		var retryTimer *time.Timer
		if backend == captureBackendGDI && s.cfg.CaptureBackend == captureBackendAuto && supportsDesktopDuplication() && s.planSupportsDXGI() {
			retryTimer = time.NewTimer(2 * time.Minute)
			retry = retryTimer.C
		}

		select {
		case <-ctx.Done():
			cancel()
			<-done
			if retryTimer != nil {
				retryTimer.Stop()
			}
			return
		case <-retry:
			cancel()
			<-done
			if retryTimer != nil {
				retryTimer.Stop()
			}
			s.app.appendLog("Повторная проверка Desktop Duplication")
			backend = captureBackendDXGI
			continue
		case err := <-done:
			cancel()
			if retryTimer != nil {
				retryTimer.Stop()
			}
			if ctx.Err() != nil {
				return
			}
			if backend == captureBackendDXGI {
				if err != nil {
					s.app.appendLog("Desktop Duplication временно недоступен: " + err.Error())
				}
				s.app.appendLog("Переход на совместимый захват без разрыва потока")
				backend = captureBackendGDI
				continue
			}
			if err != nil {
				s.app.appendLog("Совместимый захват остановился: " + err.Error())
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			backend = captureBackendGDI
		}
	}
}

func (s *captureSupervisor) runProducer(ctx context.Context, backend string) error {
	var cmd *exec.Cmd
	var err error
	if backend == captureBackendDXGI {
		args, buildErr := buildDXGICaptureArgs(s.cfg, s.plan)
		if buildErr != nil {
			return buildErr
		}
		cmd = exec.CommandContext(ctx, resolveExecutable(s.cfg.FFmpegPath), args...)
	} else {
		exe, exeErr := os.Executable()
		if exeErr != nil {
			return exeErr
		}
		cmd = exec.CommandContext(ctx, exe,
			"--gdi-capture",
			strconv.Itoa(s.plan.X), strconv.Itoa(s.plan.Y),
			strconv.Itoa(s.plan.Width), strconv.Itoa(s.plan.Height),
			strconv.Itoa(s.plan.OutputWidth), strconv.Itoa(s.plan.OutputHeight),
			strconv.Itoa(s.cfg.FPS), boolText(s.cfg.Cursor),
		)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	hideChildWindow(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	if !s.normalPriority {
		lowerProcessPriority(cmd.Process.Pid)
	}

	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		s.scanProducerLog(backend, stderr)
	}()

	readErr := s.readFrames(ctx, stdout)
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	<-stderrDone
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return readErr
	}
	if waitErr != nil {
		return waitErr
	}
	return io.EOF
}

func (s *captureSupervisor) readFrames(ctx context.Context, r io.Reader) error {
	for {
		if _, err := io.ReadFull(r, s.scratch); err != nil {
			return err
		}
		if s.privacy != nil {
			applyPrivacyPixelation(s.scratch, s.plan, s.privacy.Regions())
		}
		s.mu.Lock()
		if !s.secureActive {
			s.latest, s.scratch = s.scratch, s.latest
			s.hasFrame = true
		}
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

func (s *captureSupervisor) writeFrames(ctx context.Context, w io.WriteCloser) error {
	defer w.Close()
	period := time.Second / time.Duration(s.cfg.FPS)
	ticker := time.NewTicker(period)
	defer ticker.Stop()

	// Never hold the supervisor lock while writing a large raw frame to FFmpeg.
	// A blocked pipe must not delay a Windows LOCK event or secure-desktop frame:
	// otherwise one old desktop frame could be emitted when the pipe recovers.
	output := make([]byte, s.frameLen)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			s.mu.RLock()
			frame := selectOutputFrame(
				s.latest, s.secureFrame, s.lockedFrame, s.protectedFrame,
				s.sessionLocked, s.displayOff, s.secureActive, s.secureHasFrame,
			)
			copy(output, frame)
			s.mu.RUnlock()
			if err := writeFull(w, output); err != nil {
				return err
			}
		}
	}
}

func selectOutputFrame(latest, secureFrame, lockedFrame, protectedFrame []byte, sessionLocked, displayOff, secureActive, secureHasFrame bool) []byte {
	// The branded LinkVideo frame is shown only after Windows reports that the
	// console display has actually powered off. A normal Win+L must continue to
	// show the real Winlogon desktop whenever the SYSTEM helper can capture it.
	if sessionLocked && displayOff {
		return lockedFrame
	}
	if secureActive && secureHasFrame {
		return secureFrame
	}
	// During the short Win+L handover keep the last real user frame instead of
	// flashing an artificial placeholder. The secure frame replaces it as soon
	// as Windows exposes the protected desktop.
	return latest
}

func (s *captureSupervisor) handleSecureFrame(frame []byte, active bool) {
	s.mu.Lock()
	changed := s.secureActive != active
	s.secureActive = active
	if !active {
		s.secureHasFrame = false
	} else if len(frame) >= s.frameLen {
		incoming := frame[:s.frameLen]
		// Never accept a completely black protected-desktop surface as a real
		// frame. Preserve the last valid Winlogon image. The branded screen is
		// selected only from the separate Windows display-power notification.
		if !secureFrameLooksBlank(incoming) {
			copy(s.secureFrame, incoming)
			s.secureHasFrame = true
		}
	}
	s.mu.Unlock()
	if changed {
		if active {
			s.app.appendLog("Захват переключён на защищённый рабочий стол Windows")
		} else {
			s.app.appendLog("Захват возвращён на рабочий стол пользователя")
		}
	}
}

func (s *captureSupervisor) handleSessionState(locked bool) {
	s.mu.Lock()
	changed := s.sessionLocked != locked
	s.sessionLocked = locked
	secure := s.secure
	s.mu.Unlock()
	if secure != nil {
		secure.SetSessionLocked(locked)
	}
	if !changed {
		return
	}
	s.app.mu.Lock()
	s.app.sessionLocked = locked
	s.app.mu.Unlock()
	if locked {
		s.app.appendLog("Сеанс Windows заблокирован; RTSP-поток продолжен без перезапуска")
	} else {
		s.app.appendLog("Сеанс Windows разблокирован; RTSP-поток продолжен без перезапуска")
	}
}

func (s *captureSupervisor) handleDisplayPowerState(off bool) {
	s.mu.Lock()
	changed := s.displayOff != off
	s.displayOff = off
	locked := s.sessionLocked
	s.mu.Unlock()
	if !changed {
		return
	}
	if off {
		if locked {
			s.app.appendLog("Windows выключила дисплеи; включён фирменный экран LinkVideo без перезапуска потока")
		} else {
			s.app.appendLog("Windows выключила дисплеи")
		}
	} else {
		s.app.appendLog("Дисплеи включены; восстановлен обычный захват экрана без перезапуска потока")
	}
}

func writeFull(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func boolText(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func (s *captureSupervisor) scanProducerLog(backend string, r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "acquirenextframe failed") || strings.Contains(lower, "887a0026") {
			// The supervisor will switch backends; keep one concise diagnostic line.
			s.app.appendLog("Windows временно закрыл доступ к Desktop Duplication")
			continue
		}
		if strings.Contains(lower, "error") || strings.Contains(lower, "failed") || strings.Contains(lower, "conversion failed") {
			s.app.appendLog("capture/" + backend + ": " + line)
		}
	}
}

func buildDXGICaptureArgs(cfg Config, plan capturePlan) ([]string, error) {
	monitors, err := listMonitors()
	if err != nil {
		return nil, fmt.Errorf("не удалось получить список мониторов: %w", err)
	}
	if len(monitors) == 0 {
		return nil, errors.New("Windows не обнаружила подключённые мониторы")
	}

	source := func(m Monitor, label string) string {
		return fmt.Sprintf(
			"ddagrab=output_idx=%d:framerate=%d:draw_mouse=%s:output_fmt=bgra:allow_fallback=1,hwdownload,format=bgra[%s]",
			m.OutputIndex, cfg.FPS, boolText(cfg.Cursor), label,
		)
	}

	var filter string
	finish := func(input string) string {
		chain := fmt.Sprintf("[%s]crop=%d:%d:0:0", input, plan.Width, plan.Height)
		if plan.OutputWidth != plan.Width || plan.OutputHeight != plan.Height {
			chain += fmt.Sprintf(",scale=%d:%d:flags=fast_bilinear", plan.OutputWidth, plan.OutputHeight)
		}
		return chain + "[capture]"
	}
	if plan.Mode == "monitor" {
		m, ok := selectedMonitor(cfg, monitors)
		if !ok {
			return nil, errors.New("выбранный монитор не найден")
		}
		filter = source(m, "raw") + ";" + finish("raw")
	} else if len(monitors) == 1 {
		filter = source(monitors[0], "raw") + ";" + finish("raw")
	} else {
		minX, minY := monitors[0].X, monitors[0].Y
		for _, m := range monitors[1:] {
			if m.X < minX {
				minX = m.X
			}
			if m.Y < minY {
				minY = m.Y
			}
		}
		parts := make([]string, 0, len(monitors)+1)
		labels := strings.Builder{}
		layout := make([]string, 0, len(monitors))
		for i, m := range monitors {
			label := fmt.Sprintf("v%d", i)
			parts = append(parts, source(m, label))
			labels.WriteString("[" + label + "]")
			layout = append(layout, fmt.Sprintf("%d_%d", m.X-minX, m.Y-minY))
		}
		stack := fmt.Sprintf("%sxstack=inputs=%d:layout=%s:fill=black[stacked]",
			labels.String(), len(monitors), strings.Join(layout, "|"))
		parts = append(parts, stack)
		parts = append(parts, finish("stacked"))
		filter = strings.Join(parts, ";")
	}

	return []string{
		"-hide_banner", "-loglevel", "warning",
		"-filter_complex", filter,
		"-map", "[capture]",
		"-an", "-sn", "-dn",
		"-pix_fmt", "bgra",
		"-fps_mode", "cfr",
		"-f", "rawvideo", "pipe:1",
	}, nil
}

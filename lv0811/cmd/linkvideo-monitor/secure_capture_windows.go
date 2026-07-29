//go:build windows

package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	secureMapMagic      = 0x4C564D53 // "SMVL"
	secureMapVersion    = 1
	secureMapHeaderSize = 64

	pageReadWrite       = 0x04
	fileMapAllAccess    = 0x000F001F
	uoiName             = 2
	desktopReadObjs     = 0x0001
	desktopCreateWindow = 0x0002
	desktopWriteObjs    = 0x0080
)

var (
	kernel32Secure                 = syscall.NewLazyDLL("kernel32.dll")
	procCreateFileMappingWSecure   = kernel32Secure.NewProc("CreateFileMappingW")
	procOpenFileMappingWSecure     = kernel32Secure.NewProc("OpenFileMappingW")
	procMapViewOfFileSecure        = kernel32Secure.NewProc("MapViewOfFile")
	procUnmapViewOfFileSecure      = kernel32Secure.NewProc("UnmapViewOfFile")
	procCloseHandleSecure          = kernel32Secure.NewProc("CloseHandle")
	procProcessIdToSessionIdSecure = kernel32Secure.NewProc("ProcessIdToSessionId")
	procGetCurrentProcessIdSecure  = kernel32Secure.NewProc("GetCurrentProcessId")

	procOpenInputDesktopSecure   = user32.NewProc("OpenInputDesktop")
	procOpenDesktopWSecure       = user32.NewProc("OpenDesktopW")
	procSetThreadDesktopSecure   = user32.NewProc("SetThreadDesktop")
	procCloseDesktopSecure       = user32.NewProc("CloseDesktop")
	procGetUserObjectInfoWSecure = user32.NewProc("GetUserObjectInformationW")
)

type secureCaptureRequest struct {
	SessionID    uint32 `json:"session_id"`
	ClientPID    uint32 `json:"client_pid"`
	MappingName  string `json:"mapping_name"`
	X            int    `json:"x"`
	Y            int    `json:"y"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	OutputWidth  int    `json:"output_width"`
	OutputHeight int    `json:"output_height"`
	FPS          int    `json:"fps"`
	Cursor       bool   `json:"cursor"`
	Privacy      bool   `json:"privacy"`
	UpdatedUnix  int64  `json:"updated_unix_ms"`
}

type windowsSecureDesktopBridge struct {
	plan        capturePlan
	cfg         Config
	sessionID   uint32
	mappingName string
	mapping     uintptr
	view        uintptr
	memory      []byte
	requestPath string
	frameBytes  int
	closeOnce   sync.Once
}

func newSecureDesktopBridge(plan capturePlan, cfg Config) (secureDesktopBridge, error) {
	sessionID, err := currentProcessSessionID()
	if err != nil {
		return nil, err
	}
	frameBytes := plan.OutputWidth * plan.OutputHeight * 4
	if frameBytes <= 0 {
		return nil, errors.New("invalid secure frame size")
	}
	mappingName := fmt.Sprintf(`Local\LinkVideoMonitorSecure_%d`, sessionID)
	namePtr, _ := syscall.UTF16PtrFromString(mappingName)
	total := secureMapHeaderSize + frameBytes
	mapping, _, callErr := procCreateFileMappingWSecure.Call(
		^uintptr(0), 0, pageReadWrite, 0, uintptr(uint32(total)), uintptr(unsafe.Pointer(namePtr)),
	)
	if mapping == 0 {
		return nil, fmt.Errorf("CreateFileMapping: %v", callErr)
	}
	view, _, callErr := procMapViewOfFileSecure.Call(mapping, fileMapAllAccess, 0, 0, uintptr(total))
	if view == 0 {
		procCloseHandleSecure.Call(mapping)
		return nil, fmt.Errorf("MapViewOfFile: %v", callErr)
	}
	memory := unsafe.Slice((*byte)(unsafe.Pointer(view)), total)
	for i := range memory {
		memory[i] = 0
	}
	binary.LittleEndian.PutUint32(memory[0:4], secureMapMagic)
	binary.LittleEndian.PutUint32(memory[4:8], secureMapVersion)
	binary.LittleEndian.PutUint32(memory[20:24], uint32(plan.OutputWidth))
	binary.LittleEndian.PutUint32(memory[24:28], uint32(plan.OutputHeight))
	binary.LittleEndian.PutUint32(memory[28:32], uint32(frameBytes))

	sessionsDir := filepath.Join(os.Getenv("PROGRAMDATA"), "LinkVideo.Monitor", "Sessions")
	if strings.TrimSpace(os.Getenv("PROGRAMDATA")) == "" {
		procUnmapViewOfFileSecure.Call(view)
		procCloseHandleSecure.Call(mapping)
		return nil, errors.New("PROGRAMDATA is unavailable")
	}
	if err := os.MkdirAll(sessionsDir, 0o777); err != nil {
		procUnmapViewOfFileSecure.Call(view)
		procCloseHandleSecure.Call(mapping)
		return nil, fmt.Errorf("create service request directory: %w", err)
	}
	return &windowsSecureDesktopBridge{
		plan: plan, cfg: cfg, sessionID: sessionID, mappingName: mappingName,
		mapping: mapping, view: view, memory: memory, frameBytes: frameBytes,
		requestPath: filepath.Join(sessionsDir, fmt.Sprintf("session-%d.json", sessionID)),
	}, nil
}

func currentProcessSessionID() (uint32, error) {
	pid, _, _ := procGetCurrentProcessIdSecure.Call()
	var sessionID uint32
	ok, _, callErr := procProcessIdToSessionIdSecure.Call(pid, uintptr(unsafe.Pointer(&sessionID)))
	if ok == 0 {
		return 0, fmt.Errorf("ProcessIdToSessionId: %v", callErr)
	}
	return sessionID, nil
}

func (b *windowsSecureDesktopBridge) request() secureCaptureRequest {
	return secureCaptureRequest{
		SessionID: b.sessionID, ClientPID: uint32(os.Getpid()), MappingName: b.mappingName,
		X: b.plan.X, Y: b.plan.Y, Width: b.plan.Width, Height: b.plan.Height,
		OutputWidth: b.plan.OutputWidth, OutputHeight: b.plan.OutputHeight,
		FPS: b.cfg.FPS, Cursor: b.cfg.Cursor, Privacy: b.cfg.PrivacyProtection,
		UpdatedUnix: time.Now().UnixMilli(),
	}
}

func (b *windowsSecureDesktopBridge) writeRequest() error {
	req := b.request()
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	dir := filepath.Dir(b.requestPath)
	tmp, err := os.CreateTemp(dir, "session-request-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := writeFull(tmp, data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return replaceFileAtomically(tmpPath, b.requestPath)
}

func (b *windowsSecureDesktopBridge) Run(ctx context.Context, handler secureFrameHandler) {
	_ = b.writeRequest()
	heartbeat := time.NewTicker(2 * time.Second)
	poll := time.NewTicker(30 * time.Millisecond)
	defer heartbeat.Stop()
	defer poll.Stop()

	frame := make([]byte, b.frameBytes)
	var lastSeq uint64
	lastActive := false
	for {
		select {
		case <-ctx.Done():
			if lastActive {
				handler(nil, false)
			}
			return
		case <-heartbeat.C:
			_ = b.writeRequest()
		case <-poll.C:
			active := atomic.LoadUint32((*uint32)(unsafe.Pointer(&b.memory[16]))) != 0
			forcedLocked := atomic.LoadUint32((*uint32)(unsafe.Pointer(&b.memory[40]))) != 0
			timestamp := int64(atomic.LoadUint64((*uint64)(unsafe.Pointer(&b.memory[32]))))
			fresh := time.Since(time.UnixMilli(timestamp)) < 1500*time.Millisecond
			// Desktop Duplication may stop producing frames while the lock screen is
			// perfectly static. Keep the last confirmed Winlogon frame active until
			// Windows reports unlock; otherwise the branded fallback reappears after
			// one or two seconds and vanishes only when a key causes a new frame.
			if active && (fresh || (forcedLocked && lastActive && lastSeq != 0)) {
				seq1 := atomic.LoadUint64((*uint64)(unsafe.Pointer(&b.memory[8])))
				if seq1 != 0 && seq1&1 == 0 && seq1 != lastSeq {
					if int(binary.LittleEndian.Uint32(b.memory[20:24])) == b.plan.OutputWidth &&
						int(binary.LittleEndian.Uint32(b.memory[24:28])) == b.plan.OutputHeight &&
						int(binary.LittleEndian.Uint32(b.memory[28:32])) == b.frameBytes {
						copy(frame, b.memory[secureMapHeaderSize:secureMapHeaderSize+b.frameBytes])
						seq2 := atomic.LoadUint64((*uint64)(unsafe.Pointer(&b.memory[8])))
						if seq1 == seq2 && seq2&1 == 0 {
							lastSeq = seq2
							handler(frame, true)
							lastActive = true
							continue
						}
					}
				}
				if !lastActive {
					handler(nil, true)
					lastActive = true
				}
				continue
			}
			if lastActive {
				handler(nil, false)
				lastActive = false
			}
		}
	}
}

func (b *windowsSecureDesktopBridge) SetSessionLocked(locked bool) {
	if b == nil || b.memory == nil || len(b.memory) < 44 {
		return
	}
	var value uint32
	if locked {
		value = 1
	}
	atomic.StoreUint32((*uint32)(unsafe.Pointer(&b.memory[40])), value)
}

func (b *windowsSecureDesktopBridge) Close() error {
	b.closeOnce.Do(func() {
		_ = os.Remove(b.requestPath)
		if b.memory != nil {
			atomic.StoreUint32((*uint32)(unsafe.Pointer(&b.memory[16])), 0)
		}
		if b.view != 0 {
			procUnmapViewOfFileSecure.Call(b.view)
			b.view = 0
		}
		if b.mapping != 0 {
			procCloseHandleSecure.Call(b.mapping)
			b.mapping = 0
		}
		b.memory = nil
	})
	return nil
}

type secureMonitorFallback struct {
	Region secureMonitorFallbackRegion
	Frame  []byte
}

type secureMapWriter struct {
	memory                    []byte
	frameBytes                int
	plan                      capturePlan
	privacy                   privacyTracker
	secondaryMonitorFallbacks []secureMonitorFallback
	hasNonBlankFrame          bool
	blankFramesIgnored        uint64
	lastBlankLog              time.Time
	lastSecondaryFallbackLog  time.Time
}

func (w *secureMapWriter) SetInactive() {
	if w == nil || len(w.memory) < 20 {
		return
	}
	w.hasNonBlankFrame = false
	w.blankFramesIgnored = 0
	w.lastBlankLog = time.Time{}
	atomic.StoreUint32((*uint32)(unsafe.Pointer(&w.memory[16])), 0)
	atomic.StoreUint64((*uint64)(unsafe.Pointer(&w.memory[32])), uint64(time.Now().UnixMilli()))
}

func (w *secureMapWriter) Write(frame []byte) (int, error) {
	if len(frame) < w.frameBytes {
		return 0, ioErrShortBuffer
	}
	forcedLocked := atomic.LoadUint32((*uint32)(unsafe.Pointer(&w.memory[40]))) != 0
	active := secureDesktopIsInput()
	_ = forcedLocked // session lock is only a wake-up hint; the Winlogon desktop must be confirmed
	if !active {
		atomic.StoreUint32((*uint32)(unsafe.Pointer(&w.memory[16])), 0)
		atomic.StoreUint64((*uint64)(unsafe.Pointer(&w.memory[32])), uint64(time.Now().UnixMilli()))
		return len(frame), nil
	}
	if w.privacy != nil {
		applyPrivacyPixelation(frame[:w.frameBytes], w.plan, w.privacy.Regions())
	}

	secondaryFallbacks := 0
	for _, fallback := range w.secondaryMonitorFallbacks {
		if secureRegionLooksBlank(frame[:w.frameBytes], w.plan.OutputWidth, w.plan.OutputHeight, fallback.Region) &&
			copyBGRARegion(frame[:w.frameBytes], w.plan.OutputWidth, w.plan.OutputHeight, fallback.Region, fallback.Frame) {
			secondaryFallbacks++
		}
	}
	if secondaryFallbacks > 0 && (w.lastSecondaryFallbackLog.IsZero() || time.Since(w.lastSecondaryFallbackLog) >= 10*time.Second) {
		secureAgentLog(fmt.Sprintf("replaced black Winlogon output on %d secondary monitor(s)", secondaryFallbacks))
		w.lastSecondaryFallbackLog = time.Now()
	}
	blank := secureFrameLooksBlank(frame[:w.frameBytes])
	if blank {
		// Some secure-desktop transitions produce a valid-sized but completely
		// black duplication surface. Never publish it as a real Winlogon frame.
		// The main process decides separately whether the physical display is off
		// by using Windows power notifications, not by guessing from pixel data.
		w.blankFramesIgnored++
		atomic.StoreUint32((*uint32)(unsafe.Pointer(&w.memory[16])), 1)
		atomic.StoreUint64((*uint64)(unsafe.Pointer(&w.memory[32])), uint64(time.Now().UnixMilli()))
		if w.lastBlankLog.IsZero() || time.Since(w.lastBlankLog) >= 10*time.Second {
			message := "ignored blank Winlogon frame; waiting for a real protected-desktop image"
			if w.hasNonBlankFrame {
				message = "ignored blank Winlogon frame; preserving last real frame"
			}
			secureAgentLog(fmt.Sprintf("%s (count=%d)", message, w.blankFramesIgnored))
			w.lastBlankLog = time.Now()
		}
		return len(frame), nil
	}
	w.hasNonBlankFrame = true
	seq := atomic.LoadUint64((*uint64)(unsafe.Pointer(&w.memory[8])))
	if seq&1 == 1 {
		seq++
	}
	atomic.StoreUint64((*uint64)(unsafe.Pointer(&w.memory[8])), seq+1)
	copy(w.memory[secureMapHeaderSize:secureMapHeaderSize+w.frameBytes], frame[:w.frameBytes])
	binary.LittleEndian.PutUint32(w.memory[20:24], uint32(w.plan.OutputWidth))
	binary.LittleEndian.PutUint32(w.memory[24:28], uint32(w.plan.OutputHeight))
	binary.LittleEndian.PutUint32(w.memory[28:32], uint32(w.frameBytes))
	atomic.StoreUint64((*uint64)(unsafe.Pointer(&w.memory[32])), uint64(time.Now().UnixMilli()))
	atomic.StoreUint32((*uint32)(unsafe.Pointer(&w.memory[16])), 1)
	atomic.StoreUint64((*uint64)(unsafe.Pointer(&w.memory[8])), seq+2)
	return len(frame), nil
}

var ioErrShortBuffer = errors.New("short secure frame buffer")

func bindCurrentThreadToWinlogonDesktop() (func(), error) {
	name, _ := syscall.UTF16PtrFromString("Winlogon")
	desktop, _, callErr := procOpenDesktopWSecure.Call(
		uintptr(unsafe.Pointer(name)), 0, 0,
		desktopReadObjs|desktopCreateWindow|desktopWriteObjs,
	)
	if desktop == 0 {
		return func() {}, fmt.Errorf("OpenDesktop(Winlogon): %v", callErr)
	}
	ok, _, callErr := procSetThreadDesktopSecure.Call(desktop)
	if ok == 0 {
		procCloseDesktopSecure.Call(desktop)
		return func() {}, fmt.Errorf("SetThreadDesktop(Winlogon): %v", callErr)
	}
	return func() { procCloseDesktopSecure.Call(desktop) }, nil
}

func runSecureGDICapture(args []string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	releaseDesktop, err := bindCurrentThreadToWinlogonDesktop()
	if err != nil {
		return err
	}
	defer releaseDesktop()
	if len(args) < 10 {
		return errors.New("usage: --secure-gdi-capture mapping x y width height output_width output_height fps cursor privacy")
	}
	mappingName := args[0]
	parse := func(i int) int { v, _ := strconv.Atoi(args[i]); return v }
	x, y := parse(1), parse(2)
	width, height := parse(3), parse(4)
	outputWidth, outputHeight := parse(5), parse(6)
	fps := parse(7)
	cursor := args[8] == "1" || strings.EqualFold(args[8], "true")
	privacyEnabled := args[9] == "1" || strings.EqualFold(args[9], "true")
	frameBytes := outputWidth * outputHeight * 4
	if frameBytes <= 0 {
		return errors.New("invalid secure frame dimensions")
	}
	namePtr, _ := syscall.UTF16PtrFromString(mappingName)
	mapping, _, callErr := procOpenFileMappingWSecure.Call(fileMapAllAccess, 0, uintptr(unsafe.Pointer(namePtr)))
	if mapping == 0 {
		return fmt.Errorf("OpenFileMapping: %v", callErr)
	}
	defer procCloseHandleSecure.Call(mapping)
	total := secureMapHeaderSize + frameBytes
	view, _, callErr := procMapViewOfFileSecure.Call(mapping, fileMapAllAccess, 0, 0, uintptr(total))
	if view == 0 {
		return fmt.Errorf("MapViewOfFile: %v", callErr)
	}
	defer procUnmapViewOfFileSecure.Call(view)
	memory := unsafe.Slice((*byte)(unsafe.Pointer(view)), total)
	if binary.LittleEndian.Uint32(memory[0:4]) != secureMapMagic {
		return errors.New("secure frame mapping has invalid signature")
	}
	plan := capturePlan{X: x, Y: y, Width: width, Height: height, OutputWidth: outputWidth, OutputHeight: outputHeight}
	writer := &secureMapWriter{memory: memory, frameBytes: frameBytes, plan: plan}
	if monitors, monitorErr := listMonitors(); monitorErr == nil {
		for _, region := range secondaryMonitorFallbackRegions(plan, monitors) {
			writer.secondaryMonitorFallbacks = append(writer.secondaryMonitorFallbacks, secureMonitorFallback{
				Region: region,
				Frame:  makeSessionLockedFrame(region.Width, region.Height),
			})
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if privacyEnabled {
		writer.privacy = newPrivacyTracker()
		go writer.privacy.Run(ctx)
	}
	return runSecureDesktopCapture(writer, secureCaptureRequest{
		MappingName: mappingName, X: x, Y: y, Width: width, Height: height,
		OutputWidth: outputWidth, OutputHeight: outputHeight, FPS: fps,
		Cursor: cursor, Privacy: privacyEnabled,
	})
}

func secureDesktopIsInput() bool {
	desktop, _, _ := procOpenInputDesktopSecure.Call(0, 0, desktopReadObjs)
	if desktop == 0 {
		return false
	}
	defer procCloseDesktopSecure.Call(desktop)
	var needed uint32
	_, _, _ = procGetUserObjectInfoWSecure.Call(desktop, uoiName, 0, 0, uintptr(unsafe.Pointer(&needed)))
	if needed == 0 || needed > 512 {
		return false
	}
	buf := make([]uint16, int(needed/2)+1)
	ok, _, _ := procGetUserObjectInfoWSecure.Call(desktop, uoiName, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2), uintptr(unsafe.Pointer(&needed)))
	if ok == 0 {
		return false
	}
	name := strings.TrimSpace(syscall.UTF16ToString(buf))
	return strings.EqualFold(name, "Winlogon")
}

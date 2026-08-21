//go:build darwin

package main

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxMacOSCompositeFrameBytes int64 = 512 << 20

type macOSCaptureTile struct {
	DisplayID uint32
	X         int
	Y         int
	Width     int
	Height    int
}

type macOSCaptureStream struct {
	tile   macOSCaptureTile
	cmd    *exec.Cmd
	mu     sync.Mutex
	latest []byte
	ready  bool
}

// runGDICapture is the platform capture entrypoint used by captureSupervisor.
// On macOS the historical function name is kept only to preserve the existing
// process boundary: the actual producers are ScreenCaptureKit streams.
func runGDICapture(out io.Writer, x, y, width, height, outputWidth, outputHeight, fps int, drawCursor bool) error {
	helper, err := macOSCaptureHelperPath()
	if err != nil {
		return err
	}
	if outputWidth < 2 || outputHeight < 2 || width < 2 || height < 2 {
		return errors.New("invalid macOS capture dimensions")
	}
	if _, err := macOSRawFrameSize(outputWidth, outputHeight); err != nil {
		return err
	}
	if fps < 1 {
		fps = 15
	}

	// A selected monitor (or a one-monitor desktop) remains a single native
	// ScreenCaptureKit stream. This also preserves the existing Retina path.
	if displayID := macOSDisplayIDForCaptureRect(x, y, width, height); displayID != 0 {
		return runMacOSDisplayCapture(helper, out, displayID, outputWidth, outputHeight, fps, drawCursor)
	}

	displays, err := macOSDisplayInfos()
	if err != nil {
		return err
	}
	tiles := macOSCaptureTiles(displays, x, y, width, height, outputWidth, outputHeight)
	if len(tiles) == 0 {
		return errors.New("не удалось сопоставить дисплеи macOS с областью захвата")
	}
	if len(tiles) == 1 && tiles[0].X == 0 && tiles[0].Y == 0 && tiles[0].Width == outputWidth && tiles[0].Height == outputHeight {
		return runMacOSDisplayCapture(helper, out, tiles[0].DisplayID, outputWidth, outputHeight, fps, drawCursor)
	}
	return runMacOSCompositeCapture(helper, out, tiles, outputWidth, outputHeight, fps, drawCursor)
}

func runMacOSDisplayCapture(helper string, out io.Writer, displayID uint32, width, height, fps int, drawCursor bool) error {
	args := macOSDisplayCaptureArgs(displayID, width, height, fps, drawCursor)
	cmd := exec.Command(helper, args...)
	cmd.Stdout = out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ScreenCaptureKit helper: %w", err)
	}
	return nil
}

func macOSDisplayCaptureArgs(displayID uint32, width, height, fps int, drawCursor bool) []string {
	return []string{
		"--capture",
		"--display-id", strconv.FormatUint(uint64(displayID), 10),
		"--width", strconv.Itoa(width),
		"--height", strconv.Itoa(height),
		"--fps", strconv.Itoa(fps),
		"--cursor", strconv.FormatBool(drawCursor),
	}
}

// macOSCaptureTiles maps the common ScreenCaptureKit point coordinate space to
// the final raw-video pixel canvas. Mapping display edges rather than widths
// avoids one-pixel seams when HD/Full-HD scaling produces fractional ratios.
func macOSCaptureTiles(displays []macOSDisplayInfo, x, y, width, height, outputWidth, outputHeight int) []macOSCaptureTile {
	if width < 2 || height < 2 || outputWidth < 2 || outputHeight < 2 {
		return nil
	}
	scaleX := float64(outputWidth) / float64(width)
	scaleY := float64(outputHeight) / float64(height)
	captureRight := x + width
	captureBottom := y + height
	tiles := make([]macOSCaptureTile, 0, len(displays))

	for _, display := range displays {
		dx := int(math.Round(display.X))
		dy := int(math.Round(display.Y))
		dw, dh := display.WidthPoints, display.HeightPoints
		if dw < 2 || dh < 2 {
			dw, dh = display.WidthPixels, display.HeightPixels
		}
		dw, dh = even(dw), even(dh)
		if dw < 2 || dh < 2 {
			continue
		}
		// Current shared capture plans are either one complete monitor or the
		// complete virtual desktop. Ignore any display outside that desktop.
		if dx < x || dy < y || dx+dw > captureRight || dy+dh > captureBottom {
			continue
		}

		left := int(math.Round(float64(dx-x) * scaleX))
		top := int(math.Round(float64(dy-y) * scaleY))
		right := int(math.Round(float64(dx+dw-x) * scaleX))
		bottom := int(math.Round(float64(dy+dh-y) * scaleY))
		if left < 0 {
			left = 0
		}
		if top < 0 {
			top = 0
		}
		if right > outputWidth {
			right = outputWidth
		}
		if bottom > outputHeight {
			bottom = outputHeight
		}
		if right-left < 2 || bottom-top < 2 {
			continue
		}
		tiles = append(tiles, macOSCaptureTile{
			DisplayID: display.ID,
			X:         left,
			Y:         top,
			Width:     right - left,
			Height:    bottom - top,
		})
	}
	return tiles
}

func runMacOSCompositeCapture(helper string, out io.Writer, tiles []macOSCaptureTile, outputWidth, outputHeight, fps int, drawCursor bool) error {
	canvasSize, err := macOSRawFrameSize(outputWidth, outputHeight)
	if err != nil {
		return err
	}
	streams := make([]*macOSCaptureStream, 0, len(tiles))
	errCh := make(chan error, len(tiles)*2)
	defer func() {
		for _, stream := range streams {
			if stream.cmd != nil && stream.cmd.Process != nil {
				_ = stream.cmd.Process.Kill()
			}
		}
	}()

	for _, tile := range tiles {
		frameSize, sizeErr := macOSRawFrameSize(tile.Width, tile.Height)
		if sizeErr != nil {
			return sizeErr
		}
		cmd := exec.Command(helper, macOSDisplayCaptureArgs(tile.DisplayID, tile.Width, tile.Height, fps, drawCursor)...)
		stdout, pipeErr := cmd.StdoutPipe()
		if pipeErr != nil {
			return pipeErr
		}
		cmd.Stderr = os.Stderr
		if startErr := cmd.Start(); startErr != nil {
			return fmt.Errorf("ScreenCaptureKit display %d: %w", tile.DisplayID, startErr)
		}
		stream := &macOSCaptureStream{tile: tile, cmd: cmd}
		streams = append(streams, stream)

		go func(stream *macOSCaptureStream, reader io.Reader, size int) {
			readBuffer := make([]byte, size)
			for {
				if _, readErr := io.ReadFull(reader, readBuffer); readErr != nil {
					waitErr := stream.cmd.Wait()
					if waitErr != nil {
						readErr = waitErr
					}
					select {
					case errCh <- fmt.Errorf("ScreenCaptureKit display %d stopped: %w", stream.tile.DisplayID, readErr):
					default:
					}
					return
				}

				stream.mu.Lock()
				if stream.latest == nil {
					stream.latest = readBuffer
					readBuffer = make([]byte, size)
				} else {
					stream.latest, readBuffer = readBuffer, stream.latest
				}
				stream.ready = true
				stream.mu.Unlock()
			}
		}(stream, stdout, frameSize)
	}

	canvas := make([]byte, canvasSize)
	interval := time.Second / time.Duration(fps)
	if interval <= 0 {
		interval = time.Second / 15
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case streamErr := <-errCh:
			return streamErr
		case <-ticker.C:
			if !macOSCompositeFrame(canvas, outputWidth, outputHeight, streams) {
				continue
			}
			if err := writeFull(out, canvas); err != nil {
				return err
			}
		}
	}
}

// macOSCompositeFrame copies the newest complete frame from every display.
// False means at least one display has not delivered its first frame yet.
func macOSCompositeFrame(canvas []byte, outputWidth, outputHeight int, streams []*macOSCaptureStream) bool {
	if len(canvas) != outputWidth*outputHeight*4 {
		return false
	}
	for _, stream := range streams {
		stream.mu.Lock()
		ready := stream.ready && len(stream.latest) == stream.tile.Width*stream.tile.Height*4
		stream.mu.Unlock()
		if !ready {
			return false
		}
	}

	for _, stream := range streams {
		stream.mu.Lock()
		tile := stream.tile
		for row := 0; row < tile.Height; row++ {
			srcStart := row * tile.Width * 4
			dstStart := ((tile.Y+row)*outputWidth + tile.X) * 4
			copy(canvas[dstStart:dstStart+tile.Width*4], stream.latest[srcStart:srcStart+tile.Width*4])
		}
		stream.mu.Unlock()
	}
	return true
}

func macOSRawFrameSize(width, height int) (int, error) {
	if width < 2 || height < 2 {
		return 0, fmt.Errorf("invalid macOS frame size %dx%d", width, height)
	}
	size := int64(width) * int64(height) * 4
	if size <= 0 || size > maxMacOSCompositeFrameBytes {
		return 0, fmt.Errorf("macOS frame is too large: %dx%d", width, height)
	}
	return int(size), nil
}

func macOSCaptureHelperPath() (string, error) {
	if value := strings.TrimSpace(os.Getenv("LINKVIDEO_CAPTURE_HELPER")); value != "" {
		return value, nil
	}

	exe, err := os.Executable()
	if err == nil && exe != "" {
		base := filepath.Dir(exe)
		candidates := []string{
			filepath.Clean(filepath.Join(base, "..", "Resources", "linkvideo-capture-helper")),
			filepath.Join(base, "linkvideo-capture-helper"),
		}
		for _, candidate := range candidates {
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				return candidate, nil
			}
		}
	}

	if path, lookErr := exec.LookPath("linkvideo-capture-helper"); lookErr == nil {
		return path, nil
	}
	return "", errors.New("не найден macOS ScreenCaptureKit helper")
}

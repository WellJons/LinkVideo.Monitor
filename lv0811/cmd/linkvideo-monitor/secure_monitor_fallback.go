package main

// secureMonitorFallbackRegion describes the portion of a physical monitor in
// the final raw-video frame produced for the selected capture plan.
type secureMonitorFallbackRegion struct {
	X      int
	Y      int
	Width  int
	Height int
}

// captureMonitorFallbackRegions maps every physical monitor intersecting the
// capture rectangle into output-frame coordinates. Unlike
// secondaryMonitorFallbackRegions, primary displays are included as well.
func captureMonitorFallbackRegions(plan capturePlan, monitors []Monitor) []secureMonitorFallbackRegion {
	if plan.Width <= 0 || plan.Height <= 0 || plan.OutputWidth <= 0 || plan.OutputHeight <= 0 {
		return nil
	}
	captureLeft, captureTop := plan.X, plan.Y
	captureRight, captureBottom := plan.X+plan.Width, plan.Y+plan.Height
	regions := make([]secureMonitorFallbackRegion, 0, len(monitors))
	for _, monitor := range monitors {
		if monitor.Width <= 0 || monitor.Height <= 0 {
			continue
		}
		left := maxInt(captureLeft, monitor.X)
		top := maxInt(captureTop, monitor.Y)
		right := minInt(captureRight, monitor.X+monitor.Width)
		bottom := minInt(captureBottom, monitor.Y+monitor.Height)
		if right <= left || bottom <= top {
			continue
		}
		outLeft := (left - captureLeft) * plan.OutputWidth / plan.Width
		outTop := (top - captureTop) * plan.OutputHeight / plan.Height
		outRight := ((right-captureLeft)*plan.OutputWidth + plan.Width - 1) / plan.Width
		outBottom := ((bottom-captureTop)*plan.OutputHeight + plan.Height - 1) / plan.Height
		outLeft = clampInt(outLeft, 0, plan.OutputWidth)
		outTop = clampInt(outTop, 0, plan.OutputHeight)
		outRight = clampInt(outRight, 0, plan.OutputWidth)
		outBottom = clampInt(outBottom, 0, plan.OutputHeight)
		if outRight <= outLeft || outBottom <= outTop {
			continue
		}
		regions = append(regions, secureMonitorFallbackRegion{
			X: outLeft, Y: outTop, Width: outRight - outLeft, Height: outBottom - outTop,
		})
	}
	return regions
}

// makeSessionLockedCaptureFrame renders a complete fallback frame. For a
// multi-monitor desktop, every physical display receives its own centered
// LinkVideo logo and message instead of placing one message across a bezel.
func makeSessionLockedCaptureFrame(plan capturePlan, monitors []Monitor) []byte {
	frame := makeSessionLockedFrame(plan.OutputWidth, plan.OutputHeight)
	regions := captureMonitorFallbackRegions(plan, monitors)
	if len(regions) <= 1 {
		return frame
	}
	for _, region := range regions {
		regionFrame := makeSessionLockedFrame(region.Width, region.Height)
		_ = copyBGRARegion(frame, plan.OutputWidth, plan.OutputHeight, region, regionFrame)
	}
	return frame
}

// secondaryMonitorFallbackRegions maps every non-primary monitor that
// intersects the capture rectangle into output-frame coordinates. Windows
// normally renders the interactive Winlogon UI only on the primary display;
// other displays may therefore be returned as completely black surfaces.
func secondaryMonitorFallbackRegions(plan capturePlan, monitors []Monitor) []secureMonitorFallbackRegion {
	if plan.Width <= 0 || plan.Height <= 0 || plan.OutputWidth <= 0 || plan.OutputHeight <= 0 {
		return nil
	}
	captureLeft, captureTop := plan.X, plan.Y
	captureRight, captureBottom := plan.X+plan.Width, plan.Y+plan.Height
	regions := make([]secureMonitorFallbackRegion, 0, len(monitors))
	for _, monitor := range monitors {
		if monitor.Primary || monitor.Width <= 0 || monitor.Height <= 0 {
			continue
		}
		left := maxInt(captureLeft, monitor.X)
		top := maxInt(captureTop, monitor.Y)
		right := minInt(captureRight, monitor.X+monitor.Width)
		bottom := minInt(captureBottom, monitor.Y+monitor.Height)
		if right <= left || bottom <= top {
			continue
		}

		outLeft := (left - captureLeft) * plan.OutputWidth / plan.Width
		outTop := (top - captureTop) * plan.OutputHeight / plan.Height
		// Round the right/bottom edge upwards so no black one-pixel seam remains
		// after scaling a virtual desktop into a smaller output frame.
		outRight := ((right-captureLeft)*plan.OutputWidth + plan.Width - 1) / plan.Width
		outBottom := ((bottom-captureTop)*plan.OutputHeight + plan.Height - 1) / plan.Height
		outLeft = clampInt(outLeft, 0, plan.OutputWidth)
		outTop = clampInt(outTop, 0, plan.OutputHeight)
		outRight = clampInt(outRight, 0, plan.OutputWidth)
		outBottom = clampInt(outBottom, 0, plan.OutputHeight)
		if outRight <= outLeft || outBottom <= outTop {
			continue
		}
		regions = append(regions, secureMonitorFallbackRegion{
			X: outLeft, Y: outTop, Width: outRight - outLeft, Height: outBottom - outTop,
		})
	}
	return regions
}

func secureRegionLooksBlank(frame []byte, frameWidth, frameHeight int, region secureMonitorFallbackRegion) bool {
	if frameWidth <= 0 || frameHeight <= 0 || len(frame) < frameWidth*frameHeight*4 || region.Width <= 0 || region.Height <= 0 {
		return true
	}
	left := clampInt(region.X, 0, frameWidth)
	top := clampInt(region.Y, 0, frameHeight)
	right := clampInt(region.X+region.Width, 0, frameWidth)
	bottom := clampInt(region.Y+region.Height, 0, frameHeight)
	if right <= left || bottom <= top {
		return true
	}

	pixels := (right - left) * (bottom - top)
	step := pixels / 8192
	if step < 1 {
		step = 1
	}
	var sampled, visible int
	maxChannel := byte(0)
	index := 0
	for y := top; y < bottom; y++ {
		row := y * frameWidth * 4
		for x := left; x < right; x++ {
			if index%step != 0 {
				index++
				continue
			}
			i := row + x*4
			b, g, r := frame[i], frame[i+1], frame[i+2]
			m := r
			if g > m {
				m = g
			}
			if b > m {
				m = b
			}
			if m > maxChannel {
				maxChannel = m
			}
			if m > 18 {
				visible++
			}
			sampled++
			index++
		}
	}
	if sampled == 0 {
		return true
	}
	return maxChannel <= 28 && visible*1000 <= sampled
}

func copyBGRARegion(dst []byte, dstWidth, dstHeight int, region secureMonitorFallbackRegion, src []byte) bool {
	if region.Width <= 0 || region.Height <= 0 || dstWidth <= 0 || dstHeight <= 0 {
		return false
	}
	if len(dst) < dstWidth*dstHeight*4 || len(src) < region.Width*region.Height*4 {
		return false
	}
	if region.X < 0 || region.Y < 0 || region.X+region.Width > dstWidth || region.Y+region.Height > dstHeight {
		return false
	}
	rowBytes := region.Width * 4
	for y := 0; y < region.Height; y++ {
		dstStart := ((region.Y+y)*dstWidth + region.X) * 4
		srcStart := y * rowBytes
		copy(dst[dstStart:dstStart+rowBytes], src[srcStart:srcStart+rowBytes])
	}
	return true
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

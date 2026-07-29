package main

import "context"

type privacyScreenRect struct {
	Left   int
	Top    int
	Right  int
	Bottom int
}

type privacyTracker interface {
	Run(context.Context)
	Regions() []privacyScreenRect
}

func applyPrivacyPixelation(frame []byte, plan capturePlan, regions []privacyScreenRect) {
	if len(frame) < plan.OutputWidth*plan.OutputHeight*4 || plan.Width < 1 || plan.Height < 1 {
		return
	}
	scaleX := float64(plan.OutputWidth) / float64(plan.Width)
	scaleY := float64(plan.OutputHeight) / float64(plan.Height)
	for _, r := range regions {
		left := int(float64(r.Left-plan.X) * scaleX)
		top := int(float64(r.Top-plan.Y) * scaleY)
		right := int(float64(r.Right-plan.X) * scaleX)
		bottom := int(float64(r.Bottom-plan.Y) * scaleY)
		if left < 0 {
			left = 0
		}
		if top < 0 {
			top = 0
		}
		if right > plan.OutputWidth {
			right = plan.OutputWidth
		}
		if bottom > plan.OutputHeight {
			bottom = plan.OutputHeight
		}
		if right-left < 3 || bottom-top < 3 {
			continue
		}
		pixelateBGRA(frame, plan.OutputWidth, plan.OutputHeight, left, top, right, bottom)
	}
}

func pixelateBGRA(frame []byte, width, height, left, top, right, bottom int) {
	block := width / 150
	if block < 10 {
		block = 10
	}
	if block > 28 {
		block = 28
	}
	// Expand to cover antialiased text at the field edges.
	left -= block / 2
	top -= block / 2
	right += block / 2
	bottom += block / 2
	if left < 0 {
		left = 0
	}
	if top < 0 {
		top = 0
	}
	if right > width {
		right = width
	}
	if bottom > height {
		bottom = height
	}

	stride := width * 4
	for by := top; by < bottom; by += block {
		ey := by + block
		if ey > bottom {
			ey = bottom
		}
		for bx := left; bx < right; bx += block {
			ex := bx + block
			if ex > right {
				ex = right
			}
			// Average a small cross-section instead of every pixel to keep the
			// privacy pass inexpensive on weak computers.
			var sb, sg, sr, count int
			stepY := (ey - by) / 3
			if stepY < 1 {
				stepY = 1
			}
			stepX := (ex - bx) / 3
			if stepX < 1 {
				stepX = 1
			}
			for y := by; y < ey; y += stepY {
				row := y * stride
				for x := bx; x < ex; x += stepX {
					i := row + x*4
					sb += int(frame[i])
					sg += int(frame[i+1])
					sr += int(frame[i+2])
					count++
				}
			}
			if count == 0 {
				continue
			}
			b, g, r := byte(sb/count), byte(sg/count), byte(sr/count)
			for y := by; y < ey; y++ {
				row := y * stride
				for x := bx; x < ex; x++ {
					i := row + x*4
					frame[i], frame[i+1], frame[i+2], frame[i+3] = b, g, r, 255
				}
			}
		}
	}
}

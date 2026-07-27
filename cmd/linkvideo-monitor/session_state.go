package main

import "context"

// sessionStateWatcher reports whether the interactive Windows session is
// locked. The non-Windows implementation always reports an unlocked session.
type sessionStateWatcher interface {
	Run(context.Context, func(bool))
}

func makeFallbackStatusFrame(width, height int) []byte {
	if width < 1 || height < 1 {
		return nil
	}
	frame := make([]byte, width*height*4)
	// Dark neutral BGRA background. It intentionally contains no previous
	// desktop pixels when a platform-specific renderer is unavailable.
	for i := 0; i+3 < len(frame); i += 4 {
		frame[i+0] = 36
		frame[i+1] = 39
		frame[i+2] = 43
		frame[i+3] = 255
	}
	return frame
}

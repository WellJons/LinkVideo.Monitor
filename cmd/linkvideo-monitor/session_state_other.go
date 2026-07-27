//go:build !windows

package main

import "context"

type unsupportedSessionStateWatcher struct{}

func newSessionStateWatcher() sessionStateWatcher { return unsupportedSessionStateWatcher{} }

func (unsupportedSessionStateWatcher) Run(ctx context.Context, changed func(bool)) {
	<-ctx.Done()
}

func makeSessionLockedFrame(width, height int) []byte {
	return makeFallbackStatusFrame(width, height)
}

func makeProtectedDesktopFrame(width, height int) []byte {
	return makeFallbackStatusFrame(width, height)
}

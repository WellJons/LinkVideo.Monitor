package main

import (
	"context"
)

type secureFrameHandler func(frame []byte, active bool)

type secureDesktopBridge interface {
	Run(ctx context.Context, handler secureFrameHandler)
	Close() error
}

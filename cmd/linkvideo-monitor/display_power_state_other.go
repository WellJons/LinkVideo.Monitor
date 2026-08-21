//go:build !windows && !darwin

package main

import "context"

type noopDisplayPowerStateWatcher struct{}

func newDisplayPowerStateWatcher() displayPowerStateWatcher { return &noopDisplayPowerStateWatcher{} }
func (w *noopDisplayPowerStateWatcher) Run(ctx context.Context, changed func(bool)) {
	<-ctx.Done()
}

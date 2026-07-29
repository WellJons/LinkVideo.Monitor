package main

import "context"

// displayPowerStateWatcher reports whether Windows has powered the console
// display off. It is intentionally separate from the session lock watcher:
// Win+L and display power-off are different events and must not share the same
// fallback decision.
type displayPowerStateWatcher interface {
	Run(context.Context, func(off bool))
}

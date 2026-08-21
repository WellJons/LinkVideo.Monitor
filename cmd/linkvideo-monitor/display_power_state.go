package main

import "context"

// displayPowerStateWatcher reports whether the captured desktop displays have entered
// their platform power-sleep state. It is intentionally separate from session lock:
// a locked login screen and a sleeping physical display require different output.
type displayPowerStateWatcher interface {
	Run(context.Context, func(off bool))
}

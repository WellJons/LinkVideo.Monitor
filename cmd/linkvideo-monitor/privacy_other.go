//go:build !windows

package main

import "context"

type noopPrivacyTracker struct{}

func newPrivacyTracker() privacyTracker                  { return &noopPrivacyTracker{} }
func (*noopPrivacyTracker) Run(context.Context)          {}
func (*noopPrivacyTracker) Regions() []privacyScreenRect { return nil }

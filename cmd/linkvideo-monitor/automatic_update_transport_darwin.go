//go:build darwin

package main

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

const macOSUpdaterUserAgentPrefix = "LinkVideo-Monitor-macOS-Updater/"

type macOSUpdateRestrictedTransport struct {
	base http.RoundTripper
}

func macOSUpdateDownloadURLAllowed(u *url.URL) bool {
	if u == nil || !strings.EqualFold(u.Scheme, "https") {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	switch host {
	case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	default:
		return false
	}
}

func (t macOSUpdateRestrictedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req != nil && strings.HasPrefix(req.Header.Get("User-Agent"), macOSUpdaterUserAgentPrefix) {
		if !macOSUpdateDownloadURLAllowed(req.URL) {
			return nil, errors.New("обновление перенаправлено за пределы доверенного GitHub release CDN")
		}
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

func init() {
	base := http.DefaultTransport
	if _, ok := base.(macOSUpdateRestrictedTransport); ok {
		return
	}
	http.DefaultTransport = macOSUpdateRestrictedTransport{base: base}
}

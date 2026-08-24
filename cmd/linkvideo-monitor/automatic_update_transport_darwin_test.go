//go:build darwin

package main

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestMacOSUpdateDownloadURLAllowed(t *testing.T) {
	allowed := []string{
		"https://github.com/WellJons/LinkVideo.Monitor.Updates/releases/download/v0.1.2/update.pkg",
		"https://release-assets.githubusercontent.com/github-production-release-asset/123",
		"https://objects.githubusercontent.com/github-production-release-asset/123",
	}
	for _, raw := range allowed {
		u, err := url.Parse(raw)
		if err != nil || !macOSUpdateDownloadURLAllowed(u) {
			t.Fatalf("trusted update URL rejected: %s", raw)
		}
	}

	blocked := []string{
		"http://github.com/WellJons/LinkVideo.Monitor.Updates/releases/download/v0.1.2/update.pkg",
		"https://example.com/update.pkg",
		"https://raw.githubusercontent.com/WellJons/LinkVideo.Monitor.Updates/main/update.pkg",
	}
	for _, raw := range blocked {
		u, err := url.Parse(raw)
		if err == nil && macOSUpdateDownloadURLAllowed(u) {
			t.Fatalf("untrusted update URL accepted: %s", raw)
		}
	}
}

func TestMacOSUpdateTransportBlocksUpdaterRedirectButNotNormalTraffic(t *testing.T) {
	calls := 0
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("base reached")
	})
	transport := macOSUpdateRestrictedTransport{base: base}

	updaterReq, _ := http.NewRequest(http.MethodGet, "https://example.com/update.pkg", nil)
	updaterReq.Header.Set("User-Agent", macOSUpdaterUserAgentPrefix+"0.1.2")
	if _, err := transport.RoundTrip(updaterReq); err == nil || err.Error() == "base reached" {
		t.Fatal("updater request to untrusted host was not blocked before base transport")
	}
	if calls != 0 {
		t.Fatalf("blocked updater request reached base transport %d times", calls)
	}

	normalReq, _ := http.NewRequest(http.MethodGet, "https://example.com/api", nil)
	if _, err := transport.RoundTrip(normalReq); err == nil || err.Error() != "base reached" {
		t.Fatalf("normal request did not pass through base transport: %v", err)
	}
	if calls != 1 {
		t.Fatalf("normal request reached base transport %d times", calls)
	}
}

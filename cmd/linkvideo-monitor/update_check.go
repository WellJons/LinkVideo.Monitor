package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Can be embedded for production builds:
// -ldflags="-X main.defaultUpdateManifestURL=https://updates.example/monitor.json"
// A private GitHub repository should not be accessed with a token embedded in
// the client. Publish this small manifest through a company API or proxy.
var defaultUpdateManifestURL string

type updateManifest struct {
	Version     string `json:"version"`
	DownloadURL string `json:"download_url"`
	SHA256      string `json:"sha256,omitempty"`
	Notes       string `json:"notes,omitempty"`
	Mandatory   bool   `json:"mandatory,omitempty"`
}

type updateCheckResult struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	Available      bool   `json:"available"`
	DownloadURL    string `json:"download_url,omitempty"`
	SHA256         string `json:"sha256,omitempty"`
	Notes          string `json:"notes,omitempty"`
	Mandatory      bool   `json:"mandatory,omitempty"`
}

func checkForUpdates(ctx context.Context) (updateCheckResult, error) {
	manifestURL := strings.TrimSpace(defaultUpdateManifestURL)
	if manifestURL == "" {
		return updateCheckResult{}, errors.New("сервер обновлений пока не настроен")
	}
	u, err := url.Parse(manifestURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return updateCheckResult{}, errors.New("задан некорректный адрес сервера обновлений")
	}
	reqCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return updateCheckResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "LinkVideo-Monitor/"+appVersion)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return updateCheckResult{}, fmt.Errorf("не удалось проверить обновления: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return updateCheckResult{}, fmt.Errorf("сервер обновлений вернул HTTP %d", resp.StatusCode)
	}
	var m updateManifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 128<<10)).Decode(&m); err != nil {
		return updateCheckResult{}, fmt.Errorf("некорректный ответ сервера обновлений: %w", err)
	}
	if strings.TrimSpace(m.Version) == "" {
		return updateCheckResult{}, errors.New("сервер не указал версию обновления")
	}
	if m.DownloadURL != "" {
		du, err := url.Parse(m.DownloadURL)
		if err != nil || du.Scheme != "https" || du.Host == "" {
			return updateCheckResult{}, errors.New("сервер вернул небезопасную ссылку обновления")
		}
	}
	return updateCheckResult{
		CurrentVersion: appVersion, LatestVersion: m.Version,
		Available:   compareVersions(m.Version, appVersion) > 0,
		DownloadURL: m.DownloadURL, SHA256: m.SHA256, Notes: m.Notes, Mandatory: m.Mandatory,
	}, nil
}

func compareVersions(a, b string) int {
	parse := func(v string) []int {
		v = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(v)), "v")
		if i := strings.IndexAny(v, "-+"); i >= 0 {
			v = v[:i]
		}
		parts := strings.Split(v, ".")
		out := make([]int, len(parts))
		for i, p := range parts {
			out[i], _ = strconv.Atoi(p)
		}
		return out
	}
	av, bv := parse(a), parse(b)
	n := len(av)
	if len(bv) > n {
		n = len(bv)
	}
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(av) {
			x = av[i]
		}
		if i < len(bv) {
			y = bv[i]
		}
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
	}
	return 0
}

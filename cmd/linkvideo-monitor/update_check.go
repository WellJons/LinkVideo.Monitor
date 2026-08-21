package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Optional build-time override for special channels:
// -ldflags="-X main.defaultUpdateManifestURL=https://.../manifest.json"
// When empty, each OS uses its own official manifest.
var defaultUpdateManifestURL string

type updateManifest struct {
	Version       string   `json:"version"`
	Platform      string   `json:"platform,omitempty"`
	Architectures []string `json:"architectures,omitempty"`
	DownloadURL   string   `json:"download_url"`
	SHA256        string   `json:"sha256,omitempty"`
	Notes         string   `json:"notes,omitempty"`
	Mandatory     bool     `json:"mandatory,omitempty"`
}

type updateCheckResult struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	Platform       string `json:"platform"`
	Architecture   string `json:"architecture"`
	Available      bool   `json:"available"`
	DownloadURL    string `json:"download_url,omitempty"`
	SHA256         string `json:"sha256,omitempty"`
	Notes          string `json:"notes,omitempty"`
	Mandatory      bool   `json:"mandatory,omitempty"`
}

type parsedVersion struct {
	core       []int
	prerelease []string
}

func checkForUpdates(ctx context.Context) (updateCheckResult, error) {
	manifestURL := strings.TrimSpace(defaultUpdateManifestURL)
	if manifestURL == "" {
		manifestURL = defaultUpdateManifestForPlatform(runtime.GOOS)
	}
	if manifestURL == "" {
		return updateCheckResult{}, errors.New("сервер обновлений для этой платформы пока не настроен")
	}
	u, err := url.Parse(manifestURL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return updateCheckResult{}, errors.New("задан некорректный адрес сервера обновлений")
	}
	currentVersion := currentReleaseVersion()
	reqCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return updateCheckResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", fmt.Sprintf("LinkVideo-Monitor/%s (%s/%s)", currentVersion, runtime.GOOS, runtime.GOARCH))
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
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
	if platform := strings.TrimSpace(strings.ToLower(m.Platform)); platform != "" && platform != runtime.GOOS {
		return updateCheckResult{}, fmt.Errorf("манифест предназначен для %s, а клиент работает на %s", platform, runtime.GOOS)
	}
	if !architectureAllowed(m.Architectures, runtime.GOARCH) {
		return updateCheckResult{}, fmt.Errorf("обновление %s не поддерживает архитектуру %s", m.Version, runtime.GOARCH)
	}
	cmp, err := compareSemanticVersions(m.Version, currentVersion)
	if err != nil {
		return updateCheckResult{}, fmt.Errorf("сервер указал некорректную версию обновления: %w", err)
	}
	if m.DownloadURL != "" {
		du, err := url.Parse(m.DownloadURL)
		if err != nil || du.Scheme != "https" || du.Host == "" {
			return updateCheckResult{}, errors.New("сервер вернул небезопасную ссылку обновления")
		}
	}
	return updateCheckResult{
		CurrentVersion: currentVersion,
		LatestVersion:  m.Version,
		Platform:       runtime.GOOS,
		Architecture:   runtime.GOARCH,
		Available:      cmp > 0,
		DownloadURL:    m.DownloadURL,
		SHA256:         m.SHA256,
		Notes:          m.Notes,
		Mandatory:      m.Mandatory,
	}, nil
}

func parseVersion(v string) (parsedVersion, error) {
	v = strings.TrimSpace(strings.ToLower(v))
	v = strings.TrimPrefix(v, "v")
	if v == "" {
		return parsedVersion{}, errors.New("пустая версия")
	}
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i]
	}
	var pre []string
	if i := strings.IndexByte(v, '-'); i >= 0 {
		preText := v[i+1:]
		v = v[:i]
		if preText == "" {
			return parsedVersion{}, errors.New("пустой prerelease")
		}
		pre = strings.Split(preText, ".")
		for _, item := range pre {
			if item == "" {
				return parsedVersion{}, errors.New("пустой prerelease-компонент")
			}
		}
	}
	parts := strings.Split(v, ".")
	if len(parts) == 0 {
		return parsedVersion{}, errors.New("отсутствует номер версии")
	}
	core := make([]int, len(parts))
	for i, part := range parts {
		if part == "" {
			return parsedVersion{}, errors.New("пустой числовой компонент")
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return parsedVersion{}, fmt.Errorf("некорректный компонент %q", part)
		}
		core[i] = n
	}
	return parsedVersion{core: core, prerelease: pre}, nil
}

func compareSemanticVersions(a, b string) (int, error) {
	av, err := parseVersion(a)
	if err != nil {
		return 0, err
	}
	bv, err := parseVersion(b)
	if err != nil {
		return 0, err
	}
	n := len(av.core)
	if len(bv.core) > n {
		n = len(bv.core)
	}
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(av.core) {
			x = av.core[i]
		}
		if i < len(bv.core) {
			y = bv.core[i]
		}
		if x < y {
			return -1, nil
		}
		if x > y {
			return 1, nil
		}
	}
	// A stable release is newer than a prerelease with the same numeric version.
	if len(av.prerelease) == 0 && len(bv.prerelease) > 0 {
		return 1, nil
	}
	if len(av.prerelease) > 0 && len(bv.prerelease) == 0 {
		return -1, nil
	}
	for i := 0; i < len(av.prerelease) || i < len(bv.prerelease); i++ {
		if i >= len(av.prerelease) {
			return -1, nil
		}
		if i >= len(bv.prerelease) {
			return 1, nil
		}
		x, y := av.prerelease[i], bv.prerelease[i]
		xn, xerr := strconv.Atoi(x)
		yn, yerr := strconv.Atoi(y)
		if xerr == nil && yerr == nil {
			if xn < yn {
				return -1, nil
			}
			if xn > yn {
				return 1, nil
			}
			continue
		}
		if xerr == nil && yerr != nil {
			return -1, nil
		}
		if xerr != nil && yerr == nil {
			return 1, nil
		}
		if x < y {
			return -1, nil
		}
		if x > y {
			return 1, nil
		}
	}
	return 0, nil
}

func compareVersions(a, b string) int {
	cmp, err := compareSemanticVersions(a, b)
	if err != nil {
		return 0
	}
	return cmp
}

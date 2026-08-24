//go:build darwin

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	macOSAutoUpdateInitialDelay     = 90 * time.Second
	macOSAutoUpdateInterval         = 6 * time.Hour
	macOSAutoUpdateRetryBase        = 5 * time.Minute
	macOSAutoUpdateRetryMax         = 1 * time.Hour
	macOSAutoUpdateInstallRetryBase = 30 * time.Minute
	macOSAutoUpdateInstallRetryMax  = 6 * time.Hour
	macOSAutoUpdateMaxBytes         = int64(512 << 20)
	macOSInstalledAppPath           = "/Applications/LinkVideo.Monitor.app"
	macOSPackageIdentifier          = "ru.linkvideo.monitor.pkg"
)

type macOSAutomaticUpdateFailureMarker struct {
	Version  string `json:"version"`
	Failures int    `json:"failures"`
	AtUnix   int64  `json:"at_unix"`
}

type macOSPackageInfo struct {
	Identifier string `xml:"identifier,attr"`
	Version    string `xml:"version,attr"`
}

func startMacOSAutomaticUpdates(a *app) {
	if _, err := macOSManagedUpdateTeamID(); err != nil {
		// Development/ad-hoc builds intentionally do not advertise or attempt a
		// managed automatic install. Manual update checks still work normally.
		return
	}
	go runMacOSAutomaticUpdateLoop(a)
}

func runMacOSAutomaticUpdateLoop(a *app) {
	timer := time.NewTimer(macOSAutoUpdateInitialDelay)
	defer timer.Stop()
	failures := 0
	for {
		<-timer.C
		launched, retryAfter, err := checkDownloadAndLaunchMacOSAutomaticUpdate(a)
		if err != nil {
			failures++
			retry := macOSAutomaticUpdateRetryDelay(failures)
			a.appendLog(fmt.Sprintf("Автообновление macOS: %v; повтор через %s", err, retry.Round(time.Second)))
			timer.Reset(retry)
			continue
		}
		failures = 0
		if launched {
			// A successful installer will terminate this process in the package
			// preinstall step and relaunch the new version from postinstall.
			return
		}
		if retryAfter > 0 {
			timer.Reset(retryAfter)
			continue
		}
		timer.Reset(macOSAutoUpdateInterval)
	}
}

func macOSAutomaticUpdateRetryDelay(failures int) time.Duration {
	if failures <= 0 {
		return macOSAutoUpdateInterval
	}
	delay := macOSAutoUpdateRetryBase
	for i := 1; i < failures && delay < macOSAutoUpdateRetryMax; i++ {
		delay *= 2
	}
	if delay > macOSAutoUpdateRetryMax {
		delay = macOSAutoUpdateRetryMax
	}
	return delay
}

func checkDownloadAndLaunchMacOSAutomaticUpdate(a *app) (bool, time.Duration, error) {
	teamID, err := macOSManagedUpdateTeamID()
	if err != nil {
		return false, 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	result, err := checkForUpdates(ctx)
	cancel()
	if err != nil {
		return false, 0, err
	}
	if !result.Available {
		cleanupStaleMacOSUpdatePackages()
		return false, 0, nil
	}
	if err := validateAutomaticUpdateDownload(result.DownloadURL, result.SHA256, result.LatestVersion); err != nil {
		return false, 0, err
	}
	if remaining := failedMacOSInstallRetryRemaining(result.LatestVersion, result.Mandatory); remaining > 0 {
		return false, remaining, nil
	}

	pkgPath, err := downloadVerifiedMacOSUpdatePackage(result)
	if err != nil {
		return false, 0, err
	}
	if err := verifyMacOSUpdatePackage(pkgPath, result.LatestVersion, teamID); err != nil {
		_ = os.Remove(pkgPath)
		return false, 0, fmt.Errorf("пакет обновления macOS не прошёл проверку: %w", err)
	}

	a.appendLog(fmt.Sprintf("Обновление macOS %s проверено; система запросит права администратора для установки", result.LatestVersion))
	if err := launchMacOSUpdateInstaller(pkgPath); err != nil {
		recordMacOSInstallFailure(result.LatestVersion)
		return false, failedMacOSInstallRetryRemaining(result.LatestVersion, result.Mandatory), fmt.Errorf("установка обновления macOS отменена или завершилась ошибкой: %w", err)
	}
	clearMacOSInstallFailure()
	a.appendLog(fmt.Sprintf("Обновление macOS %s установлено; запускается новая версия", result.LatestVersion))
	return true, 0, nil
}

func macOSManagedUpdateTeamID() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	want := filepath.Join(macOSInstalledAppPath, "Contents", "MacOS", "LinkVideo.Monitor")
	if filepath.Clean(exe) != want {
		return "", fmt.Errorf("managed updater доступен только для %s", want)
	}
	out, err := exec.Command("/usr/bin/codesign", "-dv", "--verbose=4", macOSInstalledAppPath).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("не удалось проверить подпись установленного приложения: %w", err)
	}
	teamID, authority, err := parseMacOSCodeSignIdentity(string(out))
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(authority, "Developer ID Application:") {
		return "", errors.New("приложение не подписано Developer ID Application")
	}
	if !strings.Contains(authority, "("+teamID+")") {
		return "", errors.New("Team ID не совпадает с Developer ID Application")
	}
	return teamID, nil
}

func parseMacOSCodeSignIdentity(output string) (teamID, authority string, err error) {
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "TeamIdentifier=") {
			teamID = strings.TrimSpace(strings.TrimPrefix(line, "TeamIdentifier="))
		}
		if strings.HasPrefix(line, "Authority=Developer ID Application:") {
			authority = strings.TrimSpace(strings.TrimPrefix(line, "Authority="))
		}
	}
	if teamID == "" || strings.EqualFold(teamID, "not set") {
		return "", "", errors.New("в подписи приложения отсутствует Team ID")
	}
	if authority == "" {
		return "", "", errors.New("в подписи приложения отсутствует Developer ID Application")
	}
	return teamID, authority, nil
}

func macOSAutomaticUpdateDirectory() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "LinkVideo.Monitor", "updates")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	_ = os.Chmod(dir, 0o700)
	return dir, nil
}

func downloadVerifiedMacOSUpdatePackage(result updateCheckResult) (string, error) {
	if err := validateAutomaticUpdateDownload(result.DownloadURL, result.SHA256, result.LatestVersion); err != nil {
		return "", err
	}
	u, err := url.Parse(result.DownloadURL)
	if err != nil {
		return "", err
	}
	name, err := url.PathUnescape(filepath.Base(u.Path))
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(name, expectedAutomaticUpdateAssetName(result.LatestVersion)) || filepath.Base(name) != name {
		return "", errors.New("сервер обновлений вернул недопустимое имя пакета")
	}
	dir, err := macOSAutomaticUpdateDirectory()
	if err != nil {
		return "", err
	}
	cleanupStaleMacOSUpdatePackages()
	finalPath := filepath.Join(dir, expectedAutomaticUpdateAssetName(result.LatestVersion))
	tmpPath := finalPath + ".download"
	_ = os.Remove(tmpPath)
	_ = os.Remove(finalPath)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, result.DownloadURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", macOSUpdaterUserAgentPrefix+appVersion)
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := (&http.Client{Timeout: 15 * time.Minute}).Do(req)
	if err != nil {
		return "", fmt.Errorf("не удалось скачать обновление: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("сервер пакета обновления вернул HTTP %d", resp.StatusCode)
	}
	if resp.Request == nil || resp.Request.URL == nil || resp.Request.URL.Scheme != "https" {
		return "", errors.New("сервер обновлений выполнил небезопасное перенаправление")
	}
	if resp.ContentLength > macOSAutoUpdateMaxBytes {
		return "", errors.New("пакет обновления превышает допустимый размер")
	}

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, macOSAutoUpdateMaxBytes+1))
	syncErr := f.Sync()
	closeErr := f.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil || n > macOSAutoUpdateMaxBytes {
		_ = os.Remove(tmpPath)
		switch {
		case copyErr != nil:
			return "", copyErr
		case syncErr != nil:
			return "", syncErr
		case closeErr != nil:
			return "", closeErr
		default:
			return "", errors.New("пакет обновления превышает допустимый размер")
		}
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, strings.TrimSpace(result.SHA256)) {
		_ = os.Remove(tmpPath)
		return "", errors.New("SHA-256 пакета не совпадает с манифестом; обновление отменено")
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	return finalPath, nil
}

func verifyMacOSUpdatePackage(pkgPath, targetVersion, expectedTeamID string) error {
	out, err := exec.Command("/usr/sbin/pkgutil", "--check-signature", pkgPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("pkgutil не подтвердил подпись: %w: %s", err, strings.TrimSpace(string(out)))
	}
	teamID, authority, err := parseMacOSPackageSignature(string(out))
	if err != nil {
		return err
	}
	if teamID != expectedTeamID {
		return fmt.Errorf("Team ID пакета %s не совпадает с приложением %s", teamID, expectedTeamID)
	}
	if !strings.HasPrefix(authority, "Developer ID Installer:") || !strings.Contains(authority, "("+expectedTeamID+")") {
		return errors.New("пакет не подписан ожидаемым Developer ID Installer")
	}

	spctlOut, spctlErr := exec.Command("/usr/sbin/spctl", "--assess", "--type", "install", "--verbose=4", pkgPath).CombinedOutput()
	if spctlErr != nil {
		return fmt.Errorf("Gatekeeper отклонил пакет: %w: %s", spctlErr, strings.TrimSpace(string(spctlOut)))
	}
	return verifyMacOSPackageMetadata(pkgPath, targetVersion)
}

func parseMacOSPackageSignature(output string) (teamID, authority string, err error) {
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if idx := strings.Index(line, "Developer ID Installer:"); idx >= 0 {
			authority = strings.TrimSpace(line[idx:])
			if open := strings.LastIndex(authority, "("); open >= 0 && strings.HasSuffix(authority, ")") {
				teamID = strings.TrimSpace(authority[open+1 : len(authority)-1])
			}
			break
		}
	}
	if authority == "" || teamID == "" {
		return "", "", errors.New("в подписи пакета отсутствует Developer ID Installer/Team ID")
	}
	return teamID, authority, nil
}

func verifyMacOSPackageMetadata(pkgPath, targetVersion string) error {
	dir, err := os.MkdirTemp("", "linkvideo-update-pkg-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	out, err := exec.Command("/usr/sbin/pkgutil", "--expand", pkgPath, dir).CombinedOutput()
	if err != nil {
		return fmt.Errorf("не удалось проверить метаданные пакета: %w: %s", err, strings.TrimSpace(string(out)))
	}
	data, err := os.ReadFile(filepath.Join(dir, "PackageInfo"))
	if err != nil {
		return err
	}
	var info macOSPackageInfo
	if err := xml.Unmarshal(data, &info); err != nil {
		return fmt.Errorf("некорректный PackageInfo: %w", err)
	}
	if info.Identifier != macOSPackageIdentifier {
		return fmt.Errorf("неожиданный package id: %s", info.Identifier)
	}
	wantVersion := updateAssetVersionBase(targetVersion)
	if info.Version != wantVersion {
		return fmt.Errorf("версия PKG %s не совпадает с манифестом %s", info.Version, wantVersion)
	}
	return nil
}

func macOSAutomaticUpdateFailureMarkerPath() string {
	dir, err := macOSAutomaticUpdateDirectory()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "install-failure.json")
}

func failedMacOSInstallRetryRemaining(targetVersion string, mandatory bool) time.Duration {
	path := macOSAutomaticUpdateFailureMarkerPath()
	if path == "" {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var marker macOSAutomaticUpdateFailureMarker
	if json.Unmarshal(data, &marker) != nil || marker.Failures < 1 || marker.AtUnix <= 0 || canonicalUpdateVersion(marker.Version) != canonicalUpdateVersion(targetVersion) {
		return 0
	}
	delay := macOSAutoUpdateInstallRetryBase
	for i := 1; i < marker.Failures && delay < macOSAutoUpdateInstallRetryMax; i++ {
		delay *= 2
	}
	maxDelay := macOSAutoUpdateInstallRetryMax
	if mandatory {
		maxDelay = time.Hour
	}
	if delay > maxDelay {
		delay = maxDelay
	}
	remaining := time.Until(time.Unix(marker.AtUnix, 0).Add(delay))
	if remaining < 0 {
		return 0
	}
	return remaining
}

func recordMacOSInstallFailure(version string) {
	path := macOSAutomaticUpdateFailureMarkerPath()
	if path == "" {
		return
	}
	marker := macOSAutomaticUpdateFailureMarker{Version: version, Failures: 1, AtUnix: time.Now().Unix()}
	if data, err := os.ReadFile(path); err == nil {
		var previous macOSAutomaticUpdateFailureMarker
		if json.Unmarshal(data, &previous) == nil && canonicalUpdateVersion(previous.Version) == canonicalUpdateVersion(version) {
			marker.Failures = previous.Failures + 1
		}
	}
	data, err := json.Marshal(marker)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

func clearMacOSInstallFailure() {
	if path := macOSAutomaticUpdateFailureMarkerPath(); path != "" {
		_ = os.Remove(path)
	}
}

func cleanupStaleMacOSUpdatePackages() {
	dir, err := macOSAutomaticUpdateDirectory()
	if err != nil {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-48 * time.Hour)
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "install-failure.json" {
			continue
		}
		name := strings.ToLower(entry.Name())
		if !strings.HasSuffix(name, ".pkg") && !strings.HasSuffix(name, ".download") {
			continue
		}
		info, statErr := entry.Info()
		if statErr == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}

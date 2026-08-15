//go:build windows

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	autoUpdateInitialDelay = 90 * time.Second
	autoUpdateInterval     = 6 * time.Hour
	autoUpdateMaxBytes     = int64(256 << 20)
)

func init() {
	if len(os.Args) > 1 && os.Args[1] == "--uac-service" {
		go runServiceAutomaticUpdates()
	}
}
func runServiceAutomaticUpdates() {
	timer := time.NewTimer(autoUpdateInitialDelay)
	defer timer.Stop()
	for {
		select {
		case <-serviceStopCh:
			return
		case <-timer.C:
			launched, err := checkDownloadAndLaunchAutomaticUpdate()
			if err != nil {
				serviceLog("automatic update: " + err.Error())
			}
			if launched {
				serviceLog("automatic update installer started; stopping service for upgrade")
				serviceStopOnce.Do(func() { close(serviceStopCh) })
				return
			}
			timer.Reset(autoUpdateInterval)
		}
	}
}
func checkDownloadAndLaunchAutomaticUpdate() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	result, err := checkForUpdates(ctx)
	cancel()
	if err != nil {
		return false, err
	}
	if !result.Available {
		cleanupStaleUpdateInstallers()
		return false, nil
	}
	if err := validateAutomaticUpdateDownload(result.DownloadURL, result.SHA256); err != nil {
		return false, err
	}
	installerPath, err := downloadVerifiedUpdateInstaller(result)
	if err != nil {
		return false, err
	}
	cmd := exec.Command(installerPath, "--silent-update-elevated")
	hideChildWindow(cmd)
	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("не удалось запустить установщик %s: %w", result.LatestVersion, err)
	}
	serviceLog(fmt.Sprintf("automatic update: %s -> %s verified and launched", appVersion, result.LatestVersion))
	return true, nil
}
func automaticUpdateDirectory() (string, error) {
	appPath, err := loadInstalledAppPath()
	if err != nil {
		return "", err
	}
	programFiles := filepath.Dir(filepath.Dir(appPath))
	dir := filepath.Join(programFiles, "LinkVideoUpdaterCache")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}
func downloadVerifiedUpdateInstaller(result updateCheckResult) (string, error) {
	u, err := url.Parse(result.DownloadURL)
	if err != nil {
		return "", err
	}
	name := filepath.Base(u.Path)
	if decoded, decodeErr := url.PathUnescape(name); decodeErr == nil {
		name = decoded
	}
	lower := strings.ToLower(name)
	if !strings.HasPrefix(lower, "linkvideo.monitor_") || !strings.HasSuffix(lower, "_setup.exe") || strings.ContainsAny(name, `\\/:*?"<>|`) {
		return "", errors.New("сервер обновлений вернул недопустимое имя установщика")
	}
	dir, err := automaticUpdateDirectory()
	if err != nil {
		return "", fmt.Errorf("не удалось подготовить папку обновлений: %w", err)
	}
	cleanupStaleUpdateInstallers()
	finalPath := filepath.Join(dir, name)
	tmpPath := finalPath + ".download"
	_ = os.Remove(tmpPath)
	_ = os.Remove(finalPath)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, result.DownloadURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "LinkVideo-Monitor-Updater/"+appVersion)
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("не удалось скачать обновление: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("сервер установщика вернул HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > autoUpdateMaxBytes {
		return "", errors.New("установщик обновления превышает допустимый размер")
	}
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	limited := io.LimitReader(resp.Body, autoUpdateMaxBytes+1)
	n, copyErr := io.Copy(io.MultiWriter(f, h), limited)
	syncErr := f.Sync()
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return "", copyErr
	}
	if syncErr != nil {
		_ = os.Remove(tmpPath)
		return "", syncErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return "", closeErr
	}
	if n > autoUpdateMaxBytes {
		_ = os.Remove(tmpPath)
		return "", errors.New("установщик обновления превышает допустимый размер")
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, strings.TrimSpace(result.SHA256)) {
		_ = os.Remove(tmpPath)
		return "", errors.New("SHA-256 установщика не совпадает с манифестом; обновление отменено")
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	return finalPath, nil
}
func cleanupStaleUpdateInstallers() {
	dir, err := automaticUpdateDirectory()
	if err != nil {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-48 * time.Hour)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if !strings.HasSuffix(name, ".exe") && !strings.HasSuffix(name, ".download") {
			continue
		}
		info, statErr := entry.Info()
		if statErr == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}

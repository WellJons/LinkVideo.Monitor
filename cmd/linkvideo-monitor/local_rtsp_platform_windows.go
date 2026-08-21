//go:build windows

package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func selectedMediaMTXRelease() mediaMTXRelease {
	major, minor, _ := windowsVersion()
	return mediaMTXReleaseForWindows(major, minor)
}

func isManagedMediaMTXPath(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "mediamtx.exe")
}

func mediaMTXDefaultPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "mediamtx.exe"), nil
}

func ensureManagedMediaMTX(a *app, release mediaMTXRelease) (string, error) {
	dest, err := mediaMTXDefaultPath()
	if err != nil {
		return "", err
	}
	needsInstall, err := managedMediaMTXNeedsInstall(dest, release)
	if err != nil {
		return "", err
	}
	if !needsInstall {
		return dest, nil
	}
	a.appendLog(fmt.Sprintf("Установка компонента локальной RTSP-трансляции MediaMTX %s…", release.Version))
	if err := downloadMediaMTX(dest, release); err != nil {
		return "", fmt.Errorf("не удалось автоматически установить локальную RTSP-камеру: %w", err)
	}
	a.appendLog(fmt.Sprintf("Компонент локальной RTSP-трансляции MediaMTX %s установлен", release.Version))
	return dest, nil
}

func mediaMTXVersionMarker(dest string) string {
	return dest + ".linkvideo-version"
}

func mediaMTXMarkerVersion(dest string) string {
	data, err := os.ReadFile(mediaMTXVersionMarker(dest))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func managedMediaMTXNeedsInstall(dest string, release mediaMTXRelease) (bool, error) {
	info, err := os.Stat(dest)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	if info.IsDir() {
		return false, fmt.Errorf("путь локального RTSP-сервера является папкой: %s", dest)
	}

	installed := mediaMTXMarkerVersion(dest)
	if installed == release.Version {
		return false, nil
	}
	if installed != "" {
		return true, nil
	}

	// Releases before 0.8.12 did not create a version marker. On modern
	// Windows that unmarked binary is the existing 1.19.3 component and can be
	// kept. On Windows 7 it is exactly the incompatible binary that must be
	// replaced once with the Go 1.20-compatible release.
	return release.LegacyConfig, nil
}

func downloadMediaMTX(dest string, release mediaMTXRelease) error {
	client := &http.Client{Timeout: 3 * time.Minute}
	req, err := http.NewRequest(http.MethodGet, release.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "LinkVideo-Monitor/"+currentReleaseVersion())
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("не удалось загрузить компонент локальной трансляции: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("сервер загрузки вернул HTTP %d", resp.StatusCode)
	}

	tmpZip, err := os.CreateTemp("", "linkvideo-mediamtx-*.zip")
	if err != nil {
		return err
	}
	zipPath := tmpZip.Name()
	defer os.Remove(zipPath)
	hash := sha256.New()
	limited := io.LimitReader(resp.Body, 120<<20)
	if _, err := io.Copy(io.MultiWriter(tmpZip, hash), limited); err != nil {
		tmpZip.Close()
		return err
	}
	if err := tmpZip.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(hash.Sum(nil)); !strings.EqualFold(got, release.SHA256) {
		return fmt.Errorf("контрольная сумма компонента локальной трансляции не совпала: %s", got)
	}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("загруженный архив повреждён: %w", err)
	}
	defer zr.Close()

	var source *zip.File
	for _, f := range zr.File {
		if strings.EqualFold(filepath.Base(f.Name), "mediamtx.exe") {
			source = f
			break
		}
	}
	if source == nil {
		return errors.New("в архиве не найден mediamtx.exe")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	in, err := source.Open()
	if err != nil {
		return err
	}
	defer in.Close()
	tmpDest := dest + ".new"
	out, err := os.OpenFile(tmpDest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmpDest)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpDest)
		return closeErr
	}
	if err := replaceFileAtomically(tmpDest, dest); err != nil {
		_ = os.Remove(tmpDest)
		return err
	}
	if err := os.WriteFile(mediaMTXVersionMarker(dest), []byte(release.Version+"\n"), 0o644); err != nil {
		return fmt.Errorf("компонент установлен, но не удалось сохранить его версию: %w", err)
	}
	return nil
}

package main

import (
	"encoding/hex"
	"errors"
	"net/url"
	"path"
	"strings"
)

const publicUpdateDownloadPrefix = "/WellJons/LinkVideo.Monitor.Updates/releases/download/"

func canonicalUpdateVersion(v string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(v)), "v")
}

func validateAutomaticUpdateDownload(downloadURL, sha256sum, targetVersion string) error {
	u, err := url.Parse(strings.TrimSpace(downloadURL))
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "github.com") {
		return errors.New("автоматическое обновление разрешено только через GitHub LinkVideo.Monitor.Updates")
	}
	if !strings.HasPrefix(u.EscapedPath(), publicUpdateDownloadPrefix) {
		return errors.New("ссылка обновления не относится к официальному каналу LinkVideo.Monitor.Updates")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return errors.New("ссылка обновления содержит недопустимые параметры")
	}
	remainder := strings.TrimPrefix(u.EscapedPath(), publicUpdateDownloadPrefix)
	parts := strings.Split(remainder, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return errors.New("ссылка обновления имеет некорректный путь release")
	}
	tag, err := url.PathUnescape(parts[0])
	if err != nil || canonicalUpdateVersion(tag) != canonicalUpdateVersion(targetVersion) {
		return errors.New("версия release в ссылке не совпадает с версией манифеста")
	}
	fileName, err := url.PathUnescape(path.Base(parts[1]))
	if err != nil {
		return errors.New("некорректное имя установщика обновления")
	}
	lowerName := strings.ToLower(fileName)
	if !strings.HasPrefix(lowerName, "linkvideo.monitor_") || !strings.HasSuffix(lowerName, "_setup.exe") {
		return errors.New("манифест указывает не на официальный установщик LinkVideo Monitor")
	}
	digest, err := hex.DecodeString(strings.TrimSpace(sha256sum))
	if err != nil || len(digest) != 32 {
		return errors.New("для автоматического обновления требуется корректная SHA-256 сумма")
	}
	return nil
}

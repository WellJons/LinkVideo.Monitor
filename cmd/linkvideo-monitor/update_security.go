package main

import (
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
)

const publicUpdateDownloadPrefix = "/WellJons/LinkVideo.Monitor.Updates/releases/download/"

func validateAutomaticUpdateDownload(downloadURL, sha256sum string) error {
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
	digest, err := hex.DecodeString(strings.TrimSpace(sha256sum))
	if err != nil || len(digest) != 32 {
		return errors.New("для автоматического обновления требуется корректная SHA-256 сумма")
	}
	return nil
}

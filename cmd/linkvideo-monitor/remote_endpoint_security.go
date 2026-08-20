package main

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

func validateRemoteAPIEndpoint(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return errors.New("указан некорректный адрес API")
	}
	if u.User != nil {
		return errors.New("адрес API не должен содержать логин или пароль")
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme != "http" {
		return errors.New("адрес API должен использовать HTTPS")
	}
	host := strings.TrimSpace(u.Hostname())
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return nil
	}
	return errors.New("HTTP разрешён только для локального тестового API; удалённый API должен использовать HTTPS")
}

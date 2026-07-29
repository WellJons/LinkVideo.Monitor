package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const adminTokenLifetime = 90 * time.Second

// The administrator password is provisioned by LinkVideo and cannot be changed
// from the client interface. Only the SHA-256 digest is embedded in the binary,
// so the plain-text password is not stored in the executable or in config files.
// A different digest may be supplied for a private build with:
// -ldflags="-X main.embeddedAdminPasswordSHA256=<64-char hex digest>"
var embeddedAdminPasswordSHA256 = "3a9d84499d1be563b706d0b303a7179c95cb91659bd1999b0ff28409cf70ecf6"

func (a *app) adminPasswordConfigured() bool {
	return validEmbeddedAdminDigest()
}

func validEmbeddedAdminDigest() bool {
	digest, err := hex.DecodeString(strings.TrimSpace(embeddedAdminPasswordSHA256))
	return err == nil && len(digest) == sha256.Size
}

func (a *app) setupAdminPassword(_ string) (string, error) {
	return "", errors.New("пароль администратора установлен LinkVideo и не может быть изменён на компьютере клиента")
}

func (a *app) verifyAdminPassword(password string) (string, error) {
	now := time.Now()
	a.mu.Lock()
	if a.adminBlockedUntil.After(now) {
		wait := time.Until(a.adminBlockedUntil).Round(time.Second)
		a.mu.Unlock()
		return "", fmt.Errorf("слишком много неверных попыток; повторите через %s", wait)
	}
	a.mu.Unlock()

	expected, err := hex.DecodeString(strings.TrimSpace(embeddedAdminPasswordSHA256))
	if err != nil || len(expected) != sha256.Size {
		return "", errors.New("встроенный пароль администратора не настроен в сборке")
	}
	actual := sha256.Sum256([]byte(password))
	if subtle.ConstantTimeCompare(actual[:], expected) != 1 {
		a.mu.Lock()
		a.adminFailures++
		if a.adminFailures >= 5 {
			a.adminFailures = 0
			a.adminBlockedUntil = time.Now().Add(30 * time.Second)
		}
		a.mu.Unlock()
		return "", errors.New("неверный пароль")
	}
	a.mu.Lock()
	a.adminFailures = 0
	a.adminBlockedUntil = time.Time{}
	defer a.mu.Unlock()
	return a.issueAdminTokenLocked(), nil
}

func (a *app) issueAdminTokenLocked() string {
	if a.adminTokens == nil {
		a.adminTokens = make(map[string]time.Time)
	}
	raw := make([]byte, 32)
	_, _ = rand.Read(raw)
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := time.Now()
	for k, expiry := range a.adminTokens {
		if !expiry.After(now) {
			delete(a.adminTokens, k)
		}
	}
	a.adminTokens[token] = now.Add(adminTokenLifetime)
	return token
}

func (a *app) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	token := strings.TrimSpace(r.Header.Get("X-LinkVideo-Admin-Token"))
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "требуется пароль администратора LinkVideo", "code": "admin_password_required"})
		return false
	}
	now := time.Now()
	a.mu.Lock()
	expiry, ok := a.adminTokens[token]
	if ok && expiry.After(now) {
		a.adminTokens[token] = now.Add(adminTokenLifetime)
	}
	a.mu.Unlock()
	if !ok || !expiry.After(now) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "пароль нужно ввести повторно", "code": "admin_token_expired"})
		return false
	}
	return true
}

func (a *app) handleAuthStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": a.adminPasswordConfigured(),
		"managed":    true,
	})
}

func (a *app) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	writeJSON(w, http.StatusForbidden, map[string]string{
		"error": "пароль администратора установлен LinkVideo и не может быть изменён на компьютере клиента",
		"code":  "admin_password_managed",
	})
}

func (a *app) handleAuthVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "не удалось прочитать пароль"})
		return
	}
	token, err := a.verifyAdminPassword(body.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func authDebugString(_ string) string {
	return fmt.Sprintf("admin auth: managed=%t", validEmbeddedAdminDigest())
}

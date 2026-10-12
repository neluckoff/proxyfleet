package control

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestTelegramSettingsDoNotExposeToken(t *testing.T) {
	s := testServer(t)
	configureTestOwner(t, s)

	invalid := httptest.NewRecorder()
	s.updateTelegramSettings(invalid, httptest.NewRequest(http.MethodPatch, "/api/settings/telegram", bytes.NewBufferString(`{"enabled":true,"chatId":"123"}`)))
	if invalid.Code != http.StatusBadRequest || s.store.snapshot().Settings.TelegramEnabled {
		t.Fatalf("invalid Telegram settings were applied: %d", invalid.Code)
	}

	response := httptest.NewRecorder()
	body := `{"enabled":true,"botToken":"123456:secret-value","chatId":"123","proxyUrl":"http://user:pass@proxy.test:3128"}`
	s.updateTelegramSettings(response, httptest.NewRequest(http.MethodPatch, "/api/settings/telegram", bytes.NewBufferString(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("Telegram settings returned %d: %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "secret-value") {
		t.Fatal("Telegram token was exposed in settings response")
	}
	settings := s.store.snapshot().Settings
	if !settings.TelegramEnabled || settings.TelegramBotToken == "" || settings.TelegramProxyURL == "" {
		t.Fatalf("Telegram settings were not persisted: %#v", settings)
	}
}

func TestChangePasswordRotatesSession(t *testing.T) {
	s := testServer(t)
	settings := configureTestOwner(t, s)
	oldSecret := settings.SessionSecret

	response := httptest.NewRecorder()
	body := `{"currentPassword":"a-long-test-password","newPassword":"a-new-long-test-password"}`
	s.changePassword(response, httptest.NewRequest(http.MethodPost, "/api/settings/password", bytes.NewBufferString(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("change password returned %d: %s", response.Code, response.Body.String())
	}
	updated := s.store.snapshot().Settings
	if updated.SessionSecret == oldSecret {
		t.Fatal("session secret was not rotated")
	}
	if bcrypt.CompareHashAndPassword([]byte(updated.AdminPasswordHash), []byte("a-new-long-test-password")) != nil {
		t.Fatal("new password was not persisted")
	}
	if len(response.Result().Cookies()) != 1 {
		t.Fatal("current session was not refreshed")
	}
}

func TestTelegramProxyValidation(t *testing.T) {
	if !validTelegramProxyURL("http://user:pass@127.0.0.1:3128") {
		t.Fatal("valid HTTP proxy was rejected")
	}
	if validTelegramProxyURL("socks5://127.0.0.1:1080") {
		t.Fatal("unsupported SOCKS proxy was accepted")
	}
}

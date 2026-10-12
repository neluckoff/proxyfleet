package control

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/neluckoff/proxyfleet/internal/model"
	"golang.org/x/crypto/bcrypt"
)

const maxLogoBytes = 512 * 1024

func systemName(settings *model.Settings) string {
	if settings != nil && strings.TrimSpace(settings.SystemName) != "" {
		return strings.TrimSpace(settings.SystemName)
	}
	return "ProxyFleet"
}

func publicBranding(settings *model.Settings) map[string]any {
	logo := ""
	if settings != nil {
		logo = settings.LogoDataURL
	}
	return map[string]any{"systemName": systemName(settings), "logoDataUrl": logo}
}

func publicSettings(settings *model.Settings) map[string]any {
	result := publicBranding(settings)
	if settings == nil {
		return result
	}
	result["publicUrl"] = settings.PublicURL
	result["telegram"] = map[string]any{
		"enabled":     settings.TelegramEnabled,
		"configured":  settings.TelegramBotToken != "",
		"botUsername": settings.TelegramBotUsername,
		"chatId":      settings.TelegramChatID,
		"proxyUrl":    settings.TelegramProxyURL,
	}
	return result
}

func (s *server) branding(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, publicBranding(s.store.snapshot().Settings))
}

func (s *server) getSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, publicSettings(s.store.snapshot().Settings))
}

func (s *server) updateBranding(w http.ResponseWriter, r *http.Request) {
	var input struct {
		SystemName  string `json:"systemName"`
		LogoDataURL string `json:"logoDataUrl"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.SystemName = strings.TrimSpace(input.SystemName)
	if len(input.SystemName) < 2 || len(input.SystemName) > 64 {
		writeError(w, http.StatusBadRequest, "Название должно содержать от 2 до 64 символов")
		return
	}
	if input.LogoDataURL != "" && !validLogoDataURL(input.LogoDataURL) {
		writeError(w, http.StatusBadRequest, "Используйте PNG, JPEG или WebP размером до 512 КБ")
		return
	}
	err := s.store.update(func(state *model.State) error {
		if state.Settings == nil {
			return errors.New("settings are missing")
		}
		state.Settings.SystemName = input.SystemName
		state.Settings.LogoDataURL = input.LogoDataURL
		state.Events = append(state.Events, newEvent("settings.branding", "Обновлено оформление панели", "", nil))
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось сохранить оформление")
		return
	}
	writeJSON(w, http.StatusOK, publicSettings(s.store.snapshot().Settings))
}

func validLogoDataURL(value string) bool {
	prefixes := []string{"data:image/png;base64,", "data:image/jpeg;base64,", "data:image/webp;base64,"}
	for _, prefix := range prefixes {
		if !strings.HasPrefix(value, prefix) {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, prefix))
		return err == nil && len(data) > 0 && len(data) <= maxLogoBytes
	}
	return false
}

func (s *server) updateTelegramSettings(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Enabled    *bool   `json:"enabled"`
		BotToken   *string `json:"botToken"`
		ChatID     *string `json:"chatId"`
		ProxyURL   *string `json:"proxyUrl"`
		ClearToken bool    `json:"clearToken"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.ProxyURL != nil {
		proxyURL := strings.TrimSpace(*input.ProxyURL)
		if proxyURL != "" && !validTelegramProxyURL(proxyURL) {
			writeError(w, http.StatusBadRequest, "Proxy должен начинаться с http:// или https://")
			return
		}
	}
	err := s.store.update(func(state *model.State) error {
		settings := state.Settings
		if settings == nil {
			return errors.New("settings are missing")
		}
		candidate := *settings
		if input.Enabled != nil {
			candidate.TelegramEnabled = *input.Enabled
		}
		if input.BotToken != nil && strings.TrimSpace(*input.BotToken) != "" {
			candidate.TelegramBotToken = strings.TrimSpace(*input.BotToken)
			candidate.TelegramBotUsername = ""
		}
		if input.ChatID != nil {
			candidate.TelegramChatID = strings.TrimSpace(*input.ChatID)
		}
		if input.ProxyURL != nil {
			candidate.TelegramProxyURL = strings.TrimSpace(*input.ProxyURL)
		}
		if input.ClearToken {
			candidate.TelegramEnabled = false
			candidate.TelegramBotToken = ""
			candidate.TelegramBotUsername = ""
		}
		if candidate.TelegramEnabled && (candidate.TelegramBotToken == "" || candidate.TelegramChatID == "") {
			return errors.New("token and chat id are required")
		}
		*settings = candidate
		state.Events = append(state.Events, newEvent("settings.telegram", "Обновлены настройки Telegram", "", nil))
		return nil
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "Для уведомлений нужны токен бота и Chat ID")
		return
	}
	writeJSON(w, http.StatusOK, publicSettings(s.store.snapshot().Settings))
}

func validTelegramProxyURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}

func (s *server) changePassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if len(input.NewPassword) < 12 {
		writeError(w, http.StatusBadRequest, "Новый пароль должен содержать минимум 12 символов")
		return
	}
	current := s.store.snapshot().Settings
	if current == nil || bcrypt.CompareHashAndPassword([]byte(current.AdminPasswordHash), []byte(input.CurrentPassword)) != nil {
		writeError(w, http.StatusUnauthorized, "Текущий пароль указан неверно")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.NewPassword), 12)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось защитить новый пароль")
		return
	}
	err = s.store.update(func(state *model.State) error {
		state.Settings.AdminPasswordHash = string(hash)
		state.Settings.SessionSecret = randomToken(32)
		state.Events = append(state.Events, newEvent("settings.password", "Пароль владельца изменён", "", nil))
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось изменить пароль")
		return
	}
	updated := s.store.snapshot().Settings
	s.setSessionCookie(w, r, updated)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

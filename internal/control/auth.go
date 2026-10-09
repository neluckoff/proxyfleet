package control

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/neluckoff/proxyfleet/internal/model"
	"golang.org/x/crypto/bcrypt"
)

var errAlreadyConfigured = errors.New("control plane is already configured")

func (s *server) setupStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"configured": s.store.snapshot().Settings != nil})
}

func (s *server) setup(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username  string `json:"username"`
		Password  string `json:"password"`
		PublicURL string `json:"publicUrl"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if !validAdminUsername(input.Username) {
		writeError(w, http.StatusBadRequest, "Логин должен содержать 3–64 символа: латинские буквы, цифры, точку, дефис или подчёркивание")
		return
	}
	if len(input.Password) < 12 {
		writeError(w, http.StatusBadRequest, "Пароль должен содержать минимум 12 символов")
		return
	}
	input.PublicURL = strings.TrimRight(strings.TrimSpace(input.PublicURL), "/")
	if !validPublicURL(input.PublicURL) {
		writeError(w, http.StatusBadRequest, "Укажите полный адрес панели с http:// или https://")
		return
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(input.Password), 12)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось защитить пароль")
		return
	}
	settings := &model.Settings{
		AdminUsername: input.Username, AdminPasswordHash: string(passwordHash),
		SessionSecret: randomToken(32), PublicURL: input.PublicURL, CreatedAt: time.Now().UTC(),
	}
	err = s.store.update(func(state *model.State) error {
		if state.Settings != nil {
			return errAlreadyConfigured
		}
		state.Settings = settings
		state.Events = append(state.Events, newEvent("control.configured", "Создан владелец панели", "", map[string]any{"username": settings.AdminUsername}))
		return nil
	})
	if errors.Is(err, errAlreadyConfigured) {
		writeError(w, http.StatusConflict, "Панель уже настроена")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось сохранить настройки")
		return
	}

	s.setSessionCookie(w, r, settings)
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "username": settings.AdminUsername})
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	settings := s.store.snapshot().Settings
	if settings == nil {
		writeError(w, http.StatusPreconditionRequired, "Сначала создайте владельца панели")
		return
	}
	userOK := subtle.ConstantTimeCompare([]byte(input.Username), []byte(settings.AdminUsername)) == 1
	passOK := bcrypt.CompareHashAndPassword([]byte(settings.AdminPasswordHash), []byte(input.Password)) == nil
	if !userOK || !passOK {
		time.Sleep(350 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, "Неверный логин или пароль")
		return
	}

	s.setSessionCookie(w, r, settings)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "username": settings.AdminUsername})
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "proxyfleet_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) setSessionCookie(w http.ResponseWriter, r *http.Request, settings *model.Settings) {
	expires := time.Now().Add(24 * time.Hour)
	http.SetCookie(w, &http.Cookie{
		Name: "proxyfleet_session", Value: signSession(settings, expires), Path: "/",
		Expires: expires, HttpOnly: true, Secure: requestIsHTTPS(r), SameSite: http.SameSiteStrictMode,
	})
}

func (s *server) withAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		settings := s.store.snapshot().Settings
		if settings == nil {
			writeError(w, http.StatusPreconditionRequired, "Сначала завершите настройку панели")
			return
		}
		cookie, err := r.Cookie("proxyfleet_session")
		if err != nil || !verifySession(settings, cookie.Value) {
			writeError(w, http.StatusUnauthorized, "Требуется вход")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func signSession(settings *model.Settings, expires time.Time) string {
	payload := settings.AdminUsername + "|" + strconv.FormatInt(expires.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(settings.SessionSecret))
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verifySession(settings *model.Settings, value string) bool {
	parts := strings.SplitN(value, ".", 2)
	if len(parts) != 2 {
		return false
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(settings.SessionSecret))
	_, _ = mac.Write(payloadBytes)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return false
	}
	payloadParts := strings.SplitN(string(payloadBytes), "|", 2)
	if len(payloadParts) != 2 || payloadParts[0] != settings.AdminUsername {
		return false
	}
	exp, err := strconv.ParseInt(payloadParts[1], 10, 64)
	return err == nil && time.Now().Unix() < exp
}

func validAdminUsername(username string) bool {
	if len(username) < 3 || len(username) > 64 {
		return false
	}
	for _, char := range username {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("._-", char) {
			continue
		}
		return false
	}
	return true
}

func validPublicURL(value string) bool {
	parsed, err := url.ParseRequestURI(value)
	return err == nil && parsed.Host != "" && (parsed.Scheme == "https" || parsed.Scheme == "http")
}

func requestIsHTTPS(r *http.Request) bool {
	forwardedProto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
	return r.TLS != nil || strings.EqualFold(forwardedProto, "https")
}

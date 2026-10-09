package control

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/neluckoff/proxyfleet/internal/model"
)

func newEvent(eventType, message, nodeID string, meta map[string]any) *model.Event {
	return &model.Event{ID: randomHex(8), Type: eventType, Message: message, NodeID: nodeID, CreatedAt: time.Now().UTC(), Meta: meta}
}

func randomToken(bytes int) string {
	buf := make([]byte, bytes)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

func randomHex(bytes int) string {
	buf := make([]byte, bytes)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func tokenHash(value string) string {
	return hex.EncodeToString(hashBytes(value))
}

func credentialHash(username, password string) string {
	return tokenHash(strings.ToLower(username) + "\x00" + password)
}

func hashBytes(value string) []byte {
	hash := sha256.Sum256([]byte(value))
	return hash[:]
}

func secureEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func validPort(port int) bool { return port >= 1024 && port <= 65535 }

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func trimTo(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func filepathClean(value string) string {
	value = strings.TrimPrefix(value, "/")
	value = strings.ReplaceAll(value, "..", "")
	return value
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "Некорректный JSON")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'; img-src 'self' data:")
		next.ServeHTTP(w, r)
	})
}

func requestLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		if !strings.HasPrefix(r.URL.Path, "/api/agent/heartbeat") {
			log.Info("http request", "method", r.Method, "path", r.URL.Path, "durationMs", time.Since(started).Milliseconds())
		}
	})
}

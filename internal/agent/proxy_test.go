package agent

import (
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProxyAuthorization(t *testing.T) {
	p := newProxyRuntime(slog.New(slog.NewTextHandler(io.Discard, nil)))
	p.users.Store(map[string]string{"minedecor": credentialHash("minedecor", "a-secure-test-password")})

	request := httptest.NewRequest(http.MethodConnect, "https://api.telegram.org", nil)
	response := httptest.NewRecorder()
	p.handleHTTP(response, request)
	if response.Code != http.StatusProxyAuthRequired {
		t.Fatalf("anonymous request returned %d", response.Code)
	}

	request = httptest.NewRequest(http.MethodConnect, "https://api.telegram.org", nil)
	request.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("minedecor:wrong")))
	response = httptest.NewRecorder()
	p.handleHTTP(response, request)
	if response.Code != http.StatusProxyAuthRequired {
		t.Fatalf("wrong password returned %d", response.Code)
	}
}

func TestPrivateTargetsBlocked(t *testing.T) {
	p := newProxyRuntime(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := p.dialTarget(context.Background(), "127.0.0.1:8080"); err == nil {
		t.Fatal("loopback target must be blocked")
	}
}

func TestCredentialHashStable(t *testing.T) {
	first := credentialHash("MineDecor", "password-123")
	second := credentialHash("minedecor", "password-123")
	if first != second {
		t.Fatal("username hashing must be case-insensitive")
	}
}

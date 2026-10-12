package agent

import (
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/neluckoff/proxyfleet/internal/model"
)

func TestProxyAuthorization(t *testing.T) {
	p := newProxyRuntime(slog.New(slog.NewTextHandler(io.Discard, nil)))
	p.httpUsers.Store(map[string]string{"minedecor": credentialHash("minedecor", "a-secure-test-password")})

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

func TestCredentialsAreScopedToProtocol(t *testing.T) {
	hash := credentialHash("customer", "a-secure-test-password")
	httpUsers, socksUsers := splitCredentials([]model.AgentUser{
		{Username: "http-only", PasswordHash: hash, Protocol: "http", Enabled: true},
		{Username: "socks-only", PasswordHash: hash, Protocol: "socks5", Enabled: true},
		{Username: "legacy", PasswordHash: hash, Protocol: "both", Enabled: true},
		{Username: "disabled", PasswordHash: hash, Protocol: "http", Enabled: false},
	})
	if _, ok := httpUsers["http-only"]; !ok {
		t.Fatal("HTTP credential is missing from HTTP listener")
	}
	if _, ok := socksUsers["http-only"]; ok {
		t.Fatal("HTTP credential leaked to SOCKS5 listener")
	}
	if _, ok := socksUsers["socks-only"]; !ok {
		t.Fatal("SOCKS5 credential is missing from SOCKS5 listener")
	}
	if _, ok := httpUsers["socks-only"]; ok {
		t.Fatal("SOCKS5 credential leaked to HTTP listener")
	}
	if _, ok := httpUsers["legacy"]; !ok {
		t.Fatal("legacy credential must remain available on HTTP")
	}
	if _, ok := socksUsers["legacy"]; !ok {
		t.Fatal("legacy credential must remain available on SOCKS5")
	}
	if _, ok := httpUsers["disabled"]; ok {
		t.Fatal("disabled credential must not be installed")
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

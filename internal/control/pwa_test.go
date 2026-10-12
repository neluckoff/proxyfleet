package control

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPWAInfoCreatesKeysWithoutExposingPrivateKey(t *testing.T) {
	s := testServer(t)
	configureTestOwner(t, s)
	response := httptest.NewRecorder()
	s.pwaInfo(response, httptest.NewRequest(http.MethodGet, "/api/pwa", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("pwa info returned %d: %s", response.Code, response.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["publicKey"] == "" {
		t.Fatal("PWA public key is missing")
	}
	if _, exposed := result["privateKey"]; exposed {
		t.Fatal("PWA private key must not be exposed")
	}
	settings := s.store.snapshot().Settings
	if settings.VAPIDPrivateKey == "" || settings.DataEncryptionKey == "" {
		t.Fatal("PWA and encryption keys were not persisted")
	}
}

func TestPWASubscriptionPreferencesRoundTrip(t *testing.T) {
	s := testServer(t)
	configureTestOwner(t, s)
	body := []byte(`{
		"endpoint":"https://push.example.test/subscription/1",
		"keys":{"p256dh":"browser-public-key","auth":"browser-auth"},
		"preferences":{"nodes":true,"proxies":false,"system":true}
	}`)
	response := httptest.NewRecorder()
	s.subscribePWA(response, httptest.NewRequest(http.MethodPost, "/api/pwa/subscriptions", bytes.NewReader(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("subscribe returned %d: %s", response.Code, response.Body.String())
	}

	infoRequest := httptest.NewRequest(http.MethodGet, "/api/pwa?endpoint=https%3A%2F%2Fpush.example.test%2Fsubscription%2F1", nil)
	infoResponse := httptest.NewRecorder()
	s.pwaInfo(infoResponse, infoRequest)
	var info struct {
		Subscription struct {
			Preferences map[string]bool `json:"preferences"`
		} `json:"subscription"`
	}
	if err := json.Unmarshal(infoResponse.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if !info.Subscription.Preferences["nodes"] || info.Subscription.Preferences["proxies"] || !info.Subscription.Preferences["system"] {
		t.Fatalf("unexpected preferences: %#v", info.Subscription.Preferences)
	}
}

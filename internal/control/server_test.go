package control

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/neluckoff/proxyfleet/internal/model"
	"golang.org/x/crypto/bcrypt"
)

func testServer(t *testing.T) *server {
	t.Helper()
	st, err := openStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &server{
		store: st, nodeImage: "proxyfleet:test",
	}
}

func configureTestOwner(t *testing.T, s *server) *model.Settings {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("a-long-test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	settings := &model.Settings{
		AdminUsername: "admin", AdminPasswordHash: string(hash),
		SessionSecret: "01234567890123456789012345678901", PublicURL: "https://proxy.example.test", CreatedAt: time.Now().UTC(),
	}
	if err := s.store.update(func(state *model.State) error { state.Settings = settings; return nil }); err != nil {
		t.Fatal(err)
	}
	return settings
}

func TestSessionSigning(t *testing.T) {
	s := testServer(t)
	settings := configureTestOwner(t, s)
	if !verifySession(settings, signSession(settings, time.Now().Add(time.Hour))) {
		t.Fatal("fresh session must be valid")
	}
	if verifySession(settings, signSession(settings, time.Now().Add(-time.Hour))) {
		t.Fatal("expired session must be rejected")
	}
}

func TestInitialSetupAndLogin(t *testing.T) {
	s := testServer(t)
	setupBody := []byte(`{"username":"owner","password":"a-very-long-password","publicUrl":"https://proxy.example.test/"}`)
	setupRequest := httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(setupBody))
	setupRequest.Header.Set("X-Forwarded-Proto", "https")
	setupResponse := httptest.NewRecorder()
	s.setup(setupResponse, setupRequest)
	if setupResponse.Code != http.StatusCreated {
		t.Fatalf("setup returned %d: %s", setupResponse.Code, setupResponse.Body.String())
	}
	settings := s.store.snapshot().Settings
	if settings == nil || settings.SessionSecret == "" || settings.DataEncryptionKey == "" || settings.VAPIDPublicKey == "" || settings.VAPIDPrivateKey == "" || settings.PublicURL != "https://proxy.example.test" {
		t.Fatalf("settings were not persisted correctly: %#v", settings)
	}
	if settings.AdminPasswordHash == "a-very-long-password" || bcrypt.CompareHashAndPassword([]byte(settings.AdminPasswordHash), []byte("a-very-long-password")) != nil {
		t.Fatal("password was not stored as a valid bcrypt hash")
	}
	cookies := setupResponse.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("setup did not create a secure session cookie: %#v", cookies)
	}

	duplicateResponse := httptest.NewRecorder()
	s.setup(duplicateResponse, httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(setupBody)))
	if duplicateResponse.Code != http.StatusConflict {
		t.Fatalf("second setup returned %d", duplicateResponse.Code)
	}

	loginBody := []byte(`{"username":"owner","password":"a-very-long-password"}`)
	loginResponse := httptest.NewRecorder()
	s.login(loginResponse, httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(loginBody)))
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login returned %d: %s", loginResponse.Code, loginResponse.Body.String())
	}
}

func TestNodeEnrollmentFlow(t *testing.T) {
	s := testServer(t)
	createBody := []byte(`{"name":"Amsterdam 01","region":"NL","advertisedHost":"proxy.example.test","httpPort":3128,"socksPort":1080}`)
	createRequest := httptest.NewRequest(http.MethodPost, "/api/nodes", bytes.NewReader(createBody))
	createResponse := httptest.NewRecorder()
	s.createNode(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create node returned %d: %s", createResponse.Code, createResponse.Body.String())
	}
	var created struct {
		EnrollToken string `json:"enrollToken"`
	}
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.EnrollToken == "" {
		t.Fatal("enrollment token is missing")
	}

	enrollPayload, _ := json.Marshal(map[string]string{"token": created.EnrollToken})
	enrollRequest := httptest.NewRequest(http.MethodPost, "/api/agent/enroll", bytes.NewReader(enrollPayload))
	enrollResponse := httptest.NewRecorder()
	s.agentEnroll(enrollResponse, enrollRequest)
	if enrollResponse.Code != http.StatusOK {
		t.Fatalf("enroll returned %d: %s", enrollResponse.Code, enrollResponse.Body.String())
	}
	var enrolled struct {
		NodeID     string `json:"nodeId"`
		AgentToken string `json:"agentToken"`
	}
	if err := json.Unmarshal(enrollResponse.Body.Bytes(), &enrolled); err != nil {
		t.Fatal(err)
	}
	if enrolled.NodeID == "" || enrolled.AgentToken == "" {
		t.Fatal("agent credentials are missing")
	}

	reuseRequest := httptest.NewRequest(http.MethodPost, "/api/agent/enroll", bytes.NewReader(enrollPayload))
	reuseResponse := httptest.NewRecorder()
	s.agentEnroll(reuseResponse, reuseRequest)
	if reuseResponse.Code != http.StatusUnauthorized {
		t.Fatalf("one-time token reuse returned %d", reuseResponse.Code)
	}

	configRequest := httptest.NewRequest(http.MethodGet, "/api/agent/config", nil)
	configRequest.Header.Set("Authorization", "Bearer "+enrolled.NodeID+"."+enrolled.AgentToken)
	configResponse := httptest.NewRecorder()
	s.withAgent(s.agentConfig).ServeHTTP(configResponse, configRequest)
	if configResponse.Code != http.StatusOK {
		t.Fatalf("config returned %d: %s", configResponse.Code, configResponse.Body.String())
	}
}

package control

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func testServer(t *testing.T) *server {
	t.Helper()
	st, err := openStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &server{
		log: slog.New(slog.NewTextHandler(io.Discard, nil)), store: st,
		adminUser: "admin", adminPassword: "a-long-test-password", sessionSecret: []byte("01234567890123456789012345678901"),
		publicURL: "https://proxy.example.test", nodeImage: "proxyfleet:test", cookieSecure: true,
	}
}

func TestSessionSigning(t *testing.T) {
	s := testServer(t)
	if !s.verifySession(s.signSession(time.Now().Add(time.Hour))) {
		t.Fatal("fresh session must be valid")
	}
	if s.verifySession(s.signSession(time.Now().Add(-time.Hour))) {
		t.Fatal("expired session must be rejected")
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

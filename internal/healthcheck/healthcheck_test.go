package healthcheck

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestControlHealthcheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	t.Setenv("CONTROL_URL", "")
	t.Setenv("HEALTHCHECK_URL", server.URL)
	if err := Run(context.Background()); err != nil {
		t.Fatalf("control healthcheck failed: %v", err)
	}
}

func TestAgentHealthcheckOverUnixSocket(t *testing.T) {
	tempDir, err := os.MkdirTemp("/tmp", "pf-health-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })
	socketPath := filepath.Join(tempDir, "agent.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()

	t.Setenv("CONTROL_URL", "https://control.example.test")
	t.Setenv("AGENT_HEALTH_SOCKET", socketPath)
	if err := Run(context.Background()); err != nil {
		t.Fatalf("agent healthcheck failed: %v", err)
	}
}

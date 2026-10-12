package agent

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAgentHealthRequiresProxyAndFreshControlSync(t *testing.T) {
	runner := &runner{proxy: newProxyRuntime(slog.New(slog.NewTextHandler(io.Discard, nil)))}

	response := httptest.NewRecorder()
	runner.health(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("uninitialized agent health returned %d", response.Code)
	}

	runner.proxy.running.Store(true)
	runner.lastSyncAt.Store(time.Now().Unix())
	response = httptest.NewRecorder()
	runner.health(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("ready agent health returned %d", response.Code)
	}

	runner.lastSyncAt.Store(time.Now().Add(-healthFreshness - time.Second).Unix())
	response = httptest.NewRecorder()
	runner.health(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("stale agent health returned %d", response.Code)
	}
}

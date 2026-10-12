package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"time"
)

const healthFreshness = 45 * time.Second

func (r *runner) serveHealth(ctx context.Context) error {
	socketPath := envAgent("AGENT_HEALTH_SOCKET", "/tmp/proxyfleet-agent-health.sock")
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(socketPath)
		return err
	}

	server := &http.Server{
		Handler:           http.HandlerFunc(r.health),
		ReadHeaderTimeout: 2 * time.Second,
		IdleTimeout:       5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		_ = listener.Close()
		_ = os.Remove(socketPath)
	}()
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			r.log.Error("health server stopped", "error", err)
		}
	}()
	return nil
}

func (r *runner) health(w http.ResponseWriter, _ *http.Request) {
	lastSyncUnix := r.lastSyncAt.Load()
	lastSyncAt := time.Unix(lastSyncUnix, 0)
	proxyRunning := r.proxy.Running()
	controlFresh := lastSyncUnix > 0 && time.Since(lastSyncAt) <= healthFreshness
	ok := proxyRunning && controlFresh

	w.Header().Set("Content-Type", "application/json")
	if !ok {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":              ok,
		"proxyRunning":    proxyRunning,
		"controlFresh":    controlFresh,
		"lastControlSync": lastSyncAt.UTC(),
	})
}

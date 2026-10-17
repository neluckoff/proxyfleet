package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/neluckoff/proxyfleet/internal/model"
)

const agentVersion = "0.5.2"

type persistedState struct {
	NodeID     string            `json:"nodeId"`
	AgentToken string            `json:"agentToken"`
	Config     model.AgentConfig `json:"config"`
}

type runner struct {
	log        *slog.Logger
	controlURL string
	enroll     string
	statePath  string
	httpClient *http.Client
	startedAt  time.Time
	state      persistedState
	proxy      *proxyRuntime
	publicIP   string
	tgOK       bool
	tgLatency  int64
	lastError  string
	lastSyncAt atomic.Int64
}

func Run(ctx context.Context, log *slog.Logger) error {
	controlURL := strings.TrimRight(strings.TrimSpace(os.Getenv("CONTROL_URL")), "/")
	if controlURL == "" {
		return errors.New("CONTROL_URL is required")
	}
	r := &runner{
		log: log, controlURL: controlURL, enroll: strings.TrimSpace(os.Getenv("ENROLL_TOKEN")),
		statePath: envAgent("AGENT_STATE_PATH", "/data/agent.json"), startedAt: time.Now(),
		httpClient: &http.Client{Timeout: 20 * time.Second}, proxy: newProxyRuntime(log),
	}
	if err := r.loadState(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load agent state: %w", err)
	}
	if r.state.AgentToken == "" {
		if r.enroll == "" {
			return errors.New("ENROLL_TOKEN is required for the first start")
		}
		if err := r.enrollNode(ctx); err != nil {
			return err
		}
	}
	if r.state.Config.Version > 0 {
		if err := r.proxy.Apply(ctx, r.state.Config); err != nil {
			log.Warn("saved proxy config could not be applied", "error", err)
		}
	}
	if err := r.serveHealth(ctx); err != nil {
		return fmt.Errorf("start health server: %w", err)
	}

	log.Info("node agent started", "nodeId", r.state.NodeID, "controlUrl", r.controlURL, "version", agentVersion)
	if err := r.sync(ctx); err != nil {
		log.Warn("initial control sync failed", "error", err)
	}
	r.checkEgress(ctx)
	_ = r.heartbeat(ctx)

	syncTicker := time.NewTicker(10 * time.Second)
	heartbeatTicker := time.NewTicker(10 * time.Second)
	egressTicker := time.NewTicker(30 * time.Second)
	defer syncTicker.Stop()
	defer heartbeatTicker.Stop()
	defer egressTicker.Stop()
	defer r.proxy.Close()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-syncTicker.C:
			if err := r.sync(ctx); err != nil {
				r.lastError = err.Error()
				log.Warn("control sync failed", "error", err)
			}
		case <-egressTicker.C:
			r.checkEgress(ctx)
		case <-heartbeatTicker.C:
			if err := r.heartbeat(ctx); err != nil {
				log.Warn("heartbeat failed", "error", err)
			}
		}
	}
}

func (r *runner) enrollNode(ctx context.Context) error {
	payload := map[string]string{"token": r.enroll}
	var response struct {
		NodeID     string `json:"nodeId"`
		AgentToken string `json:"agentToken"`
	}
	if err := r.request(ctx, http.MethodPost, "/api/agent/enroll", payload, &response, false); err != nil {
		return fmt.Errorf("enroll node: %w", err)
	}
	r.state.NodeID = response.NodeID
	r.state.AgentToken = response.AgentToken
	if err := r.saveState(); err != nil {
		return fmt.Errorf("save enrollment: %w", err)
	}
	r.log.Info("node enrolled", "nodeId", response.NodeID)
	return nil
}

func (r *runner) sync(ctx context.Context) error {
	var config model.AgentConfig
	if err := r.request(ctx, http.MethodGet, "/api/agent/config", nil, &config, true); err != nil {
		return err
	}
	if config.Version == r.state.Config.Version && r.proxy.Running() {
		r.lastSyncAt.Store(time.Now().Unix())
		return nil
	}
	if err := r.proxy.Apply(ctx, config); err != nil {
		return fmt.Errorf("apply proxy config: %w", err)
	}
	r.state.Config = config
	r.lastError = ""
	if err := r.saveState(); err != nil {
		return err
	}
	r.lastSyncAt.Store(time.Now().Unix())
	r.log.Info("proxy configuration applied", "version", config.Version, "httpPort", config.HTTPPort, "socksPort", config.SOCKSPort, "webProxyPort", config.WebProxyPort, "users", len(config.Users))
	return nil
}

func (r *runner) heartbeat(ctx context.Context) error {
	stats := r.proxy.Stats()
	system := readSystemStats()
	beat := model.Heartbeat{
		AgentVersion: agentVersion, PublicIP: r.publicIP, TelegramOK: r.tgOK, TelegramLatencyMS: r.tgLatency,
		LastError: r.lastError, UptimeSeconds: int64(time.Since(r.startedAt).Seconds()),
		ActiveConnections: stats.Active, BytesUp: stats.Up, BytesDown: stats.Down,
		MemoryAllocBytes: system.MemoryAllocBytes, MemorySysBytes: system.MemorySysBytes, Goroutines: system.Goroutines,
		HostMemoryUsedBytes: system.HostMemoryUsedBytes, HostMemoryTotalBytes: system.HostMemoryTotalBytes, LoadOne: system.LoadOne,
	}
	return r.request(ctx, http.MethodPost, "/api/agent/heartbeat", beat, nil, true)
}

func (r *runner) checkEgress(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	started := time.Now()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.telegram.org", nil)
	response, err := r.httpClient.Do(req)
	if err != nil {
		r.tgOK = false
		r.tgLatency = 0
		r.lastError = "Telegram: " + err.Error()
	} else {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		_ = response.Body.Close()
		r.tgOK = response.StatusCode >= 200 && response.StatusCode < 500
		r.tgLatency = time.Since(started).Milliseconds()
		if r.tgOK && strings.HasPrefix(r.lastError, "Telegram:") {
			r.lastError = ""
		}
	}

	if r.publicIP == "" {
		req, _ = http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org", nil)
		if response, err = r.httpClient.Do(req); err == nil {
			data, _ := io.ReadAll(io.LimitReader(response.Body, 128))
			_ = response.Body.Close()
			r.publicIP = strings.TrimSpace(string(data))
		}
	}
}

func (r *runner) request(ctx context.Context, method, path string, body, target any, authenticated bool) error {
	var requestBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		requestBody = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.controlURL+path, requestBody)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if authenticated {
		req.Header.Set("Authorization", "Bearer "+r.state.NodeID+"."+r.state.AgentToken)
	}
	response, err := r.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("control returned %s: %s", response.Status, strings.TrimSpace(string(data)))
	}
	if target != nil {
		if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(target); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) loadState() error {
	data, err := os.ReadFile(r.statePath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &r.state)
}

func (r *runner) saveState() error {
	if err := os.MkdirAll(filepath.Dir(r.statePath), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.statePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, r.statePath)
}

func envAgent(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const webProxyDecoy = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Welcome</title><style>body{margin:0;min-height:100vh;display:grid;place-items:center;background:#111;color:#ddd;font:16px system-ui,sans-serif}main{max-width:36rem;padding:3rem}h1{color:#fff}</style></head><body><main><h1>Welcome</h1><p>This service is online.</p></main></body></html>`

type webProxyRuntime struct {
	log       *slog.Logger
	mu        sync.Mutex
	cmd       *exec.Cmd
	done      chan struct{}
	dataDir   string
	signature string
	required  atomic.Bool
	running   atomic.Bool
}

func newWebProxyRuntime(log *slog.Logger) *webProxyRuntime {
	return &webProxyRuntime{log: log, dataDir: envAgent("WEB_PROXY_DATA_DIR", "/data/webproxy")}
}

func (w *webProxyRuntime) Ready() bool {
	return !w.required.Load() || w.running.Load()
}

func (w *webProxyRuntime) Apply(parent context.Context, port int, host, publicIP string, users map[string]string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.required.Store(len(users) > 0)
	if len(users) == 0 {
		w.stopLocked()
		return nil
	}
	if port < 1024 || port > 65535 {
		return fmt.Errorf("invalid WEB proxy upstream port %d", port)
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || strings.ContainsAny(host, "/: ") || net.ParseIP(host) != nil {
		return fmt.Errorf("invalid WEB proxy hostname %q", host)
	}
	if net.ParseIP(strings.TrimSpace(publicIP)) == nil {
		return fmt.Errorf("invalid WEB proxy public IP %q", publicIP)
	}
	signature := webProxySignature(port, host, publicIP, users)
	if w.running.Load() && w.signature == signature {
		return nil
	}
	w.stopLocked()
	publicDir := filepath.Join(w.dataDir, "public")
	if err := os.MkdirAll(publicDir, 0o700); err != nil {
		return fmt.Errorf("create WEB proxy data directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(publicDir, "index.html"), []byte(webProxyDecoy), 0o600); err != nil {
		return fmt.Errorf("write WEB proxy decoy: %w", err)
	}
	configPath := filepath.Join(w.dataDir, "tgwebproxy.toml")
	adminSocket := filepath.Join(w.dataDir, "admin.sock")
	config := renderTGWebProxyConfig(port, host, publicDir, adminSocket, users)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		return fmt.Errorf("write WEB proxy config: %w", err)
	}
	binary := envAgent("TGWEBPROXY_BINARY", "/usr/local/bin/tgwebproxy")
	if _, err := os.Stat(binary); err != nil {
		return fmt.Errorf("tgwebproxy binary is unavailable: %w", err)
	}
	cmd := exec.CommandContext(parent, binary, "-config", configPath)
	cmd.Dir = w.dataDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start tgwebproxy WEB relay: %w", err)
	}
	w.cmd = cmd
	w.done = make(chan struct{})
	w.signature = signature
	w.running.Store(true)
	done := w.done
	go w.observe(cmd, done)
	w.log.Info("Telegram WEB proxy listener started", "upstream", net.JoinHostPort("127.0.0.1", fmt.Sprint(port)), "host", host, "proxies", len(users))
	return nil
}

func (w *webProxyRuntime) observe(cmd *exec.Cmd, done chan struct{}) {
	err := cmd.Wait()
	close(done)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cmd != cmd {
		return
	}
	w.running.Store(false)
	w.cmd = nil
	w.done = nil
	if err != nil && !errors.Is(err, context.Canceled) {
		w.log.Error("Telegram WEB proxy stopped", "error", err)
	}
}

func (w *webProxyRuntime) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.required.Store(false)
	w.stopLocked()
}

func (w *webProxyRuntime) stopLocked() {
	if w.cmd == nil || w.cmd.Process == nil {
		w.running.Store(false)
		w.cmd = nil
		w.done = nil
		return
	}
	cmd, done := w.cmd, w.done
	w.cmd = nil
	w.done = nil
	w.running.Store(false)
	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
}

func webProxySignature(port int, host, publicIP string, users map[string]string) string {
	keys := sortedWebProxyUsers(users)
	var value strings.Builder
	fmt.Fprintf(&value, "%d\n%s\n%s\n", port, host, strings.TrimSpace(publicIP))
	for _, key := range keys {
		fmt.Fprintf(&value, "%s=%s\n", key, users[key])
	}
	sum := sha256.Sum256([]byte(value.String()))
	return hex.EncodeToString(sum[:])
}

func renderTGWebProxyConfig(port int, host, publicDir, adminSocket string, users map[string]string) string {
	keys := sortedWebProxyUsers(users)
	var config strings.Builder
	fmt.Fprintf(&config, `domain = %q
listen = %q
behind_proxy = true
site = %q
log_level = "info"
max_streams = 64

[admin]
listen = %q
`, host, net.JoinHostPort("127.0.0.1", fmt.Sprint(port)), publicDir, adminSocket)
	for _, key := range keys {
		fmt.Fprintf(&config, `
[[secret]]
value = %q
label = %q
`, webProxyRelaySecret(users[key]), key)
	}
	return config.String()
}

func webProxyRelaySecret(secret string) string {
	secret = strings.ToLower(strings.TrimSpace(secret))
	if strings.HasPrefix(secret, "dd") {
		return secret
	}
	return "dd" + secret
}

func sortedWebProxyUsers(users map[string]string) []string {
	keys := make([]string, 0, len(users))
	for key := range users {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
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

type mtProtoRuntime struct {
	log       *slog.Logger
	mu        sync.Mutex
	cmd       *exec.Cmd
	done      chan struct{}
	dataDir   string
	port      int
	signature string
	required  atomic.Bool
	running   atomic.Bool
}

func newMTProtoRuntime(log *slog.Logger) *mtProtoRuntime {
	return &mtProtoRuntime{log: log, dataDir: envAgent("MTPROTO_DATA_DIR", "/data/mtproto")}
}

func (m *mtProtoRuntime) Ready() bool {
	return !m.required.Load() || m.running.Load()
}

func (m *mtProtoRuntime) Apply(parent context.Context, port int, users map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.required.Store(len(users) > 0)
	if len(users) == 0 {
		m.stopLocked()
		return nil
	}
	if port < 1024 || port > 65535 {
		return fmt.Errorf("invalid MTProto port %d", port)
	}
	signature := mtProtoSignature(port, users)
	if m.running.Load() && m.port == port && m.signature == signature {
		return nil
	}
	m.stopLocked()
	if err := os.MkdirAll(m.dataDir, 0o700); err != nil {
		return fmt.Errorf("create MTProto data directory: %w", err)
	}
	configPath := filepath.Join(m.dataDir, "telemt.toml")
	if err := os.WriteFile(configPath, []byte(renderTelemtConfig(port, users)), 0o600); err != nil {
		return fmt.Errorf("write MTProto config: %w", err)
	}
	binary := envAgent("TELEMT_BINARY", "/usr/local/bin/telemt")
	if _, err := os.Stat(binary); err != nil {
		return fmt.Errorf("Telemt binary is unavailable: %w", err)
	}
	cmd := exec.CommandContext(parent, binary, configPath)
	cmd.Dir = m.dataDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "RUST_LOG=warn")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Telemt: %w", err)
	}
	m.cmd = cmd
	m.done = make(chan struct{})
	m.port = port
	m.signature = signature
	m.running.Store(true)
	done := m.done
	go m.observe(cmd, done)
	m.log.Info("MTProto listener started", "port", port, "proxies", len(users))
	return nil
}

func (m *mtProtoRuntime) observe(cmd *exec.Cmd, done chan struct{}) {
	err := cmd.Wait()
	close(done)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != cmd {
		return
	}
	m.running.Store(false)
	m.cmd = nil
	m.done = nil
	if err != nil && !errors.Is(err, context.Canceled) {
		m.log.Error("MTProto listener stopped", "error", err)
	}
}

func (m *mtProtoRuntime) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.required.Store(false)
	m.stopLocked()
}

func (m *mtProtoRuntime) stopLocked() {
	if m.cmd == nil || m.cmd.Process == nil {
		m.running.Store(false)
		m.cmd = nil
		m.done = nil
		return
	}
	cmd, done := m.cmd, m.done
	m.cmd = nil
	m.done = nil
	m.running.Store(false)
	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	}
}

func mtProtoSignature(port int, users map[string]string) string {
	keys := make([]string, 0, len(users))
	for key := range users {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var value strings.Builder
	value.WriteString(fmt.Sprintf("%d\n", port))
	for _, key := range keys {
		value.WriteString(key)
		value.WriteByte('=')
		value.WriteString(users[key])
		value.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(value.String()))
	return hex.EncodeToString(sum[:])
}

func renderTelemtConfig(port int, users map[string]string) string {
	keys := make([]string, 0, len(users))
	for key := range users {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var config strings.Builder
	fmt.Fprintf(&config, `[general]
use_middle_proxy = true
log_level = "normal"

[general.modes]
classic = false
secure = true
tls = false

[general.links]
show = []

[server]
port = %d

[[server.listeners]]
ip = "0.0.0.0"

[server.api]
enabled = false

[access.users]
`, port)
	for _, key := range keys {
		fmt.Fprintf(&config, "%q = %q\n", key, users[key])
	}
	return config.String()
}

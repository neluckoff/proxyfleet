package agent

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/neluckoff/proxyfleet/internal/model"
)

type proxyStats struct {
	Active int64
	Up     int64
	Down   int64
}

type proxyRuntime struct {
	log        *slog.Logger
	mu         sync.Mutex
	users      atomic.Value
	allowLocal atomic.Bool
	httpServer *http.Server
	httpLn     net.Listener
	socksLn    net.Listener
	cancel     context.CancelFunc
	httpPort   int
	socksPort  int
	running    atomic.Bool
	active     atomic.Int64
	up         atomic.Int64
	down       atomic.Int64
}

func newProxyRuntime(log *slog.Logger) *proxyRuntime {
	p := &proxyRuntime{log: log}
	p.users.Store(map[string]string{})
	return p
}

func (p *proxyRuntime) Running() bool { return p.running.Load() }

func (p *proxyRuntime) Stats() proxyStats {
	return proxyStats{Active: p.active.Load(), Up: p.up.Load(), Down: p.down.Load()}
}

func (p *proxyRuntime) Apply(parent context.Context, config model.AgentConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	users := make(map[string]string, len(config.Users))
	for _, user := range config.Users {
		if user.Enabled {
			users[user.Username] = user.PasswordHash
		}
	}
	p.users.Store(users)
	p.allowLocal.Store(config.AllowPrivateTargets)

	if p.running.Load() && p.httpPort == config.HTTPPort && p.socksPort == config.SOCKSPort {
		return nil
	}
	p.closeLocked()

	httpLn, err := net.Listen("tcp", ":"+strconv.Itoa(config.HTTPPort))
	if err != nil {
		return fmt.Errorf("listen HTTP proxy: %w", err)
	}
	socksLn, err := net.Listen("tcp", ":"+strconv.Itoa(config.SOCKSPort))
	if err != nil {
		_ = httpLn.Close()
		return fmt.Errorf("listen SOCKS5 proxy: %w", err)
	}

	ctx, cancel := context.WithCancel(parent)
	p.cancel = cancel
	p.httpLn = httpLn
	p.socksLn = socksLn
	p.httpPort = config.HTTPPort
	p.socksPort = config.SOCKSPort
	p.httpServer = &http.Server{Handler: http.HandlerFunc(p.handleHTTP), ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	p.running.Store(true)

	go func() {
		if err := p.httpServer.Serve(httpLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
			p.log.Error("HTTP proxy stopped", "error", err)
		}
	}()
	go p.serveSOCKS(ctx, socksLn)
	return nil
}

func (p *proxyRuntime) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeLocked()
}

func (p *proxyRuntime) closeLocked() {
	p.running.Store(false)
	if p.cancel != nil {
		p.cancel()
	}
	if p.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = p.httpServer.Shutdown(ctx)
		cancel()
	}
	if p.httpLn != nil {
		_ = p.httpLn.Close()
	}
	if p.socksLn != nil {
		_ = p.socksLn.Close()
	}
	p.cancel = nil
	p.httpServer = nil
	p.httpLn = nil
	p.socksLn = nil
}

func (p *proxyRuntime) authenticate(username, password string) bool {
	expected, ok := p.users.Load().(map[string]string)[username]
	if !ok {
		return false
	}
	actual := credentialHash(username, password)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}

func (p *proxyRuntime) handleHTTP(w http.ResponseWriter, r *http.Request) {
	username, password, ok := parseProxyAuthorization(r.Header.Get("Proxy-Authorization"))
	if !ok || !p.authenticate(username, password) {
		w.Header().Set("Proxy-Authenticate", `Basic realm="ProxyFleet"`)
		http.Error(w, "Proxy authentication required", http.StatusProxyAuthRequired)
		return
	}
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}
	p.handleForward(w, r)
}

func (p *proxyRuntime) handleConnect(w http.ResponseWriter, r *http.Request) {
	target := r.Host
	if !strings.Contains(target, ":") {
		target += ":443"
	}
	destination, err := p.dialTarget(r.Context(), target)
	if err != nil {
		http.Error(w, "Target unavailable", http.StatusBadGateway)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		_ = destination.Close()
		http.Error(w, "Hijacking unsupported", http.StatusInternalServerError)
		return
	}
	client, _, err := hijacker.Hijack()
	if err != nil {
		_ = destination.Close()
		return
	}
	_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	p.relay(client, destination)
}

func (p *proxyRuntime) handleForward(w http.ResponseWriter, r *http.Request) {
	r.RequestURI = ""
	r.Header.Del("Proxy-Authorization")
	r.Header.Del("Proxy-Connection")
	if r.URL.Scheme == "" {
		r.URL.Scheme = "http"
	}
	if r.URL.Host == "" {
		r.URL.Host = r.Host
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return p.dialTarget(ctx, address)
		},
		ForceAttemptHTTP2: true,
		IdleConnTimeout:   30 * time.Second,
	}
	response, err := transport.RoundTrip(r)
	if err != nil {
		http.Error(w, "Target unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	for key, values := range response.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	written, _ := io.Copy(w, response.Body)
	p.down.Add(written)
	if r.ContentLength > 0 {
		p.up.Add(r.ContentLength)
	}
}

func (p *proxyRuntime) dialTarget(ctx context.Context, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if port == "" {
		return nil, errors.New("target port is required")
	}
	if !p.allowLocal.Load() {
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, errors.New("target has no addresses")
		}
		for _, ip := range ips {
			if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
				return nil, errors.New("private targets are blocked")
			}
		}
	}
	dialer := net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	return dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
}

func (p *proxyRuntime) relay(client, destination net.Conn) {
	p.active.Add(1)
	defer p.active.Add(-1)
	defer client.Close()
	defer destination.Close()

	type result struct {
		direction string
		bytes     int64
	}
	done := make(chan result, 2)
	go func() {
		n, _ := io.Copy(destination, client)
		done <- result{direction: "up", bytes: n}
	}()
	go func() {
		n, _ := io.Copy(client, destination)
		done <- result{direction: "down", bytes: n}
	}()
	first := <-done
	_ = client.SetDeadline(time.Now())
	_ = destination.SetDeadline(time.Now())
	second := <-done
	for _, item := range []result{first, second} {
		if item.direction == "up" {
			p.up.Add(item.bytes)
		} else {
			p.down.Add(item.bytes)
		}
	}
}

func (p *proxyRuntime) serveSOCKS(ctx context.Context, listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			p.log.Warn("SOCKS5 accept failed", "error", err)
			continue
		}
		go p.handleSOCKS(conn)
	}
}

func (p *proxyRuntime) handleSOCKS(client net.Conn) {
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(30 * time.Second))
	reader := bufio.NewReader(client)
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil || header[0] != 5 {
		return
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(reader, methods); err != nil {
		return
	}
	supportsAuth := false
	for _, method := range methods {
		if method == 2 {
			supportsAuth = true
		}
	}
	if !supportsAuth {
		_, _ = client.Write([]byte{5, 0xff})
		return
	}
	_, _ = client.Write([]byte{5, 2})

	if !p.socksAuthenticate(reader, client) {
		return
	}
	request := make([]byte, 4)
	if _, err := io.ReadFull(reader, request); err != nil || request[0] != 5 || request[1] != 1 {
		p.socksReply(client, 7)
		return
	}
	host, err := readSOCKSHost(reader, request[3])
	if err != nil {
		p.socksReply(client, 8)
		return
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(reader, portBytes); err != nil {
		return
	}
	port := binary.BigEndian.Uint16(portBytes)
	destination, err := p.dialTarget(context.Background(), net.JoinHostPort(host, strconv.Itoa(int(port))))
	if err != nil {
		p.socksReply(client, 5)
		return
	}
	p.socksReply(client, 0)
	_ = client.SetDeadline(time.Time{})
	p.relay(client, destination)
}

func (p *proxyRuntime) socksAuthenticate(reader *bufio.Reader, client net.Conn) bool {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil || header[0] != 1 {
		return false
	}
	usernameBytes := make([]byte, int(header[1]))
	if _, err := io.ReadFull(reader, usernameBytes); err != nil {
		return false
	}
	passwordLength, err := reader.ReadByte()
	if err != nil {
		return false
	}
	passwordBytes := make([]byte, int(passwordLength))
	if _, err := io.ReadFull(reader, passwordBytes); err != nil {
		return false
	}
	ok := p.authenticate(string(usernameBytes), string(passwordBytes))
	status := byte(1)
	if ok {
		status = 0
	}
	_, _ = client.Write([]byte{1, status})
	return ok
}

func (p *proxyRuntime) socksReply(client net.Conn, status byte) {
	_, _ = client.Write([]byte{5, status, 0, 1, 0, 0, 0, 0, 0, 0})
}

func readSOCKSHost(reader *bufio.Reader, addressType byte) (string, error) {
	switch addressType {
	case 1:
		value := make([]byte, 4)
		_, err := io.ReadFull(reader, value)
		return net.IP(value).String(), err
	case 3:
		length, err := reader.ReadByte()
		if err != nil {
			return "", err
		}
		value := make([]byte, int(length))
		_, err = io.ReadFull(reader, value)
		return string(value), err
	case 4:
		value := make([]byte, 16)
		_, err := io.ReadFull(reader, value)
		return net.IP(value).String(), err
	default:
		return "", errors.New("unsupported address type")
	}
}

func parseProxyAuthorization(value string) (string, string, bool) {
	if !strings.HasPrefix(value, "Basic ") {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "Basic "))
	if err != nil {
		return "", "", false
	}
	username, password, ok := strings.Cut(string(decoded), ":")
	return username, password, ok
}

func credentialHash(username, password string) string {
	hash := sha256.Sum256([]byte(strings.ToLower(username) + "\x00" + password))
	return hex.EncodeToString(hash[:])
}

package control

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/neluckoff/proxyfleet/internal/model"
)

const version = "0.1.0"

//go:embed web/*
var webAssets embed.FS

type server struct {
	log           *slog.Logger
	store         *store
	adminUser     string
	adminPassword string
	sessionSecret []byte
	publicURL     string
	nodeImage     string
	cookieSecure  bool
}

func Run(ctx context.Context, log *slog.Logger) error {
	dataPath := env("DATA_PATH", "/data/state.json")
	st, err := openStore(dataPath)
	if err != nil {
		return fmt.Errorf("open state: %w", err)
	}

	secret := os.Getenv("SESSION_SECRET")
	if len(secret) < 32 {
		return errors.New("SESSION_SECRET must contain at least 32 characters")
	}
	adminPassword := os.Getenv("ADMIN_PASSWORD")
	if len(adminPassword) < 12 {
		return errors.New("ADMIN_PASSWORD must contain at least 12 characters")
	}

	s := &server{
		log:           log,
		store:         st,
		adminUser:     env("ADMIN_USERNAME", "admin"),
		adminPassword: adminPassword,
		sessionSecret: []byte(secret),
		publicURL:     strings.TrimRight(env("PUBLIC_URL", "http://localhost:8080"), "/"),
		nodeImage:     env("NODE_IMAGE", "ghcr.io/neluckoff/proxyfleet:latest"),
		cookieSecure:  envBool("COOKIE_SECURE", false),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": version})
	})
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("POST /api/agent/enroll", s.agentEnroll)
	mux.HandleFunc("GET /api/agent/config", s.withAgent(s.agentConfig))
	mux.HandleFunc("POST /api/agent/heartbeat", s.withAgent(s.agentHeartbeat))
	mux.Handle("/api/", s.withAdmin(s.adminMux()))

	staticRoot, err := fs.Sub(webAssets, "web")
	if err != nil {
		return err
	}
	static := http.FileServer(http.FS(staticRoot))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		if strings.Contains(filepathClean(r.URL.Path), ".") {
			static.ServeHTTP(w, r)
			return
		}
		r.URL.Path = "/"
		static.ServeHTTP(w, r)
	})

	addr := env("LISTEN_ADDR", ":8080")
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           requestLog(log, securityHeaders(mux)),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Info("control plane starting", "address", addr, "publicUrl", s.publicURL, "version", version)
	err = httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *server) adminMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"username": s.adminUser, "version": version})
	})
	mux.HandleFunc("GET /api/overview", s.overview)
	mux.HandleFunc("GET /api/nodes", s.listNodes)
	mux.HandleFunc("POST /api/nodes", s.createNode)
	mux.HandleFunc("PATCH /api/nodes/{id}", s.updateNode)
	mux.HandleFunc("DELETE /api/nodes/{id}", s.deleteNode)
	mux.HandleFunc("POST /api/nodes/{id}/rotate-enrollment", s.rotateEnrollment)
	mux.HandleFunc("GET /api/users", s.listUsers)
	mux.HandleFunc("POST /api/users", s.createUser)
	mux.HandleFunc("PATCH /api/users/{id}", s.updateUser)
	mux.HandleFunc("DELETE /api/users/{id}", s.deleteUser)
	mux.HandleFunc("GET /api/events", s.listEvents)
	return mux
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	userOK := subtle.ConstantTimeCompare([]byte(input.Username), []byte(s.adminUser)) == 1
	passOK := subtle.ConstantTimeCompare(hashBytes(input.Password), hashBytes(s.adminPassword)) == 1
	if !userOK || !passOK {
		time.Sleep(350 * time.Millisecond)
		writeError(w, http.StatusUnauthorized, "Неверный логин или пароль")
		return
	}

	expires := time.Now().Add(24 * time.Hour)
	http.SetCookie(w, &http.Cookie{
		Name: "proxyfleet_session", Value: s.signSession(expires), Path: "/",
		Expires: expires, HttpOnly: true, Secure: s.cookieSecure, SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "username": s.adminUser})
}

func (s *server) logout(w http.ResponseWriter, _ *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "proxyfleet_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.cookieSecure, SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) overview(w http.ResponseWriter, _ *http.Request) {
	state := s.store.snapshot()
	now := time.Now()
	online, healthy := 0, 0
	var up, down, active int64
	for _, node := range state.Nodes {
		if nodeOnline(node, now) {
			online++
			if node.TelegramOK {
				healthy++
			}
		}
		up += node.BytesUp
		down += node.BytesDown
		active += node.ActiveConnections
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nodes": len(state.Nodes), "online": online, "healthy": healthy,
		"users": len(state.Users), "bytesUp": up, "bytesDown": down, "activeConnections": active,
	})
}

func (s *server) listNodes(w http.ResponseWriter, _ *http.Request) {
	state := s.store.snapshot()
	result := make([]map[string]any, 0, len(state.Nodes))
	for _, node := range state.Nodes {
		result = append(result, publicNode(node))
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) createNode(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name           string `json:"name"`
		Region         string `json:"region"`
		AdvertisedHost string `json:"advertisedHost"`
		HTTPPort       int    `json:"httpPort"`
		SOCKSPort      int    `json:"socksPort"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		writeError(w, http.StatusBadRequest, "Укажите название ноды")
		return
	}
	if input.HTTPPort == 0 {
		input.HTTPPort = 3128
	}
	if input.SOCKSPort == 0 {
		input.SOCKSPort = 1080
	}
	if !validPort(input.HTTPPort) || !validPort(input.SOCKSPort) || input.HTTPPort == input.SOCKSPort {
		writeError(w, http.StatusBadRequest, "Проверьте порты HTTP и SOCKS5")
		return
	}

	id := randomHex(8)
	token := randomToken(32)
	node := &model.Node{
		ID: id, Name: input.Name, Region: strings.TrimSpace(input.Region), AdvertisedHost: strings.TrimSpace(input.AdvertisedHost),
		HTTPPort: input.HTTPPort, SOCKSPort: input.SOCKSPort, EnrollTokenHash: tokenHash(token), ConfigVersion: 1, CreatedAt: time.Now().UTC(),
	}
	err := s.store.update(func(state *model.State) error {
		state.Nodes = append(state.Nodes, node)
		state.Events = append(state.Events, newEvent("node.created", "Создана новая нода", id, map[string]any{"name": node.Name}))
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось сохранить ноду")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"node": publicNode(node), "enrollToken": token, "installCommand": s.installCommand(token)})
}

func (s *server) updateNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var input struct {
		Name                *string `json:"name"`
		Region              *string `json:"region"`
		AdvertisedHost      *string `json:"advertisedHost"`
		HTTPPort            *int    `json:"httpPort"`
		SOCKSPort           *int    `json:"socksPort"`
		AllowPrivateTargets *bool   `json:"allowPrivateTargets"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	var updated *model.Node
	err := s.store.update(func(state *model.State) error {
		node := findNode(state, id)
		if node == nil {
			return os.ErrNotExist
		}
		if input.Name != nil && strings.TrimSpace(*input.Name) != "" {
			node.Name = strings.TrimSpace(*input.Name)
		}
		if input.Region != nil {
			node.Region = strings.TrimSpace(*input.Region)
		}
		if input.AdvertisedHost != nil {
			node.AdvertisedHost = strings.TrimSpace(*input.AdvertisedHost)
		}
		if input.HTTPPort != nil && validPort(*input.HTTPPort) {
			node.HTTPPort = *input.HTTPPort
		}
		if input.SOCKSPort != nil && validPort(*input.SOCKSPort) {
			node.SOCKSPort = *input.SOCKSPort
		}
		if node.HTTPPort == node.SOCKSPort {
			return errors.New("ports must differ")
		}
		if input.AllowPrivateTargets != nil {
			node.AllowPrivateTargets = *input.AllowPrivateTargets
		}
		node.ConfigVersion++
		updated = node
		state.Events = append(state.Events, newEvent("node.updated", "Конфигурация ноды обновлена", id, nil))
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "Нода не найдена")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "Не удалось обновить ноду: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, publicNode(updated))
}

func (s *server) deleteNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.store.update(func(state *model.State) error {
		before := len(state.Nodes)
		state.Nodes = slices.DeleteFunc(state.Nodes, func(node *model.Node) bool { return node.ID == id })
		if len(state.Nodes) == before {
			return os.ErrNotExist
		}
		for _, user := range state.Users {
			user.NodeIDs = slices.DeleteFunc(user.NodeIDs, func(nodeID string) bool { return nodeID == id })
		}
		state.Events = append(state.Events, newEvent("node.deleted", "Нода удалена", id, nil))
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "Нода не найдена")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось удалить ноду")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) rotateEnrollment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	token := randomToken(32)
	err := s.store.update(func(state *model.State) error {
		node := findNode(state, id)
		if node == nil {
			return os.ErrNotExist
		}
		node.EnrollTokenHash = tokenHash(token)
		node.AgentTokenHash = ""
		node.LastSeenAt = time.Time{}
		state.Events = append(state.Events, newEvent("node.enrollment_rotated", "Ключ подключения ноды перевыпущен", id, nil))
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "Нода не найдена")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось перевыпустить ключ")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enrollToken": token, "installCommand": s.installCommand(token)})
}

func (s *server) listUsers(w http.ResponseWriter, _ *http.Request) {
	state := s.store.snapshot()
	result := make([]map[string]any, 0, len(state.Users))
	for _, user := range state.Users {
		result = append(result, publicUser(user))
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) createUser(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name     string   `json:"name"`
		Username string   `json:"username"`
		Password string   `json:"password"`
		NodeIDs  []string `json:"nodeIds"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Username = strings.TrimSpace(input.Username)
	if input.Name == "" || input.Username == "" {
		writeError(w, http.StatusBadRequest, "Укажите название и логин")
		return
	}
	if input.Password == "" {
		input.Password = randomToken(18)
	}
	if len(input.Password) < 12 {
		writeError(w, http.StatusBadRequest, "Пароль должен содержать не менее 12 символов")
		return
	}
	user := &model.ProxyUser{ID: randomHex(8), Name: input.Name, Username: input.Username, PasswordHash: credentialHash(input.Username, input.Password), NodeIDs: uniqueStrings(input.NodeIDs), Enabled: true, CreatedAt: time.Now().UTC()}
	err := s.store.update(func(state *model.State) error {
		for _, existing := range state.Users {
			if strings.EqualFold(existing.Username, user.Username) {
				return errors.New("login exists")
			}
		}
		if !nodeIDsExist(state, user.NodeIDs) {
			return errors.New("unknown node")
		}
		state.Users = append(state.Users, user)
		bumpNodes(state, user.NodeIDs)
		state.Events = append(state.Events, newEvent("user.created", "Создан пользователь прокси", "", map[string]any{"username": user.Username}))
		return nil
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "Не удалось создать пользователя: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"user": publicUser(user), "password": input.Password})
}

func (s *server) updateUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var input struct {
		Name     *string   `json:"name"`
		Password *string   `json:"password"`
		NodeIDs  *[]string `json:"nodeIds"`
		Enabled  *bool     `json:"enabled"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	var result *model.ProxyUser
	var revealedPassword string
	err := s.store.update(func(state *model.State) error {
		for _, user := range state.Users {
			if user.ID != id {
				continue
			}
			oldNodeIDs := append([]string(nil), user.NodeIDs...)
			if input.Name != nil && strings.TrimSpace(*input.Name) != "" {
				user.Name = strings.TrimSpace(*input.Name)
			}
			if input.Enabled != nil {
				user.Enabled = *input.Enabled
			}
			if input.NodeIDs != nil {
				nodes := uniqueStrings(*input.NodeIDs)
				if !nodeIDsExist(state, nodes) {
					return errors.New("unknown node")
				}
				user.NodeIDs = nodes
			}
			if input.Password != nil {
				revealedPassword = *input.Password
				if revealedPassword == "" {
					revealedPassword = randomToken(18)
				}
				if len(revealedPassword) < 12 {
					return errors.New("password too short")
				}
				user.PasswordHash = credentialHash(user.Username, revealedPassword)
			}
			bumpNodes(state, uniqueStrings(append(oldNodeIDs, user.NodeIDs...)))
			result = user
			state.Events = append(state.Events, newEvent("user.updated", "Пользователь прокси обновлён", "", map[string]any{"username": user.Username}))
			return nil
		}
		return os.ErrNotExist
	})
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "Пользователь не найден")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "Не удалось обновить пользователя: "+err.Error())
		return
	}
	response := map[string]any{"user": publicUser(result)}
	if revealedPassword != "" {
		response["password"] = revealedPassword
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *server) deleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.store.update(func(state *model.State) error {
		var nodeIDs []string
		before := len(state.Users)
		state.Users = slices.DeleteFunc(state.Users, func(user *model.ProxyUser) bool {
			if user.ID == id {
				nodeIDs = user.NodeIDs
				return true
			}
			return false
		})
		if len(state.Users) == before {
			return os.ErrNotExist
		}
		bumpNodes(state, nodeIDs)
		state.Events = append(state.Events, newEvent("user.deleted", "Пользователь прокси удалён", "", nil))
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "Пользователь не найден")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось удалить пользователя")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) listEvents(w http.ResponseWriter, _ *http.Request) {
	events := s.store.snapshot().Events
	slices.Reverse(events)
	if len(events) > 100 {
		events = events[:100]
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *server) agentEnroll(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &input) || input.Token == "" {
		return
	}
	agentToken := randomToken(32)
	var nodeID string
	err := s.store.update(func(state *model.State) error {
		for _, node := range state.Nodes {
			if node.EnrollTokenHash != "" && secureEqual(node.EnrollTokenHash, tokenHash(input.Token)) {
				nodeID = node.ID
				node.AgentTokenHash = tokenHash(agentToken)
				node.EnrollTokenHash = ""
				node.LastSeenAt = time.Now().UTC()
				state.Events = append(state.Events, newEvent("node.enrolled", "Нода успешно подключена", node.ID, nil))
				return nil
			}
		}
		return os.ErrNotExist
	})
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Ключ подключения недействителен или уже использован")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodeId": nodeID, "agentToken": agentToken})
}

func (s *server) agentConfig(w http.ResponseWriter, _ *http.Request, node *model.Node) {
	state := s.store.snapshot()
	users := make([]model.AgentUser, 0)
	for _, user := range state.Users {
		if slices.Contains(user.NodeIDs, node.ID) {
			users = append(users, model.AgentUser{ID: user.ID, Username: user.Username, PasswordHash: user.PasswordHash, Enabled: user.Enabled})
		}
	}
	writeJSON(w, http.StatusOK, model.AgentConfig{Version: node.ConfigVersion, NodeName: node.Name, HTTPPort: node.HTTPPort, SOCKSPort: node.SOCKSPort, AllowPrivateTargets: node.AllowPrivateTargets, Users: users})
}

func (s *server) agentHeartbeat(w http.ResponseWriter, r *http.Request, authenticated *model.Node) {
	var beat model.Heartbeat
	if !decodeJSON(w, r, &beat) {
		return
	}
	err := s.store.update(func(state *model.State) error {
		node := findNode(state, authenticated.ID)
		if node == nil {
			return os.ErrNotExist
		}
		wasOffline := !nodeOnline(node, time.Now())
		telegramChanged := node.LastSeenAt.IsZero() || node.TelegramOK != beat.TelegramOK
		node.LastSeenAt = time.Now().UTC()
		node.AgentVersion = beat.AgentVersion
		node.PublicIP = beat.PublicIP
		node.TelegramOK = beat.TelegramOK
		node.TelegramLatencyMS = beat.TelegramLatencyMS
		node.LastError = trimTo(beat.LastError, 300)
		node.UptimeSeconds = beat.UptimeSeconds
		node.ActiveConnections = beat.ActiveConnections
		node.BytesUp = beat.BytesUp
		node.BytesDown = beat.BytesDown
		if wasOffline {
			state.Events = append(state.Events, newEvent("node.online", "Нода вышла на связь", node.ID, nil))
		}
		if telegramChanged {
			message := "Telegram на ноде недоступен"
			if beat.TelegramOK {
				message = "Проверка Telegram восстановлена"
			}
			state.Events = append(state.Events, newEvent("node.telegram", message, node.ID, map[string]any{"ok": beat.TelegramOK, "latencyMs": beat.TelegramLatencyMS}))
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "Нода не найдена")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) withAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("proxyfleet_session")
		if err != nil || !s.verifySession(cookie.Value) {
			writeError(w, http.StatusUnauthorized, "Требуется вход")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) withAgent(next func(http.ResponseWriter, *http.Request, *model.Node)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		parts := strings.SplitN(auth, ".", 2)
		if len(parts) != 2 {
			writeError(w, http.StatusUnauthorized, "Неверный ключ агента")
			return
		}
		state := s.store.snapshot()
		node := findNode(&state, parts[0])
		if node == nil || node.AgentTokenHash == "" || !secureEqual(node.AgentTokenHash, tokenHash(parts[1])) {
			writeError(w, http.StatusUnauthorized, "Неверный ключ агента")
			return
		}
		next(w, r, node)
	}
}

func (s *server) signSession(expires time.Time) string {
	payload := s.adminUser + "|" + strconv.FormatInt(expires.Unix(), 10)
	mac := hmac.New(sha256.New, s.sessionSecret)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *server) verifySession(value string) bool {
	parts := strings.SplitN(value, ".", 2)
	if len(parts) != 2 {
		return false
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, s.sessionSecret)
	_, _ = mac.Write(payloadBytes)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return false
	}
	payloadParts := strings.SplitN(string(payloadBytes), "|", 2)
	if len(payloadParts) != 2 || payloadParts[0] != s.adminUser {
		return false
	}
	exp, err := strconv.ParseInt(payloadParts[1], 10, 64)
	return err == nil && time.Now().Unix() < exp
}

func (s *server) installCommand(token string) string {
	return fmt.Sprintf("docker run -d --name proxyfleet-node --restart unless-stopped --network host -v proxyfleet_node_data:/data -e CONTROL_URL=%s -e ENROLL_TOKEN=%s %s agent", shellQuote(s.publicURL), shellQuote(token), shellQuote(s.nodeImage))
}

func publicNode(node *model.Node) map[string]any {
	status := "pending"
	if !node.LastSeenAt.IsZero() {
		status = "offline"
		if nodeOnline(node, time.Now()) {
			status = "online"
		}
	}
	return map[string]any{
		"id": node.ID, "name": node.Name, "region": node.Region, "advertisedHost": node.AdvertisedHost,
		"httpPort": node.HTTPPort, "socksPort": node.SOCKSPort, "allowPrivateTargets": node.AllowPrivateTargets,
		"configVersion": node.ConfigVersion, "createdAt": node.CreatedAt, "lastSeenAt": node.LastSeenAt,
		"agentVersion": node.AgentVersion, "publicIp": node.PublicIP, "telegramOk": node.TelegramOK,
		"telegramLatencyMs": node.TelegramLatencyMS, "lastError": node.LastError, "uptimeSeconds": node.UptimeSeconds,
		"activeConnections": node.ActiveConnections, "bytesUp": node.BytesUp, "bytesDown": node.BytesDown, "status": status,
	}
}

func publicUser(user *model.ProxyUser) map[string]any {
	return map[string]any{"id": user.ID, "name": user.Name, "username": user.Username, "nodeIds": user.NodeIDs, "enabled": user.Enabled, "createdAt": user.CreatedAt}
}

func findNode(state *model.State, id string) *model.Node {
	for _, node := range state.Nodes {
		if node.ID == id {
			return node
		}
	}
	return nil
}

func nodeOnline(node *model.Node, now time.Time) bool {
	return !node.LastSeenAt.IsZero() && now.Sub(node.LastSeenAt) < 35*time.Second
}

func nodeIDsExist(state *model.State, ids []string) bool {
	for _, id := range ids {
		if findNode(state, id) == nil {
			return false
		}
	}
	return true
}

func bumpNodes(state *model.State, ids []string) {
	for _, id := range ids {
		if node := findNode(state, id); node != nil {
			node.ConfigVersion++
		}
	}
}

func newEvent(eventType, message, nodeID string, meta map[string]any) *model.Event {
	return &model.Event{ID: randomHex(8), Type: eventType, Message: message, NodeID: nodeID, CreatedAt: time.Now().UTC(), Meta: meta}
}

func randomToken(bytes int) string {
	buf := make([]byte, bytes)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

func randomHex(bytes int) string {
	buf := make([]byte, bytes)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func tokenHash(value string) string {
	return hex.EncodeToString(hashBytes(value))
}

func credentialHash(username, password string) string {
	return tokenHash(strings.ToLower(username) + "\x00" + password)
}

func hashBytes(value string) []byte {
	hash := sha256.Sum256([]byte(value))
	return hash[:]
}

func secureEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func validPort(port int) bool { return port >= 1024 && port <= 65535 }

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func trimTo(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func filepathClean(value string) string {
	value = strings.TrimPrefix(value, "/")
	value = strings.ReplaceAll(value, "..", "")
	return value
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "Некорректный JSON")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'; img-src 'self' data:")
		next.ServeHTTP(w, r)
	})
}

func requestLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		if !strings.HasPrefix(r.URL.Path, "/api/agent/heartbeat") {
			log.Info("http request", "method", r.Method, "path", r.URL.Path, "durationMs", time.Since(started).Milliseconds())
		}
	})
}

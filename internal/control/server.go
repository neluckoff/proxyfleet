package control

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"
)

const version = "0.1.0"

//go:embed web/*
var webAssets embed.FS

type server struct {
	store     *store
	nodeImage string
}

func Run(ctx context.Context, log *slog.Logger) error {
	dataPath := env("DATA_PATH", "/data/state.json")
	st, err := openStore(dataPath)
	if err != nil {
		return fmt.Errorf("open state: %w", err)
	}

	s := &server{
		store:     st,
		nodeImage: env("NODE_IMAGE", "ghcr.io/neluckoff/proxyfleet:latest"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": version})
	})
	mux.HandleFunc("GET /api/setup/status", s.setupStatus)
	mux.HandleFunc("POST /api/setup", s.setup)
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

	log.Info("control plane starting", "address", addr, "version", version)
	err = httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *server) adminMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, _ *http.Request) {
		settings := s.store.snapshot().Settings
		if settings == nil {
			writeError(w, http.StatusPreconditionRequired, "Сначала завершите настройку панели")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"username": settings.AdminUsername, "version": version})
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

func (s *server) listEvents(w http.ResponseWriter, _ *http.Request) {
	events := s.store.snapshot().Events
	slices.Reverse(events)
	if len(events) > 100 {
		events = events[:100]
	}
	writeJSON(w, http.StatusOK, events)
}

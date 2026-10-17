package control

import (
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/neluckoff/proxyfleet/internal/model"
)

func (s *server) agentEnroll(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &input) || input.Token == "" {
		return
	}
	agentToken := randomToken(32)
	var nodeID string
	var nodeName string
	err := s.store.update(func(state *model.State) error {
		for _, node := range state.Nodes {
			if node.EnrollTokenHash != "" && secureEqual(node.EnrollTokenHash, tokenHash(input.Token)) {
				nodeID = node.ID
				nodeName = node.Name
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
	s.notifyCategory("nodes", "Нода подключена: "+nodeName)
	writeJSON(w, http.StatusOK, map[string]any{"nodeId": nodeID, "agentToken": agentToken})
}

func (s *server) agentConfig(w http.ResponseWriter, _ *http.Request, node *model.Node) {
	state := s.store.snapshot()
	users := make([]model.AgentUser, 0)
	for _, user := range state.Users {
		if slices.Contains(user.NodeIDs, node.ID) {
			agentUser := model.AgentUser{ID: user.ID, Username: user.Username, PasswordHash: user.PasswordHash, Protocol: user.Protocol, Enabled: user.Enabled}
			if user.Protocol == "webproxy" {
				agentUser.WebProxySecret, _ = webProxySecret(&state, user)
			}
			users = append(users, agentUser)
		}
	}
	writeJSON(w, http.StatusOK, model.AgentConfig{
		Version: node.ConfigVersion, NodeName: node.Name, HTTPPort: node.HTTPPort, SOCKSPort: node.SOCKSPort,
		WebProxyPort: node.WebProxyPort, WebProxyHost: webProxyHost(node), WebProxyPublicIP: node.PublicIP,
		AllowPrivateTargets: node.AllowPrivateTargets, Users: users,
	})
}

func (s *server) agentHeartbeat(w http.ResponseWriter, r *http.Request, authenticated *model.Node) {
	var beat model.Heartbeat
	if !decodeJSON(w, r, &beat) {
		return
	}
	var notifications []string
	err := s.store.update(func(state *model.State) error {
		node := findNode(state, authenticated.ID)
		if node == nil {
			return os.ErrNotExist
		}
		wasOffline := !nodeOnline(node, time.Now())
		telegramChanged := node.LastSeenAt.IsZero() || node.TelegramOK != beat.TelegramOK
		trafficUp := counterDelta(node.BytesUp, beat.BytesUp)
		trafficDown := counterDelta(node.BytesDown, beat.BytesDown)
		node.LastSeenAt = time.Now().UTC()
		node.AgentVersion = beat.AgentVersion
		if beat.PublicIP != "" && node.PublicIP != beat.PublicIP {
			node.PublicIP = beat.PublicIP
			for _, user := range state.Users {
				if user.Protocol == "webproxy" && slices.Contains(user.NodeIDs, node.ID) {
					node.ConfigVersion++
					break
				}
			}
		}
		node.TelegramOK = beat.TelegramOK
		node.TelegramLatencyMS = beat.TelegramLatencyMS
		node.LastError = trimTo(beat.LastError, 300)
		node.UptimeSeconds = beat.UptimeSeconds
		node.ActiveConnections = beat.ActiveConnections
		node.BytesUp = beat.BytesUp
		node.BytesDown = beat.BytesDown
		node.MemoryAllocBytes = beat.MemoryAllocBytes
		node.MemorySysBytes = beat.MemorySysBytes
		node.Goroutines = beat.Goroutines
		node.HostMemoryUsedBytes = beat.HostMemoryUsedBytes
		node.HostMemoryTotalBytes = beat.HostMemoryTotalBytes
		node.LoadOne = beat.LoadOne
		node.OfflineNotified = false
		recordTraffic(state, time.Now().UTC(), trafficUp, trafficDown)
		recordActivitySample(state, time.Now().UTC())
		if wasOffline {
			state.Events = append(state.Events, newEvent("node.online", "Нода вышла на связь", node.ID, nil))
			notifications = append(notifications, "Нода вышла на связь: "+node.Name)
		}
		if telegramChanged {
			message := "Telegram на ноде недоступен"
			if beat.TelegramOK {
				message = "Проверка Telegram восстановлена"
			}
			state.Events = append(state.Events, newEvent("node.telegram", message, node.ID, map[string]any{"ok": beat.TelegramOK, "latencyMs": beat.TelegramLatencyMS}))
			notifications = append(notifications, message+": "+node.Name)
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "Нода не найдена")
		return
	}
	for _, message := range notifications {
		s.notifyCategory("nodes", message)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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

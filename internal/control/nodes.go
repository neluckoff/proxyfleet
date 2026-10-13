package control

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/neluckoff/proxyfleet/internal/model"
)

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
		MTProtoPort    int    `json:"mtProtoPort"`
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
	if input.MTProtoPort == 0 {
		input.MTProtoPort = 8443
	}
	if !validPort(input.HTTPPort) || !validPort(input.SOCKSPort) || !validPort(input.MTProtoPort) || !portsUnique(input.HTTPPort, input.SOCKSPort, input.MTProtoPort) {
		writeError(w, http.StatusBadRequest, "Проверьте порты HTTP, SOCKS5 и MTProto — они должны отличаться")
		return
	}

	id := randomHex(8)
	token := randomToken(32)
	node := &model.Node{
		ID: id, Name: input.Name, Region: strings.TrimSpace(input.Region), AdvertisedHost: strings.TrimSpace(input.AdvertisedHost),
		HTTPPort: input.HTTPPort, SOCKSPort: input.SOCKSPort, MTProtoPort: input.MTProtoPort, EnrollTokenHash: tokenHash(token), ConfigVersion: 1, CreatedAt: time.Now().UTC(),
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
	s.notifyCategory("nodes", "Создана нода: "+node.Name)
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
		MTProtoPort         *int    `json:"mtProtoPort"`
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
		if input.HTTPPort != nil {
			if !validPort(*input.HTTPPort) {
				return errors.New("invalid HTTP port")
			}
			node.HTTPPort = *input.HTTPPort
		}
		if input.SOCKSPort != nil {
			if !validPort(*input.SOCKSPort) {
				return errors.New("invalid SOCKS5 port")
			}
			node.SOCKSPort = *input.SOCKSPort
		}
		if input.MTProtoPort != nil {
			if !validPort(*input.MTProtoPort) {
				return errors.New("invalid MTProto port")
			}
			node.MTProtoPort = *input.MTProtoPort
		}
		if !portsUnique(node.HTTPPort, node.SOCKSPort, node.MTProtoPort) {
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
		message := "Не удалось обновить ноду: " + err.Error()
		if strings.HasPrefix(err.Error(), "invalid ") || err.Error() == "ports must differ" {
			message = "Проверьте порты HTTP, SOCKS5 и MTProto — допустимы значения 1024–65535, порты должны отличаться"
		}
		writeError(w, http.StatusBadRequest, message)
		return
	}
	s.notifyCategory("nodes", "Обновлена конфигурация ноды: "+updated.Name)
	writeJSON(w, http.StatusOK, publicNode(updated))
}

func (s *server) deleteNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	deletedName := ""
	err := s.store.update(func(state *model.State) error {
		before := len(state.Nodes)
		state.Nodes = slices.DeleteFunc(state.Nodes, func(node *model.Node) bool {
			if node.ID == id {
				deletedName = node.Name
				return true
			}
			return false
		})
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
	s.notifyCategory("nodes", "Удалена нода: "+deletedName)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) rotateEnrollment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	token := randomToken(32)
	nodeName := ""
	err := s.store.update(func(state *model.State) error {
		node := findNode(state, id)
		if node == nil {
			return os.ErrNotExist
		}
		node.EnrollTokenHash = tokenHash(token)
		node.AgentTokenHash = ""
		node.LastSeenAt = time.Time{}
		nodeName = node.Name
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
	s.notifyCategory("nodes", "Перевыпущен ключ подключения ноды: "+nodeName)
	writeJSON(w, http.StatusOK, map[string]any{"enrollToken": token, "installCommand": s.installCommand(token)})
}

func (s *server) installCommand(token string) string {
	settings := s.store.snapshot().Settings
	publicURL := "http://localhost:8080"
	if settings != nil {
		publicURL = settings.PublicURL
	}
	return fmt.Sprintf("docker run -d --name proxyfleet-node --restart unless-stopped --network host -v proxyfleet_node_data:/data -e CONTROL_URL=%s -e ENROLL_TOKEN=%s %s agent", shellQuote(publicURL), shellQuote(token), shellQuote(s.nodeImage))
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
		"httpPort": node.HTTPPort, "socksPort": node.SOCKSPort, "mtProtoPort": node.MTProtoPort, "allowPrivateTargets": node.AllowPrivateTargets,
		"configVersion": node.ConfigVersion, "createdAt": node.CreatedAt, "lastSeenAt": node.LastSeenAt,
		"agentVersion": node.AgentVersion, "publicIp": node.PublicIP, "telegramOk": node.TelegramOK,
		"telegramLatencyMs": node.TelegramLatencyMS, "lastError": node.LastError, "uptimeSeconds": node.UptimeSeconds,
		"activeConnections": node.ActiveConnections, "bytesUp": node.BytesUp, "bytesDown": node.BytesDown, "status": status,
		"memoryAllocBytes": node.MemoryAllocBytes, "memorySysBytes": node.MemorySysBytes, "goroutines": node.Goroutines,
		"hostMemoryUsedBytes": node.HostMemoryUsedBytes, "hostMemoryTotalBytes": node.HostMemoryTotalBytes, "loadOne": node.LoadOne,
	}
}

func portsUnique(ports ...int) bool {
	seen := make(map[int]struct{}, len(ports))
	for _, port := range ports {
		if _, exists := seen[port]; exists {
			return false
		}
		seen[port] = struct{}{}
	}
	return true
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

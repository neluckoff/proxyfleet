package control

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/neluckoff/proxyfleet/internal/model"
)

var proxyUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{3,64}$`)

func (s *server) listUsers(w http.ResponseWriter, _ *http.Request) {
	state := s.store.snapshot()
	result := make([]map[string]any, 0, len(state.Users))
	for _, user := range state.Users {
		result = append(result, publicProxy(&state, user))
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) createUser(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name     string   `json:"name"`
		Username string   `json:"username"`
		Password string   `json:"password"`
		Protocol string   `json:"protocol"`
		NodeID   string   `json:"nodeId"`
		NodeIDs  []string `json:"nodeIds"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Username = strings.TrimSpace(input.Username)
	input.Protocol = strings.ToLower(strings.TrimSpace(input.Protocol))
	input.NodeID = strings.TrimSpace(input.NodeID)
	if input.NodeID == "" && len(input.NodeIDs) > 0 {
		input.NodeID = input.NodeIDs[0]
	}
	if input.Name == "" {
		input.Name = input.Username
	}
	if !proxyUsernamePattern.MatchString(input.Username) {
		writeError(w, http.StatusBadRequest, "Логин должен содержать 3–64 латинских символа, цифры, точку, дефис или подчёркивание")
		return
	}
	if !validProxyProtocol(input.Protocol) {
		writeError(w, http.StatusBadRequest, "Выберите протокол HTTP или SOCKS5")
		return
	}
	if input.NodeID == "" {
		writeError(w, http.StatusBadRequest, "Выберите ноду для прокси")
		return
	}
	if input.Password == "" {
		input.Password = randomToken(18)
	}
	if len(input.Password) < 12 {
		writeError(w, http.StatusBadRequest, "Пароль должен содержать не менее 12 символов")
		return
	}
	user := &model.ProxyUser{ID: randomHex(8), Name: input.Name, Username: input.Username, PasswordHash: credentialHash(input.Username, input.Password), Protocol: input.Protocol, NodeIDs: []string{input.NodeID}, Enabled: true, CreatedAt: time.Now().UTC()}
	err := s.store.update(func(state *model.State) error {
		for _, existing := range state.Users {
			if strings.EqualFold(existing.Username, user.Username) {
				return errors.New("login exists")
			}
		}
		node := findNode(state, input.NodeID)
		if node == nil {
			return errors.New("unknown node")
		}
		if proxyHost(node) == "" {
			return errors.New("node has no public address")
		}
		state.Users = append(state.Users, user)
		bumpNodes(state, user.NodeIDs)
		state.Events = append(state.Events, newEvent("proxy.created", "Прокси создан", input.NodeID, map[string]any{"username": user.Username, "protocol": user.Protocol}))
		return nil
	})
	if err != nil {
		message := "Не удалось создать прокси: " + err.Error()
		if err.Error() == "login exists" {
			message = "Прокси с таким логином уже существует"
		} else if err.Error() == "node has no public address" {
			message = "У выбранной ноды нет публичного адреса"
		}
		writeError(w, http.StatusBadRequest, message)
		return
	}
	state := s.store.snapshot()
	result := publicProxy(&state, user)
	s.notify("Создан прокси: " + user.Name)
	writeJSON(w, http.StatusCreated, map[string]any{"proxy": result, "user": result, "password": input.Password, "proxyUrl": connectionURL(&state, user, input.Password)})
}

func (s *server) updateUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var input struct {
		Name     *string   `json:"name"`
		Password *string   `json:"password"`
		Protocol *string   `json:"protocol"`
		NodeID   *string   `json:"nodeId"`
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
			if input.Protocol != nil {
				protocol := strings.ToLower(strings.TrimSpace(*input.Protocol))
				if !validProxyProtocol(protocol) {
					return errors.New("unknown protocol")
				}
				user.Protocol = protocol
			}
			if input.NodeID != nil {
				nodeID := strings.TrimSpace(*input.NodeID)
				node := findNode(state, nodeID)
				if node == nil {
					return errors.New("unknown node")
				}
				if proxyHost(node) == "" {
					return errors.New("node has no public address")
				}
				user.NodeIDs = []string{nodeID}
			} else if input.NodeIDs != nil {
				nodes := uniqueStrings(*input.NodeIDs)
				if len(nodes) == 0 || !nodeIDsExist(state, nodes) {
					return errors.New("unknown node")
				}
				user.NodeIDs = []string{nodes[0]}
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
			state.Events = append(state.Events, newEvent("proxy.updated", "Прокси обновлён", firstNodeID(user), map[string]any{"username": user.Username, "protocol": user.Protocol}))
			return nil
		}
		return os.ErrNotExist
	})
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "Прокси не найден")
		return
	}
	if err != nil {
		message := "Не удалось обновить прокси: " + err.Error()
		if err.Error() == "node has no public address" {
			message = "У выбранной ноды нет публичного адреса"
		}
		writeError(w, http.StatusBadRequest, message)
		return
	}
	state := s.store.snapshot()
	public := publicProxy(&state, result)
	s.notify("Обновлён прокси: " + result.Name)
	response := map[string]any{"proxy": public, "user": public}
	if revealedPassword != "" {
		response["password"] = revealedPassword
		response["proxyUrl"] = connectionURL(&state, result, revealedPassword)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *server) deleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	deletedName := ""
	err := s.store.update(func(state *model.State) error {
		var nodeIDs []string
		before := len(state.Users)
		state.Users = slices.DeleteFunc(state.Users, func(user *model.ProxyUser) bool {
			if user.ID == id {
				nodeIDs = user.NodeIDs
				deletedName = user.Name
				return true
			}
			return false
		})
		if len(state.Users) == before {
			return os.ErrNotExist
		}
		bumpNodes(state, nodeIDs)
		state.Events = append(state.Events, newEvent("proxy.deleted", "Прокси удалён", "", nil))
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "Прокси не найден")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось удалить прокси")
		return
	}
	s.notify("Удалён прокси: " + deletedName)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func validProxyProtocol(protocol string) bool {
	return protocol == "http" || protocol == "socks5"
}

func firstNodeID(user *model.ProxyUser) string {
	if len(user.NodeIDs) == 0 {
		return ""
	}
	return user.NodeIDs[0]
}

func publicProxy(state *model.State, user *model.ProxyUser) map[string]any {
	protocol := user.Protocol
	if protocol != "http" && protocol != "socks5" {
		protocol = "both"
	}
	node := findNode(state, firstNodeID(user))
	status := "unassigned"
	nodeName, nodeRegion, host, endpoint := "", "", "", ""
	port := 0
	if node != nil {
		nodeName, nodeRegion = node.Name, node.Region
		host = proxyHost(node)
		port = proxyPort(node, protocol)
		if host != "" && port > 0 {
			endpoint = net.JoinHostPort(host, strconv.Itoa(port))
		}
	}
	if !user.Enabled {
		status = "disabled"
	} else if node != nil {
		status = "pending"
		if !node.LastSeenAt.IsZero() {
			status = "offline"
			if nodeOnline(node, time.Now()) {
				status = "online"
			}
		}
	}
	maskedURL := ""
	if endpoint != "" {
		maskedURL = fmt.Sprintf("%s://%s:••••@%s", proxyScheme(protocol), url.User(user.Username).String(), endpoint)
	}
	return map[string]any{
		"id": user.ID, "name": user.Name, "username": user.Username, "protocol": protocol,
		"nodeId": firstNodeID(user), "nodeIds": user.NodeIDs, "nodeName": nodeName, "nodeRegion": nodeRegion,
		"host": host, "port": port, "endpoint": endpoint, "maskedUrl": maskedURL,
		"status": status, "enabled": user.Enabled, "createdAt": user.CreatedAt,
	}
}

func connectionURL(state *model.State, user *model.ProxyUser, password string) string {
	node := findNode(state, firstNodeID(user))
	if node == nil || password == "" {
		return ""
	}
	host := proxyHost(node)
	port := proxyPort(node, user.Protocol)
	if host == "" || port == 0 {
		return ""
	}
	return (&url.URL{
		Scheme: proxyScheme(user.Protocol),
		User:   url.UserPassword(user.Username, password),
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
	}).String()
}

func proxyHost(node *model.Node) string {
	host := strings.TrimSpace(node.AdvertisedHost)
	if host == "" {
		host = strings.TrimSpace(node.PublicIP)
	}
	host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
	return strings.Trim(host, "/")
}

func proxyPort(node *model.Node, protocol string) int {
	if protocol == "socks5" {
		return node.SOCKSPort
	}
	return node.HTTPPort
}

func proxyScheme(protocol string) string {
	if protocol == "socks5" {
		return "socks5"
	}
	return "http"
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

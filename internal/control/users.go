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
		Source      string   `json:"source"`
		Name        string   `json:"name"`
		Username    string   `json:"username"`
		Password    string   `json:"password"`
		Protocol    string   `json:"protocol"`
		NodeID      string   `json:"nodeId"`
		NodeIDs     []string `json:"nodeIds"`
		Provider    string   `json:"provider"`
		ExternalURL string   `json:"externalUrl"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Source = strings.ToLower(strings.TrimSpace(input.Source))
	if input.Source == "external" {
		s.createExternalProxy(w, input.Name, input.Provider, input.ExternalURL)
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
		if input.Protocol == "webproxy" {
			input.Name = "Telegram WEB proxy"
		}
	}
	if input.Protocol != "webproxy" && !proxyUsernamePattern.MatchString(input.Username) {
		writeError(w, http.StatusBadRequest, "Логин должен содержать 3–64 латинских символа, цифры, точку, дефис или подчёркивание")
		return
	}
	if !validProxyProtocol(input.Protocol) {
		writeError(w, http.StatusBadRequest, "Выберите протокол HTTP, SOCKS5 или Telegram WEB")
		return
	}
	if input.NodeID == "" {
		writeError(w, http.StatusBadRequest, "Выберите ноду для прокси")
		return
	}
	id := randomHex(8)
	secret := ""
	if input.Protocol == "webproxy" {
		input.Username = "webproxy_" + id
		secret = randomHex(16)
		input.Password = ""
	} else {
		if input.Password == "" {
			input.Password = randomToken(18)
		}
		if len(input.Password) < 12 {
			writeError(w, http.StatusBadRequest, "Пароль должен содержать не менее 12 символов")
			return
		}
	}
	user := &model.ProxyUser{ID: id, Name: input.Name, Username: input.Username, Protocol: input.Protocol, Source: "managed", NodeIDs: []string{input.NodeID}, Enabled: true, CreatedAt: time.Now().UTC()}
	if input.Protocol != "webproxy" {
		user.PasswordHash = credentialHash(input.Username, input.Password)
	}
	err := s.store.update(func(state *model.State) error {
		for _, existing := range state.Users {
			if input.Protocol != "webproxy" && proxySource(existing) == "managed" && existing.Protocol != "webproxy" && strings.EqualFold(existing.Username, user.Username) {
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
		if input.Protocol == "webproxy" {
			if !validWebProxyHost(webProxyHost(node)) {
				return errors.New("node has no WEB proxy hostname")
			}
			if net.ParseIP(strings.TrimSpace(node.PublicIP)) == nil {
				return errors.New("node public IP is unavailable")
			}
			if state.Settings == nil {
				return errors.New("settings are missing")
			}
			if state.Settings.DataEncryptionKey == "" {
				state.Settings.DataEncryptionKey = randomToken(32)
			}
			ciphertext, encryptErr := encryptStoredValue(state.Settings.DataEncryptionKey, secret)
			if encryptErr != nil {
				return encryptErr
			}
			user.WebProxySecretCiphertext = ciphertext
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
		} else if err.Error() == "node has no WEB proxy hostname" {
			message = "Для Telegram WEB укажите отдельный домен в публичном адресе ноды"
		} else if err.Error() == "node public IP is unavailable" {
			message = "Публичный IP ноды ещё не определён — дождитесь подключения агента"
		}
		writeError(w, http.StatusBadRequest, message)
		return
	}
	state := s.store.snapshot()
	result := publicProxy(&state, user)
	s.notifyCategory("proxies", "Создан прокси: "+user.Name)
	credential := input.Password
	if input.Protocol == "webproxy" {
		credential = secret
	}
	response := map[string]any{"proxy": result, "user": result, "proxyUrl": connectionURL(&state, user, credential)}
	if input.Protocol == "webproxy" {
		response["secret"] = webProxyClientSecret(secret)
		response["tgUrl"] = webProxyTGURL(&state, user, secret)
	} else {
		response["password"] = input.Password
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *server) createExternalProxy(w http.ResponseWriter, name, provider, rawURL string) {
	parsed, err := parseExternalProxyURL(rawURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name = strings.TrimSpace(name)
	provider = strings.TrimSpace(provider)
	if name == "" {
		name = parsed.Hostname()
	}
	user := &model.ProxyUser{
		ID: randomHex(8), Name: name, Username: externalUsername(parsed), Protocol: parsed.Scheme,
		Source: "external", Provider: provider, NodeIDs: []string{}, Enabled: true, CreatedAt: time.Now().UTC(),
	}
	err = s.store.update(func(state *model.State) error {
		if state.Settings == nil {
			return errors.New("settings are missing")
		}
		if state.Settings.DataEncryptionKey == "" {
			state.Settings.DataEncryptionKey = randomToken(32)
		}
		ciphertext, encryptErr := encryptStoredValue(state.Settings.DataEncryptionKey, parsed.String())
		if encryptErr != nil {
			return encryptErr
		}
		user.ExternalURLCiphertext = ciphertext
		state.Users = append(state.Users, user)
		state.Events = append(state.Events, newEvent("proxy.external.created", "Добавлен внешний прокси", "", map[string]any{"provider": provider, "protocol": user.Protocol, "source": "external"}))
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось безопасно сохранить внешний прокси")
		return
	}
	state := s.store.snapshot()
	result := publicProxy(&state, user)
	s.notifyCategory("proxies", "Добавлен внешний прокси: "+user.Name)
	writeJSON(w, http.StatusCreated, map[string]any{"proxy": result, "user": result})
}

func (s *server) updateUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var input struct {
		Name        *string   `json:"name"`
		Password    *string   `json:"password"`
		Protocol    *string   `json:"protocol"`
		NodeID      *string   `json:"nodeId"`
		NodeIDs     *[]string `json:"nodeIds"`
		Enabled     *bool     `json:"enabled"`
		Provider    *string   `json:"provider"`
		ExternalURL *string   `json:"externalUrl"`
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
			if proxySource(user) == "external" {
				if input.Provider != nil {
					user.Provider = strings.TrimSpace(*input.Provider)
				}
				if input.ExternalURL != nil && strings.TrimSpace(*input.ExternalURL) != "" {
					parsed, parseErr := parseExternalProxyURL(*input.ExternalURL)
					if parseErr != nil {
						return parseErr
					}
					if state.Settings == nil {
						return errors.New("settings are missing")
					}
					if state.Settings.DataEncryptionKey == "" {
						state.Settings.DataEncryptionKey = randomToken(32)
					}
					ciphertext, encryptErr := encryptStoredValue(state.Settings.DataEncryptionKey, parsed.String())
					if encryptErr != nil {
						return encryptErr
					}
					user.ExternalURLCiphertext = ciphertext
					user.Protocol = parsed.Scheme
					user.Username = externalUsername(parsed)
					user.ExternalLastCheckedAt = time.Time{}
					user.ExternalLatencyMS = 0
					user.ExternalLastError = ""
				}
				result = user
				state.Events = append(state.Events, newEvent("proxy.external.updated", "Внешний прокси обновлён", "", map[string]any{"provider": user.Provider, "protocol": user.Protocol}))
				return nil
			}
			if input.Protocol != nil {
				protocol := strings.ToLower(strings.TrimSpace(*input.Protocol))
				if !validProxyProtocol(protocol) {
					return errors.New("unknown protocol")
				}
				if protocol == "webproxy" || user.Protocol == "webproxy" {
					if protocol != user.Protocol {
						return errors.New("webproxy conversion is not supported")
					}
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
				if user.Protocol == "webproxy" {
					if !validWebProxyHost(webProxyHost(node)) {
						return errors.New("node has no WEB proxy hostname")
					}
					if net.ParseIP(strings.TrimSpace(node.PublicIP)) == nil {
						return errors.New("node public IP is unavailable")
					}
				}
				user.NodeIDs = []string{nodeID}
			} else if input.NodeIDs != nil {
				nodes := uniqueStrings(*input.NodeIDs)
				if len(nodes) == 0 || !nodeIDsExist(state, nodes) {
					return errors.New("unknown node")
				}
				user.NodeIDs = []string{nodes[0]}
			}
			if input.Password != nil && user.Protocol == "webproxy" {
				revealedPassword = randomHex(16)
				if state.Settings == nil {
					return errors.New("settings are missing")
				}
				if state.Settings.DataEncryptionKey == "" {
					state.Settings.DataEncryptionKey = randomToken(32)
				}
				ciphertext, encryptErr := encryptStoredValue(state.Settings.DataEncryptionKey, revealedPassword)
				if encryptErr != nil {
					return encryptErr
				}
				user.WebProxySecretCiphertext = ciphertext
			} else if input.Password != nil {
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
		} else if err.Error() == "node has no WEB proxy hostname" {
			message = "Для Telegram WEB укажите отдельный домен в публичном адресе ноды"
		} else if err.Error() == "node public IP is unavailable" {
			message = "Публичный IP ноды ещё не определён — дождитесь подключения агента"
		} else if err.Error() == "webproxy conversion is not supported" {
			message = "Нельзя преобразовать Telegram WEB в HTTP или SOCKS5 — создайте отдельный прокси"
		}
		writeError(w, http.StatusBadRequest, message)
		return
	}
	state := s.store.snapshot()
	public := publicProxy(&state, result)
	s.notifyCategory("proxies", "Обновлён прокси: "+result.Name)
	response := map[string]any{"proxy": public, "user": public}
	if revealedPassword != "" {
		if result.Protocol == "webproxy" {
			response["secret"] = webProxyClientSecret(revealedPassword)
			response["tgUrl"] = webProxyTGURL(&state, result, revealedPassword)
		} else {
			response["password"] = revealedPassword
		}
		response["proxyUrl"] = connectionURL(&state, result, revealedPassword)
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *server) revealExternalProxy(w http.ResponseWriter, r *http.Request) {
	state := s.store.snapshot()
	user := findProxyUser(&state, r.PathValue("id"))
	if user == nil {
		writeError(w, http.StatusNotFound, "Прокси не найден")
		return
	}
	if proxySource(user) != "external" {
		writeError(w, http.StatusBadRequest, "Для собственного прокси пароль можно только перевыпустить")
		return
	}
	value, err := externalProxyURL(&state, user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось расшифровать адрес прокси")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"proxyUrl": value})
}

func (s *server) checkExternalProxy(w http.ResponseWriter, r *http.Request) {
	state := s.store.snapshot()
	user := findProxyUser(&state, r.PathValue("id"))
	if user == nil {
		writeError(w, http.StatusNotFound, "Прокси не найден")
		return
	}
	if proxySource(user) != "external" {
		writeError(w, http.StatusBadRequest, "Состояние собственного прокси определяется по ноде")
		return
	}
	value, err := externalProxyURL(&state, user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать адрес внешнего прокси")
		return
	}
	parsed, err := parseExternalProxyURL(value)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	started := time.Now()
	dialer := net.Dialer{Timeout: 8 * time.Second}
	connection, dialErr := dialer.DialContext(r.Context(), "tcp", parsed.Host)
	if connection != nil {
		_ = connection.Close()
	}
	checkedAt := time.Now().UTC()
	latency := time.Since(started).Milliseconds()
	errorMessage := ""
	if dialErr != nil {
		errorMessage = trimTo(dialErr.Error(), 240)
		latency = 0
	}
	_ = s.store.update(func(current *model.State) error {
		stored := findProxyUser(current, user.ID)
		if stored == nil {
			return os.ErrNotExist
		}
		stored.ExternalLastCheckedAt = checkedAt
		stored.ExternalLatencyMS = latency
		stored.ExternalLastError = errorMessage
		message := "Внешний прокси доступен"
		if errorMessage != "" {
			message = "Внешний прокси недоступен"
		}
		current.Events = append(current.Events, newEvent("proxy.external.checked", message, "", map[string]any{"proxyId": stored.ID, "latencyMs": latency}))
		return nil
	})
	updated := s.store.snapshot()
	writeJSON(w, http.StatusOK, map[string]any{"reachable": dialErr == nil, "proxy": publicProxy(&updated, findProxyUser(&updated, user.ID))})
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
	s.notifyCategory("proxies", "Удалён прокси: "+deletedName)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func validProxyProtocol(protocol string) bool {
	return protocol == "http" || protocol == "socks5" || protocol == "webproxy"
}

func proxySource(user *model.ProxyUser) string {
	if user != nil && user.Source == "external" {
		return "external"
	}
	return "managed"
}

func firstNodeID(user *model.ProxyUser) string {
	if len(user.NodeIDs) == 0 {
		return ""
	}
	return user.NodeIDs[0]
}

func findProxyUser(state *model.State, id string) *model.ProxyUser {
	for _, user := range state.Users {
		if user.ID == id {
			return user
		}
	}
	return nil
}

func publicProxy(state *model.State, user *model.ProxyUser) map[string]any {
	if user == nil {
		return map[string]any{}
	}
	protocol := user.Protocol
	if protocol != "http" && protocol != "socks5" && protocol != "webproxy" {
		protocol = "http"
	}
	source := proxySource(user)
	node := findNode(state, firstNodeID(user))
	status := "unassigned"
	nodeName, nodeRegion, host, endpoint, maskedURL := "", "", "", "", ""
	port := 0
	if source == "external" {
		nodeName = "Внешний прокси"
		nodeRegion = user.Provider
		status = "unchecked"
		if !user.Enabled {
			status = "disabled"
		} else if !user.ExternalLastCheckedAt.IsZero() {
			status = "online"
			if user.ExternalLastError != "" {
				status = "unavailable"
			}
		}
		if value, err := externalProxyURL(state, user); err == nil {
			if parsed, parseErr := url.Parse(value); parseErr == nil {
				host, endpoint = parsed.Hostname(), parsed.Host
				if parsed.Port() != "" {
					port, _ = strconv.Atoi(parsed.Port())
				}
				maskedURL = maskedExternalProxyURL(parsed)
			}
		}
	} else {
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
		if endpoint != "" && protocol == "webproxy" {
			maskedURL = fmt.Sprintf("tg://webproxy?server=%s&secret=••••", url.QueryEscape(host))
		} else if endpoint != "" {
			maskedURL = fmt.Sprintf("%s://%s:••••@%s", proxyScheme(protocol), url.User(user.Username).String(), endpoint)
		}
	}
	return map[string]any{
		"id": user.ID, "name": user.Name, "username": user.Username, "protocol": protocol,
		"source": source, "provider": user.Provider,
		"nodeId": firstNodeID(user), "nodeIds": user.NodeIDs, "nodeName": nodeName, "nodeRegion": nodeRegion,
		"host": host, "port": port, "endpoint": endpoint, "maskedUrl": maskedURL,
		"status": status, "enabled": user.Enabled, "createdAt": user.CreatedAt,
		"lastCheckedAt": user.ExternalLastCheckedAt, "latencyMs": user.ExternalLatencyMS, "lastError": user.ExternalLastError,
	}
}

func parseExternalProxyURL(value string) (*url.URL, error) {
	value = strings.TrimSpace(value)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return nil, errors.New("Укажите полный адрес прокси, например http://login:password@host:3128")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "socks5" {
		return nil, errors.New("Внешний прокси должен использовать http:// или socks5://")
	}
	if parsed.Port() == "" {
		return nil, errors.New("Укажите порт внешнего прокси")
	}
	if parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("Адрес внешнего прокси не должен содержать путь, параметры или фрагмент")
	}
	parsed.Path, parsed.RawPath, parsed.RawQuery, parsed.Fragment = "", "", "", ""
	return parsed, nil
}

func externalUsername(parsed *url.URL) string {
	if parsed != nil && parsed.User != nil && parsed.User.Username() != "" {
		return parsed.User.Username()
	}
	return "—"
}

func externalProxyURL(state *model.State, user *model.ProxyUser) (string, error) {
	if state == nil || state.Settings == nil || user == nil {
		return "", errors.New("proxy settings are unavailable")
	}
	return decryptStoredValue(state.Settings.DataEncryptionKey, user.ExternalURLCiphertext)
}

func maskedExternalProxyURL(parsed *url.URL) string {
	if parsed == nil {
		return ""
	}
	if parsed.User == nil || parsed.User.Username() == "" {
		return parsed.Scheme + "://" + parsed.Host
	}
	return fmt.Sprintf("%s://%s:••••@%s", parsed.Scheme, url.User(parsed.User.Username()).String(), parsed.Host)
}

func connectionURL(state *model.State, user *model.ProxyUser, password string) string {
	if proxySource(user) == "external" {
		return ""
	}
	node := findNode(state, firstNodeID(user))
	if node == nil || password == "" {
		return ""
	}
	host := proxyHost(node)
	port := proxyPort(node, user.Protocol)
	if host == "" || port == 0 {
		return ""
	}
	if user.Protocol == "webproxy" {
		return fmt.Sprintf("tg://webproxy?server=%s&secret=%s", url.QueryEscape(host), url.QueryEscape(webProxyClientSecret(password)))
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
	if protocol == "webproxy" {
		return 443
	}
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

func webProxySecret(state *model.State, user *model.ProxyUser) (string, error) {
	if state == nil || state.Settings == nil || user == nil || user.WebProxySecretCiphertext == "" {
		return "", errors.New("WEB proxy secret is unavailable")
	}
	return decryptStoredValue(state.Settings.DataEncryptionKey, user.WebProxySecretCiphertext)
}

func webProxyClientSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if strings.HasPrefix(secret, "dd") {
		return secret
	}
	return "dd" + secret
}

func webProxyTGURL(state *model.State, user *model.ProxyUser, secret string) string {
	node := findNode(state, firstNodeID(user))
	if node == nil || !validWebProxyHost(webProxyHost(node)) {
		return ""
	}
	return fmt.Sprintf("tg://webproxy?server=%s&secret=%s", url.QueryEscape(webProxyHost(node)), url.QueryEscape(webProxyClientSecret(secret)))
}

func webProxyHost(node *model.Node) string {
	if node == nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSpace(node.AdvertisedHost))
	host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
	return strings.Trim(host, "/")
}

func validWebProxyHost(host string) bool {
	if host == "" || len(host) > 253 || net.ParseIP(host) != nil || !strings.Contains(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}
	return true
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

package control

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/neluckoff/proxyfleet/internal/model"
)

func TestCreateProxyReturnsConnectionURL(t *testing.T) {
	s := testServer(t)
	nodeResponse := httptest.NewRecorder()
	s.createNode(nodeResponse, httptest.NewRequest(http.MethodPost, "/api/nodes", bytes.NewBufferString(`{
		"name":"Warsaw Gateway","region":"PL","advertisedHost":"proxy.example.test","httpPort":3128,"socksPort":1080
	}`)))
	if nodeResponse.Code != http.StatusCreated {
		t.Fatalf("create node returned %d: %s", nodeResponse.Code, nodeResponse.Body.String())
	}
	var nodeResult struct {
		Node struct {
			ID string `json:"id"`
		} `json:"node"`
	}
	if err := json.Unmarshal(nodeResponse.Body.Bytes(), &nodeResult); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{
		"name": "Telegram bot", "username": "telegram_bot", "password": "a-secure-test-password",
		"protocol": "socks5", "nodeId": nodeResult.Node.ID,
	})
	response := httptest.NewRecorder()
	s.createUser(response, httptest.NewRequest(http.MethodPost, "/api/proxies", bytes.NewReader(body)))
	if response.Code != http.StatusCreated {
		t.Fatalf("create proxy returned %d: %s", response.Code, response.Body.String())
	}
	var result struct {
		Proxy struct {
			Protocol string `json:"protocol"`
			NodeID   string `json:"nodeId"`
			Endpoint string `json:"endpoint"`
		} `json:"proxy"`
		ProxyURL string `json:"proxyUrl"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Proxy.Protocol != "socks5" || result.Proxy.NodeID != nodeResult.Node.ID {
		t.Fatalf("proxy assignment is wrong: %#v", result.Proxy)
	}
	if result.Proxy.Endpoint != "proxy.example.test:1080" {
		t.Fatalf("unexpected endpoint: %q", result.Proxy.Endpoint)
	}
	if result.ProxyURL != "socks5://telegram_bot:a-secure-test-password@proxy.example.test:1080" {
		t.Fatalf("unexpected proxy URL: %q", result.ProxyURL)
	}
}

func TestCreateWebProxyEncryptsSecretAndFeedsAgent(t *testing.T) {
	s := testServer(t)
	configureTestOwner(t, s)
	nodeResponse := httptest.NewRecorder()
	s.createNode(nodeResponse, httptest.NewRequest(http.MethodPost, "/api/nodes", bytes.NewBufferString(`{
		"name":"Telegram Edge","advertisedHost":"telegram.example.test","webProxyPort":8443
	}`)))
	var nodeResult struct {
		Node struct {
			ID string `json:"id"`
		} `json:"node"`
	}
	if err := json.Unmarshal(nodeResponse.Body.Bytes(), &nodeResult); err != nil {
		t.Fatal(err)
	}
	if err := s.store.update(func(state *model.State) error {
		state.Nodes[0].PublicIP = "203.0.113.10"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{"name": "Family Telegram", "protocol": "webproxy", "nodeId": nodeResult.Node.ID})
	response := httptest.NewRecorder()
	s.createUser(response, httptest.NewRequest(http.MethodPost, "/api/proxies", bytes.NewReader(body)))
	if response.Code != http.StatusCreated {
		t.Fatalf("create Telegram WEB proxy returned %d: %s", response.Code, response.Body.String())
	}
	var result struct {
		Proxy struct {
			ID        string `json:"id"`
			Protocol  string `json:"protocol"`
			Endpoint  string `json:"endpoint"`
			MaskedURL string `json:"maskedUrl"`
		} `json:"proxy"`
		Secret   string `json:"secret"`
		ProxyURL string `json:"proxyUrl"`
		TGURL    string `json:"tgUrl"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Proxy.Protocol != "webproxy" || result.Proxy.Endpoint != "telegram.example.test:443" {
		t.Fatalf("unexpected public Telegram WEB data: %#v", result.Proxy)
	}
	if len(result.Secret) != 34 || !strings.HasPrefix(result.Secret, "dd") {
		t.Fatalf("unexpected client secret: %q", result.Secret)
	}
	if !strings.HasPrefix(result.ProxyURL, "tg://webproxy?") || !strings.Contains(result.ProxyURL, result.Secret) {
		t.Fatalf("unexpected WEB proxy URL: %q", result.ProxyURL)
	}
	if !strings.HasPrefix(result.TGURL, "tg://webproxy?") || !strings.Contains(result.TGURL, result.Secret) {
		t.Fatalf("unexpected tg URL: %q", result.TGURL)
	}
	if strings.Contains(result.Proxy.MaskedURL, result.Secret) {
		t.Fatal("public proxy data leaked the WEB proxy secret")
	}
	persisted, err := os.ReadFile(s.store.path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(persisted, []byte(result.Secret)) || bytes.Contains(persisted, []byte(result.Secret[2:])) {
		t.Fatal("WEB proxy secret was persisted in plain text")
	}

	state := s.store.snapshot()
	agentResponse := httptest.NewRecorder()
	s.agentConfig(agentResponse, httptest.NewRequest(http.MethodGet, "/api/agent/config", nil), state.Nodes[0])
	var config model.AgentConfig
	if err := json.Unmarshal(agentResponse.Body.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	if config.WebProxyPort != 8443 || config.WebProxyHost != "telegram.example.test" || config.WebProxyPublicIP != "203.0.113.10" || len(config.Users) != 1 || config.Users[0].WebProxySecret != result.Secret[2:] {
		t.Fatalf("agent config is missing Telegram WEB access: %#v", config)
	}

	rotateRequest := httptest.NewRequest(http.MethodPatch, "/api/proxies/"+result.Proxy.ID, bytes.NewBufferString(`{"password":""}`))
	rotateRequest.SetPathValue("id", result.Proxy.ID)
	rotateResponse := httptest.NewRecorder()
	s.updateUser(rotateResponse, rotateRequest)
	if rotateResponse.Code != http.StatusOK {
		t.Fatalf("rotate WEB proxy secret returned %d: %s", rotateResponse.Code, rotateResponse.Body.String())
	}
	var rotated struct {
		Secret   string `json:"secret"`
		ProxyURL string `json:"proxyUrl"`
	}
	if err := json.Unmarshal(rotateResponse.Body.Bytes(), &rotated); err != nil {
		t.Fatal(err)
	}
	if rotated.Secret == result.Secret || len(rotated.Secret) != 34 || !strings.Contains(rotated.ProxyURL, rotated.Secret) {
		t.Fatalf("WEB proxy secret was not rotated: %#v", rotated)
	}

	disableRequest := httptest.NewRequest(http.MethodPatch, "/api/proxies/"+result.Proxy.ID, bytes.NewBufferString(`{"enabled":false}`))
	disableRequest.SetPathValue("id", result.Proxy.ID)
	disableResponse := httptest.NewRecorder()
	s.updateUser(disableResponse, disableRequest)
	if disableResponse.Code != http.StatusOK {
		t.Fatalf("disable WEB proxy returned %d: %s", disableResponse.Code, disableResponse.Body.String())
	}
	state = s.store.snapshot()
	agentResponse = httptest.NewRecorder()
	s.agentConfig(agentResponse, httptest.NewRequest(http.MethodGet, "/api/agent/config", nil), state.Nodes[0])
	if err := json.Unmarshal(agentResponse.Body.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Users) != 1 || config.Users[0].Enabled {
		t.Fatalf("disabled WEB proxy remained active: %#v", config.Users)
	}
}

func TestExternalProxyIsEncryptedAndCanBeRevealed(t *testing.T) {
	s := testServer(t)
	configureTestOwner(t, s)
	rawURL := "socks5://external-user:external-password@proxy.vendor.test:1080"
	body, _ := json.Marshal(map[string]any{
		"source": "external", "name": "Vendor proxy", "provider": "Example Vendor", "externalUrl": rawURL,
	})
	response := httptest.NewRecorder()
	s.createUser(response, httptest.NewRequest(http.MethodPost, "/api/proxies", bytes.NewReader(body)))
	if response.Code != http.StatusCreated {
		t.Fatalf("create external proxy returned %d: %s", response.Code, response.Body.String())
	}
	var result struct {
		Proxy struct {
			ID        string `json:"id"`
			Source    string `json:"source"`
			Provider  string `json:"provider"`
			MaskedURL string `json:"maskedUrl"`
		} `json:"proxy"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Proxy.Source != "external" || result.Proxy.Provider != "Example Vendor" || strings.Contains(result.Proxy.MaskedURL, "external-password") {
		t.Fatalf("external proxy public data is wrong: %#v", result.Proxy)
	}
	persisted, err := os.ReadFile(s.store.path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(persisted, []byte(rawURL)) || bytes.Contains(persisted, []byte("external-password")) {
		t.Fatal("external credentials were persisted in plain text")
	}

	revealRequest := httptest.NewRequest(http.MethodPost, "/api/proxies/"+result.Proxy.ID+"/reveal", nil)
	revealRequest.SetPathValue("id", result.Proxy.ID)
	revealResponse := httptest.NewRecorder()
	s.revealExternalProxy(revealResponse, revealRequest)
	if revealResponse.Code != http.StatusOK {
		t.Fatalf("reveal returned %d: %s", revealResponse.Code, revealResponse.Body.String())
	}
	var revealed map[string]string
	if err := json.Unmarshal(revealResponse.Body.Bytes(), &revealed); err != nil {
		t.Fatal(err)
	}
	if revealed["proxyUrl"] != rawURL {
		t.Fatalf("unexpected revealed URL: %q", revealed["proxyUrl"])
	}
}

func TestExternalProxyRejectsTelegramWebURL(t *testing.T) {
	if _, err := parseExternalProxyURL("tg://webproxy?server=proxy.example.test&secret=dd0123456789abcdef0123456789abcdef"); err == nil {
		t.Fatal("Telegram WEB URL must not be accepted as an external HTTP/SOCKS5 proxy")
	}
}

func TestOneNodeAcceptsMultipleProxies(t *testing.T) {
	s := testServer(t)
	state := s.store.snapshot()
	if len(state.Nodes) != 0 {
		t.Fatal("test store must start empty")
	}
	nodeResponse := httptest.NewRecorder()
	s.createNode(nodeResponse, httptest.NewRequest(http.MethodPost, "/api/nodes", bytes.NewBufferString(`{"name":"Shared node","advertisedHost":"shared.example.test"}`)))
	var nodeResult struct {
		Node struct {
			ID string `json:"id"`
		} `json:"node"`
	}
	_ = json.Unmarshal(nodeResponse.Body.Bytes(), &nodeResult)

	for _, username := range []string{"first_proxy", "second_proxy"} {
		body, _ := json.Marshal(map[string]any{"username": username, "protocol": "http", "nodeId": nodeResult.Node.ID})
		response := httptest.NewRecorder()
		s.createUser(response, httptest.NewRequest(http.MethodPost, "/api/proxies", bytes.NewReader(body)))
		if response.Code != http.StatusCreated {
			t.Fatalf("create %s returned %d: %s", username, response.Code, response.Body.String())
		}
	}
	if got := len(s.store.snapshot().Users); got != 2 {
		t.Fatalf("expected two proxies on one node, got %d", got)
	}
}

func TestDeleteProxyRemovesAccessAndBumpsNodeConfig(t *testing.T) {
	s := testServer(t)
	nodeResponse := httptest.NewRecorder()
	s.createNode(nodeResponse, httptest.NewRequest(http.MethodPost, "/api/nodes", bytes.NewBufferString(`{"name":"Delete test","advertisedHost":"delete.example.test"}`)))
	var nodeResult struct {
		Node struct {
			ID string `json:"id"`
		} `json:"node"`
	}
	_ = json.Unmarshal(nodeResponse.Body.Bytes(), &nodeResult)

	createResponse := httptest.NewRecorder()
	createBody, _ := json.Marshal(map[string]any{"username": "delete_me", "protocol": "http", "nodeId": nodeResult.Node.ID})
	s.createUser(createResponse, httptest.NewRequest(http.MethodPost, "/api/proxies", bytes.NewReader(createBody)))
	var created struct {
		Proxy struct {
			ID string `json:"id"`
		} `json:"proxy"`
	}
	_ = json.Unmarshal(createResponse.Body.Bytes(), &created)
	before := s.store.snapshot().Nodes[0].ConfigVersion

	request := httptest.NewRequest(http.MethodDelete, "/api/proxies/"+created.Proxy.ID, nil)
	request.SetPathValue("id", created.Proxy.ID)
	response := httptest.NewRecorder()
	s.deleteUser(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("delete proxy returned %d: %s", response.Code, response.Body.String())
	}
	state := s.store.snapshot()
	if len(state.Users) != 0 {
		t.Fatalf("deleted proxy is still stored: %#v", state.Users)
	}
	if state.Nodes[0].ConfigVersion <= before {
		t.Fatal("node config version was not bumped after proxy deletion")
	}
}

func TestDisabledProxyKeepsItsEndpointDetails(t *testing.T) {
	state := model.State{
		Nodes: []*model.Node{{ID: "node-1", Name: "Warsaw", AdvertisedHost: "proxy.example.test", HTTPPort: 3128}},
	}
	proxy := &model.ProxyUser{ID: "proxy-1", Name: "Client", Username: "client", Protocol: "http", NodeIDs: []string{"node-1"}, Enabled: false}
	public := publicProxy(&state, proxy)
	if public["status"] != "disabled" || public["nodeName"] != "Warsaw" || public["endpoint"] != "proxy.example.test:3128" {
		t.Fatalf("disabled proxy lost endpoint details: %#v", public)
	}
}

func TestLegacyProxyDefaultsToHTTP(t *testing.T) {
	state := model.State{
		Nodes: []*model.Node{{ID: "node-1", AdvertisedHost: "proxy.example.test", HTTPPort: 3128, SOCKSPort: 1080}},
	}
	proxy := &model.ProxyUser{ID: "proxy-1", Protocol: "both", NodeIDs: []string{"node-1"}, Enabled: true}
	public := publicProxy(&state, proxy)
	if public["protocol"] != "http" || public["endpoint"] != "proxy.example.test:3128" {
		t.Fatalf("legacy proxy must resolve to one HTTP endpoint: %#v", public)
	}
}

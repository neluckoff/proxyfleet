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

package agent

import (
	"strings"
	"testing"
)

func TestRenderTGWebProxyConfigSupportsMultipleSecrets(t *testing.T) {
	config := renderTGWebProxyConfig(8443, "cdn.example.test", "/data/webproxy/public", "/data/webproxy/admin.sock", map[string]string{
		"proxy-b": "22222222222222222222222222222222",
		"proxy-a": "11111111111111111111111111111111",
	})
	for _, expected := range []string{
		`domain = "cdn.example.test"`,
		`listen = "127.0.0.1:8443"`,
		`behind_proxy = true`,
		`site = "/data/webproxy/public"`,
		`listen = "/data/webproxy/admin.sock"`,
		`value = "dd11111111111111111111111111111111"`,
		`label = "proxy-a"`,
		`value = "dd22222222222222222222222222222222"`,
		`label = "proxy-b"`,
	} {
		if !strings.Contains(config, expected) {
			t.Fatalf("generated config does not contain %q:\n%s", expected, config)
		}
	}
	if strings.Index(config, "proxy-a") > strings.Index(config, "proxy-b") {
		t.Fatal("users must be rendered deterministically")
	}
	if strings.Count(config, "[[secret]]") != 2 {
		t.Fatalf("expected one WEB profile per user:\n%s", config)
	}
}

func TestWebProxyRelaySecretUsesPaddedIntermediateMode(t *testing.T) {
	if got := webProxyRelaySecret("AABBCCDDEEFF00112233445566778899"); got != "ddaabbccddeeff00112233445566778899" {
		t.Fatalf("unexpected relay secret: %q", got)
	}
	if got := webProxyRelaySecret("dd00112233445566778899aabbccddeeff"); got != "dd00112233445566778899aabbccddeeff" {
		t.Fatalf("dd prefix was duplicated: %q", got)
	}
}

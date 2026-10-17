package agent

import (
	"strings"
	"testing"
)

func TestRenderTelemtWebConfigSupportsMultipleSecrets(t *testing.T) {
	config := renderTelemtWebConfig(8443, "cdn.example.test", "203.0.113.10:443", "/data/webproxy/public", map[string]string{
		"proxy-b": "22222222222222222222222222222222",
		"proxy-a": "11111111111111111111111111111111",
	})
	for _, expected := range []string{
		`transport = "web"`,
		`reuse_allow = false`,
		`port = 8443`,
		`host = "cdn.example.test"`,
		`public_addr = "203.0.113.10:443"`,
		`carrier = "https-lanes"`,
		`secret_mode = "dd"`,
		`"proxy-a" = "11111111111111111111111111111111"`,
		`"proxy-b" = "22222222222222222222222222222222"`,
	} {
		if !strings.Contains(config, expected) {
			t.Fatalf("generated config does not contain %q:\n%s", expected, config)
		}
	}
	if strings.Index(config, "proxy-a") > strings.Index(config, "proxy-b") {
		t.Fatal("users must be rendered deterministically")
	}
	if strings.Count(config, "[[web.vhosts.profiles]]") != 2 {
		t.Fatalf("expected one WEB profile per user:\n%s", config)
	}
}

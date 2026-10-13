package agent

import (
	"strings"
	"testing"
)

func TestRenderTelemtConfigSupportsMultipleSecrets(t *testing.T) {
	config := renderTelemtConfig(8443, map[string]string{
		"proxy-b": "22222222222222222222222222222222",
		"proxy-a": "11111111111111111111111111111111",
	})
	for _, expected := range []string{
		"port = 8443",
		"secure = true",
		"show = []",
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
}

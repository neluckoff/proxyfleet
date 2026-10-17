package control

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenStoreMigratesLegacyProxyToHTTP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	data := []byte(`{"nodes":[],"users":[{"id":"legacy","protocol":"both","nodeIds":[],"enabled":true}],"events":[]}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	users := store.snapshot().Users
	if len(users) != 1 || users[0].Protocol != "http" {
		t.Fatalf("legacy protocol was not migrated to HTTP: %#v", users)
	}
}

func TestOpenStoreMigratesMTProtoToTelegramWeb(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	data := []byte(`{
		"nodes":[{"id":"node-1","mtProtoPort":9443,"configVersion":4}],
		"users":[{"id":"telegram","protocol":"mtproto","mtProtoSecretCiphertext":"encrypted-secret","nodeIds":["node-1"],"enabled":true}],
		"events":[]
	}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	state := store.snapshot()
	if len(state.Nodes) != 1 || state.Nodes[0].WebProxyPort != 9443 || state.Nodes[0].LegacyMTProtoPort != 0 || state.Nodes[0].ConfigVersion != 5 {
		t.Fatalf("legacy node was not migrated to Telegram WEB: %#v", state.Nodes)
	}
	if len(state.Users) != 1 || state.Users[0].Protocol != "webproxy" || state.Users[0].WebProxySecretCiphertext != "encrypted-secret" || state.Users[0].LegacyMTProtoSecret != "" {
		t.Fatalf("legacy proxy was not migrated to Telegram WEB: %#v", state.Users)
	}
}

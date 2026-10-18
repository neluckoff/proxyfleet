package control

import (
	"bytes"
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

func TestOpenStoreRemovesRetiredTelegramProxyData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	data := []byte(`{
		"nodes":[
			{"id":"node-1","mtProtoPort":9443,"configVersion":4},
			{"id":"node-2","webProxyPort":8443,"configVersion":7}
		],
		"users":[
			{"id":"telegram-old","protocol":"mtproto","mtProtoSecretCiphertext":"encrypted-secret","nodeIds":["node-1"],"enabled":true},
			{"id":"telegram-web","protocol":"webproxy","webProxySecretCiphertext":"encrypted-secret","nodeIds":["node-2"],"enabled":true}
		],
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
	if len(state.Nodes) != 2 || state.Nodes[0].ConfigVersion != 5 || state.Nodes[1].ConfigVersion != 8 {
		t.Fatalf("node configuration was not bumped after removing the retired proxy: %#v", state.Nodes)
	}
	if len(state.Users) != 0 {
		t.Fatalf("retired Telegram proxy remained in state: %#v", state.Users)
	}
	persisted, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range [][]byte{
		[]byte(`"mtProtoPort"`),
		[]byte(`"mtProtoSecretCiphertext"`),
		[]byte(`"webProxyPort"`),
		[]byte(`"webProxySecretCiphertext"`),
	} {
		if bytes.Contains(persisted, key) {
			t.Fatalf("retired field %s remained in persisted state", key)
		}
	}
}

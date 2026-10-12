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

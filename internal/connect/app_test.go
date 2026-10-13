package connect

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/oauth2"
)

type testAppKeys struct {
	mu       sync.Mutex
	keys     map[string]string
	revision map[string]uint64
}

func (k *testAppKeys) Key(id string) (string, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.keys[id]
	return v, ok
}
func (k *testAppKeys) Set(id, value string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.keys[id] = value
	k.revision[id]++
}
func (k *testAppKeys) KeyRevision(id string) uint64 {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.revision[id]
}
func (k *testAppKeys) SetAtRevision(id, value string, version uint64) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.revision[id] != version {
		return false
	}
	k.keys[id] = value
	k.revision[id]++
	return true
}
func TestAppManagerStoresCompletedGrantsOnlyInMemoryAndRejectsLateCallbacks(t *testing.T) {
	withPlugs(t, google{})
	keys := &testAppKeys{keys: map[string]string{}, revision: map[string]uint64{}}
	profile := t.TempDir()
	if err := os.WriteFile(filepath.Join(profile, StoreFileName), []byte("damaged terminal file"), 0600); err != nil {
		t.Fatal(err)
	}
	manager := NewAppManager(profile, map[string]ClientCredential{"google": {ID: "app-client", Secret: "app-secret"}}, keys)
	plug := fakePlug{service: (google{}).Service(), account: "me@example.com"}
	flow := &Flow{manager: manager, plug: plug, cancel: func() {}, appRevision: keys.KeyRevision("google")}
	status, err := flow.settle(context.Background(), flowResult{keys: &oauth2.Token{AccessToken: "issued-secret", RefreshToken: "renew-secret"}})
	if err != nil || !status.Connected || !manager.Connected("google") {
		t.Fatal(status, err)
	}
	raw, _ := os.ReadFile(filepath.Join(profile, StoreFileName))
	if string(raw) != "damaged terminal file" {
		t.Fatal("app changed terminal credentials", string(raw))
	}
	held, _ := keys.Key("google")
	if !strings.Contains(held, "issued-secret") {
		t.Fatal("grant was not handed to the app source")
	}
	late := &Flow{manager: manager, plug: plug, cancel: func() {}, appRevision: keys.KeyRevision("google")}
	keys.Set("google", "")
	_, err = late.settle(context.Background(), flowResult{keys: &oauth2.Token{AccessToken: "late-secret"}})
	if err == nil || manager.Connected("google") {
		t.Fatal("callback undid disconnect", err)
	}
}

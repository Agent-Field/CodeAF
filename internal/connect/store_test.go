package connect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestStoreRoundTrip(t *testing.T) {
	s := newStore(t.TempDir())

	if _, ok, err := s.get("google"); err != nil || ok {
		t.Fatalf("a store with no file yet: got ok=%v err=%v, want ok=false err=nil", ok, err)
	}

	saved := stored{
		Account: "me@example.com",
		Keys: &oauth2.Token{
			AccessToken:  "access-one",
			RefreshToken: "refresh-one",
			TokenType:    "Bearer",
			Expiry:       time.Now().Add(time.Hour).Round(time.Second),
		},
	}
	if err := s.put("google", saved); err != nil {
		t.Fatalf("put: %v", err)
	}

	loaded, ok, err := s.get("google")
	if err != nil || !ok {
		t.Fatalf("get after put: ok=%v err=%v", ok, err)
	}
	if loaded.Account != saved.Account {
		t.Errorf("account: got %q, want %q", loaded.Account, saved.Account)
	}
	if loaded.Keys.AccessToken != "access-one" || loaded.Keys.RefreshToken != "refresh-one" {
		t.Errorf("the key set did not survive the round trip")
	}
	if !loaded.Keys.Expiry.Equal(saved.Keys.Expiry) {
		t.Errorf("expiry: got %v, want %v", loaded.Keys.Expiry, saved.Keys.Expiry)
	}
	if !loaded.usable() {
		t.Errorf("an entry with both keys must count as usable")
	}
}

func TestStoreFileIsPrivate(t *testing.T) {
	directory := t.TempDir()
	s := newStore(directory)
	if err := s.put("google", stored{Keys: &oauth2.Token{AccessToken: "a", RefreshToken: "r"}}); err != nil {
		t.Fatalf("put: %v", err)
	}
	info, err := os.Stat(filepath.Join(directory, StoreFileName))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("file mode: got %04o, want 0600", mode)
	}
}

func TestStorePreservesOtherServices(t *testing.T) {
	s := newStore(t.TempDir())
	if err := s.put("google", stored{Account: "me@example.com", Keys: &oauth2.Token{RefreshToken: "g"}}); err != nil {
		t.Fatalf("put google: %v", err)
	}
	if err := s.put("elsewhere", stored{Account: "me@elsewhere", Keys: &oauth2.Token{RefreshToken: "e"}}); err != nil {
		t.Fatalf("put elsewhere: %v", err)
	}
	if err := s.remove("google"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	entries, err := s.load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := entries["google"]; ok {
		t.Errorf("the removed service is still in the file")
	}
	if entries["elsewhere"].Account != "me@elsewhere" {
		t.Errorf("removing one service disturbed another: %+v", entries)
	}
}

func TestStoreRemoveOfNothingSucceeds(t *testing.T) {
	s := newStore(t.TempDir())
	if err := s.remove("google"); err != nil {
		t.Errorf("removing a service that was never connected: %v", err)
	}
}

func TestStoreDamagedFileIsANamedError(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, StoreFileName)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := newStore(directory).load(); err == nil {
		t.Fatalf("a damaged file must be an error")
	} else if !strings.Contains(err.Error(), path) {
		t.Errorf("the error must name the file, got %q", err)
	}
	if _, err := NewManager(directory, nil); err == nil {
		t.Errorf("a damaged file must fail at startup, not later")
	}
}

func TestStoreLeavesNoTemporaryFiles(t *testing.T) {
	directory := t.TempDir()
	s := newStore(directory)
	for i := 0; i < 3; i++ {
		if err := s.put("google", stored{Keys: &oauth2.Token{AccessToken: "a"}}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != StoreFileName {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v, want only %s", names, StoreFileName)
	}
}

func TestStoredUsable(t *testing.T) {
	cases := []struct {
		name  string
		entry stored
		want  bool
	}{
		{"no keys at all", stored{}, false},
		{"an empty key set", stored{Keys: &oauth2.Token{}}, false},
		{"a refresh key alone", stored{Keys: &oauth2.Token{RefreshToken: "r"}}, true},
		{"an access key alone", stored{Keys: &oauth2.Token{AccessToken: "a"}}, true},
	}
	for _, c := range cases {
		if got := c.entry.usable(); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// TestStoreWritesNoSecretsInPlainKeys guards the shape of the file so that a
// later change cannot quietly start writing the client secret alongside the
// account's keys.
func TestStoreWritesNoClientSecret(t *testing.T) {
	directory := t.TempDir()
	manager, _ := NewManager(directory, map[string]ClientCredential{
		"google": {ID: "client-id", Secret: "the-client-secret"},
	})
	if err := manager.store.put("google", stored{Keys: &oauth2.Token{RefreshToken: "r"}}); err != nil {
		t.Fatalf("put: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(directory, StoreFileName))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(raw), "the-client-secret") {
		t.Errorf("the client secret must never reach the store file")
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("the file must be a JSON object keyed by service: %v", err)
	}
	if _, ok := entries["google"]; !ok {
		t.Errorf("want a top-level key for the service, got %v", entries)
	}
}

package connect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/oauth2"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// StoreFileName is the file every connection lives in, named here so that a
// doctor screen and a "where are my things" answer can say the same word the
// code reads.
const StoreFileName = "credentials.json"

// stored is one service's entry in the file.
type stored struct {
	// Account is the address the keys belong to, absent when the service
	// never told us. See the emptiness law on [Status].
	Account string `json:"account,omitempty"`
	// Keys is the key set the service issued. It is written here and read
	// back here and goes nowhere else: it is never logged, never rendered,
	// and never carried in an error.
	Keys *oauth2.Token `json:"keys,omitempty"`
}

// usable reports whether the entry can still do work. An entry with a refresh
// key can always be revived; one with only an access key works until that key
// expires; one with neither is a leftover from a half-finished connection and
// counts as nothing.
func (s stored) usable() bool {
	if s.Keys == nil {
		return false
	}
	return strings.TrimSpace(s.Keys.RefreshToken) != "" || strings.TrimSpace(s.Keys.AccessToken) != ""
}

// store is the file of connections: a JSON object keyed by service identifier.
//
// THE FILE IS PRIVATE AND STAYS PRIVATE: it is created at mode 0600 inside a
// 0700 directory, and every write goes through a temporary file that is chmod'd
// before a byte is written to it, so the keys are never momentarily readable by
// anyone else.
//
// THE FILE IS REPLACED WHOLE AND ATOMICALLY: read, replace one service's entry,
// write a temporary file beside it, rename over the old one. Entries for other
// services — and any key a later version of aforge adds — survive untouched,
// and a process killed mid-write leaves the previous file intact rather than
// half of a new one.
type store struct {
	path string
	// mu serialises this process's own writes. Two sessions in two
	// processes are still safe because of the rename, but they can lose each
	// other's last change; that is an acceptable trade for a file a person
	// touches a handful of times a year.
	mu sync.Mutex
}

// newStore names the file under profileDir, or under aforge's state root when
// profileDir is empty — the same resolution every other persisted setting uses.
func newStore(profileDir string) *store {
	profileDir = strings.TrimSpace(profileDir)
	if profileDir != "" {
		return &store{path: filepath.Join(profileDir, StoreFileName)}
	}
	return &store{path: home.Join(StoreFileName)}
}

// load reads the whole file. A file that is not there yet is an empty set of
// connections, not an error: nobody has connected anything, which is a perfectly
// ordinary state for a fresh install.
func (s *store) load() (map[string]stored, error) {
	raw, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return map[string]stored{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read connections: %w", err)
	}
	entries := make(map[string]stored)
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("read connections: %s is damaged: %w", s.path, err)
	}
	return entries, nil
}

// get reads one service's entry, reporting separately whether there was one.
func (s *store) get(id string) (stored, bool, error) {
	entries, err := s.load()
	if err != nil {
		return stored{}, false, err
	}
	entry, ok := entries[id]
	return entry, ok, nil
}

// put replaces one service's entry, preserving everything else in the file.
func (s *store) put(id string, entry stored) error {
	return s.replace(id, func(entries map[string]stored) {
		entries[id] = entry
	})
}

// remove forgets one service's entry, preserving everything else in the file.
// Removing an entry that is not there is a success, not an error.
func (s *store) remove(id string) error {
	return s.replace(id, func(entries map[string]stored) {
		delete(entries, id)
	})
}

// replace is the single writer every change goes through: the read, the one
// edit, the temporary file and the rename described on [store].
func (s *store) replace(id string, edit func(map[string]stored)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := s.load()
	if err != nil {
		return fmt.Errorf("save connections: preserve existing file: %w", err)
	}
	edit(entries)

	encoded, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("save connection %s: %w", id, err)
	}
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("save connection %s: %w", id, err)
	}
	temporary, err := os.CreateTemp(directory, ".credentials-*.json")
	if err != nil {
		return fmt.Errorf("save connection %s: %w", id, err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("save connection %s: %w", id, err)
	}
	if _, err := temporary.Write(append(encoded, '\n')); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("save connection %s: %w", id, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("save connection %s: %w", id, err)
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("save connection %s: %w", id, err)
	}
	removeTemporary = false
	return nil
}

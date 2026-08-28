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
	// Scopes is what the person actually agreed to when these keys were
	// issued. It is the answer to a question the keys themselves cannot
	// answer — "may this connection send mail?" — and without it a build
	// that starts asking for more than the last one did would carry on
	// using an old permission until the first request failed at the far end.
	//
	// An entry written before this field existed has none, which is read as
	// covering nothing. See [stored.covers].
	Scopes []string `json:"scopes,omitempty"`
	// Auth says which of the two kinds of connection this entry is:
	// [AuthKey] for a key the person pasted, absent for the browser kind.
	//
	// THE ABSENCE IS THE LEGACY READING. Every entry written before there
	// was a second kind is a browser connection, and an entry that says
	// nothing must go on being read as exactly that.
	Auth string `json:"auth,omitempty"`
	// Key is the key the person pasted, on an [AuthKey] entry. It is written
	// here and read back here and goes nowhere else, exactly as Keys is:
	// never logged, never rendered, never carried in an error.
	Key string `json:"key,omitempty"`
	// KeyEnv is the NAME of the environment variable this connection reads
	// its key from, where the person named one instead of pasting a key
	// (keyref.go). An entry carries one or the other and never both, so
	// nothing anywhere has to read a value to decide which of the two it is.
	//
	// It is not a secret and it is the one part of a key connection this
	// package will say out loud: a screen shows "from $STRIPE_KEY" where a
	// pasted key shows nothing, because the name of a variable is a fact
	// about the person's own machine and a key is not.
	KeyEnv string `json:"keyEnv,omitempty"`
	// Blank is the one piece of the service's address the catalog, or the
	// tool-server list, could not know — a workspace, a domain — as the person
	// gave it. It is kept rather than the finished address so that a service
	// that moves house keeps working: the catalog says the shape, this says the
	// piece.
	Blank string `json:"blank,omitempty"`
}

// usable reports whether the entry can still do work.
//
// A key entry is usable when there is a key in it — or the name of a variable
// to read one from — and nothing more: there is nothing to renew and nothing to
// expire, so the only question is whether the person ever gave one. A named
// variable that is not set counts as usable HERE and refuses at the moment a
// client is built ([stored.secret]), because "you have not connected this" and
// "the variable you named is empty" are two different things to be told and
// only the second one says what to do about it.
//
// For a browser entry, one with a refresh key can always
// be revived; one with only an access key works until that key expires; one
// with neither is a leftover from a half-finished connection and counts as
// nothing.
func (s stored) usable() bool {
	if s.Auth == AuthKey {
		return strings.TrimSpace(s.Key) != "" || strings.TrimSpace(s.KeyEnv) != ""
	}
	if s.Keys == nil {
		return false
	}
	return strings.TrimSpace(s.Keys.RefreshToken) != "" || strings.TrimSpace(s.Keys.AccessToken) != ""
}

// covers reports whether what this entry was granted includes everything the
// service now asks for.
//
// A CONNECTION THAT IS SHORT OF A PERMISSION IS NOT A CONNECTION. The person
// agreed to something narrower than what this build needs — they signed in when
// aforge could only read their mail, and it can send now — and the honest thing
// is to put them back through the sign-in they already know rather than to let a
// tool call fail at the far end with a sentence written by Google.
//
// An entry from before permissions were written down covers nothing, for the
// same reason: what it was granted is unknown, and unknown is not enough.
// Wanting nothing is covered by anything, which is what a plug that asks for no
// permissions at all means.
func (s stored) covers(wanted []string) bool {
	if len(wanted) == 0 {
		return true
	}
	held := make(map[string]bool, len(s.Scopes))
	for _, scope := range s.Scopes {
		if scope = strings.TrimSpace(scope); scope != "" {
			held[scope] = true
		}
	}
	for _, want := range wanted {
		if want = strings.TrimSpace(want); want != "" && !held[want] {
			return false
		}
	}
	return true
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

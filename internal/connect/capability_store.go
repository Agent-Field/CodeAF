package connect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// CapabilityFileName is the file a person's answers live in, named here so that
// a doctor screen and a "where are my things" answer can say the same word the
// code reads.
//
// IT IS NOT THE CREDENTIALS FILE, and the split is deliberate. What is in
// credentials.json is a secret: keys that open somebody's mailbox, kept at 0600
// and never printed. What is in here is a POLICY: four words about what the
// person agreed to, which they should be able to open, read, copy between
// machines and hand to anybody helping them without handing over their account
// as well. Two different things with two different lifetimes do not belong in
// one file merely because one package writes both.
const CapabilityFileName = "connections.json"

// capabilityPolicy is the whole file: an object per service, each one a set of
// answers keyed by capability.
type capabilityPolicy map[string]map[string]CapabilityState

// capabilityStore is that file on disk.
//
// THE FILE IS REPLACED WHOLE AND ATOMICALLY, the same read-replace-rename every
// persisted setting in this program goes through (store.go here, budget.go in
// internal/config): read, edit one answer, write a temporary file beside it,
// rename over the old one. Other services' answers survive, and a process killed
// mid-write leaves the previous file intact rather than half of a new one.
//
// THE FILE HOLDS ONLY WHAT SOMEBODY SAID — the emptiness law applied to disk. A
// state that equals the default is not written down, it is FORGOTTEN, so an
// untouched machine has no file at all and a file that exists is a short list of
// real decisions rather than a full table nobody made. That matters more than
// the bytes it saves: defaults are allowed to change between builds, and a
// default written to disk is a decision nobody made that outlives the reasoning
// behind it.
//
// A DAMAGED OR ABSENT FILE IS ALL DEFAULTS, NEVER AN ERROR. Reading it is what a
// settings panel does on the way to being drawn and what the gate does on the
// way to judging a call; neither has anywhere to put a failure, and the honest
// fallback — the behaviour this build had before the feature existed — is one
// nobody can be hurt by.
type capabilityStore struct {
	path string
	// mu serialises this process's reads and writes. Two processes are still
	// safe because of the rename, but they can lose each other's last change;
	// that is the same trade store.go makes for a file a person touches a
	// handful of times a year.
	mu sync.RWMutex
}

var (
	capabilityStoresMu sync.Mutex
	capabilityStores   = map[string]*capabilityStore{}
)

// capabilityStoreAt hands back THE store for one directory, building it once.
//
// One store per file rather than one per manager, because the lock is only worth
// anything if everybody who writes that file takes the same one: two managers
// over one profile — a chat session and a settings panel in the same process —
// would otherwise read-modify-write over each other and lose an answer a person
// had just given.
func capabilityStoreAt(directory string) *capabilityStore {
	path := filepath.Join(directory, CapabilityFileName)
	capabilityStoresMu.Lock()
	defer capabilityStoresMu.Unlock()
	if existing, ok := capabilityStores[path]; ok {
		return existing
	}
	fresh := &capabilityStore{path: path}
	capabilityStores[path] = fresh
	return fresh
}

// get reads one answer, reporting separately whether anybody gave one.
func (s *capabilityStore) get(service, capability string) (CapabilityState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.read()[service][capability]
	return state, ok
}

// set writes one answer.
func (s *capabilityStore) set(service, capability string, state CapabilityState) error {
	return s.replace(func(policy capabilityPolicy) {
		if policy[service] == nil {
			policy[service] = map[string]CapabilityState{}
		}
		policy[service][capability] = state
	})
}

// clear forgets one answer, which is what setting a capability back to its
// default means. Clearing something nobody ever set is a success and writes
// nothing.
func (s *capabilityStore) clear(service, capability string) error {
	return s.replace(func(policy capabilityPolicy) {
		delete(policy[service], capability)
	})
}

// replace is the single writer every change goes through.
//
// It writes NOTHING when the edit changed nothing, and it removes the file when
// the last answer in it is forgotten. Both are the emptiness law: a person who
// sets a control to what it already said must leave the disk exactly as they
// found it, and a person who undoes every answer they ever gave must be left
// with the machine they started with rather than with an empty object recording
// that they once had opinions.
func (s *capabilityStore) replace(edit func(capabilityPolicy)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	before := s.read()
	after := before.clone()
	edit(after)
	after.prune()
	if before.same(after) {
		return nil
	}
	if len(after) == 0 {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("save connection settings: %w", err)
		}
		return nil
	}

	encoded, err := json.MarshalIndent(after, "", "  ")
	if err != nil {
		return fmt.Errorf("save connection settings: %w", err)
	}
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("save connection settings: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".connections-*.json")
	if err != nil {
		return fmt.Errorf("save connection settings: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	// Readable, unlike the credentials beside it: this is what the person
	// agreed to, not what lets anybody act on it.
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("save connection settings: %w", err)
	}
	if _, err := temporary.Write(append(encoded, '\n')); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("save connection settings: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("save connection settings: %w", err)
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("save connection settings: %w", err)
	}
	removeTemporary = false
	return nil
}

// read loads the file with the lock already held, and answers with defaults for
// everything it cannot make sense of.
//
// The damage is handled CELL BY CELL as well as whole: a file that will not
// parse is nothing, and a file that parses with one word nobody recognises in it
// loses that one answer and keeps the rest. A hand-edited file saying "always"
// where it meant "yes" should cost the person that row, not their whole panel.
func (s *capabilityStore) read() capabilityPolicy {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return capabilityPolicy{}
	}
	var decoded map[string]map[string]string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return capabilityPolicy{}
	}
	policy := make(capabilityPolicy, len(decoded))
	for rawService, answers := range decoded {
		service := normalize(rawService)
		if service == "" {
			continue
		}
		for rawCapability, word := range answers {
			capability, state := normalize(rawCapability), CapabilityState(normalize(word))
			if capability == "" || !state.valid() {
				continue
			}
			if policy[service] == nil {
				policy[service] = map[string]CapabilityState{}
			}
			policy[service][capability] = state
		}
	}
	return policy
}

// clone is a deep copy, so that an edit can be compared against what was on
// disk before it.
func (p capabilityPolicy) clone() capabilityPolicy {
	copied := make(capabilityPolicy, len(p))
	for service, answers := range p {
		fresh := make(map[string]CapabilityState, len(answers))
		for capability, state := range answers {
			fresh[capability] = state
		}
		copied[service] = fresh
	}
	return copied
}

// prune drops the services left holding no answers, so that emptiness is always
// spelled the same way and [capabilityPolicy.same] can be trusted.
func (p capabilityPolicy) prune() {
	for service, answers := range p {
		if len(answers) == 0 {
			delete(p, service)
		}
	}
}

// same reports whether two policies say the same thing. Both sides are pruned by
// construction: what comes off disk drops its empty objects on the way in, and
// what goes to disk is pruned before the comparison.
func (p capabilityPolicy) same(other capabilityPolicy) bool {
	if len(p) != len(other) {
		return false
	}
	for service, answers := range p {
		theirs, ok := other[service]
		if !ok || len(theirs) != len(answers) {
			return false
		}
		for capability, state := range answers {
			if theirs[capability] != state {
				return false
			}
		}
	}
	return true
}

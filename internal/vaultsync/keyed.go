package vaultsync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/keys"
)

// Carrier is one kind of state that rides in the vault: it is captured into the
// vault before a push or pull, and restored from it after a merge.
type Carrier interface {
	capture(v VaultFile) error
	// captureEdits preserves local edits before a pull. Each carrier keeps
	// its own rules for removals.
	captureEdits(v VaultFile) error
	restore(v VaultFile) error
}

// Keyset is a machine's home for a set of named keys, such as the key fields of
// a profile config. The vault treats each key as its own entry, so the set says
// which keys this machine could hold and how to change them.
type Keyset interface {
	// Values answers every key held here by name. ErrDamaged means what is held
	// cannot be read, and is neither captured nor written over.
	Values() (map[string]string, error)
	// Holds reports whether this machine has a place for the named key, so a
	// restore cannot introduce keys it cannot use. GuardedKeys also checks it
	// before a removal; ordinary Keys retains its ledger-based removal rule.
	Holds(name string) bool
	// Apply sets each named key to its value, and removes it when the value is
	// empty. It changes nothing else the machine holds.
	Apply(changes map[string]string) error
}

// Keyed carries a Keyset one vault entry per key, the way .env secrets are
// merged per name: each key has its own stamp and its own tombstone, so a key
// set on one machine is never dragged back by an older copy of another, and an
// edit to something else in the same file never touches a key's age.
//
// A key is stamped only when its value changed since the last capture. The
// ledger, a local file of digests (never the values), remembers what was last
// captured or restored per key, which is also how a removal is told from a key
// that was never here: only a key the ledger knows can be tombstoned.
type Keyed struct {
	scope         string
	set           Keyset
	ledger        string
	ownedRemovals bool
	// Captured values guard an apply across the network round, not only the write.
	baseline *map[string]string
}

// Keys carries set under the reserved scope named by kind, remembering digests
// in the file at ledgerPath. The scope matches no project, so a workspace .env
// never receives these entries.
func Keys(kind string, set Keyset, ledgerPath string) Keyed {
	return Keyed{scope: "home:keys:" + kind, set: set, ledger: ledgerPath}
}

// GuardedKeys opts a carrier into cross-round edit protection and ownership
// checks for removals. Ordinary Keys retain their existing capture and restore rules.
// Each explicit capture starts a guarded snapshot; pulls preserve it until the
// next capture so edits made afterward remain detectable.
func GuardedKeys(kind string, set Keyset, ledgerPath string) Keyed {
	k := Keys(kind, set, ledgerPath)
	k.baseline = new(map[string]string)
	k.ownedRemovals = true
	return k
}

func (k Keyed) id(name string) string { return k.scope + "/" + name }

// keyedRun is one capture or restore: the vault, what is held now, and the
// ledger as it is being updated.
type keyedRun struct {
	Keyed
	v       VaultFile
	held    map[string]string
	digests map[string]string
}

func (k Keyed) begin(v VaultFile) (*keyedRun, error) {
	held, err := k.set.Values()
	if err != nil {
		return nil, err
	}
	digests, err := readLedger(k.ledger)
	return &keyedRun{Keyed: k, v: v, held: held, digests: digests}, err
}

func (k Keyed) capture(v VaultFile) error {
	run, err := k.begin(v)
	if errors.Is(err, ErrDamaged) {
		return nil
	}
	if err != nil {
		return err
	}
	if k.baseline != nil {
		*k.baseline = run.held
	}
	return run.finish(run.captureAll())
}

// captureEdits preserves an existing guarded snapshot. Capturing again here
// would let idle sync mark an unpublished edit as received, hiding it from the
// next capture. Ordinary keys keep their existing capture and removal rules.
func (k Keyed) captureEdits(v VaultFile) error {
	if k.baseline != nil && *k.baseline != nil {
		return nil
	}
	return k.capture(v)
}

func (k Keyed) restore(v VaultFile) error {
	run, err := k.begin(v)
	if errors.Is(err, ErrDamaged) {
		return nil
	}
	if err != nil {
		return err
	}
	return run.finish(run.restoreAll())
}

// finish saves the ledger only when the step succeeded, so a failed step is
// retried in full next time.
func (r *keyedRun) finish(err error) error {
	if err != nil {
		return err
	}
	return writeLedger(r.ledger, r.digests)
}

// captureAll stores each key whose value changed since it was last seen, and
// tombstones each key the ledger knew that is no longer held.
func (r *keyedRun) captureAll() error {
	for name, value := range r.held {
		if err := r.captureKey(name, value); err != nil {
			return err
		}
	}
	for name := range r.digests {
		if _, still := r.held[name]; !still && (!r.ownedRemovals || r.set.Holds(name)) {
			remove := func() error { return r.v.Delete(r.id(name)) }
			if timed, ok := r.set.(interface{ KeyTime(string) time.Time }); ok {
				if vault, ok := r.v.(interface{ DeleteAt(string, time.Time) error }); ok {
					remove = func() error { return vault.DeleteAt(r.id(name), timed.KeyTime(name)) }
				}
			}
			if err := remove(); err != nil {
				return err
			}
			delete(r.digests, name)
		}
	}
	return nil
}

func (r *keyedRun) captureKey(name, value string) error {
	if r.digests[name] == digest(value) {
		return nil
	}
	if e, err := r.v.Get(r.id(name)); err != nil || e.Value != value {
		at := time.Now()
		if timed, ok := r.set.(interface{ KeyTime(string) time.Time }); ok {
			if known := timed.KeyTime(name); !known.IsZero() {
				at = known
			}
		}
		if err := r.v.PutAt(r.id(name), keys.Entry{Name: name, Value: value, Scope: r.scope}, at); err != nil {
			return err
		}
	}
	r.digests[name] = digest(value)
	return nil
}

// restoreAll writes each vault key this machine holds differently, removes each
// key the vault deleted that this machine had received or sent, and records what
// it now shares with the vault.
func (r *keyedRun) restoreAll() error {
	live, err := r.liveKeys()
	if err != nil {
		return err
	}
	changes, err := r.changes(live)
	if err != nil {
		return err
	}
	if len(changes) > 0 {
		apply := func() error { return r.set.Apply(changes) }
		if guarded, ok := r.set.(interface {
			ApplyIf(map[string]string, map[string]string) error
		}); ok {
			expected := r.held
			if r.baseline != nil && *r.baseline != nil {
				expected = *r.baseline
			}
			apply = func() error {
				if err := guarded.ApplyIf(changes, expected); err != nil {
					return err
				}
				// A CAS retry may restore twice in one network round. Advance
				// only keys this restore could apply, so its own writes are
				// not mistaken for local edits on the second merge.
				if r.baseline != nil && *r.baseline != nil {
					for name, value := range changes {
						if r.held[name] == expected[name] {
							if value == "" {
								delete(expected, name)
							} else {
								expected[name] = value
							}
						}
					}
				}
				return nil
			}
		}
		if err := apply(); err != nil {
			return err
		}
	}
	r.record(live, changes)
	return nil
}

func (r *keyedRun) liveKeys() (map[string]string, error) {
	entries, err := r.v.Entries(r.scope)
	live := make(map[string]string, len(entries))
	for _, e := range entries {
		live[e.Name] = e.Value
	}
	return live, err
}

// changes is what to apply here: live keys that differ, and keys the vault
// deleted that this machine still holds and once shared. The tombstones are read
// once for the whole scope: a restore follows every pull, and asking the vault
// per known key opened it a thousand times a round for a thousand memories.
func (r *keyedRun) changes(live map[string]string) (map[string]string, error) {
	changes := map[string]string{}
	for name, value := range live {
		if r.set.Holds(name) && r.held[name] != value {
			changes[name] = value
		}
	}
	removed, err := r.v.Removed(r.id(""))
	if err != nil {
		return nil, err
	}
	for _, id := range removed {
		name := strings.TrimPrefix(id, r.id(""))
		if _, known := r.digests[name]; known && r.held[name] != "" {
			changes[name] = ""
		}
	}
	return changes, nil
}

// record notes, per key, the value this machine and the vault now agree on.
func (r *keyedRun) record(live, changes map[string]string) {
	for name, value := range live {
		if _, changed := changes[name]; changed || r.held[name] == value {
			r.digests[name] = digest(value)
		}
	}
	for name, value := range changes {
		if value == "" {
			delete(r.digests, name)
		}
	}
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func readLedger(path string) (map[string]string, error) {
	raw, err := os.ReadFile(path)
	digests := map[string]string{}
	if errors.Is(err, os.ErrNotExist) {
		return digests, nil
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(raw, &digests) != nil {
		// A damaged ledger only forgets which keys were shared, so a removal is
		// not passed on; it never loses a key.
		return map[string]string{}, nil
	}
	return digests, nil
}

// writeLedger replaces the ledger file, at mode 0600 like the vault it sits by.
func writeLedger(path string, digests map[string]string) error {
	raw, err := json.Marshal(digests)
	if err != nil {
		return err
	}
	if have, err := os.ReadFile(path); err == nil && string(have) == string(raw) {
		return nil
	}
	return writeAtomic(path, raw, 0o600)
}

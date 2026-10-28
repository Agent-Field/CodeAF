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
	// captureEdits is capture that never records a removal, for the moment
	// before a pull, when a gap here may only mean the state was never sent.
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
	// Holds reports whether this machine has a place for the named key. A key
	// with no place here (a service this machine does not list) is not an
	// opinion of this machine: it is never captured, removed or tombstoned.
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
	scope  string
	set    Keyset
	ledger string
}

// Keys carries set under the reserved scope named by kind, remembering digests
// in the file at ledgerPath. The scope matches no project, so a workspace .env
// never receives these entries.
func Keys(kind string, set Keyset, ledgerPath string) Keyed {
	return Keyed{scope: "home:keys:" + kind, set: set, ledger: ledgerPath}
}

func (k Keyed) id(name string) string { return k.scope + "/" + name }

// keyedRun is one capture or restore: the vault, what is held now, and the
// ledger as it is being updated.
type keyedRun struct {
	Keyed
	v         VaultFile
	held      map[string]string
	digests   map[string]string
	skipped   map[string]int64
	metadata  map[string]string
	revisions map[string]keys.Revision
}

func (k Keyed) begin(v VaultFile) (*keyedRun, error) {
	held, err := k.set.Values()
	if err != nil {
		return nil, err
	}
	ledger, err := readLedger(k.ledger)
	return &keyedRun{Keyed: k, v: v, held: held, digests: ledger.Digests, skipped: ledger.Skipped}, err
}

func (k Keyed) capture(v VaultFile) error {
	run, err := k.begin(v)
	if errors.Is(err, ErrDamaged) {
		return nil
	}
	if err != nil {
		return err
	}
	return run.finish(run.captureAll())
}

// captureEdits is capture: a key is tombstoned only when the ledger says this
// machine held it, so a key it was never given is already not a removal.
func (k Keyed) captureEdits(v VaultFile) error { return k.capture(v) }

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
	return writeLedger(r.ledger, keyedLedger{Digests: r.digests, Skipped: r.skipped})
}

// captureAll stores each key whose value changed since it was last seen, and
// tombstones each key the ledger knew that is no longer held.
func (r *keyedRun) captureAll() error {
	if described, ok := r.set.(interface{ Metadata(string, string) string }); ok {
		r.metadata = map[string]string{}
		for name, value := range r.held {
			if metadata := described.Metadata(name, value); metadata != "" {
				r.metadata[name] = metadata
			}
		}
		if len(r.metadata) > 0 {
			if revised, ok := r.v.(interface {
				Revisions(string) (map[string]keys.Revision, error)
			}); ok {
				var err error
				r.revisions, err = revised.Revisions(r.scope)
				if err != nil {
					return err
				}
			}
		}
	}
	for name, value := range r.held {
		if err := r.captureKey(name, value); err != nil {
			return err
		}
	}
	for name := range r.digests {
		if _, still := r.held[name]; !still {
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
		return r.annotateKey(name, value)
	}
	if e, err := r.v.Get(r.id(name)); err == nil && e.Value == value {
		r.digests[name] = digest(value)
		return r.annotateKey(name, value)
	} else {
		at := time.Now()
		if timed, ok := r.set.(interface{ KeyTime(string) time.Time }); ok {
			if known := timed.KeyTime(name); !known.IsZero() {
				at = known
			}
		}
		entry := keys.Entry{Name: name, Value: value, Scope: r.scope}
		put := func() error { return r.v.PutAt(r.id(name), entry, at) }
		if metadata := r.metadata[name]; metadata != "" {
			if annotated, ok := r.v.(interface {
				PutMetadataAt(string, keys.Entry, time.Time, string) error
			}); ok {
				put = func() error { return annotated.PutMetadataAt(r.id(name), entry, at, metadata) }
			}
		}
		if err := put(); err != nil {
			return err
		}
	}
	r.digests[name] = digest(value)
	return nil
}

func (r *keyedRun) annotateKey(name, value string) error {
	metadata := r.metadata[name]
	if metadata == "" || r.revisions[name].Metadata != "" {
		return nil
	}
	owner, ok := r.set.(interface {
		MayAnnotate(string, string, int64) bool
	})
	if !ok || !owner.MayAnnotate(name, value, r.revisions[name].Stamp) {
		return nil
	}
	if annotated, ok := r.v.(interface {
		Annotate(string, keys.Entry, string) error
	}); ok {
		return annotated.Annotate(r.id(name), keys.Entry{Name: name, Value: value, Scope: r.scope}, metadata)
	}
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
	if accept, ok := r.set.(interface {
		Accept(string, string, string) bool
	}); ok && len(changes) > 0 {
		stamped, ok := r.v.(interface {
			Revisions(string) (map[string]keys.Revision, error)
		})
		if !ok {
			return errors.New("vaultsync: selective restore needs slot revisions")
		}
		revisions, err := stamped.Revisions(r.scope)
		if err != nil {
			return err
		}
		for name, value := range changes {
			if value == "" {
				continue
			}
			revision := revisions[name]
			if revision.Stamp <= r.skipped[name] || !accept.Accept(name, value, revision.Metadata) {
				r.skipped[name] = max(r.skipped[name], revision.Stamp)
				// Keep the local digest, so rejecting a remote value never
				// masquerades as a new edit on the next capture.
				if held, ok := r.held[name]; ok {
					r.digests[name] = digest(held)
				}
				delete(changes, name)
			}
		}
	}
	if len(changes) > 0 {
		apply := func() error { return r.set.Apply(changes) }
		if guarded, ok := r.set.(interface {
			ApplyIf(map[string]string, map[string]string) error
		}); ok {
			apply = func() error { return guarded.ApplyIf(changes, r.held) }
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

type keyedLedger struct {
	Digests map[string]string `json:"digests"`
	Skipped map[string]int64  `json:"skipped,omitempty"`
}

func readLedger(path string) (keyedLedger, error) {
	raw, err := os.ReadFile(path)
	ledger := keyedLedger{Digests: map[string]string{}, Skipped: map[string]int64{}}
	if errors.Is(err, os.ErrNotExist) {
		return ledger, nil
	}
	if err != nil {
		return ledger, err
	}
	var old map[string]string
	if json.Unmarshal(raw, &old) == nil {
		if old != nil {
			ledger.Digests = old
		}
		return ledger, nil
	}
	if json.Unmarshal(raw, &ledger) != nil || ledger.Digests == nil {
		// A damaged ledger only forgets which keys were shared, so a removal is
		// not passed on; it never loses a key.
		return keyedLedger{Digests: map[string]string{}, Skipped: map[string]int64{}}, nil
	}
	if ledger.Skipped == nil {
		ledger.Skipped = map[string]int64{}
	}
	return ledger, nil
}

// writeLedger replaces the ledger file, at mode 0600 like the vault it sits by.
func writeLedger(path string, ledger keyedLedger) error {
	raw, err := json.Marshal(ledger)
	if err != nil {
		return err
	}
	if have, err := os.ReadFile(path); err == nil && string(have) == string(raw) {
		return nil
	}
	return writeAtomic(path, raw, 0o600)
}

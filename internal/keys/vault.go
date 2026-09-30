package keys

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
)

// Entry is one secret: the env var Name injected at exec time, its Value, and
// the project Scope it belongs to (normalized base.remote URL, else root cell id).
type Entry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Scope string `json:"scope"`
}

// record is one slot of the vault: a live secret, or the tombstone a delete
// leaves so that a merge cannot bring the secret back. Updated is this slot's
// own stamp in unix ms; a slot written before slots had stamps reads as stamped
// by its document. A tombstone carries no name, value or scope, so a reader
// that does not know the field sees an entry that matches no project.
// Tombstones are never collected: pruning them is out of scope, and the cost is
// one small record per deleted secret.
type record struct {
	Entry
	Deleted bool  `json:"deleted,omitempty"`
	Updated int64 `json:"updated,omitempty"`
}

// stampOf is the time a slot was last written, falling back to its document's.
func (r record) stampOf(doc *vaultDoc) int64 {
	if r.Updated != 0 {
		return r.Updated
	}
	return doc.Updated
}

// live reports whether the slot holds a secret.
func (r record) live() bool { return !r.Deleted }

// nextStamp is now, or one past the slot's previous stamp when the clock has
// not moved, so two writes to one slot in the same millisecond still order.
func nextStamp(prev record, doc *vaultDoc) int64 { return stampAt(time.Now(), prev, doc) }

// stampAt is nextStamp for an edit made at a known time.
func stampAt(at time.Time, prev record, doc *vaultDoc) int64 {
	return max(at.UnixMilli(), prev.stampOf(doc)+1)
}

// vaultDoc is the stored object. Stage 0 has no policy: every secret is
// available to the local devices.
type vaultDoc struct {
	V       uint16            `json:"V"`
	Secrets map[string]record `json:"secrets"`
	Updated int64             `json:"updated"` // unix ms
}

// ErrNotFound reports an unknown secret id.
var ErrNotFound = errors.New("keys: no such secret")

// Vault is the local secret store rooted at a codeaf home.
type Vault struct {
	mu   sync.Mutex
	path string
	key  []byte
}

// Open prepares the vault under home, creating the home and the identity on
// first use. The vault seals under the identity's cell key: one root secret.
func Open(home string) (*Vault, error) {
	id, err := identity.Ensure(home)
	if err != nil {
		return nil, err
	}
	return &Vault{path: filepath.Join(home, "vault.enc"), key: id.CellKey()}, nil
}

// Exists reports whether a vault has been written under home. Asking does not
// create one.
func Exists(home string) bool {
	_, err := os.Stat(filepath.Join(home, "vault.enc"))
	return err == nil
}

// Fingerprint names the vault file's current content, or answers false when no
// vault has been written. The sealed bytes change exactly when the vault does,
// so two equal fingerprints mean nothing new to send.
func Fingerprint(home string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(home, "vault.enc"))
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), true
}

// Put stores or replaces the secret under id.
func (v *Vault) Put(id string, e Entry) error { return v.PutAt(id, e, time.Now()) }

// PutAt is Put for an edit that was made earlier than it is being recorded, such
// as a file saved before the vault heard of it. The slot is stamped with the
// time of the edit, so recording an old edit late never beats a newer one that
// another machine has already sent.
func (v *Vault) PutAt(id string, e Entry, at time.Time) error {
	return v.update(func(d *vaultDoc) error {
		d.Secrets[id] = record{Entry: e, Updated: stampAt(at, d.Secrets[id], d)}
		return nil
	})
}

// Get returns the secret under id.
func (v *Vault) Get(id string) (Entry, error) {
	d, err := v.read()
	if err != nil {
		return Entry{}, err
	}
	r, ok := d.Secrets[id]
	if !ok || !r.live() {
		return Entry{}, ErrNotFound
	}
	return r.Entry, nil
}

// Deleted reports whether id holds a tombstone, so a reader can tell a secret
// that was removed from one that was never there.
func (v *Vault) Deleted(id string) (bool, error) {
	d, err := v.read()
	return d.Secrets[id].Deleted, err
}

// Delete removes the secret under id and leaves a tombstone, so merging with a
// device that still holds the secret does not bring it back. A missing id is
// not an error and leaves nothing.
func (v *Vault) Delete(id string) error {
	return v.update(func(d *vaultDoc) error {
		prev, ok := d.Secrets[id]
		if !ok || !prev.live() {
			return errUnchanged
		}
		d.Secrets[id] = record{Deleted: true, Updated: nextStamp(prev, d)}
		return nil
	})
}

// Entries returns the project's secrets sorted by name, the one stable order
// every reader of the vault shares.
func (v *Vault) Entries(project string) ([]Entry, error) {
	d, err := v.read()
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, r := range d.Secrets {
		if r.live() && r.Scope == project {
			out = append(out, r.Entry)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// IDs lists the ids that start with prefix, in order, the removed ones too: a
// reader that keeps one slot per path needs the paths that were deleted as much
// as the live ones, since a tombstone is what tells it to delete the file.
func (v *Vault) IDs(prefix string) ([]string, error) {
	d, err := v.read()
	if err != nil {
		return nil, err
	}
	var out []string
	for id := range d.Secrets {
		if strings.HasPrefix(id, prefix) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}

// Env returns NAME=value pairs for the project, sorted by name, ready to
// append to an exec environment.
func (v *Vault) Env(project string) ([]string, error) {
	entries, err := v.Entries(project)
	var out []string
	for _, e := range entries {
		out = append(out, e.Name+"="+e.Value)
	}
	return out, err
}

// ImportDotenv reads a .env file into entries scoped to project. A name
// already stored for that project keeps its id and takes the new value.
func (v *Vault) ImportDotenv(path, project string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pairs := parseDotenv(string(raw))
	changed := false
	err = v.update(func(d *vaultDoc) error {
		for _, p := range pairs {
			id, err := idFor(d, p.name, project)
			if err != nil {
				return err
			}
			next := Entry{Name: p.name, Value: p.value, Scope: project}
			if prev := d.Secrets[id]; prev.Entry != next || !prev.live() {
				d.Secrets[id], changed = record{Entry: next, Updated: nextStamp(prev, d)}, true
			}
		}
		if !changed {
			return errUnchanged
		}
		return nil
	})
	return len(pairs), err
}

func idFor(d *vaultDoc, name, project string) (string, error) {
	for id, r := range d.Secrets {
		if r.live() && r.Name == name && r.Scope == project {
			return id, nil
		}
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (v *Vault) read() (*vaultDoc, error) {
	blob, err := os.ReadFile(v.path)
	if errors.Is(err, os.ErrNotExist) {
		return &vaultDoc{V: 1, Secrets: map[string]record{}}, nil
	}
	if err != nil {
		return nil, err
	}
	d, err := openDoc(v.key, blob)
	if errors.Is(err, ErrOtherKey) && v.adoptStaged() {
		return v.read()
	}
	return d, err
}

// adoptStaged finishes a reseal that stopped between the identity changing and
// the vault following it: a staged vault that opens under this key replaces the
// live one. It answers whether it did, so a vault that is simply another
// identity's is left alone and reported as it is.
func (v *Vault) adoptStaged() bool {
	next := filepath.Join(filepath.Dir(v.path), nextFile)
	blob, err := os.ReadFile(next)
	if err != nil {
		return false
	}
	if _, err := open(v.key, blob); err != nil {
		return false
	}
	return os.Rename(next, v.path) == nil
}

// openDoc opens a sealed vault envelope and decodes the document inside.
func openDoc(key, blob []byte) (*vaultDoc, error) {
	plain, err := open(key, blob)
	if err != nil {
		return nil, err
	}
	var d vaultDoc
	if err := json.Unmarshal(plain, &d); err != nil {
		return nil, fmt.Errorf("keys: corrupt vault: %w", err)
	}
	if d.Secrets == nil {
		d.Secrets = map[string]record{}
	}
	return &d, nil
}

// errUnchanged is what an update function returns when it made no change: the
// vault is then not rewritten and the caller sees no error.
var errUnchanged = errors.New("keys: nothing to write")

// update applies fn to the stored document and writes it back atomically.
func (v *Vault) update(fn func(*vaultDoc) error) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	d, err := v.read()
	if err != nil {
		return err
	}
	if err := fn(d); err != nil {
		return ignoreUnchanged(err)
	}
	d.Updated = time.Now().UnixMilli()
	return v.write(d)
}

// write seals the document and replaces the vault file atomically. The caller
// holds the lock and has already set the document's Updated stamp.
func (v *Vault) write(d *vaultDoc) error {
	plain, err := json.Marshal(d)
	if err != nil {
		return err
	}
	blob, err := seal(v.key, plain)
	if err != nil {
		return err
	}
	return writeAtomic(v.path, blob)
}

func ignoreUnchanged(err error) error {
	if errors.Is(err, errUnchanged) {
		return nil
	}
	return err
}

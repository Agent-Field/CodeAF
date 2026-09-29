package keys

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Entry is one secret: the env var Name injected at exec time, its Value, and
// the project Scope it belongs to (normalized base.remote URL, else root cell id).
type Entry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Scope string `json:"scope"`
}

// vaultDoc is the stored object. Stage 0 has no policy: every secret is
// available to the local devices.
type vaultDoc struct {
	V       uint16           `json:"V"`
	Secrets map[string]Entry `json:"secrets"`
	Updated int64            `json:"updated"` // unix ms
}

// ErrNotFound reports an unknown secret id.
var ErrNotFound = errors.New("keys: no such secret")

// Vault is the local secret store rooted at a codeaf home.
type Vault struct {
	mu   sync.Mutex
	path string
	key  []byte
}

// Open prepares the vault under home, creating the home and key on first use.
func Open(home string) (*Vault, error) {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, err
	}
	key, err := loadKey(filepath.Join(home, "vault.key"))
	if err != nil {
		return nil, err
	}
	return &Vault{path: filepath.Join(home, "vault.enc"), key: key}, nil
}

// Exists reports whether a vault has been written under home. Asking does not
// create one.
func Exists(home string) bool {
	_, err := os.Stat(filepath.Join(home, "vault.enc"))
	return err == nil
}

// Put stores or replaces the secret under id.
func (v *Vault) Put(id string, e Entry) error {
	return v.update(func(d *vaultDoc) error {
		d.Secrets[id] = e
		return nil
	})
}

// Get returns the secret under id.
func (v *Vault) Get(id string) (Entry, error) {
	d, err := v.read()
	if err != nil {
		return Entry{}, err
	}
	e, ok := d.Secrets[id]
	if !ok {
		return Entry{}, ErrNotFound
	}
	return e, nil
}

// Delete removes the secret under id; a missing id is not an error.
func (v *Vault) Delete(id string) error {
	return v.update(func(d *vaultDoc) error {
		delete(d.Secrets, id)
		return nil
	})
}

// Env returns NAME=value pairs for the project, sorted by name, ready to
// append to an exec environment.
func (v *Vault) Env(project string) ([]string, error) {
	d, err := v.read()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range d.Secrets {
		if e.Scope == project {
			out = append(out, e.Name+"="+e.Value)
		}
	}
	sort.Strings(out)
	return out, nil
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
			if d.Secrets[id] != next {
				d.Secrets[id], changed = next, true
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
	for id, e := range d.Secrets {
		if e.Name == name && e.Scope == project {
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
		return &vaultDoc{V: 1, Secrets: map[string]Entry{}}, nil
	}
	if err != nil {
		return nil, err
	}
	plain, err := open(v.key, blob)
	if err != nil {
		return nil, err
	}
	var d vaultDoc
	if err := json.Unmarshal(plain, &d); err != nil {
		return nil, fmt.Errorf("keys: corrupt vault: %w", err)
	}
	if d.Secrets == nil {
		d.Secrets = map[string]Entry{}
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

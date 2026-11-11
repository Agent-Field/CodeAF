package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// checkoutsDoc is <root>/checkouts.json: where this machine has each watched
// repository checked out. Older files may use a local alias as the key.
type checkoutsDoc struct {
	Schema    int               `json:"schema"`
	Checkouts map[string]string `json:"checkouts"`
}

// CheckoutsPath is <root>/checkouts.json.
func (st *Store) CheckoutsPath() string { return filepath.Join(st.root, "checkouts.json") }

// Checkouts answers GitHub owner/name to folder. Local aliases are resolved
// from origin or the watched repositories; unresolved aliases stay off the wire.
func (st *Store) Checkouts() (map[string]string, error) {
	ids, err := st.repositoryIdentities()
	if err != nil {
		return nil, err
	}
	return ids.dirs, nil
}

func (st *Store) checkoutRecords() (map[string]string, error) {
	out := map[string]string{}
	if st == nil {
		return out, nil
	}
	data, err := os.ReadFile(st.CheckoutsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	var d checkoutsDoc
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("factory store: cannot read checkouts.json: %w", err)
	}
	if d.Schema > Schema {
		return nil, fmt.Errorf("%w: checkouts.json", ErrNewer)
	}
	for k, v := range d.Checkouts {
		if v = strings.TrimSpace(v); v != "" {
			out[k] = v
		}
	}
	return out, nil
}

// CheckoutDir answers the folder for owner/name or an unambiguous local alias.
// A full name never falls back to another owner's checkout of the same name.
func (st *Store) CheckoutDir(repo string) string {
	ids, err := st.repositoryIdentities()
	if err != nil {
		return ""
	}
	return ids.dirs[ids.resolve(repo)]
}

// SetCheckout records dir as where repo is checked out, temp+rename under the
// file's own flock. Nothing is written when the record already says so.
func (st *Store) SetCheckout(repo, dir string) error {
	if st == nil {
		return errors.New("factory store: no store")
	}
	repo, dir = strings.TrimSpace(repo), strings.TrimSpace(dir)
	if repo == "" || dir == "" {
		return errors.New("factory store: a checkout needs a repository and a folder")
	}
	if err := os.MkdirAll(st.root, 0o700); err != nil {
		return err
	}
	return st.underLock(filepath.Join(st.root, "checkouts.lock"), func() error {
		m, err := st.checkoutRecords()
		if err != nil {
			return err
		}
		if m[repo] == dir {
			return nil
		}
		m[repo] = dir
		data, err := json.MarshalIndent(checkoutsDoc{Schema: Schema, Checkouts: m}, "", "  ")
		if err != nil {
			return err
		}
		return writeAtomic(st.CheckoutsPath(), append(data, '\n'))
	})
}

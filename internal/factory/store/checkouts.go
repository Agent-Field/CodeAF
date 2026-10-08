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
// repository checked out, keyed by the short name the floor shows.
type checkoutsDoc struct {
	Schema    int               `json:"schema"`
	Checkouts map[string]string `json:"checkouts"`
}

// CheckoutsPath is <root>/checkouts.json.
func (st *Store) CheckoutsPath() string { return filepath.Join(st.root, "checkouts.json") }

// Checkouts answers repository short name to folder. NO FILE IS NO CHECKOUTS
// and no error. A key given as `owner/name` is stored under its short name.
func (st *Store) Checkouts() (map[string]string, error) {
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

// CheckoutDir is the folder recorded for repo, which may be the short name or
// `owner/name`: the full name is tried first, then the part after the slash.
// "" is not known.
func (st *Store) CheckoutDir(repo string) string {
	m, err := st.Checkouts()
	if err != nil {
		return ""
	}
	repo = strings.TrimSpace(repo)
	if d := m[repo]; d != "" {
		return d
	}
	if _, short, ok := strings.Cut(repo, "/"); ok {
		return m[short]
	}
	return ""
}

// SetCheckout records dir as where repo is checked out, temp+rename under the
// file's own flock. Nothing is written when the record already says so.
func (st *Store) SetCheckout(repo, dir string) error {
	if st == nil {
		return errors.New("factory store: no store")
	}
	repo, dir = strings.TrimSpace(repo), strings.TrimSpace(dir)
	if _, short, ok := strings.Cut(repo, "/"); ok {
		repo = short
	}
	if repo == "" || dir == "" {
		return errors.New("factory store: a checkout needs a repository and a folder")
	}
	if err := os.MkdirAll(st.root, 0o700); err != nil {
		return err
	}
	return st.underLock(filepath.Join(st.root, "checkouts.lock"), func() error {
		m, err := st.Checkouts()
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

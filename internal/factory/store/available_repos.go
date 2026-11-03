package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// AvailableRepos is the last complete discovery. Credential is a token hash,
// never a credential; a different token must establish its own visibility.
type AvailableRepos struct {
	Identity   string             `json:"identity"`
	Credential string             `json:"credential"`
	ReadAt     time.Time          `json:"read_at"`
	Repos      []factory.RepoInfo `json:"repos"`
}

// AvailableRepos reads the last discovery, including an empty successful list.
func (st *Store) AvailableRepos() (AvailableRepos, error) {
	var out AvailableRepos
	data, err := os.ReadFile(filepath.Join(st.root, "available-repos.json"))
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(data, &out)
	return out, err
}

// SetAvailableRepos publishes only a complete discovery, using the store's
// atomic write discipline so a restart cannot observe half a list.
func (st *Store) SetAvailableRepos(out AvailableRepos) error {
	data, err := json.Marshal(out)
	if err != nil {
		return err
	}
	return st.underLock(filepath.Join(st.root, "available-repos.lock"), func() error {
		return writeAtomic(filepath.Join(st.root, "available-repos.json"), data)
	})
}

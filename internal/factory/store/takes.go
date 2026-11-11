package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// takesDoc is <root>/takes.json: which repositories each handle's machines
// may pull when a job has nobody. A missing handle means no toggle has been
// saved, and the worker keeps the consent it sent when it attached. An
// empty map for a handle is a saved choice to take nothing unassigned.
type takesDoc struct {
	Schema   int                        `json:"schema"`
	ByHandle map[string]map[string]bool `json:"by_handle"`
}

// TakesPath is <root>/takes.json.
func (st *Store) TakesPath() string { return filepath.Join(st.root, "takes.json") }

func (st *Store) takesLock() string { return filepath.Join(st.root, "takes.lock") }

// Takes is the saved unassigned-work map for handle. set is false when this
// handle has never been written, which is not the same as a saved empty map.
func (st *Store) Takes(handle string) (map[string]bool, bool, error) {
	if st == nil {
		return nil, false, errors.New("factory store: no store")
	}
	doc, err := st.readTakes()
	if err != nil {
		return nil, false, err
	}
	saved, ok := doc.ByHandle[factory.NormalizeHandle(handle)]
	if !ok {
		return nil, false, nil
	}
	return cloneBools(saved), true, nil
}

// SetTakes replaces handle's saved map. An empty repo name is refused so a
// blank key cannot become a toggle nothing can address.
func (st *Store) SetTakes(handle string, repos map[string]bool) error {
	if st == nil {
		return errors.New("factory store: no store")
	}
	handle = factory.NormalizeHandle(handle)
	next := map[string]bool{}
	for repo, yes := range repos {
		repo = strings.TrimSpace(repo)
		if repo == "" {
			return errors.New("factory store: takes_unassigned repo must not be empty")
		}
		next[repo] = yes
	}
	if err := os.MkdirAll(st.root, 0o700); err != nil {
		return err
	}
	return st.underLock(st.takesLock(), func() error {
		doc, err := st.readTakes()
		if err != nil {
			return err
		}
		if doc.ByHandle == nil {
			doc.ByHandle = map[string]map[string]bool{}
		}
		doc.ByHandle[handle] = next
		return st.writeTakes(doc)
	})
}

func (st *Store) readTakes() (takesDoc, error) {
	data, err := os.ReadFile(st.TakesPath())
	if err != nil {
		if os.IsNotExist(err) {
			return takesDoc{Schema: Schema, ByHandle: map[string]map[string]bool{}}, nil
		}
		return takesDoc{}, err
	}
	var doc takesDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return takesDoc{}, fmt.Errorf("factory store: cannot read takes.json: %w", err)
	}
	if doc.Schema > Schema {
		return takesDoc{}, fmt.Errorf("%w: takes.json", ErrNewer)
	}
	if doc.ByHandle == nil {
		doc.ByHandle = map[string]map[string]bool{}
	}
	return doc, nil
}

func (st *Store) writeTakes(doc takesDoc) error {
	doc.Schema = Schema
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(st.TakesPath(), append(data, '\n'))
}

func cloneBools(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

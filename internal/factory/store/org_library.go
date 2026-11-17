package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// The organization's Library (factory/org_library.go) is kept in one file of
// its own, written whole under a cross-process lock: there are hundreds of pages
// at most, and every change is a person's click.
const orgLibraryFile = "org-library.json"

// OrgLibrary reads every page; a store that never kept one answers none.
func (st *Store) OrgLibrary() (factory.OrgLibrary, error) {
	out := factory.OrgLibrary{}
	raw, err := os.ReadFile(filepath.Join(st.root, orgLibraryFile))
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.New("invalid organization library")
	}
	if out == nil {
		out = factory.OrgLibrary{}
	}
	return out, nil
}

// EditOrgLibrary changes the pages under the lock; an error from edit keeps
// them as they were.
func (st *Store) EditOrgLibrary(edit func(all factory.OrgLibrary) error) (factory.OrgLibrary, error) {
	var out factory.OrgLibrary
	err := st.underLock(filepath.Join(st.root, "org-library.lock"), func() error {
		all, err := st.OrgLibrary()
		if err != nil {
			return err
		}
		if err := edit(all); err != nil {
			return err
		}
		raw, err := json.Marshal(all)
		if err != nil {
			return err
		}
		out = all
		return writeAtomic(filepath.Join(st.root, orgLibraryFile), append(raw, '\n'))
	})
	return out, err
}

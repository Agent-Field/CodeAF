package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// The organization's agents (factory/org_agent.go) are kept in one file of
// their own, written whole under a cross-process lock: there are tens of them,
// not thousands, and every change is a person's click.
const orgAgentsFile = "org-agents.json"

// OrgAgents reads every agent; a store that never kept one answers none.
func (st *Store) OrgAgents() (factory.OrgAgents, error) {
	out := factory.OrgAgents{}
	raw, err := os.ReadFile(filepath.Join(st.root, orgAgentsFile))
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.New("invalid organization agents")
	}
	if out == nil {
		out = factory.OrgAgents{}
	}
	return out, nil
}

// EditOrgAgents changes the agents under the lock; an error from edit keeps
// them as they were.
func (st *Store) EditOrgAgents(edit func(all factory.OrgAgents) error) (factory.OrgAgents, error) {
	var out factory.OrgAgents
	err := st.underLock(filepath.Join(st.root, "org-agents.lock"), func() error {
		all, err := st.OrgAgents()
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
		return writeAtomic(filepath.Join(st.root, orgAgentsFile), append(raw, '\n'))
	})
	return out, err
}

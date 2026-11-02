package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// houseRulesFile keeps the factory's own rules ([factory.HouseRules]) beside
// its runner settings. It is not a number, so [Store.List] never reads it as a
// job.
const houseRulesFile = "factory-rules.json"

// HouseRules are the rules this factory keeps for every repository it watches.
// A factory that was never given any answers empty lists, never an error.
//
// A FILE THAT CANNOT BE READ IS AN ERROR, NOT AN EMPTY SET: rules that silently
// vanish would let a job start that the person said must ask first.
func (st *Store) HouseRules() (factory.HouseRules, error) {
	empty, _ := factory.HouseRules{}.Clean()
	raw, err := os.ReadFile(filepath.Join(st.root, houseRulesFile))
	if errors.Is(err, os.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	var out factory.HouseRules
	if err := json.Unmarshal(raw, &out); err != nil {
		return empty, errors.New("the factory's rules file cannot be read")
	}
	return out.Clean()
}

// ChangeHouseRules replaces the factory's rules under a cross-process lock and
// answers what was saved. change sees the current rules and writes the new
// ones; the result is cleaned, and refused whole when a line or a list is too
// long, so nothing half-saved is ever read back.
func (st *Store) ChangeHouseRules(change func(*factory.HouseRules) error) (factory.HouseRules, error) {
	var out factory.HouseRules
	err := st.underLock(filepath.Join(st.root, "factory-rules.lock"), func() error {
		cur, err := st.HouseRules()
		if err != nil {
			return err
		}
		if err := change(&cur); err != nil {
			return err
		}
		if out, err = cur.Clean(); err != nil {
			return err
		}
		raw, err := json.Marshal(out)
		if err != nil {
			return err
		}
		return writeAtomic(filepath.Join(st.root, houseRulesFile), append(raw, '\n'))
	})
	return out, err
}

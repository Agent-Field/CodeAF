package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// houseRulesFile keeps the factory's own rules ([factory.HouseRules]) beside
// its runner settings, with who last changed them and when. It is not a
// number, so [Store.List] never reads it as a job.
const houseRulesFile = "factory-rules.json"

// HouseRules are the rules this factory keeps for every repository it watches.
// A factory that was never given any answers empty lists, never an error.
//
// A FILE THAT CANNOT BE READ IS AN ERROR, NOT AN EMPTY SET: rules that silently
// vanish would let a job start that the person said must ask first.
func (st *Store) HouseRules() (factory.HouseRules, error) {
	kept, err := st.KeptHouseRules()
	return kept.HouseRules, err
}

// KeptHouseRules are [Store.HouseRules] with their last change. Rules that
// were never written carry no change.
func (st *Store) KeptHouseRules() (factory.KeptRules, error) {
	empty, _ := factory.HouseRules{}.Clean()
	raw, err := os.ReadFile(filepath.Join(st.root, houseRulesFile))
	if errors.Is(err, os.ErrNotExist) {
		return factory.KeptRules{HouseRules: empty}, nil
	}
	if err != nil {
		return factory.KeptRules{HouseRules: empty}, err
	}
	var out factory.KeptRules
	if err := json.Unmarshal(raw, &out); err != nil {
		return factory.KeptRules{HouseRules: empty}, errors.New("the factory's rules file cannot be read")
	}
	clean, err := out.HouseRules.Clean()
	if err != nil {
		return factory.KeptRules{HouseRules: empty}, err
	}
	out.HouseRules = clean
	return out, nil
}

// ChangeHouseRules replaces the factory's rules under a cross-process lock,
// records by as their last change, and answers what was saved. change sees
// the current rules and writes the new ones; the result is cleaned, and
// refused whole when a line or a list is too long, so nothing half-saved is
// ever read back.
func (st *Store) ChangeHouseRules(by factory.RulesChange, change func(*factory.HouseRules) error) (factory.HouseRules, error) {
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
		raw, err := json.Marshal(factory.KeptRules{HouseRules: out, RulesChange: by})
		if err != nil {
			return err
		}
		return writeAtomic(filepath.Join(st.root, houseRulesFile), append(raw, '\n'))
	})
	return out, err
}

package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// policiesFile keeps every group's go-ahead policy ([factory.GroupPolicy])
// beside the factory's rules. It is not a number, so [Store.List] never reads
// it as a job.
const policiesFile = "factory-policies.json"

// Policies are every group policy this factory keeps. A factory that was never
// given one answers none, never an error.
//
// A FILE THAT CANNOT BE READ IS AN ERROR, NOT AN EMPTY SET, and the go-ahead
// pass starts nothing on it: a policy that silently vanished would stop
// nothing harmful, but one half-read could start jobs past a stop.
func (st *Store) Policies() (factory.Policies, error) {
	raw, err := os.ReadFile(filepath.Join(st.root, policiesFile))
	if errors.Is(err, os.ErrNotExist) {
		return factory.Policies{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out factory.Policies
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.New("the factory's go-ahead policies cannot be read")
	}
	if out == nil {
		out = factory.Policies{}
	}
	return out, nil
}

// Policy is one group's policy, and false when the factory keeps none for it.
func (st *Store) Policy(group string) (factory.GroupPolicy, bool, error) {
	all, err := st.Policies()
	if err != nil {
		return factory.GroupPolicy{}, false, err
	}
	p, ok := all[group]
	return p, ok, nil
}

// ChangePolicy replaces one group's policy under a cross-process lock and
// answers what was saved. change sees the policy kept now (a zero one with the
// group's name when there is none) and writes the new one; its version goes up
// by one on every save. A policy whose allowances the factory would not keep
// is refused whole, so nothing half-saved is ever read back.
func (st *Store) ChangePolicy(group string, change func(*factory.GroupPolicy) error) (factory.GroupPolicy, error) {
	var out factory.GroupPolicy
	err := st.underLock(filepath.Join(st.root, "factory-policies.lock"), func() error {
		all, err := st.Policies()
		if err != nil {
			return err
		}
		cur, ok := all[group]
		if !ok {
			cur = factory.GroupPolicy{Group: group}
		}
		version := cur.Version
		if err := change(&cur); err != nil {
			return err
		}
		if err := factory.CheckAllowances(cur.Allowances); err != nil {
			return err
		}
		cur.Group, cur.Version = group, version+1
		if cur.Allowances == nil {
			cur.Allowances = []factory.Allowance{}
		}
		all[group] = cur
		raw, err := json.Marshal(all)
		if err != nil {
			return err
		}
		out = cur
		return writeAtomic(filepath.Join(st.root, policiesFile), append(raw, '\n'))
	})
	return out, err
}

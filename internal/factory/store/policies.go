package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// policiesFile is not a job number, so List never reads settings as a job.
const policiesFile = "factory-policies.json"

func (st *Store) policies() (map[string]factory.GroupPolicy, error) {
	raw, err := os.ReadFile(filepath.Join(st.root, policiesFile))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]factory.GroupPolicy{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out map[string]factory.GroupPolicy
	if json.Unmarshal(raw, &out) != nil || out == nil {
		return nil, errors.New("the factory's policy file cannot be read")
	}
	for group, p := range out {
		if group != p.Group || p.Check() != nil || p.Version < 1 {
			return nil, errors.New("the factory's policy file cannot be read")
		}
	}
	return out, nil
}

// Policy reads fresh settings. Missing settings answer an empty policy; an
// unreadable settings file always refuses, including for an unset group.
func (st *Store) Policy(group string) (factory.GroupPolicy, error) {
	all, err := st.policies()
	if err != nil {
		return factory.GroupPolicy{}, err
	}
	p, ok := all[group]
	if !ok {
		p = factory.GroupPolicy{Group: group, Allowances: []factory.Allowance{}}
	}
	if p.Allowances == nil {
		p.Allowances = []factory.Allowance{}
	}
	return p, nil
}

// ChangePolicy replaces one group's choice under a cross-process lock. The
// version advances only after a valid replacement is ready for atomic writing.
func (st *Store) ChangePolicy(group string, change func(*factory.GroupPolicy) error) (factory.GroupPolicy, error) {
	var out factory.GroupPolicy
	if checked, err := factory.CheckGroup(group); err != nil || checked == "" || checked != group {
		return out, fmt.Errorf("a policy requires a non-empty, trimmed group")
	}
	err := st.underLock(filepath.Join(st.root, "factory-policies.lock"), func() error {
		all, err := st.policies()
		if err != nil {
			return err
		}
		cur, ok := all[group]
		if !ok {
			cur = factory.GroupPolicy{Group: group, Allowances: []factory.Allowance{}}
		}
		version := cur.Version
		if err := change(&cur); err != nil {
			return err
		}
		if cur.Group != group {
			return fmt.Errorf("a policy cannot change its group")
		}
		cur.Version = version + 1
		if cur.Allowances == nil {
			cur.Allowances = []factory.Allowance{}
		}
		if err := cur.Check(); err != nil {
			return err
		}
		all[group] = cur
		raw, err := json.Marshal(all)
		if err != nil {
			return err
		}
		if err := writeAtomic(filepath.Join(st.root, policiesFile), append(raw, '\n')); err != nil {
			return err
		}
		out = cur
		return nil
	})
	return out, err
}

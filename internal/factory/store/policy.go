package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/acl"
)

// ErrPolicyStale says the policy changed after the caller read it. Rev is the
// revision now stored, so a door can answer 412 and let the client re-read.
type ErrPolicyStale struct {
	Rev int
}

// Error says in words what to do next.
func (e *ErrPolicyStale) Error() string {
	return "the organization policy changed since you read it; read it again before saving"
}

// policyCache keeps the last policy read, keyed by its revision, so the
// decider does not parse the document on every check. The revision itself is
// read on each call, which is how another process's PUT takes effect without
// a restart.
type policyCache struct {
	sync.Mutex
	loaded bool
	rev    int
	doc    acl.Policy
}

// policyReader is what a database or a transaction offers for reading the row.
type policyReader interface {
	QueryRow(string, ...any) *sql.Row
}

// policyRevision reads the stored revision, and zero when no policy was ever
// saved, which means the default policy.
func policyRevision(q policyReader) (int, error) {
	var rev int
	err := q.QueryRow(`SELECT rev FROM org_policy WHERE id = 1`).Scan(&rev)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return rev, err
}

// checkPolicyRev fails with ErrPolicyStale unless the stored revision is want.
func checkPolicyRev(q policyReader, want int) error {
	rev, err := policyRevision(q)
	if err == nil && want != rev {
		return &ErrPolicyStale{Rev: rev}
	}
	return err
}

// corruptPolicyRow marks a stored document that does not parse, so the reader
// can fall back to the default policy with a warning instead of failing every
// request that asks who may see what.
type corruptPolicyRow struct{ error }

// readPolicy reads and parses the single policy row.
func readPolicy(q policyReader) (acl.Policy, error) {
	var p acl.Policy
	var doc string
	var rev int
	var updatedAt, updatedBy string
	err := q.QueryRow(`SELECT rev, doc, updated_at, updated_by FROM org_policy WHERE id = 1`).Scan(&rev, &doc, &updatedAt, &updatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return acl.DefaultPolicy(), nil
	}
	if err != nil {
		return p, err
	}
	err = json.Unmarshal([]byte(doc), &p)
	if err == nil {
		err = p.Validate(acl.Policy{}, nil)
	}
	if err != nil {
		p = acl.DefaultPolicy()
		err = &corruptPolicyRow{err}
	}
	p.Rev, p.UpdatedAt, p.UpdatedBy = rev, updatedAt, updatedBy
	return p, err
}

// copyPolicy returns a policy that shares no slice or map with p, because the
// cached document is handed to many requests.
func copyPolicy(p acl.Policy) acl.Policy {
	p.Roles = slices.Clone(p.Roles)
	for i := range p.Roles {
		p.Roles[i].Verbs = slices.Clone(p.Roles[i].Verbs)
	}
	p.Defaults = maps.Clone(p.Defaults)
	p.Agents.Scopes = maps.Clone(p.Agents.Scopes)
	return p
}

// OrgPolicy returns the organization's policy, or the default policy when none
// was ever saved. FIXES KEEP DEFAULTS: a server that never saved a policy
// behaves exactly as it did before policies existed.
func (st *Store) OrgPolicy() (acl.Policy, error) {
	st.policy.Lock()
	defer st.policy.Unlock()
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return acl.DefaultPolicy(), err
	}
	rev, err := policyRevision(db)
	if err != nil {
		return acl.DefaultPolicy(), err
	}
	if !st.policy.loaded || st.policy.rev != rev {
		p, err := readPolicy(db)
		if err != nil {
			var corrupt *corruptPolicyRow
			if !errors.As(err, &corrupt) {
				return acl.DefaultPolicy(), err
			}
			slog.Warn("the organization policy could not be read; using the default", "revision", p.Rev, "error", err)
		}
		st.policy.loaded, st.policy.rev, st.policy.doc = true, p.Rev, p
	}
	return copyPolicy(st.policy.doc), nil
}

// SavePolicy stores next as the new policy when the stored revision is still
// ifRev, and returns it with its new revision and the time and author set.
func (st *Store) SavePolicy(next acl.Policy, ifRev int) (acl.Policy, error) {
	return st.SavePolicyAt(next, ifRev, st.now())
}

// SavePolicyAt checks live grants at the same time used to decide event audiences.
func (st *Store) SavePolicyAt(next acl.Policy, ifRev int, now time.Time) (acl.Policy, error) {
	st.policy.Lock()
	defer st.policy.Unlock()
	err := st.wsTx(func(tx *sql.Tx) error {
		if err := checkPolicyRev(tx, ifRev); err != nil {
			return err
		}
		prev, err := readPolicy(tx)
		var corrupt *corruptPolicyRow
		if err != nil && !errors.As(err, &corrupt) {
			return err
		}
		// Checking live grants in the write transaction closes the race with a concurrent addition.
		at := accessTime(now)
		inUse := map[string]bool{prev.OwnerRole: true, prev.OrgRole: true, prev.Operator.Role: true, prev.Agents.DefaultScope: true}
		for _, role := range prev.Agents.Scopes {
			inUse[role] = true
		}
		rows, err := tx.Query(`SELECT DISTINCT role FROM grants WHERE expires_at IS NULL OR expires_at > ?`, at)
		if err != nil {
			return err
		}
		for rows.Next() {
			var role string
			if err := rows.Scan(&role); err != nil {
				rows.Close()
				return err
			}
			inUse[role] = true
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if err := next.Validate(prev, func(role string) bool { return inUse[role] }); err != nil {
			return err
		}
		next.Rev = ifRev + 1
		next.UpdatedAt = at
		doc, err := json.Marshal(next)
		if err != nil {
			return fmt.Errorf("the organization policy could not be saved: %w", err)
		}
		_, err = tx.Exec(`INSERT INTO org_policy (id, rev, doc, updated_at, updated_by) VALUES (1, ?, ?, ?, ?)
			ON CONFLICT (id) DO UPDATE SET rev = excluded.rev, doc = excluded.doc, updated_at = excluded.updated_at,
			updated_by = excluded.updated_by`, next.Rev, string(doc), next.UpdatedAt, next.UpdatedBy)
		return err
	})
	if err != nil {
		return acl.Policy{}, err
	}
	st.policy.loaded, st.policy.rev, st.policy.doc = true, next.Rev, copyPolicy(next)
	return next, nil
}

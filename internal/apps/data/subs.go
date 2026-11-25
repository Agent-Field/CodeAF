package data

import (
	"strings"
)

// LIVE QUERIES SEND ROW DIFFS, NEVER PAGES. After every committed change, each
// subscription on the changed type is asked two things about the records the
// change touched: which of them match its query now (upserts, sent as rows
// so a client can place them by its own order) and which matched before and
// no longer do (removes, sent as ids). A change that touched nothing a
// subscription can see sends it nothing. Total is the query's new count, so
// a list's "412 deals" stays right without a refetch.
//
// Matching "before" is done by SQLite, not by a second matcher in Go: the
// records as they were are laid out as a VALUES table named r, and the same
// compiled WHERE clause is asked of it. There is one definition of what a
// query matches.

// SubsMax is the most subscriptions one store holds.
const SubsMax = 1000

// SubSpec is what a subscription watches: a query over a type, or (Record
// set) one record.
type SubSpec struct {
	ID     string
	Type   string
	Query  string
	Record string
}

// Diff is what one change means to one subscription.
type Diff struct {
	Sub     string
	Upserts []Row
	Removes []string
	Total   int
}

// subscription is one live query and who it is for.
type subscription struct {
	spec SubSpec
	sc   Scope
	fn   func(Diff)
}

// Subscribe starts a live query; fn is told of every change that touches
// it, on the store's dispatch goroutine, in commit order (so it must not
// block). A subscription with an id already held replaces it.
func (s *Store) Subscribe(sc Scope, spec SubSpec, fn func(Diff)) error {
	spec.ID = strings.TrimSpace(spec.ID)
	if spec.ID == "" || len(spec.ID) > 200 {
		return errorf(CodeInvalid, "sub is required")
	}
	sch := s.schema.Load()
	t, err := sch.allow(sc, spec.Type, false)
	if err != nil {
		return err
	}
	if spec.Record != "" {
		if !validID(spec.Record) {
			return errorf(CodeInvalid, "%q is not a record id", spec.Record)
		}
	} else if _, err := compileQuery(sch, t, sc, spec.Query, s.now()); err != nil {
		return err
	}
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	if _, held := s.subs[spec.ID]; !held && len(s.subs) >= SubsMax {
		return errorf(CodeInvalid, "there are already %d live queries", SubsMax)
	}
	s.subs[spec.ID] = &subscription{spec: spec, sc: sc, fn: fn}
	return nil
}

// Unsubscribe stops a live query; one that is not held is not an error.
func (s *Store) Unsubscribe(id string) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	delete(s.subs, id)
}

// deliver tells every subscription on c's type what c means to it.
func (s *Store) deliver(c *Change) {
	s.subsMu.Lock()
	var subs []*subscription
	for _, sub := range s.subs {
		if sub.spec.Type == c.Type {
			subs = append(subs, sub)
		}
	}
	s.subsMu.Unlock()
	if len(subs) == 0 {
		return
	}
	ids := make([]string, 0, len(c.Records))
	for _, rc := range c.Records {
		ids = append(ids, rc.ID)
	}
	for _, sub := range subs {
		d, ok := s.diff(sub, c, ids)
		if !ok {
			continue
		}
		func() {
			defer func() { _ = recover() }()
			sub.fn(d)
		}()
	}
}

// diff is what c means to one subscription, and whether it means anything.
// A subscription its app may no longer read (its capabilities changed) or
// whose query no longer compiles (a field it named was removed) is told
// nothing.
func (s *Store) diff(sub *subscription, c *Change, ids []string) (Diff, bool) {
	sch := s.schema.Load()
	t, err := sch.allow(sub.sc, sub.spec.Type, false)
	if err != nil {
		return Diff{}, false
	}
	d := Diff{Sub: sub.spec.ID, Upserts: []Row{}, Removes: []string{}}
	if sub.spec.Record != "" {
		touched := false
		for _, id := range ids {
			touched = touched || id == sub.spec.Record
		}
		if !touched {
			return Diff{}, false
		}
		r, err := loadRecord(s.rdb, sub.spec.Record)
		if err != nil {
			return Diff{}, false
		}
		if r == nil || r.DeletedAt != "" || r.Type != t.Name {
			d.Removes = append(d.Removes, sub.spec.Record)
			return d, true
		}
		d.Upserts = append(d.Upserts, s.row(t, r, sub.sc, false))
		d.Total = 1
		return d, true
	}
	q, err := compileQuery(sch, t, sub.sc, sub.spec.Query, s.now())
	if err != nil {
		return Diff{}, false
	}
	where, args := q.filter()

	matchedNow := map[string]bool{}
	marks := placeholders(len(ids))
	nowArgs := append(append([]any(nil), args...), anys(ids)...)
	rows, err := s.rdb.Query(`SELECT `+recordColumnsR+` FROM records r WHERE `+where+` AND r.id IN (`+marks+`)`, nowArgs...)
	if err != nil {
		return Diff{}, false
	}
	byID := map[string]*record{}
	for rows.Next() {
		r, err := scanRecord(rows.Scan)
		if err != nil {
			rows.Close()
			return Diff{}, false
		}
		matchedNow[r.ID] = true
		byID[r.ID] = r
	}
	rows.Close()
	// Upserts keep the change's order, which is the order the call named.
	for _, id := range ids {
		if r := byID[id]; r != nil {
			d.Upserts = append(d.Upserts, s.row(t, r, sub.sc, false))
		}
	}

	if len(c.befores) > 0 {
		var values []string
		var beforeArgs []any
		for _, id := range ids {
			b := c.befores[id]
			if b == nil {
				continue
			}
			values = append(values, "(?, ?, ?, ?, ?, ?, NULL)")
			beforeArgs = append(beforeArgs, b.ID, b.Type, canonicalJSON(b.Data), canonicalJSON(b.Meta), b.CreatedAt, b.UpdatedAt)
		}
		if len(values) > 0 {
			stmt := `WITH r(id, type, data_json, meta_json, created_at, updated_at, deleted_at) AS (VALUES ` +
				strings.Join(values, ", ") + `) SELECT r.id FROM r WHERE ` + where
			rows, err := s.rdb.Query(stmt, append(beforeArgs, args...)...)
			if err != nil {
				return Diff{}, false
			}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return Diff{}, false
				}
				if !matchedNow[id] {
					d.Removes = append(d.Removes, id)
				}
			}
			rows.Close()
		}
	}
	if len(d.Upserts) == 0 && len(d.Removes) == 0 {
		return Diff{}, false
	}
	if err := s.rdb.QueryRow(`SELECT count(*) FROM records r WHERE `+where, args...).Scan(&d.Total); err != nil {
		return Diff{}, false
	}
	return d, true
}

// placeholders is n question marks for an IN list.
func placeholders(n int) string {
	if n == 0 {
		return "NULL"
	}
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

func anys(ids []string) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

package data

import (
	"math"
	"strings"
	"sync/atomic"
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
//
// EVERY DIFF CARRIES ITS CURSOR, the outbox sequence of the change it tells
// of. A client that reconnects subscribes again with the last cursor it saw
// and is sent one diff covering everything since: the records changed in
// between that match now, and the ones that do not as removes. When that
// cannot be told exactly (the outbox was trimmed past the cursor, or too much
// changed to send as one diff), it is sent a reset instead, and refetches.
//
// A live query may also watch one of the work tables (work items, timers,
// sign-offs, comments), all of it or one record's; its upserts are that
// table's rows.

// SubsMax is the most subscriptions one store holds.
const SubsMax = 1000

// beforeChunk is how many records as they were one query lays out.
const beforeChunk = 500

// resumeMax is the most records a resumed subscription is sent as one diff;
// past it, a reset.
const resumeMax = 5000

// SubSpec is what a subscription watches: a query over a type, one record
// (Record set), or a work table (Table set, narrowed to one record by Type
// and Record). Resume says After is the cursor the client last saw.
type SubSpec struct {
	ID     string
	Type   string
	Query  string
	Record string
	Table  string
	Resume bool
	After  int64
}

// Diff is what one change means to one subscription. Reset says the client
// must refetch: what changed since its cursor cannot be sent as a diff.
type Diff struct {
	Sub     string
	Upserts []Row
	Removes []string
	Total   int
	Cursor  int64
	Reset   bool
}

// subscription is one live query and who it is for.
type subscription struct {
	spec SubSpec
	sc   Scope
	fn   func(Diff)
	// from is, for a resuming subscription, the cursor it was answered
	// with: a change at or before it is brought by the resume, so it is not
	// told live as well. It is zero for any other subscription.
	from atomic.Int64
}

// resumeOp is a subscription asking to be brought up to date, which the
// dispatcher answers between two changes, so nothing is told out of order.
type resumeOp struct {
	sub   *subscription
	after int64
}

// Subscribe starts a live query; fn is told of every change that touches
// it, on the store's dispatch goroutine, in commit order (so it must not
// block). A subscription with an id already held replaces it. It answers the
// cursor the store stands at.
func (s *Store) Subscribe(sc Scope, spec SubSpec, fn func(Diff)) (int64, error) {
	spec.ID = strings.TrimSpace(spec.ID)
	if spec.ID == "" || len(spec.ID) > 200 {
		return 0, errorf(CodeInvalid, "sub is required")
	}
	sch := s.schema.Load()
	switch spec.Table {
	case "":
		t, err := sch.allow(sc, spec.Type, false)
		if err != nil {
			return 0, err
		}
		spec.Type = t.Name
		if spec.Record != "" {
			if !validID(spec.Record) {
				return 0, errorf(CodeInvalid, "%q is not a record id", spec.Record)
			}
		} else if _, err := compileQuery(sch, t, sc, spec.Query, s.now()); err != nil {
			return 0, err
		}
	case TableWorkItems, TableTimers, TableComments, TableSignoffs:
		if spec.Record != "" || spec.Type != "" || spec.Table == TableSignoffs || spec.Table == TableComments {
			t, err := sch.allow(sc, spec.Type, false)
			if err != nil {
				return 0, err
			}
			spec.Type = t.Name
			if !validID(spec.Record) {
				return 0, errorf(CodeInvalid, "a live query on %s names its record", spec.Table)
			}
		}
	default:
		return 0, errorf(CodeInvalid, "%q is not a table a live query can watch", spec.Table)
	}
	// The subscription is held BEFORE the cursor is read, so no change can
	// commit in between and be told to nobody.
	sub := &subscription{spec: spec, sc: sc, fn: fn}
	if spec.Resume {
		// Until its cursor is known, a resuming subscription is told
		// nothing live: the resume, which runs after every change already
		// queued, brings all of it.
		sub.from.Store(math.MaxInt64)
	}
	s.subsMu.Lock()
	if _, held := s.subs[spec.ID]; !held && len(s.subs) >= SubsMax {
		s.subsMu.Unlock()
		return 0, errorf(CodeInvalid, "there are already %d live queries", SubsMax)
	}
	s.subs[spec.ID] = sub
	s.subsMu.Unlock()
	cursor, err := s.Cursor()
	if err != nil {
		s.Unsubscribe(spec.ID)
		return 0, err
	}
	if spec.Resume {
		// A resume brings everything up to the moment it runs, which is
		// after this cursor, so a change at or before it is not told live
		// as well.
		sub.from.Store(cursor)
	}
	// Always queued, even from the current cursor: a change that landed
	// while the cursor was unknown was held back from live delivery, and the
	// resume is what brings it.
	if spec.Resume {
		s.closeMu.RLock()
		defer s.closeMu.RUnlock()
		if s.closed.Load() {
			return 0, errorf(CodeClosed, "the data store is closed")
		}
		s.notify <- &Change{resume: &resumeOp{sub: sub, after: spec.After}}
	}
	return cursor, nil
}

// Unsubscribe stops a live query; one that is not held is not an error.
func (s *Store) Unsubscribe(id string) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	delete(s.subs, id)
}

// held answers the subscription held under id, if it is still sub.
func (s *Store) held(sub *subscription) bool {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	return s.subs[sub.spec.ID] == sub
}

// tell hands one diff to a subscription, never letting its panic reach the
// dispatcher.
func tell(sub *subscription, d Diff) {
	defer func() { _ = recover() }()
	sub.fn(d)
}

// deliver tells every subscription what c means to it.
func (s *Store) deliver(c *Change) {
	if c.resume != nil {
		if s.held(c.resume.sub) {
			s.resume(c.resume)
		}
		return
	}
	ids := make(map[string][]string)
	for _, rc := range c.Records {
		ids[rc.Type] = append(ids[rc.Type], rc.ID)
	}
	s.subsMu.Lock()
	var subs []*subscription
	for _, sub := range s.subs {
		if c.Cursor > 0 && c.Cursor <= sub.from.Load() {
			continue
		}
		if sub.spec.Table == "" && len(ids[sub.spec.Type]) > 0 || sub.spec.Table != "" && len(c.Items) > 0 {
			subs = append(subs, sub)
		}
	}
	s.subsMu.Unlock()
	for _, sub := range subs {
		var d Diff
		var ok bool
		if sub.spec.Table != "" {
			d, ok = s.tableDiff(sub, c)
		} else {
			d, ok = s.diff(sub, c, ids[sub.spec.Type])
		}
		if ok {
			d.Cursor = c.Cursor
			tell(sub, d)
		}
	}
}

// resume brings one subscription up to date from its cursor: one diff of
// everything since, or a reset.
func (s *Store) resume(op *resumeOp) {
	sub := op.sub
	cursor, err := s.Cursor()
	if err != nil {
		return
	}
	reset := Diff{Sub: sub.spec.ID, Upserts: []Row{}, Removes: []string{}, Cursor: cursor, Reset: true}
	// A cursor past where the store stands came from some other store (a
	// draft made again, a copy restored): nothing it says can be trusted.
	if op.after > cursor || op.after < s.trimmed() {
		tell(sub, reset)
		return
	}
	sch := s.schema.Load()
	if sub.spec.Table != "" {
		// A table's rows are few and bounded: anything new on it since the
		// cursor is a refetch.
		var n int
		_ = s.rdb.QueryRow(`SELECT count(*) FROM outbox WHERE seq > ? AND event LIKE ?`, op.after, tableEvents[sub.spec.Table]+".%").Scan(&n)
		if n > 0 {
			tell(sub, reset)
		}
		return
	}
	t := sch.types[sub.spec.Type]
	if t == nil {
		return
	}
	var ids []string
	err = eachRow(s.rdb, `SELECT DISTINCT record FROM outbox WHERE seq > ? AND type_id = ? AND event LIKE 'record.%' LIMIT ?`,
		[]any{op.after, t.ID, resumeMax + 1}, func(scan func(...any) error) error {
			var id string
			if err := scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
			return nil
		})
	if err != nil {
		return
	}
	if len(ids) > resumeMax {
		tell(sub, reset)
		return
	}
	if len(ids) == 0 {
		return
	}
	// With no "before" to compare against, every changed record that does
	// not match now is sent as a remove; a client drops an id it never had.
	d, ok := s.diff(sub, &Change{}, ids)
	if !ok {
		d = Diff{Sub: sub.spec.ID, Upserts: []Row{}, Removes: []string{}}
	}
	matched := map[string]bool{}
	for _, r := range d.Upserts {
		matched[r.ID()] = true
	}
	d.Removes = []string{}
	for _, id := range ids {
		if !matched[id] {
			d.Removes = append(d.Removes, id)
		}
	}
	if !ok {
		if sub.spec.Record == "" {
			if q, err := compileQuery(sch, t, sub.sc, sub.spec.Query, s.now()); err == nil {
				where, args := q.filter()
				_ = s.rdb.QueryRow(`SELECT count(*) FROM records r WHERE `+where, args...).Scan(&d.Total)
			}
		}
	}
	d.Cursor = cursor
	tell(sub, d)
}

// tableEvents is the outbox event prefix each work table's events have.
var tableEvents = map[string]string{
	TableWorkItems: "work_item", TableTimers: "timer", TableSignoffs: "signoff", TableComments: "comment",
}

// tableDiff is what c means to a subscription on a work table: the rows of
// that table c wrote that the subscription may see and watches.
func (s *Store) tableDiff(sub *subscription, c *Change) (Diff, bool) {
	sch := s.schema.Load()
	d := Diff{Sub: sub.spec.ID, Upserts: []Row{}, Removes: []string{}}
	for _, it := range c.Items {
		if it.Table != sub.spec.Table || sub.spec.Record != "" && it.Record != sub.spec.Record {
			continue
		}
		typeID := ""
		if t := sch.types[it.Type]; t != nil {
			typeID = t.ID
		}
		if !sch.seen(sub.sc, it.App, typeID) {
			continue
		}
		d.Upserts = append(d.Upserts, Row(it.Row))
	}
	if len(d.Upserts) == 0 {
		return Diff{}, false
	}
	d.Total = s.tableTotal(sub)
	return d, true
}

// tableTotal is how many rows a work-table subscription watches now.
func (s *Store) tableTotal(sub *subscription) int {
	sp := sub.spec
	switch sp.Table {
	case TableWorkItems:
		list, _ := s.WorkItems(sub.sc, WorkFilter{Type: sp.Type, Record: sp.Record})
		return len(list)
	case TableTimers:
		list, _ := s.Timers(sub.sc, sp.Record, false)
		return len(list)
	case TableSignoffs:
		list, _ := s.Signoffs(sub.sc, sp.Type, sp.Record)
		return len(list)
	case TableComments:
		list, _ := s.Comments(sub.sc, sp.Type, sp.Record, "", "")
		return len(list)
	}
	return 0
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
		r, err := loadRecord(s.rdb, sch, sub.spec.Record)
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

	// The ids go in as one JSON array: an import of fifty thousand rows is
	// one change, and that many placeholders is past SQLite's limit.
	matchedNow := map[string]bool{}
	nowArgs := append(append([]any(nil), args...), canonicalJSON(ids))
	byID := map[string]*record{}
	err = eachRow(s.rdb, `SELECT `+recordColumnsR+` FROM records r WHERE `+where+` AND r.id IN (SELECT value FROM json_each(?))`, nowArgs,
		func(scan func(...any) error) error {
			r, err := scanRecord(sch, scan)
			if err != nil {
				return err
			}
			matchedNow[r.ID] = true
			byID[r.ID] = r
			return nil
		})
	if err != nil {
		return Diff{}, false
	}
	// Upserts keep the change's order, which is the order the call named.
	for _, id := range ids {
		if r := byID[id]; r != nil {
			d.Upserts = append(d.Upserts, s.row(t, r, sub.sc, false))
		}
	}

	var befores []*record
	for _, id := range ids {
		if b := c.befores[id]; b != nil {
			befores = append(befores, b)
		}
	}
	// The records as they were are asked about a chunk at a time, each chunk
	// well under SQLite's parameter limit.
	for len(befores) > 0 {
		chunk := befores[:min(len(befores), beforeChunk)]
		befores = befores[len(chunk):]
		var values []string
		var beforeArgs []any
		for _, b := range chunk {
			values = append(values, "(?, ?, ?, ?, ?, ?, ?, NULL)")
			beforeArgs = append(beforeArgs, b.ID, t.ID, b.Rev, canonicalJSON(t.encode(b.Data, b.unknown)), canonicalJSON(t.encodeMeta(b.Meta)), b.CreatedAt, b.UpdatedAt)
		}
		stmt := `WITH r(id, type_id, rev, data_json, meta_json, created_at, updated_at, deleted_at) AS (VALUES ` +
			strings.Join(values, ", ") + `) SELECT r.id FROM r WHERE ` + where
		err := eachRow(s.rdb, stmt, append(beforeArgs, args...), func(scan func(...any) error) error {
			var id string
			if err := scan(&id); err != nil {
				return err
			}
			if !matchedNow[id] {
				d.Removes = append(d.Removes, id)
			}
			return nil
		})
		if err != nil {
			return Diff{}, false
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

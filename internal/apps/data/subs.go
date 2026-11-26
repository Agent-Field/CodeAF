package data

import (
	"errors"
	"slices"
	"strings"
)

// NATIVE LIVE QUERIES START WITH A SNAPSHOT AND SEND ROW DIFFS AFTER IT. Each
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

// SubscriptionOwner identifies an authenticated connection or handle. The
// engine allocates one per owner and never accepts it from wire parameters.
type SubscriptionOwner struct{ marker byte }

type subscriptionKey struct {
	owner *SubscriptionOwner
	id    string
}

// SubSpec is a watch on a record, query or work table. A federated list or
// aggregate names its dependencies and uses InvalidateOnly.
type SubSpec struct {
	ID             string
	Type           string
	Query          string
	Record         string
	Table          string
	Resume         bool
	After          int64
	InvalidateOnly bool
	TypeIDs        []string
	Bindings       []string
	// Authorize rechecks a trusted handle's policy. It must not write to
	// the store or block the dispatcher.
	Authorize func() bool
}

// Snapshot pairs the first page with the cursor of the same committed state.
type Snapshot struct {
	Cursor int64
	Rows   []Row
	Next   string
	Total  int
}

// Diff is what one change means to one subscription. Reset clears cached
// state and directs the client to refetch under its current policy.
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
	key      subscriptionKey
	spec     SubSpec
	sc       Scope
	fn       func(Diff)
	typeID   string
	related  map[string]bool
	bindings map[string]bool
	from     int64
	revoked  bool
}

type resumeOp struct {
	sub   *subscription
	after int64
}

// Subscribe is the compatibility entry for a store's default local owner.
func (s *Store) Subscribe(sc Scope, spec SubSpec, fn func(Diff)) (int64, error) {
	snap, err := s.watch(nil, sc, spec, fn, false)
	return snap.Cursor, err
}

// Watch starts a connection-owned subscription and returns its initial page
// and outbox cursor. Resuming and invalidation-only watches return a cursor
// and deliver a delta or reset separately.
func (s *Store) Watch(owner *SubscriptionOwner, sc Scope, spec SubSpec, fn func(Diff)) (Snapshot, error) {
	if owner == nil {
		return Snapshot{}, errorf(CodeInvalid, "a subscription needs an authenticated owner")
	}
	return s.watch(owner, sc, spec, fn, true)
}

func (s *Store) watch(owner *SubscriptionOwner, sc Scope, spec SubSpec, fn func(Diff), snapshot bool) (Snapshot, error) {
	spec.ID = strings.TrimSpace(spec.ID)
	if spec.ID == "" || len(spec.ID) > 200 || fn == nil || len(spec.TypeIDs) > PageMax || len(spec.Bindings) > PageMax {
		return Snapshot{}, errorf(CodeInvalid, "invalid subscription")
	}
	key := subscriptionKey{owner, spec.ID}
	sub := &subscription{key: key, spec: spec, sc: sc, fn: fn, related: map[string]bool{}, bindings: map[string]bool{}}
	var snap Snapshot
	err := s.write(func(w *writeTx) error {
		if err := s.planWatch(w.schema, sub); err != nil {
			return err
		}
		if !sub.authorized(w.schema) {
			return notAllowed(sc.App, spec.Type, false)
		}
		var err error
		if snapshot && !spec.Resume && !spec.InvalidateOnly {
			snap, err = s.watchSnapshot(sub)
			if err != nil {
				return err
			}
		}
		if snap.Cursor, err = s.Cursor(); err != nil {
			return err
		}
		sub.from = snap.Cursor
		w.afterCommit = func() {
			s.subsMu.Lock()
			s.subs[key] = sub
			s.subsMu.Unlock()
		}
		s.subsMu.Lock()
		_, held := s.subs[key]
		full := len(s.subs) >= SubsMax
		s.subsMu.Unlock()
		if !held && full {
			return errorf(CodeInvalid, "there are already %d live queries", SubsMax)
		}
		if spec.Resume {
			w.change = &Change{resume: &resumeOp{sub: sub, after: spec.After}}
		}
		return nil
	})
	if err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

func (s *Store) planWatch(sch *schema, sub *subscription) error {
	sp := sub.spec
	switch sp.Table {
	case "", TableWorkItems, TableTimers, TableComments, TableSignoffs:
	default:
		return errorf(CodeInvalid, "%q is not a live query table", sp.Table)
	}
	if sp.Record != "" && !validID(sp.Record) ||
		sp.Record == "" && (sp.Table == TableComments || sp.Table == TableSignoffs || sp.Table != "" && sp.Type != "") {
		return errorf(CodeInvalid, "a record watch requires a record id")
	}
	if sp.Table == "" || sp.Record != "" {
		t, err := sch.allow(sub.sc, sp.Type, false)
		if err != nil {
			return err
		}
		sub.typeID = t.ID
		if sp.Record == "" {
			q, err := compileQuery(sch, t, sub.sc, sp.Query, s.now())
			if err != nil {
				return err
			}
			sub.related = q.related
		}
	}
	for _, id := range sp.TypeIDs {
		t, err := sch.allow(sub.sc, id, false)
		if err != nil {
			return err
		}
		sub.related[t.ID] = true
	}
	for _, binding := range sp.Bindings {
		sub.bindings[binding] = true
	}
	return nil
}

func (sub *subscription) authorized(sch *schema) bool {
	if sub.spec.Authorize != nil && !sub.spec.Authorize() {
		return false
	}
	if sub.sc.App != "" && sch.caps[sub.sc.App] == nil {
		return false
	}
	if sub.typeID != "" {
		if t, err := sch.allow(sub.sc, sub.spec.Type, false); err != nil || t.ID != sub.typeID {
			return false
		}
	}
	for id := range sub.related {
		if _, err := sch.allow(sub.sc, id, false); err != nil {
			return false
		}
	}
	return true
}

// HasSourceWatch avoids journaling periodic invalidations without a listener.
func (s *Store) HasSourceWatch(typeID, binding string) bool {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	for _, sub := range s.subs {
		if sub.bindings[binding] && (sub.typeID == typeID || sub.related[typeID]) {
			return true
		}
	}
	return false
}

// Unsubscribe stops a compatibility subscription.
func (s *Store) Unsubscribe(id string) { s.Unwatch(nil, id) }

// Unwatch stops a subscription belonging to owner only.
func (s *Store) Unwatch(owner *SubscriptionOwner, id string) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	delete(s.subs, subscriptionKey{owner, strings.TrimSpace(id)})
}

func (s *Store) held(sub *subscription) bool {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	return s.subs[sub.key] == sub
}

func tell(sub *subscription, d Diff) {
	defer func() { _ = recover() }()
	sub.fn(d)
}

func (s *Store) reset(sub *subscription, cursor int64) {
	sub.from = cursor
	if s.held(sub) {
		tell(sub, Diff{Sub: sub.spec.ID, Upserts: []Row{}, Removes: []string{}, Cursor: cursor, Reset: true})
	}
}

// deliver projects each event against the latest committed policy. The
// visibility lock orders projection and enqueue against policy commits.
func (s *Store) deliver(c *Change) {
	s.visibility.RLock()
	defer s.visibility.RUnlock()
	sch := s.schema.Load()
	if c.resume != nil {
		if s.held(c.resume.sub) {
			s.resume(c.resume)
		}
		return
	}
	ids := make(map[string][]string)
	for _, rc := range c.Records {
		ids[rc.TypeID] = append(ids[rc.TypeID], rc.ID)
	}
	s.subsMu.Lock()
	subs := make([]*subscription, 0, len(s.subs))
	for _, sub := range s.subs {
		subs = append(subs, sub)
	}
	s.subsMu.Unlock()
	for _, sub := range subs {
		if c.Cursor > 0 && c.Cursor <= sub.from || !s.held(sub) {
			continue
		}
		allowed := sub.authorized(sch)
		if !allowed || sub.revoked {
			if allowed == sub.revoked {
				sub.revoked = !allowed
				s.reset(sub, c.Cursor)
			}
			continue
		}
		reset := false
		for id := range ids {
			reset = reset || sub.related[id] && id != sub.typeID
		}
		if inv := c.invalidation; inv != nil {
			typeMatch := inv.typeID == "" || sub.typeID == inv.typeID || sub.related[inv.typeID] ||
				sub.spec.Table != "" && sub.typeID == ""
				// A primary-type watcher opened before a binding existed still
			// needs its reset. Related inputs use their declared bindings.
			reset = reset || typeMatch && (inv.binding == "" || sub.typeID == inv.typeID || sub.bindings[inv.binding])
		}
		touched := sub.spec.Table == "" && len(ids[sub.typeID]) > 0 || sub.spec.Table != "" && len(c.Items) > 0
		if reset || sub.spec.InvalidateOnly && touched {
			// A refetch covers everything committed up to this cursor.
			cursor, err := s.Cursor()
			if err == nil {
				s.reset(sub, cursor)
			}
			continue
		}
		if !touched {
			continue
		}
		var d Diff
		var ok bool
		if sub.spec.Table != "" {
			d, ok = s.tableDiff(sub, c)
		} else {
			d, ok = s.diff(sub, c, ids[sub.typeID])
		}
		if ok && s.held(sub) {
			d.Cursor = c.Cursor
			tell(sub, d)
		}
	}
}

// resume uses the same policy and dependency set as a live delivery.
func (s *Store) resume(op *resumeOp) {
	sub := op.sub
	cursor, err := s.Cursor()
	if err != nil {
		return
	}
	sub.from = cursor
	if !sub.authorized(s.schema.Load()) {
		sub.revoked = true
		s.reset(sub, cursor)
		return
	}
	if op.after > cursor || op.after < s.trimmed() {
		s.reset(sub, cursor)
		return
	}
	var ids []string
	deps := sortedKeys(sub.related)
	if sub.typeID != "" {
		deps = append(deps, sub.typeID)
	}
	events := 0
	err = eachRow(s.rdb, `SELECT event, type_id, record FROM outbox
		WHERE seq > ? AND (binding = '' OR binding IN (SELECT value FROM json_each(?)))
		AND ((event IN (?, ?, ?) AND (type_id = '' OR type_id IN (SELECT value FROM json_each(?)) OR ?))
			OR (event LIKE 'record.%' AND type_id IN (SELECT value FROM json_each(?)) AND (? = '' OR record = ?))
			OR (? != '' AND event LIKE ? AND (? = '' OR record = ?)))
		ORDER BY seq LIMIT ?`,
		[]any{op.after, canonicalJSON(sortedKeys(sub.bindings)), EventPolicyChanged, EventMappingChanged, EventFreshnessChanged,
			canonicalJSON(deps), sub.spec.Table != "" && sub.typeID == "", canonicalJSON(deps),
			sub.spec.Record, sub.spec.Record, sub.spec.Table, tableEvents[sub.spec.Table] + ".%", sub.spec.Record, sub.spec.Record, resumeMax + 1},
		func(scan func(...any) error) error {
			var event, typeID, id string
			if err := scan(&event, &typeID, &id); err != nil {
				return err
			}
			events++
			if sub.spec.InvalidateOnly || sub.spec.Table != "" || typeID != sub.typeID || !strings.HasPrefix(event, "record.") {
				return errSubscriptionReset
			}
			ids = append(ids, id)
			return nil
		})
	if err != nil || events > resumeMax {
		s.reset(sub, cursor)
		return
	}
	if len(ids) == 0 {
		return
	}
	ids = uniqueSorted(ids)
	d, ok := s.diff(sub, &Change{}, ids)
	if !ok {
		if sub.spec.Record != "" {
			s.reset(sub, cursor)
			return
		}
		sch := s.schema.Load()
		t := sch.byID[sub.typeID]
		q, err := compileQuery(sch, t, sub.sc, sub.spec.Query, s.now())
		if err != nil {
			s.reset(sub, cursor)
			return
		}
		where, args := q.filter()
		d = Diff{Sub: sub.spec.ID, Upserts: []Row{}}
		if err := s.rdb.QueryRow(`SELECT count(*) FROM records r WHERE `+where, args...).Scan(&d.Total); err != nil {
			s.reset(sub, cursor)
			return
		}
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
	d.Cursor = cursor
	tell(sub, d)
}

var errSubscriptionReset = errors.New("subscription needs a fresh snapshot")

func uniqueSorted(ids []string) []string {
	slices.Sort(ids)
	return slices.Compact(ids)
}

func (s *Store) watchSnapshot(sub *subscription) (Snapshot, error) {
	sp := sub.spec
	if sp.Table != "" {
		rows, err := s.watchTable(sub)
		return Snapshot{Rows: rows, Total: len(rows)}, err
	}
	if sp.Record != "" {
		r, err := s.Get(sub.sc, sp.Type, sp.Record, false)
		if e, ok := AsError(err); ok && e.Code == CodeNotFound {
			return Snapshot{Rows: []Row{}}, nil
		}
		if err != nil {
			return Snapshot{}, err
		}
		return Snapshot{Rows: []Row{r}, Total: 1}, nil
	}
	page, err := s.Query(sub.sc, sp.Type, sp.Query, "", 0)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Rows: append([]Row{}, page.Rows...), Next: page.Next, Total: page.Total}, nil
}

func (s *Store) watchTable(sub *subscription) ([]Row, error) {
	sp := sub.spec
	rows := []Row{}
	switch sp.Table {
	case TableWorkItems:
		list, err := s.WorkItems(sub.sc, WorkFilter{Type: sp.Type, Record: sp.Record})
		for _, item := range list {
			rows = append(rows, item.Row())
		}
		return rows, err
	case TableTimers:
		list, err := s.Timers(sub.sc, sp.Record, false)
		for _, item := range list {
			rows = append(rows, item.Row())
		}
		return rows, err
	case TableSignoffs:
		list, err := s.Signoffs(sub.sc, sp.Type, sp.Record)
		for _, item := range list {
			rows = append(rows, item.Row())
		}
		return rows, err
	case TableComments:
		list, err := s.Comments(sub.sc, sp.Type, sp.Record, "", "")
		for _, item := range list {
			rows = append(rows, item.Row())
		}
		return rows, err
	}
	return rows, nil
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
		if !sch.seen(sub.sc, it.App, typeID) || !sch.canReadSource(sub.sc, it.sourceType) ||
			it.Table == TableWorkItems && it.Row["identity_match"] != nil && !identityOwner(sub.sc) {
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
// whose query no longer compiles receives its reset at the policy change.
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

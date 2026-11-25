package data

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// The bounds of one call.
const (
	// IDsMax is the most records one update, delete or create names.
	IDsMax = 1000
	// HistoryMax is the most history entries one data.history answers.
	HistoryMax = 200
)

// The ops a batch and a history row carry.
const (
	OpCreate  = "create"
	OpUpdate  = "update"
	OpDelete  = "delete"
	OpUndo    = "undo"
	OpImport  = "import"
	OpRefresh = "refresh"
)

// The sources of an ai field's value.
const (
	SourceAI     = "ai"
	SourcePinned = "pinned"
)

// Row is one record as a caller sees it: its fields by name beside id, type,
// created_at and updated_at, and "_meta" saying which values are masked,
// which ai values came from AI or were pinned by a person, and whether a
// freeze rule holds the record.
type Row map[string]any

// RowMeta is a row's "_meta".
type RowMeta struct {
	AI     map[string]string `json:"ai,omitempty"`
	Masked []string          `json:"masked,omitempty"`
	Frozen bool              `json:"frozen,omitempty"`
	// Writable is the fields a frozen record still lets be changed.
	Writable []string `json:"writable,omitempty"`
}

// ID is the row's id.
func (r Row) ID() string { s, _ := r["id"].(string); return s }

// Change is one committed batch: every record it touched, before and after.
// Before is nil for a created record; After is nil for a deleted one. The
// rows are whole, never masked.
type Change struct {
	Draft   string
	Batch   string
	App     string
	Actor   string
	Op      string
	Type    string
	At      time.Time
	Records []RecordChange

	// befores are the live records as they were, for subscriptions to know
	// which of them a query matched before the change.
	befores map[string]*record
}

// RecordChange is one record of a [Change], and the fields that changed.
type RecordChange struct {
	ID      string
	Before  Row
	After   Row
	Changed []string
}

// WriteResult is what a write answers: its batch (empty when nothing
// changed), the rows as they now are, and (for an undo) the ids of the
// records it removed, an undone create.
type WriteResult struct {
	Batch   string
	Rows    []Row
	Removes []string
}

// record is one row of the records table.
type record struct {
	ID        string
	Type      string
	Data      map[string]any
	Meta      recordMeta
	CreatedAt string
	UpdatedAt string
	DeletedAt string
}

// recordMeta is what a record keeps beside its values.
type recordMeta struct {
	// AI is field → "ai" or "pinned" for every ai field holding a value.
	AI map[string]string `json:"ai,omitempty"`
}

// state is a record as a history row keeps it.
type state struct {
	Data    map[string]any `json:"data"`
	Meta    recordMeta     `json:"meta"`
	Deleted bool           `json:"deleted,omitempty"`
}

func (r *record) state() state {
	return state{Data: r.Data, Meta: r.Meta, Deleted: r.DeletedAt != ""}
}

// key is r's state without its computed fields, the comparison an undo makes
// to know nothing has changed the record since: a computed value recomputed
// by the clock is not a change anyone made.
func (r *record) key(t *Type) string {
	data := make(map[string]any, len(r.Data))
	for k, v := range r.Data {
		if f := t.Fields[k]; f != nil && f.Computed != "" {
			continue
		}
		data[k] = v
	}
	return canonicalJSON(state{Data: data, Meta: r.Meta, Deleted: r.DeletedAt != ""})
}

func (r *record) clone() *record {
	c := *r
	c.Data = make(map[string]any, len(r.Data))
	for k, v := range r.Data {
		c.Data[k] = v
	}
	c.Meta.AI = map[string]string{}
	for k, v := range r.Meta.AI {
		c.Meta.AI[k] = v
	}
	return &c
}

// values is what an expression reads: the fields and the row's own names.
func (r *record) values() map[string]any {
	v := make(map[string]any, len(r.Data)+3)
	for k, x := range r.Data {
		v[k] = x
	}
	v["id"], v["created_at"], v["updated_at"], v["type"] = r.ID, r.CreatedAt, r.UpdatedAt, r.Type
	return v
}

// scanRecord reads one record from a row of id, type, data_json, meta_json,
// created_at, updated_at, deleted_at.
func scanRecord(scan func(...any) error) (*record, error) {
	var r record
	var data, meta string
	var deleted sql.NullString
	if err := scan(&r.ID, &r.Type, &data, &meta, &r.CreatedAt, &r.UpdatedAt, &deleted); err != nil {
		return nil, err
	}
	r.DeletedAt = deleted.String
	if err := json.Unmarshal([]byte(data), &r.Data); err != nil {
		return nil, fmt.Errorf("record %s is unreadable: %w", r.ID, err)
	}
	if r.Data == nil {
		r.Data = map[string]any{}
	}
	_ = json.Unmarshal([]byte(meta), &r.Meta)
	if r.Meta.AI == nil {
		r.Meta.AI = map[string]string{}
	}
	return &r, nil
}

const recordColumns = `id, type, data_json, meta_json, created_at, updated_at, deleted_at`

// loadRecord reads one record, deleted or not; nil when there is none.
func loadRecord(q querier, id string) (*record, error) {
	r, err := scanRecord(q.QueryRow(`SELECT `+recordColumns+` FROM records WHERE id = ?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

// row is r as a scope sees it. A masked value reads as "••••" unless
// revealed, and never for an agent; a hidden field is left out for an app.
// frozen is the record's freeze state, already decided.
func (s *Store) row(t *Type, r *record, sc Scope, reveal bool) Row {
	out := Row{"id": r.ID, "type": r.Type, "created_at": r.CreatedAt, "updated_at": r.UpdatedAt}
	var meta RowMeta
	for _, name := range t.Order {
		f := t.Fields[name]
		if f.Hidden && sc.App != "" {
			continue
		}
		v, ok := r.Data[name]
		if !ok {
			continue
		}
		if f.Masked && (!reveal || sc.Agent) {
			out[name] = masked
			meta.Masked = append(meta.Masked, name)
			continue
		}
		out[name] = v
	}
	if len(r.Meta.AI) > 0 {
		meta.AI = map[string]string{}
		for k, v := range r.Meta.AI {
			if f := t.Fields[k]; f != nil && (!f.Hidden || sc.App == "") {
				meta.AI[k] = v
			}
		}
	}
	if frozen, except := s.frozen(t, r); frozen {
		meta.Frozen = true
		meta.Writable = except
	}
	out["_meta"] = meta
	return out
}

// whole is r as a [Change] carries it: every value, unmasked.
func whole(r *record) Row {
	if r == nil {
		return nil
	}
	out := Row{"id": r.ID, "type": r.Type, "created_at": r.CreatedAt, "updated_at": r.UpdatedAt}
	for k, v := range r.Data {
		out[k] = v
	}
	if len(r.Meta.AI) > 0 {
		ai := map[string]string{}
		for k, v := range r.Meta.AI {
			ai[k] = v
		}
		out["_meta"] = RowMeta{AI: ai}
	}
	return out
}

// frozen says whether a freeze rule holds r, and which fields every holding
// rule still lets be written.
func (s *Store) frozen(t *Type, r *record) (bool, []string) {
	if len(t.Freeze) == 0 || r.DeletedAt != "" {
		return false, nil
	}
	var except map[string]bool
	held := false
	values := r.values()
	for _, rule := range t.Freeze {
		if !rule.holds(t, values, s.now()) {
			continue
		}
		if !held {
			held = true
			except = map[string]bool{}
			for k := range rule.except {
				except[k] = true
			}
			continue
		}
		for k := range except {
			if !rule.except[k] {
				delete(except, k)
			}
		}
	}
	if !held {
		return false, nil
	}
	return true, sortedKeys(except)
}

// holds says whether one rule freezes a record with these values.
func (rule *freezeRule) holds(t *Type, values map[string]any, now time.Time) bool {
	if rule.when != nil {
		return rule.when.evalBool(t, values, nil)
	}
	if rule.Period == "" {
		return false
	}
	s, _ := values[rule.Field].(string)
	at, err := readTime(s, now)
	if s == "" || err != nil {
		return false
	}
	return now.After(periodEnd(at.Local(), rule.Period).Add(rule.after))
}

// periodEnd is the first instant after the period holding at.
func periodEnd(at time.Time, period string) time.Time {
	y, m, d := at.Date()
	loc := at.Location()
	switch period {
	case "week":
		start := time.Date(y, m, d, 0, 0, 0, 0, loc)
		offset := (int(start.Weekday()) + 6) % 7 // days since Monday
		return start.AddDate(0, 0, 7-offset)
	case "month":
		return time.Date(y, m+1, 1, 0, 0, 0, 0, loc)
	case "quarter":
		q := (int(m) - 1) / 3
		return time.Date(y, time.Month(q*3+4), 1, 0, 0, 0, 0, loc)
	default:
		return time.Date(y+1, 1, 1, 0, 0, 0, 0, loc)
	}
}

// newBatch starts a batch on w: its id, its row, and the change it reports.
func (w *writeTx) newBatch(sc Scope, op, typ string) (string, error) {
	id := w.s.ids.next(w.now)
	if _, err := w.tx.Exec(`INSERT INTO batches(batch, app, actor, op, type, at) VALUES(?, ?, ?, ?, ?, ?)`,
		id, sc.App, sc.actor(), op, typ, w.now.UTC().Format(stamp)); err != nil {
		return "", err
	}
	w.change = &Change{Batch: id, App: sc.App, Actor: sc.actor(), Op: op, Type: typ, At: w.now}
	return id, nil
}

// history writes one history row and adds the record to the change.
func (w *writeTx) history(sc Scope, op, note string, before, after *record) error {
	var b, a any
	var id, typ string
	if before != nil {
		b = canonicalJSON(before.state())
		id, typ = before.ID, before.Type
	}
	if after != nil {
		a = canonicalJSON(after.state())
		id, typ = after.ID, after.Type
	}
	if _, err := w.tx.Exec(`
		INSERT INTO history(batch, type, id, app, actor, op, note, before_json, after_json, at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.change.Batch, typ, id, sc.App, sc.actor(), op, note, b, a, w.now.UTC().Format(stamp)); err != nil {
		return err
	}
	w.note(before, after)
	return nil
}

// note adds one record to the change w is making.
func (w *writeTx) note(before, after *record) {
	id := ""
	if before != nil {
		id = before.ID
	}
	if after != nil {
		id = after.ID
	}
	rc := RecordChange{ID: id, Before: whole(before), After: whole(after), Changed: changedFields(before, after)}
	if after != nil && after.DeletedAt != "" {
		rc.After = nil
	}
	if before != nil && before.DeletedAt != "" {
		rc.Before = nil
		before = nil
	}
	w.change.Records = append(w.change.Records, rc)
	if before != nil {
		if w.change.befores == nil {
			w.change.befores = map[string]*record{}
		}
		w.change.befores[id] = before
	}
}

// changedFields names the fields whose values differ between two states.
func changedFields(before, after *record) []string {
	seen := map[string]bool{}
	var out []string
	add := func(k string) {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	var b, a map[string]any
	if before != nil && before.DeletedAt == "" {
		b = before.Data
	}
	if after != nil && after.DeletedAt == "" {
		a = after.Data
	}
	for k, v := range a {
		if canonicalJSON(v) != canonicalJSON(b[k]) || b == nil {
			add(k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			add(k)
		}
	}
	sort.Strings(out)
	return out
}

// save writes r: an insert for a new record, else an update.
func (w *writeTx) save(r *record, insert bool) error {
	data, meta := canonicalJSON(r.Data), canonicalJSON(r.Meta)
	var deleted any
	if r.DeletedAt != "" {
		deleted = r.DeletedAt
	}
	if insert {
		_, err := w.tx.Exec(`INSERT INTO records(id, type, data_json, meta_json, created_at, updated_at, deleted_at) VALUES(?, ?, ?, ?, ?, ?, ?)`,
			r.ID, r.Type, data, meta, r.CreatedAt, r.UpdatedAt, deleted)
		return err
	}
	_, err := w.tx.Exec(`UPDATE records SET data_json = ?, meta_json = ?, updated_at = ?, deleted_at = ? WHERE id = ?`,
		data, meta, r.UpdatedAt, deleted, r.ID)
	return err
}

// applySet writes a set of values into r for scope sc. Values were coerced
// by prepareSet; here the ai rules apply: an AI write leaves a person's
// pinned value alone, and a person's write pins the value it writes.
func applySet(t *Type, r *record, set map[string]any, sc Scope) {
	for _, name := range sortedKeys(set) {
		v := set[name]
		f := t.Fields[name]
		if f.AI != "" && sc.AI && r.Meta.AI[name] == SourcePinned {
			continue
		}
		if v == nil {
			delete(r.Data, name)
			delete(r.Meta.AI, name)
			continue
		}
		r.Data[name] = v
		if f.AI != "" {
			if sc.AI {
				r.Meta.AI[name] = SourceAI
			} else {
				r.Meta.AI[name] = SourcePinned
			}
		}
	}
}

// prepareSet checks and coerces a set of values against t: every name a
// visible field that is not computed, every value one of its field's.
// Referenced records must exist.
func (w *writeTx) prepareSet(t *Type, set map[string]any, lenient bool) (map[string]any, error) {
	out := make(map[string]any, len(set))
	for _, name := range sortedKeys(set) {
		f := t.field(name)
		if f == nil {
			if reserved[name] || strings.HasPrefix(name, "_") {
				continue
			}
			return nil, fieldErr(name, "%s records have no field %q", t.Name, name)
		}
		if f.Computed != "" {
			return nil, fieldErr(name, "%s is computed; it cannot be written", name)
		}
		v, err := f.coerce(set[name], coercion{now: w.now, lenient: lenient})
		if err != nil {
			return nil, fieldErr(name, "%s %v", name, err)
		}
		if err := w.checkRefs(f, v); err != nil {
			return nil, err
		}
		out[name] = v
	}
	return out, nil
}

// checkRefs checks every id a ref or refs value holds names a live record of
// the field's type.
func (w *writeTx) checkRefs(f *Field, v any) error {
	var ids []any
	switch f.Type {
	case TypeRef:
		if v != nil {
			ids = []any{v}
		}
	case TypeRefs:
		ids, _ = v.([]any)
	default:
		return nil
	}
	for _, id := range ids {
		var n int
		if err := w.tx.QueryRow(`SELECT count(*) FROM records WHERE id = ? AND type = ? AND deleted_at IS NULL`, id, f.To).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return fieldErr(f.Name, "%s names a %s record that does not exist", f.Name, f.To)
		}
	}
	return nil
}

// compute fills r's computed fields. A computed field may read another, so
// the fields are computed again until nothing changes (at most once per
// field). An expression that cannot be evaluated (a null in arithmetic)
// leaves its field empty.
func compute(t *Type, r *record, changed []string) {
	if len(t.computedOrder) == 0 {
		return
	}
	for pass := 0; pass <= len(t.computedOrder); pass++ {
		moved := false
		values := r.values()
		for _, name := range t.computedOrder {
			f := t.Fields[name]
			var v any
			if out, err := f.computed.eval(t, values, changed); err == nil {
				coerced, err := f.coerce(out, coercion{now: time.Now(), lenient: true})
				if err == nil {
					v = coerced
				}
			}
			old, had := r.Data[name]
			if v == nil {
				if had {
					delete(r.Data, name)
					moved = true
				}
			} else if !had || canonicalJSON(old) != canonicalJSON(v) {
				r.Data[name] = v
				moved = true
			}
			values[name] = v
		}
		if !moved {
			return
		}
	}
}

// checkRecord checks what must hold of a whole record before it is saved:
// required fields have values, and unique values are not held by any other
// live record of the type. fields limits the checks to those fields (an
// update checks only what it wrote); nil checks every field.
func (w *writeTx) checkRecord(t *Type, r *record, fields []string) error {
	if fields == nil {
		fields = t.Order
	}
	for _, name := range fields {
		f := t.field(name)
		if f == nil {
			continue
		}
		v, has := r.Data[name]
		if f.Required && !has {
			return fieldErr(name, "%s is required", name)
		}
		if f.Unique && has {
			var other string
			err := w.tx.QueryRow(fmt.Sprintf(`SELECT id FROM records WHERE type = '%s' AND deleted_at IS NULL AND id != ? AND %s = ? LIMIT 1`,
				mustIdent(t.Name), jsonPath("data_json", mustIdent(name))), r.ID, sqlValue(v)).Scan(&other)
			if err == nil {
				return &Error{Code: CodeDuplicate, Field: name, Message: fmt.Sprintf("another %s already has this %s", t.Name, name)}
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
	}
	return nil
}

// sqlValue is a stored value as SQLite's json_extract answers it: a bool is
// 1 or 0, and a list compares as its JSON text.
func sqlValue(v any) any {
	switch x := v.(type) {
	case bool:
		if x {
			return 1
		}
		return 0
	case []any, map[string]any:
		return canonicalJSON(x)
	}
	return v
}

// mustIdent answers name when it is an identifier and panics otherwise. A
// name reaching SQL that is not one is a bug in this package, never a
// person's input (the schema refuses such names), and the writer turns the
// panic into a failed write.
func mustIdent(name string) string {
	if !identShape.MatchString(name) {
		panic(fmt.Sprintf("data: %q reached SQL as a name", name))
	}
	return name
}

// Create makes one record per set, in one batch.
func (s *Store) Create(sc Scope, typ string, sets []map[string]any) (*WriteResult, error) {
	if len(sets) == 0 {
		return nil, errorf(CodeInvalid, "nothing to create")
	}
	if len(sets) > IDsMax {
		return nil, errorf(CodeInvalid, "one call creates at most %d records", IDsMax)
	}
	return s.create(sc, typ, sets, OpCreate, false)
}

func (s *Store) create(sc Scope, typ string, sets []map[string]any, op string, lenient bool) (*WriteResult, error) {
	var made []*record
	var t *Type
	var batch string
	err := s.write(func(w *writeTx) error {
		made = nil
		var err error
		if t, err = w.schema.allow(sc, typ, true); err != nil {
			return err
		}
		if batch, err = w.newBatch(sc, op, typ); err != nil {
			return err
		}
		for _, set := range sets {
			r, err := w.createOne(t, sc, set, op, lenient)
			if err != nil {
				return err
			}
			made = append(made, r)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := &WriteResult{Batch: batch, Rows: make([]Row, 0, len(made))}
	for _, r := range made {
		out.Rows = append(out.Rows, s.row(t, r, sc, false))
	}
	return out, nil
}

// createOne makes one record inside w.
func (w *writeTx) createOne(t *Type, sc Scope, set map[string]any, op string, lenient bool) (*record, error) {
	return w.createOneAs(t, sc, set, op, lenient, "")
}

// createOneAs makes one record with the id given (a seeded sample row that
// other rows point to), or a new one.
func (w *writeTx) createOneAs(t *Type, sc Scope, set map[string]any, op string, lenient bool, id string) (*record, error) {
	values, err := w.prepareSet(t, set, lenient)
	if err != nil {
		return nil, err
	}
	if id == "" {
		id = w.s.ids.next(w.now)
	}
	at := w.now.UTC().Format(stamp)
	r := &record{ID: id, Type: t.Name, Data: map[string]any{}, Meta: recordMeta{AI: map[string]string{}}, CreatedAt: at, UpdatedAt: at}
	for _, name := range t.Order {
		f := t.field(name)
		if f == nil || f.Default == nil {
			continue
		}
		if _, given := values[name]; given {
			continue
		}
		v, err := f.coerce(f.Default, coercion{now: w.now})
		if err != nil {
			continue
		}
		r.Data[name] = v
	}
	applySet(t, r, values, sc)
	compute(t, r, sortedKeys(r.Data))
	if err := w.checkRecord(t, r, nil); err != nil {
		return nil, err
	}
	if err := w.save(r, true); err != nil {
		return nil, err
	}
	return r, w.history(sc, op, "", nil, r)
}

// UpdateOptions are an update's or delete's choices. Unfreeze is the reason
// a person gives for writing through a freeze rule; it is kept on the
// history row, and an agent or run cannot give one.
type UpdateOptions struct {
	Unfreeze string
}

// Update writes set into every record named, in one batch. A record that
// would not change is left alone and writes no history.
func (s *Store) Update(sc Scope, typ string, ids []string, set map[string]any, opts UpdateOptions) (*WriteResult, error) {
	ids, err := cleanIDs(ids)
	if err != nil {
		return nil, err
	}
	if len(set) == 0 {
		return nil, errorf(CodeInvalid, "nothing to set")
	}
	var t *Type
	var rows []*record
	var batch string
	err = s.write(func(w *writeTx) error {
		rows, batch = nil, ""
		var err error
		if t, err = w.schema.allow(sc, typ, true); err != nil {
			return err
		}
		values, err := w.prepareSet(t, set, false)
		if err != nil {
			return err
		}
		if len(values) == 0 {
			return errorf(CodeInvalid, "nothing to set")
		}
		for _, id := range ids {
			before, err := w.live(t, id)
			if err != nil {
				return err
			}
			if err := w.s.checkFrozen(t, before, sortedKeys(values), sc, opts.Unfreeze); err != nil {
				return err
			}
			after := before.clone()
			applySet(t, after, values, sc)
			changed := changedFields(before, after)
			compute(t, after, changed)
			if after.key(t) == before.key(t) {
				rows = append(rows, before)
				continue
			}
			if err := w.checkRecord(t, after, sortedKeys(values)); err != nil {
				return err
			}
			if w.change == nil {
				if batch, err = w.newBatch(sc, OpUpdate, typ); err != nil {
					return err
				}
			}
			after.UpdatedAt = w.now.UTC().Format(stamp)
			if err := w.save(after, false); err != nil {
				return err
			}
			if err := w.history(sc, OpUpdate, unfreezeNote(opts, w.s, t, before), before, after); err != nil {
				return err
			}
			rows = append(rows, after)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := &WriteResult{Batch: batch, Rows: make([]Row, 0, len(rows))}
	for _, r := range rows {
		out.Rows = append(out.Rows, s.row(t, r, sc, false))
	}
	return out, nil
}

// Delete deletes every record named, in one batch. A deleted record is kept
// (deleted_at is set) so its batch can be undone.
func (s *Store) Delete(sc Scope, typ string, ids []string, opts UpdateOptions) (*WriteResult, error) {
	ids, err := cleanIDs(ids)
	if err != nil {
		return nil, err
	}
	var batch string
	err = s.write(func(w *writeTx) error {
		t, err := w.schema.allow(sc, typ, true)
		if err != nil {
			return err
		}
		if batch, err = w.newBatch(sc, OpDelete, typ); err != nil {
			return err
		}
		for _, id := range ids {
			before, err := w.live(t, id)
			if err != nil {
				return err
			}
			if err := w.s.checkFrozen(t, before, nil, sc, opts.Unfreeze); err != nil {
				return err
			}
			after := before.clone()
			after.UpdatedAt = w.now.UTC().Format(stamp)
			after.DeletedAt = after.UpdatedAt
			if err := w.save(after, false); err != nil {
				return err
			}
			if err := w.history(sc, OpDelete, unfreezeNote(opts, w.s, t, before), before, after); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &WriteResult{Batch: batch, Rows: []Row{}}, nil
}

// unfreezeNote is the history note of a write made through a freeze rule.
func unfreezeNote(opts UpdateOptions, s *Store, t *Type, before *record) string {
	if frozen, _ := s.frozen(t, before); frozen && opts.Unfreeze != "" {
		return "unfrozen: " + opts.Unfreeze
	}
	return ""
}

// checkFrozen refuses a write to fields a freeze rule holds (nil fields is
// the whole record, a delete). A person may write through it by giving a
// reason; an agent or a run may not.
func (s *Store) checkFrozen(t *Type, r *record, fields []string, sc Scope, reason string) error {
	frozen, except := s.frozen(t, r)
	if !frozen {
		return nil
	}
	if strings.TrimSpace(reason) != "" && !sc.Agent {
		return nil
	}
	allowed := map[string]bool{}
	for _, k := range except {
		allowed[k] = true
	}
	if fields == nil {
		return &Error{Code: CodeFrozen, Message: fmt.Sprintf("this %s is frozen and cannot be deleted", t.Name)}
	}
	for _, k := range fields {
		if !allowed[k] {
			return &Error{Code: CodeFrozen, Field: k, Message: fmt.Sprintf("this %s is frozen; %s cannot be changed", t.Name, k)}
		}
	}
	return nil
}

// live reads a live record of t inside w, or not_found.
func (w *writeTx) live(t *Type, id string) (*record, error) {
	r, err := loadRecord(w.tx, id)
	if err != nil {
		return nil, err
	}
	if r == nil || r.Type != t.Name || r.DeletedAt != "" {
		return nil, errorf(CodeNotFound, "there is no %s record %s", t.Name, id)
	}
	return r, nil
}

// cleanIDs checks a call's ids: at least one, at most [IDsMax], each a ULID,
// repeats dropped.
func cleanIDs(ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, errorf(CodeInvalid, "ids is required")
	}
	if len(ids) > IDsMax {
		return nil, errorf(CodeInvalid, "one call names at most %d records", IDsMax)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if !validID(id) {
			return nil, errorf(CodeInvalid, "%q is not a record id", id)
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

// Undo reverts one batch, as a new batch, when every record it touched is
// still as the batch left it. If anything has changed one of them since, the
// undo is refused whole: putting the old values back would silently throw
// away the later change.
func (s *Store) Undo(sc Scope, batch string) (*WriteResult, error) {
	batch = strings.TrimSpace(batch)
	if !validID(batch) {
		return nil, errorf(CodeInvalid, "%q is not a batch", batch)
	}
	var t *Type
	var rows []*record
	var removes []string
	var undo string
	err := s.write(func(w *writeTx) error {
		rows, removes, undo = nil, nil, ""
		var typ string
		var undone sql.NullString
		err := w.tx.QueryRow(`SELECT type, undone_by FROM batches WHERE batch = ?`, batch).Scan(&typ, &undone)
		if errors.Is(err, sql.ErrNoRows) {
			return errorf(CodeNotFound, "there is no batch %s", batch)
		}
		if err != nil {
			return err
		}
		if undone.Valid {
			return errorf(CodeStale, "that change was already undone")
		}
		if t, err = w.schema.allow(sc, typ, true); err != nil {
			return err
		}
		type step struct {
			id            string
			before, after *state
		}
		var steps []step
		hist, err := w.tx.Query(`SELECT id, before_json, after_json FROM history WHERE batch = ? ORDER BY seq`, batch)
		if err != nil {
			return err
		}
		for hist.Next() {
			var id string
			var b, a sql.NullString
			if err := hist.Scan(&id, &b, &a); err != nil {
				hist.Close()
				return err
			}
			st := step{id: id}
			for _, pair := range []struct {
				raw sql.NullString
				to  **state
			}{{b, &st.before}, {a, &st.after}} {
				if pair.raw.Valid {
					var x state
					if err := json.Unmarshal([]byte(pair.raw.String), &x); err != nil {
						hist.Close()
						return err
					}
					*pair.to = &x
				}
			}
			steps = append(steps, st)
		}
		hist.Close()
		if len(steps) == 0 {
			return errorf(CodeStale, "that change touched no records")
		}
		if undo, err = w.newBatch(sc, OpUndo, typ); err != nil {
			return err
		}
		if _, err := w.tx.Exec(`UPDATE batches SET undoes = ? WHERE batch = ?`, batch, undo); err != nil {
			return err
		}
		stale := 0
		for _, st := range steps {
			current, err := loadRecord(w.tx, st.id)
			if err != nil {
				return err
			}
			expected := &record{Type: typ, Data: map[string]any{}, Meta: recordMeta{AI: map[string]string{}}}
			if st.after != nil {
				expected.Data, expected.Meta = st.after.Data, st.after.Meta
				if st.after.Deleted {
					expected.DeletedAt = "deleted"
				}
			}
			if expected.Data == nil {
				expected.Data = map[string]any{}
			}
			if expected.Meta.AI == nil {
				expected.Meta.AI = map[string]string{}
			}
			if current == nil || st.after == nil || current.key(t) != expected.key(t) {
				stale++
				continue
			}
			restored := current.clone()
			restored.UpdatedAt = w.now.UTC().Format(stamp)
			if st.before == nil {
				restored.DeletedAt = restored.UpdatedAt
			} else {
				restored.Data, restored.Meta = st.before.Data, st.before.Meta
				if restored.Data == nil {
					restored.Data = map[string]any{}
				}
				if restored.Meta.AI == nil {
					restored.Meta.AI = map[string]string{}
				}
				restored.DeletedAt = ""
				if st.before.Deleted {
					restored.DeletedAt = restored.UpdatedAt
				}
				compute(t, restored, changedFields(current, restored))
				if restored.DeletedAt == "" {
					if err := w.checkRecord(t, restored, uniqueFields(t)); err != nil {
						return err
					}
				}
			}
			if err := w.save(restored, false); err != nil {
				return err
			}
			if err := w.history(sc, OpUndo, "undo "+batch, current, restored); err != nil {
				return err
			}
			if restored.DeletedAt == "" {
				rows = append(rows, restored)
			} else {
				removes = append(removes, restored.ID)
			}
		}
		if stale > 0 {
			return errorf(CodeStale, "%d of the records this changed have changed since; undoing would overwrite that", stale)
		}
		_, err = w.tx.Exec(`UPDATE batches SET undone_by = ? WHERE batch = ?`, undo, batch)
		return err
	})
	if err != nil {
		return nil, err
	}
	out := &WriteResult{Batch: undo, Rows: make([]Row, 0, len(rows)), Removes: append([]string{}, removes...)}
	for _, r := range rows {
		out.Rows = append(out.Rows, s.row(t, r, sc, false))
	}
	return out, nil
}

// uniqueFields is t's unique fields, the checks a restored record needs.
func uniqueFields(t *Type) []string {
	var out []string
	for _, name := range t.Order {
		if f := t.field(name); f != nil && f.Unique {
			out = append(out, name)
		}
	}
	if out == nil {
		return []string{}
	}
	return out
}

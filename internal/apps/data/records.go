package data

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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
	OpCreate     = "create"
	OpUpdate     = "update"
	OpDelete     = "delete"
	OpUndo       = "undo"
	OpImport     = "import"
	OpRefresh    = "refresh"
	OpTransition = "transition"
	OpChecklist  = "checklist"
	OpReverse    = "reverse"
	OpSeed       = "seed"
	OpSync       = "sync"
)

// The sources of an ai field's value, as a row's _meta.ai shows them.
const (
	SourceAI     = "ai"
	SourcePinned = "pinned"
)

// Row is one record as a caller sees it: its fields by name beside id, type,
// created_at, updated_at and _rev (its revision, which a write may name to
// refuse a stale edit), and "_meta" saying which values are masked, who set
// each value, and whether a freeze rule holds the record.
type Row map[string]any

// RowMeta is a row's "_meta".
type RowMeta struct {
	// AI is, for every ai field holding a value, whether AI filled it or a
	// person set it ("pinned").
	AI map[string]string `json:"ai,omitempty"`
	// By is the kind of writer (person, ai, flow, import) that last set or
	// cleared each field.
	By     map[string]string `json:"by,omitempty"`
	Masked []string          `json:"masked,omitempty"`
	Frozen bool              `json:"frozen,omitempty"`
	// Writable is the fields a frozen record still lets be changed.
	Writable []string `json:"writable,omitempty"`
	// Reverses and ReversedBy link a ledger entry and its reversal.
	Reverses   string `json:"reverses,omitempty"`
	ReversedBy string `json:"reversed_by,omitempty"`
}

// ID is the row's id.
func (r Row) ID() string { s, _ := r["id"].(string); return s }

// Change is one committed write: every record it touched, before and after,
// and every work item, timer, sign-off or comment it made or moved. Before is
// nil for a created record; After is nil for a deleted one. The rows are
// whole, never masked. Cursor is the change's place in the outbox, which a
// subscription resumes from.
type Change struct {
	Draft       string
	Batch       string
	App         string
	Actor       string
	ByKind      string
	Cause       string
	CauseChain  []string
	SourceEvent string
	intake      *IntakeTrigger
	Op          string
	Type        string // Empty for a batch containing several types.
	At          time.Time
	Records     []RecordChange
	Items       []ItemChange
	Cursor      int64

	// befores are the live records as they were, for subscriptions to know
	// which of them a query matched before the change.
	befores map[string]*record
	// resume is set on the one change that is a subscription resuming, which
	// the dispatcher answers in commit order.
	resume       *resumeOp
	invalidation *invalidation
}

// RecordChange is one record of a [Change], and the fields that changed.
type RecordChange struct {
	TypeID  string
	Type    string
	ID      string
	Before  Row
	After   Row
	Changed []string
}

// WriteResult is what a write answers: its batch (empty when nothing
// changed), the rows as they now are, (for an undo) the ids of the records
// it removed, and the values it left alone because a person set them.
type WriteResult struct {
	Batch   string
	Rows    []Row
	Removes []string
	Held    []Held
}

// Held is one value a write did not make because a person's value holds.
type Held struct {
	ID    string
	Field string
}

// record is one row of the records table, with its values by field name.
type record struct {
	ID        string
	Type      string
	Rev       int64
	Data      map[string]any
	Meta      recordMeta
	CreatedAt string
	UpdatedAt string
	DeletedAt string
	// unknown is stored values under ids the schema does not know, kept as
	// they are on every save.
	unknown map[string]any
}

// recordMeta is what a record keeps beside its values: who last set or
// cleared each field, and a ledger entry's links.
type recordMeta struct {
	Maker          string                             `json:"maker,omitempty"`
	LastTransition string                             `json:"last_transition,omitempty"`
	Checklists     map[string]map[string]checkReceipt `json:"checklists,omitempty"`
	Sources        map[string]ExternalKey             `json:"sources,omitempty"`
	By             map[string]string                  `json:"by,omitempty"`
	Reverses       string                             `json:"reverses,omitempty"`
	ReversedBy     string                             `json:"reversed_by,omitempty"`
}

// state is a record as a history row keeps it.
type state struct {
	Data      map[string]any `json:"data"`
	Meta      recordMeta     `json:"meta"`
	Deleted   bool           `json:"deleted,omitempty"`
	CreatedAt string         `json:"created_at,omitempty"`
	UpdatedAt string         `json:"updated_at,omitempty"`
}

func (r *record) state() state {
	return state{Data: r.Data, Meta: r.Meta, Deleted: r.DeletedAt != ""}
}

// encode turns values by field name into the stored form, by field id. A
// name the type does not know is a bug in this package and panics, which the
// writer turns into a failed write.
func (t *Type) encode(data map[string]any, unknown map[string]any) map[string]any {
	out := make(map[string]any, len(data)+len(unknown))
	for k, v := range unknown {
		out[k] = v
	}
	for name, v := range data {
		f := t.Fields[name]
		if f == nil {
			panic(fmt.Sprintf("data: %s has no field %q to store", t.Name, name))
		}
		out[f.ID] = v
	}
	return out
}

// decode turns stored values by field id into values by name, keeping what
// it does not know apart.
func (t *Type) decode(stored map[string]any) (data, unknown map[string]any) {
	data = make(map[string]any, len(stored))
	for id, v := range stored {
		if f := t.byID[id]; f != nil {
			data[f.Name] = v
			continue
		}
		if unknown == nil {
			unknown = map[string]any{}
		}
		unknown[id] = v
	}
	return data, unknown
}

// encodeMeta and decodeMeta do the same for the meta's field names.
func (t *Type) encodeMeta(m recordMeta) recordMeta {
	out := m
	out.Sources = nil
	if len(m.By) > 0 {
		out.By = map[string]string{}
		for name, kind := range m.By {
			if f := t.Fields[name]; f != nil {
				out.By[f.ID] = kind
			}
		}
	}
	for name, key := range m.Sources {
		if f := t.Fields[name]; f != nil {
			if out.Sources == nil {
				out.Sources = map[string]ExternalKey{}
			}
			out.Sources[f.ID] = key
		}
	}
	return out
}

func (m *recordMeta) copySource(name string, from recordMeta) {
	if key, ok := from.Sources[name]; ok {
		if m.Sources == nil {
			m.Sources = map[string]ExternalKey{}
		}
		m.Sources[name] = key
	} else {
		delete(m.Sources, name)
	}
}

func (t *Type) decodeMeta(m recordMeta) recordMeta {
	out := m
	out.Sources = nil
	out.By = map[string]string{}
	for id, kind := range m.By {
		if f := t.byID[id]; f != nil {
			out.By[f.Name] = kind
		}
	}
	for name, key := range m.Sources {
		if f := t.byID[name]; f != nil {
			if out.Sources == nil {
				out.Sources = map[string]ExternalKey{}
			}
			out.Sources[f.Name] = key
		}
	}
	return out
}

// encodeState is a record's state as a history row stores it, by id.
func (t *Type) encodeState(r *record) string {
	return canonicalJSON(state{
		Data: t.encode(r.Data, r.unknown), Meta: t.encodeMeta(r.Meta), Deleted: r.DeletedAt != "",
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	})
}

// decodeState reads a stored history state back by name.
func (t *Type) decodeState(raw string) *state {
	var st state
	_ = json.Unmarshal([]byte(raw), &st)
	st.Data, _ = t.decode(st.Data)
	st.Meta = t.decodeMeta(st.Meta)
	return &st
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
	c.Meta.Sources = maps.Clone(r.Meta.Sources)
	c.Meta.By = map[string]string{}
	for k, v := range r.Meta.By {
		c.Meta.By[k] = v
	}
	return &c
}

// values is what an expression reads: the fields and the row's own names.
func (r *record) values() map[string]any {
	v := make(map[string]any, len(r.Data)+4)
	for k, x := range r.Data {
		v[k] = x
	}
	v["id"], v["created_at"], v["updated_at"], v["type"] = r.ID, r.CreatedAt, r.UpdatedAt, r.Type
	return v
}

// scanRecord reads one record from a row of [recordColumns], by name.
func scanRecord(sch *schema, scan func(...any) error) (*record, error) {
	var r record
	var typeID, data, meta string
	var deleted sql.NullString
	if err := scan(&r.ID, &typeID, &r.Rev, &data, &meta, &r.CreatedAt, &r.UpdatedAt, &deleted); err != nil {
		return nil, err
	}
	r.DeletedAt = deleted.String
	var stored map[string]any
	if err := json.Unmarshal([]byte(data), &stored); err != nil {
		return nil, fmt.Errorf("record %s is unreadable: %w", r.ID, err)
	}
	var m recordMeta
	_ = json.Unmarshal([]byte(meta), &m)
	t := sch.byID[typeID]
	if t == nil {
		return nil, fmt.Errorf("record %s is of a type this store does not know", r.ID)
	}
	r.Type = t.Name
	r.Data, r.unknown = t.decode(stored)
	r.Meta = t.decodeMeta(m)
	return &r, nil
}

const recordColumns = `id, type_id, rev, data_json, meta_json, created_at, updated_at, deleted_at`

// loadRecord reads one record, deleted or not; nil when there is none.
func loadRecord(q querier, sch *schema, id string) (*record, error) {
	r, err := scanRecord(sch, q.QueryRow(`SELECT `+recordColumns+` FROM records WHERE id = ?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

// row is r as a scope sees it. A masked value reads as "••••" unless
// revealed, and never for an agent; a hidden field is left out for an app.
func (s *Store) row(t *Type, r *record, sc Scope, reveal bool) Row {
	return s.rowWithSchema(s.schema.Load(), t, r, sc, reveal)
}

func (s *Store) rowWithSchema(sch *schema, t *Type, r *record, sc Scope, reveal bool) Row {
	out := Row{"id": r.ID, "type": r.Type, "created_at": r.CreatedAt, "updated_at": r.UpdatedAt, "_rev": r.Rev}
	meta := RowMeta{Reverses: r.Meta.Reverses, ReversedBy: r.Meta.ReversedBy}
	for _, name := range t.Order {
		f := t.Fields[name]
		mode := t.FieldPolicy(f)
		if mode == "hide" {
			if _, ok := r.Data[name]; ok {
				s.AuditRecordPolicy(sc, t, "field_hidden")
			}
			continue
		}
		if f.Hidden && sc.App != "" {
			continue
		}
		v, hasValue := r.Data[name]
		if hasValue && mode == "" && (!f.Masked || reveal && !sc.isAgent()) {
			var allowed bool
			v, allowed = s.projectReference(sch, sc, f, v)
			if !allowed {
				s.AuditRecordPolicy(sc, t, "field_hidden")
				continue
			}
		}
		if by, ok := r.Meta.By[name]; ok {
			if meta.By == nil {
				meta.By = map[string]string{}
			}
			meta.By[name] = by
		}
		if !hasValue {
			continue
		}
		if f.AI != "" && r.Meta.By[name] != BySync {
			if meta.AI == nil {
				meta.AI = map[string]string{}
			}
			meta.AI[name] = aiSource(r.Meta.By[name])
		}
		if mode == "mask" || f.Masked && (!reveal || sc.isAgent()) {
			if mode == "mask" {
				s.AuditRecordPolicy(sc, t, "field_masked")
			}
			out[name] = masked
			meta.Masked = append(meta.Masked, name)
			continue
		}
		out[name] = v
	}
	if frozen, except := s.frozen(t, r); frozen {
		meta.Frozen = true
		meta.Writable = except
	}
	out["_meta"] = meta
	return out
}

// aiSource is how an ai field's value is shown: pinned when a person set it.
func aiSource(by string) string {
	if by == ByPerson || by == ByImport || by == ByFlow {
		return SourcePinned
	}
	return SourceAI
}

// whole is r as a [Change] carries it: every value, unmasked.
func whole(t *Type, r *record) Row {
	if r == nil {
		return nil
	}
	out := Row{"id": r.ID, "type": r.Type, "created_at": r.CreatedAt, "updated_at": r.UpdatedAt, "_rev": r.Rev}
	for k, v := range r.Data {
		out[k] = v
	}
	meta := RowMeta{Reverses: r.Meta.Reverses, ReversedBy: r.Meta.ReversedBy}
	if len(r.Meta.By) > 0 {
		meta.By = map[string]string{}
		for k, v := range r.Meta.By {
			meta.By[k] = v
			if f := t.Fields[k]; f != nil && f.AI != "" && v != BySync {
				if _, has := r.Data[k]; has {
					if meta.AI == nil {
						meta.AI = map[string]string{}
					}
					meta.AI[k] = aiSource(v)
				}
			}
		}
	}
	out["_meta"] = meta
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
func (w *writeTx) newBatch(sc Scope, op string, t *Type) (string, error) {
	id := w.s.ids.next(w.now)
	kind := sc.kind(op)
	actor := w.schema.actor(sc)
	typ, typeID := "", ""
	if t != nil {
		typ, typeID = t.Name, t.ID
	}
	if _, err := w.tx.Exec(`INSERT INTO batches(batch, app, actor, by_kind, cause, op, type_id, at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		id, sc.App, actor, kind, sc.Cause, op, typeID, w.now.UTC().Format(stamp)); err != nil {
		return "", err
	}
	w.change = &Change{Batch: id, App: sc.App, Actor: actor, ByKind: kind, Cause: sc.Cause, CauseChain: sc.CauseChain, SourceEvent: sc.sourceEvent, intake: sc.intake, Op: op, Type: typ, At: w.now}
	return id, nil
}

// history writes one history row, its outbox event, and adds the record to
// the change.
func (w *writeTx) history(t *Type, sc Scope, op, note string, before, after *record) error {
	var b, a any
	var id string
	if before != nil {
		b = t.encodeState(before)
		id = before.ID
	}
	if after != nil {
		a = t.encodeState(after)
		id = after.ID
	}
	cause := sc.Cause
	if cause == "" && op == OpUndo {
		cause = note
	}
	if _, err := w.tx.Exec(`
		INSERT INTO history(batch, type_id, record, app, actor, by_kind, cause, op, note, before_json, after_json, at)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.change.Batch, t.ID, id, sc.App, w.schema.actor(sc), sc.kind(op), cause, op, note, b, a, w.now.UTC().Format(stamp)); err != nil {
		return err
	}
	return w.note(t, before, after)
}

// note adds one record to the change w is making, and writes the outbox
// event that tells the engine of it in the same transaction.
func (w *writeTx) note(t *Type, before, after *record) error {
	id := ""
	if before != nil {
		id = before.ID
	}
	if after != nil {
		id = after.ID
	}
	rc := RecordChange{TypeID: t.ID, Type: t.Name, ID: id, Before: whole(t, before), After: whole(t, after), Changed: changedFields(before, after)}
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
	if w.change.ByKind == BySync || w.change.Op == OpRefresh {
		return w.emit(outboxRow{event: EventFreshnessChanged, typeID: t.ID, record: id})
	}
	event := EventRecordUpdated
	switch {
	case rc.Before == nil:
		event = EventRecordCreated
	case rc.After == nil:
		event = EventRecordDeleted
	}
	payload := map[string]string{"op": w.change.Op}
	if event == EventRecordCreated && w.change.intake != nil {
		payload["channel"], payload["run"], payload["record_type"] = w.change.intake.Channel, w.change.intake.Run, w.change.intake.Type
	}
	return w.emit(outboxRow{event: event, typeID: t.ID, record: id, payload: payload})
}

// nonNil is a list that encodes as [] when empty.
func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
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

// save enforces rules shared by all record changes before it writes the row.
// The rule outcomes commit in the same transaction as the record.
func (w *writeTx) save(t *Type, r *record, insert bool, sc Scope) error {
	if t.rules.invalid != nil {
		return t.rules.invalid
	}
	var before *record
	if t.HasRowPolicy() && !sc.sync && !sc.policyMaintenance {
		if !insert {
			var err error
			before, err = loadRecord(w.tx, w.schema, r.ID)
			if err != nil {
				return err
			}
			if before != nil && !w.s.LiveRowAllowed(sc, t, before.Data) {
				return errorf(CodeNotFound, "there is no %s record %s", t.Name, r.ID)
			}
		}
		if (sc.below() || sc.Flow) && !w.s.LiveRowAllowed(sc, t, r.Data) {
			return errorf(CodeNotAllowed, "the record does not match the person's row policy")
		}
	}
	if insert {
		// Agent and flow writes in Personal are on behalf of the space's person.
		r.Meta.Maker = w.schema.person
	} else if before == nil && (len(t.rules.bookings) != 0 || len(t.rules.approvals) != 0) {
		var err error
		before, err = loadRecord(w.tx, w.schema, r.ID)
		if err != nil {
			return err
		}
	}
	if err := w.recordApprovalRoute(sc, t, before, r); err != nil {
		return err
	}
	if err := w.checkBookings(t, before, r, sc); err != nil {
		return err
	}
	data, meta := canonicalJSON(t.encode(r.Data, r.unknown)), canonicalJSON(t.encodeMeta(r.Meta))
	var deleted any
	if r.DeletedAt != "" {
		deleted = r.DeletedAt
	}
	if insert {
		_, err := w.tx.Exec(`INSERT INTO records(id, type_id, rev, data_json, meta_json, created_at, updated_at, deleted_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
			r.ID, t.ID, r.Rev, data, meta, r.CreatedAt, r.UpdatedAt, deleted)
		return err
	}
	_, err := w.tx.Exec(`UPDATE records SET rev = ?, data_json = ?, meta_json = ?, updated_at = ?, deleted_at = ? WHERE id = ?`,
		r.Rev, data, meta, r.UpdatedAt, deleted, r.ID)
	return err
}

// applySet writes a set of values into r for scope sc, and answers the
// fields it left alone. PERSON WINS BY PROVENANCE: a value a person set or
// cleared (or brought in by import) holds against an AI write until the person
// resets it; every value written records the kind of writer that wrote it.
func applySet(r *record, set map[string]any, sc Scope, op string) (held []string) {
	kind := sc.kind(op)
	for _, name := range sortedKeys(set) {
		if sc.below() || sc.sync {
			if by := r.Meta.By[name]; by == ByPerson || by == ByImport {
				held = append(held, name)
				continue
			}
		}
		if v := set[name]; v == nil {
			delete(r.Data, name)
		} else {
			r.Data[name] = v
		}
		r.Meta.By[name] = kind
		delete(r.Meta.Sources, name)
		if sc.sourceKey != (ExternalKey{}) {
			if r.Meta.Sources == nil {
				r.Meta.Sources = map[string]ExternalKey{}
			}
			r.Meta.Sources[name] = sc.sourceKey
		}
	}
	return held
}

// prepared are the choices a write's values are checked with: lenient reads
// loose spellings (an import's "yes"); state lets a lifecycle's state be
// set (an import or a seeded sample bringing records as they already are);
// numbers lets a sequence's field be set (an import keeping its numbers).
type prepared struct {
	lenient bool
	state   bool
	numbers bool
}

// prepareSet checks and coerces a set of values against t: every name a
// visible field that is not computed and not owned by a lifecycle or a
// sequence, every value one of its field's. Referenced records must exist.
func (w *writeTx) prepareSet(t *Type, set map[string]any, opt prepared) (map[string]any, error) {
	out := make(map[string]any, len(set))
	for _, key := range sortedKeys(set) {
		f := t.field(key)
		if f == nil {
			if reserved[key] || strings.HasPrefix(key, "_") {
				continue
			}
			return nil, fieldErr(key, "%s records have no field %q", t.Name, key)
		}
		name := f.Name
		if f.Computed != "" {
			return nil, fieldErr(name, "%s is computed; it cannot be written", name)
		}
		if lc := t.Lifecycle; lc != nil && lc.Field == name && !opt.state {
			return nil, &Error{Code: CodeLifecycle, Field: name, Message: fmt.Sprintf("%s moves only by a transition: %s", name, lc.names())}
		}
		if f.Sequence != nil && !opt.numbers {
			return nil, fieldErr(name, "%s is numbered by the store; it cannot be written", name)
		}
		// A form that saves the row it was shown sends "••••" back for a
		// masked value. That is the mask, not a new value: writing it would
		// destroy the real one.
		if f.Masked && set[key] == masked {
			continue
		}
		v, err := f.coerce(set[key], coercion{now: w.now, lenient: opt.lenient, me: w.schema.person})
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
	target := w.schema.typeNamed(f.To)
	if target == nil {
		return fieldErr(f.Name, "%s points to %s records, and there is no such type", f.Name, f.To)
	}
	for _, id := range ids {
		var n int
		if err := w.tx.QueryRow(`SELECT count(*) FROM records WHERE id = `+identityRefSQL(target, "?")+` AND type_id = ? AND deleted_at IS NULL`, id, target.ID).Scan(&n); err != nil {
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
func compute(t *Type, r *record, changed []string, me string) {
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
				coerced, err := f.coerce(out, coercion{now: time.Now(), lenient: true, me: me})
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
		if f.unique() && has {
			taken, err := w.taken(t, f, r.ID, v)
			if err != nil {
				return err
			}
			if taken {
				return &Error{Code: CodeDuplicate, Field: name, Message: fmt.Sprintf("another %s already has this %s", t.Name, name)}
			}
		}
	}
	return nil
}

// taken says whether a live record of t other than id holds v in f. A text
// value is compared without regard to case: "ACME" and "acme" are one key.
func (w *writeTx) taken(t *Type, f *Field, id string, v any) (bool, error) {
	col := jsonPath("data_json", f.ID)
	cmp := col + " = ?"
	if _, text := v.(string); text {
		cmp = col + " = ? COLLATE NOCASE"
	}
	var other string
	err := w.tx.QueryRow(`SELECT id FROM records WHERE type_id = '`+mustID(t.ID)+`' AND deleted_at IS NULL AND id != ? AND `+cmp+` LIMIT 1`,
		id, sqlValue(v)).Scan(&other)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
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

// Create makes one record per set, in one batch. A lifecycle's records start
// in its first state, and a sequence's field is numbered by the store.
func (s *Store) Create(sc Scope, typ string, sets []map[string]any) (*WriteResult, error) {
	if len(sets) == 0 {
		return nil, errorf(CodeInvalid, "nothing to create")
	}
	if len(sets) > IDsMax {
		return nil, errorf(CodeInvalid, "one call creates at most %d records", IDsMax)
	}
	return s.create(sc, typ, sets, OpCreate)
}

func (s *Store) create(sc Scope, typ string, sets []map[string]any, op string) (*WriteResult, error) {
	sc, flushAudit := s.WithPolicyAudit(sc)
	defer flushAudit()
	var made []*record
	var t *Type
	var batch string
	err := s.writeAs(sc, func(w *writeTx) error {
		made = nil
		var err error
		if t, err = w.schema.allow(sc, typ, true); err != nil {
			return err
		}
		if batch, err = w.newBatch(sc, op, t); err != nil {
			return err
		}
		for _, set := range sets {
			r, err := w.createOne(t, sc, set, op)
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
	out := &WriteResult{Batch: batch, Rows: make([]Row, 0, len(made)), Held: []Held{}}
	for _, r := range made {
		if s.LiveRowAllowed(sc, t, r.Data) {
			out.Rows = append(out.Rows, s.row(t, r, sc, false))
		}
	}
	return out, nil
}

// createOne makes one record inside w.
func (w *writeTx) createOne(t *Type, sc Scope, set map[string]any, op string) (*record, error) {
	return w.createOneAs(t, sc, set, op, "")
}

// createOneAs makes one record with the id given (a seeded sample row that
// other rows point to), or a new one. An import or a seed may bring a
// record's state and number as they already are; a create may name only the
// state a lifecycle starts in.
func (w *writeTx) createOneAs(t *Type, sc Scope, set map[string]any, op, id string) (*record, error) {
	carry := op == OpSeed || (op == OpImport && sc.intake == nil)
	opt := prepared{lenient: op == OpImport || op == OpSeed, state: sc.intake == nil, numbers: carry}
	values, err := w.prepareSet(t, set, opt)
	if err != nil {
		return nil, err
	}
	defaultState := ""
	if lc := t.Lifecycle; lc != nil {
		v, given := values[lc.Field]
		if given && !carry && v != nil && v != lc.initial(t) {
			return nil, &Error{Code: CodeLifecycle, Field: lc.Field, Message: fmt.Sprintf("a new %s starts as %s; it moves on by a transition", t.Name, lc.initial(t))}
		}
		if v == nil {
			values[lc.Field] = lc.initial(t)
		}
		if !given {
			defaultState = lc.Field
		}
	}
	if id == "" {
		id = w.s.ids.next(w.now)
	}
	at := w.now.UTC().Format(stamp)
	r := &record{ID: id, Type: t.Name, Rev: 1, Data: map[string]any{}, Meta: recordMeta{By: map[string]string{}}, CreatedAt: at, UpdatedAt: at}
	for _, name := range t.Order {
		f := t.field(name)
		if f == nil || f.Default == nil {
			continue
		}
		if _, given := values[name]; given {
			continue
		}
		v, err := f.coerce(f.Default, coercion{now: w.now, me: w.schema.person})
		if err != nil {
			continue
		}
		r.Data[name] = v
	}
	applySet(r, values, sc, op)
	// Assigning the lifecycle's initial state is a schema default.
	delete(r.Meta.By, defaultState)
	for _, name := range t.Order {
		f := t.field(name)
		if f == nil || f.Sequence == nil || r.Data[name] != nil {
			continue
		}
		n, err := w.nextNumber(t, f, id)
		if err != nil {
			return nil, err
		}
		r.Data[name] = n
	}
	compute(t, r, sortedKeys(r.Data), w.schema.person)
	if err := w.checkRecord(t, r, nil); err != nil {
		return nil, err
	}
	if err := w.save(t, r, true, sc); err != nil {
		return nil, err
	}
	if err := w.history(t, sc, op, "", nil, r); err != nil {
		return nil, err
	}
	return r, w.assign(sc, t, r, "")
}

// nextNumber takes a sequence's next number, formatted, skipping any number
// a record already holds (one imported with its own). The counter is a row
// in the writer's transaction, so two creates never take one number, and a
// number given out is never given again even if its record is undone.
func (w *writeTx) nextNumber(t *Type, f *Field, id string) (string, error) {
	for range 10_000 {
		var n int64
		if err := w.tx.QueryRow(`INSERT INTO sequences(field_id, next) VALUES(?, 2)
			ON CONFLICT(field_id) DO UPDATE SET next = next + 1 RETURNING next - 1`, f.ID).Scan(&n); err != nil {
			return "", err
		}
		v := fmt.Sprintf("%s%0*d", f.Sequence.Prefix, f.Sequence.digits(), n)
		taken, err := w.taken(t, f, id, v)
		if err != nil {
			return "", err
		}
		if !taken {
			return v, nil
		}
	}
	return "", fieldErr(f.Name, "%s ran out of free numbers", f.Name)
}

// UpdateOptions are an update's or delete's choices. Unfreeze is the reason
// a person gives for writing through a freeze rule; it is kept on the
// history row, and an agent or run cannot give one. Rev, when set, is the
// revision the caller saw; a record that has moved past it is refused as
// stale rather than overwritten. Reset hands fields a person set back to AI:
// their values stay until an AI write replaces them.
type UpdateOptions struct {
	Unfreeze string
	Rev      int64
	Reset    []string
}

// Update writes set into every record named, in one batch. A record that
// would not change is left alone and writes no history.
func (s *Store) Update(sc Scope, typ string, ids []string, set map[string]any, opts UpdateOptions) (*WriteResult, error) {
	sc, flushAudit := s.WithPolicyAudit(sc)
	defer flushAudit()
	ids, err := cleanIDs(ids)
	if err != nil {
		return nil, err
	}
	if len(set) == 0 && len(opts.Reset) == 0 {
		return nil, errorf(CodeInvalid, "nothing to set")
	}
	if opts.Rev != 0 && len(ids) != 1 {
		return nil, errorf(CodeInvalid, "a revision names one record")
	}
	if len(opts.Reset) > 0 && sc.below() {
		return nil, &Error{Code: CodeNotAllowed, Need: "reset", Message: "only the person hands a value back to AI"}
	}
	var t *Type
	var rows []*record
	var held []Held
	var batch string
	err = s.writeAs(sc, func(w *writeTx) error {
		rows, held, batch = nil, nil, ""
		var err error
		if t, err = w.schema.allow(sc, typ, true); err != nil {
			return err
		}
		if t.Ledger {
			return &Error{Code: CodeLedger, Message: fmt.Sprintf("%s entries are never changed; reverse the entry instead", t.Name)}
		}
		values, err := w.prepareSet(t, set, prepared{})
		if err != nil {
			return err
		}
		var reset []string
		for _, name := range opts.Reset {
			f := t.field(name)
			if f == nil {
				return fieldErr(name, "%s records have no field %q", t.Name, name)
			}
			reset = append(reset, f.Name)
		}
		if len(values) == 0 && len(reset) == 0 {
			return errorf(CodeInvalid, "nothing to set")
		}
		for _, id := range ids {
			before, err := w.liveFor(sc, t, id)
			if err != nil {
				return err
			}
			if opts.Rev != 0 && before.Rev != opts.Rev {
				return &Error{Code: CodeStale, Message: fmt.Sprintf("this %s has changed since revision %d; it is at %d", t.Name, opts.Rev, before.Rev)}
			}
			if err := w.s.checkFrozen(t, before, sortedKeys(values), sc, opts.Unfreeze); err != nil {
				return err
			}
			after := before.clone()
			for _, name := range reset {
				delete(after.Meta.By, name)
			}
			for _, name := range applySet(after, values, sc, OpUpdate) {
				held = append(held, Held{ID: id, Field: name})
			}
			changed := changedFields(before, after)
			compute(t, after, changed, w.schema.person)
			if after.key(t) == before.key(t) {
				rows = append(rows, before)
				continue
			}
			if err := w.checkRecord(t, after, sortedKeys(values)); err != nil {
				return err
			}
			if w.change == nil {
				if batch, err = w.newBatch(sc, OpUpdate, t); err != nil {
					return err
				}
			}
			after.UpdatedAt = w.now.UTC().Format(stamp)
			after.Rev++
			if err := w.save(t, after, false, sc); err != nil {
				return err
			}
			if err := w.history(t, sc, OpUpdate, unfreezeNote(opts.Unfreeze, w.s, t, before), before, after); err != nil {
				return err
			}
			rows = append(rows, after)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := &WriteResult{Batch: batch, Rows: make([]Row, 0, len(rows)), Held: append([]Held{}, held...)}
	for _, r := range rows {
		if s.LiveRowAllowed(sc, t, r.Data) {
			out.Rows = append(out.Rows, s.row(t, r, sc, false))
		}
	}
	return out, nil
}

// Delete deletes every record named, in one batch. A deleted record is kept
// (deleted_at is set) so its batch can be undone. A ledger entry is never
// deleted.
func (s *Store) Delete(sc Scope, typ string, ids []string, opts UpdateOptions) (*WriteResult, error) {
	ids, err := cleanIDs(ids)
	if err != nil {
		return nil, err
	}
	var batch string
	err = s.writeAs(sc, func(w *writeTx) error {
		t, err := w.schema.allow(sc, typ, true)
		if err != nil {
			return err
		}
		if t.Ledger {
			return &Error{Code: CodeLedger, Message: fmt.Sprintf("%s entries are never deleted; reverse the entry instead", t.Name)}
		}
		if batch, err = w.newBatch(sc, OpDelete, t); err != nil {
			return err
		}
		for _, id := range ids {
			before, err := w.liveFor(sc, t, id)
			if err != nil {
				return err
			}
			if opts.Rev != 0 && before.Rev != opts.Rev {
				return &Error{Code: CodeStale, Message: fmt.Sprintf("this %s has changed since revision %d; it is at %d", t.Name, opts.Rev, before.Rev)}
			}
			if err := w.s.checkFrozen(t, before, nil, sc, opts.Unfreeze); err != nil {
				return err
			}
			after := before.clone()
			after.UpdatedAt = w.now.UTC().Format(stamp)
			after.DeletedAt = after.UpdatedAt
			after.Rev++
			if err := w.save(t, after, false, sc); err != nil {
				return err
			}
			if err := w.history(t, sc, OpDelete, unfreezeNote(opts.Unfreeze, w.s, t, before), before, after); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &WriteResult{Batch: batch, Rows: []Row{}, Held: []Held{}}, nil
}

// unfreezeNote is the history note of a write made through a freeze rule.
func unfreezeNote(reason string, s *Store, t *Type, before *record) string {
	if frozen, _ := s.frozen(t, before); frozen && reason != "" {
		return "unfrozen: " + reason
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
	if strings.TrimSpace(reason) != "" && !sc.isAgent() && !sc.AI && !sc.Flow {
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
	r, err := loadRecord(w.tx, w.schema, id)
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
// away the later change. A ledger's batches are never undone; an entry is
// reversed instead.
func (s *Store) Undo(sc Scope, batch string) (*WriteResult, error) {
	sc, flushAudit := s.WithPolicyAudit(sc)
	defer flushAudit()
	batch = strings.TrimSpace(batch)
	if !validID(batch) {
		return nil, errorf(CodeInvalid, "%q is not a batch", batch)
	}
	var t *Type
	var rows []*record
	var removes []string
	var undo string
	err := s.writeAs(sc, func(w *writeTx) error {
		rows, removes, undo = nil, nil, ""
		var typeID, byKind, op string
		var undone sql.NullString
		err := w.tx.QueryRow(`SELECT type_id, undone_by, by_kind, op FROM batches WHERE batch = ?`, batch).Scan(&typeID, &undone, &byKind, &op)
		if errors.Is(err, sql.ErrNoRows) {
			return errorf(CodeNotFound, "there is no batch %s", batch)
		}
		if err != nil {
			return err
		}
		if undone.Valid {
			return errorf(CodeStale, "that change was already undone")
		}
		if op == OpIdentityMerge || op == OpIdentitySplit {
			return errorf(CodeNotAllowed, "use the identity commands to reverse a merge")
		}
		if op == OpTransition && typeID == "" {
			return errorf(CodeLifecycle, "undoing this would move lifecycle states; fire a transition back instead")
		}
		known := w.schema.byID[typeID]
		if known == nil {
			return errorf(CodeNotFound, "there is no batch %s", batch)
		}
		if t, err = w.schema.allow(sc, known.Name, true); err != nil {
			return err
		}
		if t.Ledger {
			return &Error{Code: CodeLedger, Message: fmt.Sprintf("%s entries are never undone; reverse the entry instead", t.Name)}
		}
		// AN AI DOES NOT UNDO WHAT A PERSON DID: that would be an AI write
		// over a person's values by another door.
		if sc.below() && (byKind == ByPerson || byKind == ByImport) {
			return &Error{Code: CodeNotAllowed, Need: "undo." + byKind, Message: "an agent cannot undo a change a person made"}
		}
		type step struct {
			id            string
			before, after *state
		}
		var steps []step
		err = eachRow(w.tx, `SELECT record, before_json, after_json FROM history WHERE batch = ? ORDER BY seq`, []any{batch}, func(scan func(...any) error) error {
			var id string
			var b, a sql.NullString
			if err := scan(&id, &b, &a); err != nil {
				return err
			}
			st := step{id: id}
			if b.Valid {
				st.before = t.decodeState(b.String)
			}
			if a.Valid {
				st.after = t.decodeState(a.String)
			}
			steps = append(steps, st)
			return nil
		})
		if err != nil {
			return err
		}
		if len(steps) == 0 {
			return errorf(CodeStale, "that change touched no records")
		}
		if undo, err = w.newBatch(sc, OpUndo, t); err != nil {
			return err
		}
		if _, err := w.tx.Exec(`UPDATE batches SET undoes = ? WHERE batch = ?`, batch, undo); err != nil {
			return err
		}
		stale := 0
		for _, st := range steps {
			current, err := loadRecord(w.tx, w.schema, st.id)
			if err != nil {
				return err
			}
			expected := &record{Type: t.Name, Data: map[string]any{}, Meta: recordMeta{By: map[string]string{}}}
			if st.after != nil {
				expected.Data, expected.Meta = st.after.Data, st.after.Meta
				if st.after.Deleted {
					expected.DeletedAt = "deleted"
				}
			}
			if current == nil || st.after == nil || current.key(t) != expected.key(t) {
				stale++
				continue
			}
			// UNDO DOES NOT MOVE A LIFECYCLE: a state changes only by a
			// transition, with its guard, its writers and its effects, so a
			// change that moved one is walked back by another transition.
			if lc := t.Lifecycle; lc != nil && st.before != nil && !st.before.Deleted && !st.after.Deleted {
				if fmt.Sprint(st.before.Data[lc.Field]) != fmt.Sprint(st.after.Data[lc.Field]) {
					return &Error{Code: CodeLifecycle, Field: lc.Field, Message: fmt.Sprintf("undoing this would move %s without a transition; fire a transition back instead", lc.Field)}
				}
			}
			// UNDO DOES NOT WRITE THROUGH A FREEZE someone else's change made.
			// Undoing the very batch that froze the record ("Mark won", then
			// Undo) is allowed: before it, the record was not frozen. A record
			// that was already frozen before the batch, or one the batch
			// created and is frozen now, stays as it is.
			if current.DeletedAt != "" && st.before != nil {
				prior := &record{ID: current.ID, Type: t.Name, Data: st.before.Data, Meta: st.before.Meta}
				if frozen, _ := w.s.frozen(t, prior); frozen {
					return &Error{Code: CodeFrozen, Message: fmt.Sprintf("a %s this change deleted was frozen; undoing would bring back a frozen record", t.Name)}
				}
			}
			if frozen, _ := w.s.frozen(t, current); frozen {
				wasFrozen := st.before == nil
				if st.before != nil {
					prior := &record{ID: current.ID, Type: t.Name, Data: st.before.Data, Meta: st.before.Meta, CreatedAt: current.CreatedAt, UpdatedAt: current.UpdatedAt}
					wasFrozen, _ = w.s.frozen(t, prior)
				}
				if wasFrozen {
					return &Error{Code: CodeFrozen, Message: fmt.Sprintf("a %s this change touched is frozen; undoing it would change a frozen record", t.Name)}
				}
			}
			restored := current.clone()
			restored.UpdatedAt = w.now.UTC().Format(stamp)
			restored.Rev++
			if st.before == nil {
				restored.DeletedAt = restored.UpdatedAt
			} else {
				restored.Data, restored.Meta = st.before.Data, st.before.Meta
				restored.DeletedAt = ""
				if st.before.Deleted {
					restored.DeletedAt = restored.UpdatedAt
				}
				compute(t, restored, changedFields(current, restored), w.schema.person)
				if restored.DeletedAt == "" {
					if err := w.checkRecord(t, restored, uniqueFields(t)); err != nil {
						return err
					}
				}
			}
			if err := w.save(t, restored, false, sc); err != nil {
				return err
			}
			if err := w.history(t, sc, OpUndo, "undo "+batch, current, restored); err != nil {
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
	out := &WriteResult{Batch: undo, Rows: make([]Row, 0, len(rows)), Removes: append([]string{}, removes...), Held: []Held{}}
	for _, r := range rows {
		if s.LiveRowAllowed(sc, t, r.Data) {
			out.Rows = append(out.Rows, s.row(t, r, sc, false))
		}
	}
	return out, nil
}

// uniqueFields is t's unique fields, the checks a restored record needs.
func uniqueFields(t *Type) []string {
	out := []string{}
	for _, name := range t.Order {
		if f := t.field(name); f != nil && f.unique() {
			out = append(out, name)
		}
	}
	return out
}

package data

import (
	"database/sql"
)

// Get answers one live record. Reveal shows its masked values, and writes one
// reveal line per masked value shown (who, record, field, when); an agent
// may not reveal.
func (s *Store) Get(sc Scope, typ, id string, reveal bool) (Row, error) {
	sch := s.schema.Load()
	t, err := sch.allow(sc, typ, false)
	if err != nil {
		return nil, err
	}
	if !validID(id) {
		return nil, errorf(CodeInvalid, "%q is not a record id", id)
	}
	if reveal && sc.Agent {
		return nil, &Error{Code: CodeNotAllowed, Need: "reveal", Message: "an agent cannot reveal a masked value"}
	}
	r, err := loadRecord(s.rdb, sch, id)
	if err != nil {
		return nil, err
	}
	if r == nil || r.Type != t.Name || r.DeletedAt != "" {
		return nil, errorf(CodeNotFound, "there is no %s record %s", t.Name, id)
	}
	if reveal {
		var shown []string
		for _, name := range t.Order {
			if f := t.Fields[name]; f.Masked && (!f.Hidden || sc.App == "") {
				if _, ok := r.Data[name]; ok {
					shown = append(shown, name)
				}
			}
		}
		if len(shown) > 0 {
			err := s.write(func(w *writeTx) error {
				for _, field := range shown {
					if _, err := w.tx.Exec(`INSERT INTO reveals(type_id, record, field_id, app, actor, at) VALUES(?, ?, ?, ?, ?, ?)`,
						t.ID, id, t.Fields[field].ID, sc.App, w.schema.actor(sc), w.now.UTC().Format(stamp)); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
	}
	return s.row(t, r, sc, reveal), nil
}

// Entry is one history row as data.history answers it: what was done, by
// whom and when, the values before and after (masked values masked), and the
// fields that changed.
type Entry struct {
	Seq     int64
	Batch   string
	Op      string
	App     string
	Actor   string
	ByKind  string
	Cause   string
	Note    string
	At      string
	Undone  bool
	Before  map[string]any
	After   map[string]any
	Changed []string
}

// History answers a record's history, newest first, at most [HistoryMax]
// entries. A deleted record still has its history.
func (s *Store) History(sc Scope, typ, id string) ([]Entry, error) {
	sch := s.schema.Load()
	t, err := sch.allow(sc, typ, false)
	if err != nil {
		return nil, err
	}
	if !validID(id) {
		return nil, errorf(CodeInvalid, "%q is not a record id", id)
	}
	rows, err := s.rdb.Query(`
		SELECT h.seq, h.batch, h.op, h.app, h.actor, h.by_kind, h.cause, h.note, h.at, h.before_json, h.after_json, b.undone_by IS NOT NULL
		FROM history h LEFT JOIN batches b ON b.batch = h.batch
		WHERE h.record = ? AND h.type_id = ? ORDER BY h.seq DESC LIMIT ?`, id, t.ID, HistoryMax)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		var before, after sql.NullString
		if err := rows.Scan(&e.Seq, &e.Batch, &e.Op, &e.App, &e.Actor, &e.ByKind, &e.Cause, &e.Note, &e.At, &before, &after, &e.Undone); err != nil {
			return nil, err
		}
		var b, a *record
		if before.Valid {
			b = recordFromState(t, id, before.String)
			e.Before = s.historyValues(t, b, sc)
		}
		if after.Valid {
			a = recordFromState(t, id, after.String)
			e.After = s.historyValues(t, a, sc)
		}
		e.Changed = changedFields(b, a)
		out = append(out, e)
	}
	return out, rows.Err()
}

// recordFromState reads a history row's state as a record.
func recordFromState(t *Type, id, raw string) *record {
	st := t.decodeState(raw)
	r := &record{ID: id, Type: t.Name, Data: st.Data, Meta: st.Meta}
	if st.Deleted {
		r.DeletedAt = "deleted"
	}
	return r
}

// historyValues is a history state's values as a scope may see them; a
// deleted state has none.
func (s *Store) historyValues(t *Type, r *record, sc Scope) map[string]any {
	if r.DeletedAt != "" {
		return nil
	}
	out := map[string]any{}
	for k, v := range r.Data {
		f := t.Fields[k]
		if f == nil || f.Hidden && sc.App != "" {
			continue
		}
		if f.Masked {
			v = masked
		}
		out[k] = v
	}
	return out
}

// refreshChunk is how many records one refresh write recomputes, so a large
// type never holds the writer for long.
const refreshChunk = 500

// Refresh recomputes every computed field that reads the clock
// (days_since(created_at) goes stale with time alone). It writes only the
// records whose values moved, keeps their updated_at, writes no history (no
// one changed anything), and tells subscribers and hooks with op "refresh".
func (s *Store) Refresh() error {
	sch := s.schema.Load()
	for _, t := range sch.sortedTypes() {
		if !t.clocked {
			continue
		}
		after := ""
		for {
			rows, err := s.rdb.Query(`SELECT id FROM records WHERE type_id = ? AND deleted_at IS NULL AND id > ? ORDER BY id LIMIT ?`, t.ID, after, refreshChunk)
			if err != nil {
				return err
			}
			var ids []string
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				ids = append(ids, id)
			}
			rows.Close()
			if len(ids) == 0 {
				break
			}
			after = ids[len(ids)-1]
			err = s.write(func(w *writeTx) error {
				t := w.schema.types[t.Name]
				if t == nil {
					return nil
				}
				for _, id := range ids {
					before, err := loadRecord(w.tx, w.schema, id)
					if err != nil {
						return err
					}
					if before == nil || before.DeletedAt != "" {
						continue
					}
					next := before.clone()
					compute(t, next, nil, w.schema.person)
					if canonicalJSON(next.Data) == canonicalJSON(before.Data) {
						continue
					}
					if err := w.save(t, next, false); err != nil {
						return err
					}
					if w.change == nil {
						w.change = &Change{App: "", Actor: "codeaf", ByKind: ByFlow, Cause: "clock", Op: OpRefresh, Type: t.Name, At: w.now}
					}
					if err := w.note(t, before, next); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

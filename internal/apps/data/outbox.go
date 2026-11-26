package data

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// THE OUTBOX IS WRITTEN IN THE SAME TRANSACTION AS THE CHANGE IT TELLS OF. A
// record committed, a transition fired, a timer come due: each writes one
// outbox row beside its own rows, so the engine (runs, triggers, effects)
// can never miss a change that committed or act on one that did not. A
// consumer reads the outbox in sequence order. A consumer that creates a
// durable run records its receipt with the run checkpoint, because an
// independently committed [Store.Claim] leaves a crash gap before the work.
//
// The outbox is also the store's change log: its sequence is the cursor a
// live query resumes from (subs.go).

// The events the outbox carries.
const (
	EventRecordCreated    = "record.created"
	EventRecordUpdated    = "record.updated"
	EventRecordDeleted    = "record.deleted"
	EventTransition       = "transition.fired"
	EventWorkItemCreated  = "work_item.created"
	EventWorkItemUpdated  = "work_item.updated"
	EventTimerCreated     = "timer.created"
	EventTimerFired       = "timer.fired"
	EventTimerCancelled   = "timer.cancelled"
	EventSignoff          = "signoff.created"
	EventComment          = "comment.created"
	EventPolicyChanged    = "policy.changed"
	EventMappingChanged   = "mapping.changed"
	EventFreshnessChanged = "freshness.changed"
)

const outboxVersion = 2

// OutboxMax is the most events one read of the outbox answers.
const OutboxMax = 1000

// Event is one outbox row: what happened, to which record, in which batch,
// by what kind of writer and why, with the event's own payload.
type Event struct {
	Seq         int64
	Event       string
	Type        string
	TypeID      string
	Version     int
	CauseChain  []string
	SourceEvent string
	Record      string
	Batch       string
	App         string
	ByKind      string
	Cause       string
	Payload     json.RawMessage
	At          string
}

// outboxRow is one event a write is about to commit.
type outboxRow struct {
	event   string
	typeID  string
	record  string
	payload any
	binding string
}

// emit writes one outbox row in w's transaction and moves the change's
// cursor to it.
func (w *writeTx) emit(o outboxRow) error {
	var batch, app, kind, cause, sourceEvent string
	chain := []string{}
	if c := w.change; c != nil {
		batch, app, kind, cause, sourceEvent = c.Batch, c.App, c.ByKind, c.Cause, c.SourceEvent
		if len(c.CauseChain) > 0 {
			chain = c.CauseChain
		}
	}
	if kind == "" {
		kind = ByFlow
	}
	res, err := w.tx.Exec(`INSERT INTO outbox(event, type_id, record, batch, app, by_kind, cause, payload_json, at, version, cause_chain, source_event, binding) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		o.event, o.typeID, o.record, batch, app, kind, cause, canonicalJSON(o.payload), w.now.UTC().Format(stamp), outboxVersion, canonicalJSON(chain), sourceEvent, o.binding)
	if err != nil {
		return err
	}
	seq, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if w.change != nil {
		w.change.Cursor = seq
	}
	w.cursor = seq
	return nil
}

// Outbox answers the events after seq, in order, at most limit (at most
// [OutboxMax]).
func (s *Store) Outbox(after int64, limit int) ([]Event, error) {
	if limit <= 0 || limit > OutboxMax {
		limit = OutboxMax
	}
	sch := s.schema.Load()
	out := []Event{}
	err := eachRow(s.rdb, `SELECT seq, event, type_id, record, batch, app, by_kind, cause, payload_json, at, version, cause_chain, source_event FROM outbox WHERE seq > ? ORDER BY seq LIMIT ?`,
		[]any{after, limit}, func(scan func(...any) error) error {
			var e Event
			var payload, chain string
			if err := scan(&e.Seq, &e.Event, &e.TypeID, &e.Record, &e.Batch, &e.App, &e.ByKind, &e.Cause, &payload, &e.At, &e.Version, &chain, &e.SourceEvent); err != nil {
				return err
			}
			if t := sch.byID[e.TypeID]; t != nil {
				e.Type = t.Name
			}
			if err := json.Unmarshal([]byte(chain), &e.CauseChain); err != nil {
				return err
			}
			e.Payload = json.RawMessage(payload)
			out = append(out, e)
			return nil
		})
	return out, err
}

// Claim takes one event for one trigger, once: true the first time, false
// for every later claim of the same pair. This receipt commits independently
// and is suitable only when the caller does not need atomic run creation.
func (s *Store) Claim(seq int64, trigger string) (bool, error) {
	if seq <= 0 || trigger == "" || len(trigger) > 200 {
		return false, errorf(CodeInvalid, "a claim names an event and a trigger")
	}
	claimed := false
	err := s.write(func(w *writeTx) error {
		var n int
		if err := w.tx.QueryRow(`SELECT count(*) FROM outbox WHERE seq = ?`, seq).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return errorf(CodeNotFound, "there is no outbox event %d", seq)
		}
		res, err := w.tx.Exec(`INSERT OR IGNORE INTO outbox_receipts(seq, trigger, at) VALUES(?, ?, ?)`, seq, trigger, w.now.UTC().Format(stamp))
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		claimed = affected == 1
		return err
	})
	return claimed, err
}

// Cursor is the sequence of the last committed outbox event: where a live
// query that has seen everything so far stands.
func (s *Store) Cursor() (int64, error) {
	var seq int64
	// The highest sequence ever given, not the highest still held: a trim
	// that empties the outbox must not wind the cursor back to zero, or a
	// live query resuming from before the trim would be told nothing changed.
	err := s.rdb.QueryRow(`SELECT COALESCE((SELECT seq FROM sqlite_sequence WHERE name = 'outbox'), 0)`).Scan(&seq)
	return seq, err
}

// TrimOutbox removes the events at or before seq, and their receipts. A live
// query resuming from before them is told to start again.
func (s *Store) TrimOutbox(through int64) error {
	if through <= 0 {
		return errors.New("trim names an outbox sequence")
	}
	return s.write(func(w *writeTx) error {
		if _, err := w.tx.Exec(`DELETE FROM outbox_receipts WHERE seq <= ?`, through); err != nil {
			return err
		}
		if _, err := w.tx.Exec(`DELETE FROM outbox WHERE seq <= ?`, through); err != nil {
			return err
		}
		_, err := w.tx.Exec(`INSERT INTO space(key, value) VALUES('trimmed', ?) ON CONFLICT(key) DO UPDATE SET value = max(CAST(value AS INTEGER), CAST(excluded.value AS INTEGER))`, through)
		return err
	})
}

// trimmed is the last outbox sequence trimmed away.
func (s *Store) trimmed() int64 {
	var seq int64
	_ = s.rdb.QueryRow(`SELECT COALESCE(CAST(value AS INTEGER), 0) FROM space WHERE key = 'trimmed'`).Scan(&seq)
	return seq
}

// invalidation names the dependencies of a payload-free change.
type invalidation struct {
	typeID  string
	binding string
}

func (w *writeTx) invalidate(event, typeID, binding string) error {
	if w.change == nil {
		w.change = &Change{At: w.now}
	}
	w.change.invalidation = &invalidation{typeID: typeID, binding: binding}
	return w.emit(outboxRow{event: event, typeID: typeID, binding: binding})
}

// InvalidatePolicy commits a reset when policy changes without rewriting a
// record. A blank type ID names all types.
func (s *Store) InvalidatePolicy(typeID string) error {
	return s.write(func(w *writeTx) error {
		if typeID != "" && w.schema.byID[typeID] == nil {
			return errorf(CodeNotFound, "there is no such record type")
		}
		return w.invalidate(EventPolicyChanged, typeID, "")
	})
}

// RecordEventValues resolves an outbox receipt through current type grants.
// Record values stay in history; masked and hidden fields never reach a run.
func (s *Store) RecordEventValues(sc Scope, typeID, record, batch string) (before, after Row, err error) {
	sc, flushAudit := s.WithPolicyAudit(sc)
	defer flushAudit()
	s.visibility.RLock()
	defer s.visibility.RUnlock()
	t, err := s.schema.Load().allow(sc, typeID, false)
	if err != nil {
		return nil, nil, err
	}
	if t.rowRule != nil {
		current, err := loadRecord(s.rdb, s.schema.Load(), record)
		if err != nil {
			return nil, nil, err
		}
		if current == nil || !s.LiveRowAllowed(sc, t, current.Data) {
			return nil, nil, errorf(CodeNotFound, "the event has no record history")
		}
	}
	var rawBefore, rawAfter sql.NullString
	err = s.rdb.QueryRow(`SELECT before_json, after_json FROM history
		WHERE batch = ? AND record = ? AND type_id = ? ORDER BY seq DESC LIMIT 1`, batch, record, t.ID).
		Scan(&rawBefore, &rawAfter)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, errorf(CodeNotFound, "the event has no record history")
	}
	if err != nil {
		return nil, nil, err
	}
	project := func(raw sql.NullString) Row {
		if !raw.Valid {
			return nil
		}
		r := recordFromState(t, record, raw.String)
		if r.DeletedAt != "" || !s.LiveRowAllowed(sc, t, r.Data) {
			return nil
		}
		out := Row{"id": record, "type": t.Name}
		if r.CreatedAt != "" {
			out["created_at"] = r.CreatedAt
		}
		if r.UpdatedAt != "" {
			out["updated_at"] = r.UpdatedAt
		}
		for name, value := range r.Data {
			if f := t.Fields[name]; f != nil && !f.Hidden && !f.Masked {
				switch t.FieldPolicy(f) {
				case "hide":
					s.AuditRecordPolicy(sc, t, "field_hidden")
				case "mask":
					s.AuditRecordPolicy(sc, t, "field_masked")
					out[name] = masked
				default:
					if !s.schema.Load().withheldRef(f) {
						out[name] = value
					}
				}
			}
		}
		return out
	}
	return project(rawBefore), project(rawAfter), nil
}

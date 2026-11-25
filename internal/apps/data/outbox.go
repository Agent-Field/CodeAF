package data

import (
	"encoding/json"
	"errors"
)

// THE OUTBOX IS WRITTEN IN THE SAME TRANSACTION AS THE CHANGE IT TELLS OF. A
// record committed, a transition fired, a timer come due: each writes one
// outbox row beside its own rows, so the engine (runs, triggers, effects)
// can never miss a change that committed or act on one that did not. A
// consumer reads the outbox in order from its own place and takes each event
// once per trigger with [Store.Claim], whose receipt is a unique row: a crash
// between acting and recording can only ever repeat the claim, which the
// unique row refuses, never the action.
//
// The outbox is also the store's change log: its sequence is the cursor a
// live query resumes from (subs.go).

// The events the outbox carries.
const (
	EventRecordCreated   = "record.created"
	EventRecordUpdated   = "record.updated"
	EventRecordDeleted   = "record.deleted"
	EventTransition      = "transition.fired"
	EventWorkItemCreated = "work_item.created"
	EventWorkItemUpdated = "work_item.updated"
	EventTimerCreated    = "timer.created"
	EventTimerFired      = "timer.fired"
	EventTimerCancelled  = "timer.cancelled"
	EventSignoff         = "signoff.created"
	EventComment         = "comment.created"
)

// OutboxMax is the most events one read of the outbox answers.
const OutboxMax = 1000

// Event is one outbox row: what happened, to which record, in which batch,
// by what kind of writer and why, with the event's own payload.
type Event struct {
	Seq     int64
	Event   string
	Type    string
	Record  string
	Batch   string
	App     string
	ByKind  string
	Cause   string
	Payload json.RawMessage
	At      string
}

// outboxRow is one event a write is about to commit.
type outboxRow struct {
	event   string
	typeID  string
	record  string
	payload any
}

// emit writes one outbox row in w's transaction and moves the change's
// cursor to it.
func (w *writeTx) emit(o outboxRow) error {
	var batch, app, kind, cause string
	if c := w.change; c != nil {
		batch, app, kind, cause = c.Batch, c.App, c.ByKind, c.Cause
	}
	if kind == "" {
		kind = ByFlow
	}
	res, err := w.tx.Exec(`INSERT INTO outbox(event, type_id, record, batch, app, by_kind, cause, payload_json, at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		o.event, o.typeID, o.record, batch, app, kind, cause, canonicalJSON(o.payload), w.now.UTC().Format(stamp))
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
	err := eachRow(s.rdb, `SELECT seq, event, type_id, record, batch, app, by_kind, cause, payload_json, at FROM outbox WHERE seq > ? ORDER BY seq LIMIT ?`,
		[]any{after, limit}, func(scan func(...any) error) error {
			var e Event
			var typeID, payload string
			if err := scan(&e.Seq, &e.Event, &typeID, &e.Record, &e.Batch, &e.App, &e.ByKind, &e.Cause, &payload, &e.At); err != nil {
				return err
			}
			if t := sch.byID[typeID]; t != nil {
				e.Type = t.Name
			}
			e.Payload = json.RawMessage(payload)
			out = append(out, e)
			return nil
		})
	return out, err
}

// Claim takes one event for one trigger, once: true the first time, false
// for every later claim of the same pair. The receipt commits on its own, so
// a consumer claims before it acts and acts only on true.
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

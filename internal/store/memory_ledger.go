package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// A memory is written by a person or the agent and by nothing else, so the
// memories table cannot be rebuilt from a transcript: the event journal in
// graph.db is the only place the words exist. This file is the seam that lets
// that truth live somewhere sealed too. Every memory event is handed, once it
// has committed, to a [MemoryLedger] the opener installed; the ledger keeps it
// beside the conversation that wrote it, and [Store.ImportMemoryEvents] replays
// it into a graph.db that lost it. The store knows neither where the ledger is
// nor what a session folder is.

// MemoryEvent is one committed memory event, as a ledger keeps it.
type MemoryEvent struct {
	// Session is the conversation that supplied the event; "" when none did.
	Session string          `json:"session,omitempty"`
	Time    time.Time       `json:"ts"`
	ID      string          `json:"id"`
	Kind    EventKind       `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

// MemoryLedger keeps memory events outside graph.db.
type MemoryLedger interface {
	// Record keeps one committed event. It is called after the commit, so an
	// error means the event is in the graph and not yet sealed.
	Record(MemoryEvent) error
}

// ledgered says which memory events are truth. A ranking snapshot is left out:
// it is telemetry about retrieval, with no conversation to belong to.
var ledgered = map[EventKind]bool{
	EventMemoryAdd: true, EventMemoryUpdate: true, EventMemorySupersede: true,
	EventMemoryForget: true, EventMemoryRestore: true,
}

// Creates reports whether the event brings a memory into being.
func (e MemoryEvent) Creates() bool {
	return e.Kind == EventMemoryAdd || e.Kind == EventMemorySupersede
}

// SetMemoryLedger installs the ledger every later memory write is handed to.
// It is set once, while the store is being wired, before any write.
func (s *Store) SetMemoryLedger(l MemoryLedger) { s.ledger = l }

// memoryEventPayload is what every ledgered payload can say about its author.
type memoryEventPayload interface{ source() string }

func (p memoryPayload) source() string          { return p.SourceSession }
func (p memoryUpdatePayload) source() string    { return p.SourceSession }
func (p memorySupersedePayload) source() string { return p.SourceSession }
func (p memoryForgetPayload) source() string    { return p.SourceSession }
func (memoryRestorePayload) source() string     { return "" }

// commitMemoryEvent journals one memory event, applies it to the views inside
// the same transaction, commits, and only then hands it to the ledger.
func (s *Store) commitMemoryEvent(tx *sql.Tx, id string, kind EventKind, payload memoryEventPayload, apply func(seq int64) error) (int64, error) {
	seq, at, err := appendEvent(tx, id, kind, payload)
	if err != nil {
		return 0, err
	}
	if err := apply(seq); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return seq, s.seal(id, kind, at, payload)
}

func (s *Store) seal(id string, kind EventKind, at time.Time, payload memoryEventPayload) error {
	if s.ledger == nil {
		return nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return s.ledger.Record(MemoryEvent{Session: payload.source(), Time: at, ID: id, Kind: kind, Payload: encoded})
}

// MemoryEventsOf reads the memory events one conversation supplied out of the
// journal, oldest first: what a session folder that never had a ledger is owed.
func (s *Store) MemoryEventsOf(session string) ([]MemoryEvent, error) {
	rows, err := s.db.Query(`
		SELECT ts, node_id, kind, payload FROM events
		WHERE kind IN (?, ?, ?, ?, ?) AND json_extract(payload, '$.source_session') = ?
		ORDER BY seq`,
		EventMemoryAdd, EventMemoryUpdate, EventMemorySupersede, EventMemoryForget, EventMemoryRestore, session)
	if err != nil {
		return nil, fmt.Errorf("read memory events: %w", err)
	}
	defer rows.Close()
	var out []MemoryEvent
	for rows.Next() {
		event := MemoryEvent{Session: session}
		var at, payload string
		if err := rows.Scan(&at, &event.ID, &event.Kind, &payload); err != nil {
			return nil, fmt.Errorf("read memory events: %w", err)
		}
		if event.Time, err = parseTime(at); err != nil {
			return nil, fmt.Errorf("read memory events: %w", err)
		}
		event.Payload = json.RawMessage(payload)
		out = append(out, event)
	}
	return out, rows.Err()
}

// ImportMemoryEvents replays ledgered events into the journal and the views, in
// the order given, without handing them back to the ledger.
//
// An event the views cannot take is skipped, not failed: it is either already
// reflected here (a memory added twice) or aimed at a memory another
// conversation's ledger holds. Replaying the same ledger twice changes nothing.
func (s *Store) ImportMemoryEvents(events []MemoryEvent) error {
	for _, event := range events {
		if !ledgered[event.Kind] {
			return fmt.Errorf("import memory events: %w: %q is not a memory event", ErrInvalid, event.Kind)
		}
		if err := s.importMemoryEvent(event); err != nil {
			return fmt.Errorf("import memory events: %w", err)
		}
	}
	return nil
}

func (s *Store) importMemoryEvent(event MemoryEvent) error {
	tx, err := s.beginWrite()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	seq, err := insertEvent(tx, event.Time, event.ID, event.Kind, event.Payload)
	if err != nil {
		return err
	}
	if replayEvent(tx, Event{Seq: seq, Time: event.Time, NodeID: event.ID, Kind: event.Kind, Payload: event.Payload}) != nil {
		return nil
	}
	return tx.Commit()
}

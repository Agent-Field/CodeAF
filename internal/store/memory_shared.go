package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Agent-Field/codeaf/internal/sqliteuri"
	"os"
	"sort"
)

// SharedMemoryEvents is the person's durable memory history, including edits
// made without a conversation. Machine-owned lines never leave this home
// (C30.1/C30.2). Events are immutable, so concurrent edits of different lines
// merge by union before the per-line time rule is applied.
func (s *Store) SharedMemoryEvents() ([]LedgerMemoryEvent, error) {
	rows, err := s.db.Query(`SELECT e.ts, e.node_id, e.kind, e.payload FROM events e
 JOIN memories m ON m.id=e.node_id WHERE m.owner<>? AND e.seq>COALESCE((SELECT MAX(seq) FROM events WHERE kind=?),0) AND e.kind IN (?,?,?,?,?) ORDER BY e.seq`,
		OwnerMachine, EventMemoryReset, EventMemoryAdd, EventMemoryUpdate, EventMemorySupersede, EventMemoryForget, EventMemoryRestore)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []LedgerMemoryEvent
	for rows.Next() {
		var e LedgerMemoryEvent
		var at, payload string
		if err := rows.Scan(&at, &e.ID, &e.Kind, &payload); err != nil {
			return nil, err
		}
		if e.Time, err = parseTime(at); err != nil {
			return nil, err
		}
		e.Payload = json.RawMessage(payload)
		var source struct {
			Session string `json:"source_session"`
		}
		if err := json.Unmarshal(e.Payload, &source); err != nil {
			return nil, err
		}
		e.Session = source.Session
		events = append(events, e)
	}
	return events, rows.Err()
}

// ImportSharedMemoryEvents applies a merged history by edit time, not arrival
// time. An older offline computer cannot undo a later edit or forget (C30.2).
// Ordinary ledger imports use this rule too, so opening a moved chat cannot
// roll a shared memory back to the version sealed in an older turn.
func (s *Store) ImportSharedMemoryEvents(events []LedgerMemoryEvent) error {
	events = append([]LedgerMemoryEvent(nil), events...)
	sort.SliceStable(events, func(i, j int) bool { return events[i].Time.Before(events[j].Time) })
	return s.ImportMemoryEvents(events)
}

// ClearSharedMemories is the explicit pairing replacement door. Machine-owned
// memories stay local; shared history is removed before the new fleet is read.
func (s *Store) ClearSharedMemories() error {
	tx, err := s.beginWrite()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, _, err := appendEvent(tx, "root", EventMemoryReset, struct{}{}); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM memories WHERE owner<>?`, OwnerMachine); err != nil {
		return err
	}
	return tx.Commit()
}

// EventMemoryReset records an explicit pairing replacement while preserving
// the append-only history. Earlier personal memory is not sent to a new fleet.
const EventMemoryReset EventKind = "memory_reset"

// prepareSharedMemoryReplay treats a replacement as another version of its
// original line. Offline edits and forgets of that original still compete by
// edit time, rather than losing merely because replacement minted a new id.
func prepareSharedMemoryReplay(tx *sql.Tx, event Event) (bool, error) {
	target := event.NodeID
	if event.Kind == EventMemorySupersede {
		var p memorySupersedePayload
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return false, err
		}
		target = p.OldID
	}
	ids, newer, err := sharedMemoryFamily(tx, event, target)
	if err != nil || newer {
		return newer, err
	}
	if event.Kind == EventMemoryAdd {
		return false, nil
	}
	for _, id := range ids {
		if id != target {
			if _, err := tx.Exec(`UPDATE memories SET status=?,updated_seq=? WHERE id=?`, MemoryForgotten, event.Seq, id); err != nil {
				return false, err
			}
		}
	}
	// An edit made without seeing an earlier forget or replacement still wins
	// when it was made later. The event itself then applies the requested words
	// or final status through the ordinary memory replay door.
	if event.Kind != EventMemoryRestore {
		if _, err := tx.Exec(`UPDATE memories SET status=? WHERE id=?`, MemoryActive, target); err != nil {
			return false, err
		}
	}
	return false, nil
}

// sharedMemoryFamily reads the replacement lineage before changing any view.
// A family with an equal or newer edit already has its winning version.
func sharedMemoryFamily(tx *sql.Tx, event Event, target string) ([]string, bool, error) {
	rows, err := tx.Query(`WITH RECURSIVE family(id) AS (
 SELECT ? UNION SELECT json_extract(e.payload,'$.new.id') FROM events e JOIN family f ON json_extract(e.payload,'$.old_id')=f.id WHERE e.kind=?
 UNION SELECT json_extract(e.payload,'$.old_id') FROM events e JOIN family f ON json_extract(e.payload,'$.new.id')=f.id WHERE e.kind=?)
 SELECT m.id,COALESCE(e.ts,'') FROM memories m JOIN family f ON f.id=m.id LEFT JOIN events e ON e.seq=m.updated_seq`, target, EventMemorySupersede, EventMemorySupersede)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id, at string
		if err := rows.Scan(&id, &at); err != nil {
			return nil, false, err
		}
		if at != "" {
			when, err := parseTime(at)
			if err != nil {
				return nil, false, err
			}
			if !when.Before(event.Time) {
				return nil, true, nil
			}
		}
		ids = append(ids, id)
	}
	return ids, false, rows.Err()
}

// SharedMemoryEventsAt reads an existing home's memory without running schema
// migrations or creating a database every idle sync round. The ordinary opener
// owns schema upgrades before the surface starts its home-state worker.
func SharedMemoryEventsAt(path string) ([]LedgerMemoryEvent, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	uri := sqliteuri.File(path)
	query := uri.Query()
	query.Set("mode", "ro")
	uri.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return (&Store{db: db}).SharedMemoryEvents()
}

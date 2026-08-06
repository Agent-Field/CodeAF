package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Facts are the notebook: durable things learned while working — preferences,
// environment, entities — as distinct from any one job's result. A fact
// carries provenance to the node that taught it, so remembering is auditable
// and a wrong memory can be traced and superseded rather than lingering as
// folklore.

// MaxFactBytes bounds one fact. A fact is one standalone line, not a report.
const MaxFactBytes = 512

// Fact is one materialized notebook entry.
type Fact struct {
	Seq    int64
	Time   time.Time
	NodeID string // the node whose work taught this
	Body   string
}

const factsSchema = `
CREATE TABLE IF NOT EXISTS facts (
    seq     INTEGER PRIMARY KEY REFERENCES events(seq),
    ts      TEXT NOT NULL,
    node_id TEXT NOT NULL,
    body    TEXT NOT NULL
);
`

type factPayload struct {
	NodeID string `json:"node_id"`
	Body   string `json:"body"`
}

// RecordFact appends one learned fact, attributed to the node that taught it.
func (s *Store) RecordFact(nodeID, body string) (Fact, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Fact{}, fmt.Errorf("record fact: %w: empty fact", ErrInvalid)
	}
	if len(body) > MaxFactBytes {
		return Fact{}, fmt.Errorf("record fact: %w: fact is %d bytes (limit %d)", ErrInvalid, len(body), MaxFactBytes)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Fact{}, fmt.Errorf("record fact: %w", err)
	}
	defer tx.Rollback()

	if nodeID != "" {
		if err := requireNode(tx, nodeID); err != nil {
			return Fact{}, fmt.Errorf("record fact: %w", err)
		}
	}
	payload := factPayload{NodeID: nodeID, Body: body}
	seq, at, err := appendEvent(tx, nodeID, EventFactLearned, payload)
	if err != nil {
		return Fact{}, fmt.Errorf("record fact: %w", err)
	}
	if err := applyFactView(tx, payload, seq, at); err != nil {
		return Fact{}, fmt.Errorf("record fact: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Fact{}, fmt.Errorf("record fact: %w", err)
	}
	return Fact{Seq: seq, Time: at, NodeID: nodeID, Body: body}, nil
}

// RecentFacts returns the newest facts first.
func (s *Store) RecentFacts(limit int) ([]Fact, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`
		SELECT seq, ts, node_id, body FROM facts
		ORDER BY seq DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("recent facts: %w", err)
	}
	return scanFacts(rows)
}

func scanFacts(rows *sql.Rows) ([]Fact, error) {
	defer rows.Close()
	facts := make([]Fact, 0)
	for rows.Next() {
		var fact Fact
		var timestamp string
		if err := rows.Scan(&fact.Seq, &timestamp, &fact.NodeID, &fact.Body); err != nil {
			return nil, fmt.Errorf("scan fact: %w", err)
		}
		at, err := parseTime(timestamp)
		if err != nil {
			return nil, fmt.Errorf("scan fact time: %w", err)
		}
		fact.Time = at
		facts = append(facts, fact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan facts: %w", err)
	}
	return facts, nil
}

func applyFactView(tx *sql.Tx, payload factPayload, seq int64, at time.Time) error {
	_, err := tx.Exec(`
		INSERT INTO facts (seq, ts, node_id, body) VALUES (?, ?, ?, ?)`,
		seq, formatTime(at), payload.NodeID, payload.Body)
	return err
}

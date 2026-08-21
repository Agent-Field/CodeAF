package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// The per-turn usage ledger.
//
// It is a second table rather than more rows in the first, and that is the whole
// of what makes it additive. Half a dozen readers count the usage table — a
// job's Runs, a receipt's calls, a rail's day — with COUNT(usage.seq), and every
// one of them means "executions" by it. Turn rows landing in that table would
// have multiplied every one of those counts by the length of the leaf, silently,
// in views nobody would think to re-read. So the node row stays exactly what it
// was, byte for byte, and the shape underneath it lands here.
//
// The event is one per node run rather than one per turn, carrying the whole
// ledger. A leaf's turns are known together, at the same moment, about the same
// execution: journaling them as one fact is both cheaper and truer than eleven
// facts that would then have to be re-associated by a reader.

// TurnUsage is one turn of one node's execution.
//
// Sent is the quantity that could not be reconstructed from anything already
// journaled: what this turn put on the wire, cache-served prefix included.
// Summed over turns it is the leaf's cumulative context pressure, which is the
// meter the runaway nodes were invisible to — see internal/exec/meter.go.
type TurnUsage struct {
	Turn             int     `json:"turn"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CachedTokens     int     `json:"cached_tokens,omitempty"`
	SentTokens       int     `json:"sent_tokens,omitempty"`
	Cost             float64 `json:"cost"`
}

// turnUsagePayload is one execution's whole ledger, as journaled.
type turnUsagePayload struct {
	NodeID string      `json:"node_id"`
	Model  string      `json:"model,omitempty"`
	Turns  []TurnUsage `json:"turns"`
}

const turnUsageSchema = `
CREATE TABLE IF NOT EXISTS usage_turns (
    seq               INTEGER NOT NULL REFERENCES events(seq),
    turn              INTEGER NOT NULL,
    ts                TEXT NOT NULL,
    node_id           TEXT NOT NULL,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    cached_tokens     INTEGER NOT NULL DEFAULT 0,
    sent_tokens       INTEGER NOT NULL DEFAULT 0,
    cost              REAL NOT NULL DEFAULT 0,
    model             TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (seq, turn)
);
CREATE INDEX IF NOT EXISTS usage_turns_node ON usage_turns (node_id);
CREATE INDEX IF NOT EXISTS usage_turns_ts ON usage_turns (ts);
`

// RecordTurnUsage appends one execution's per-turn shape to the journal.
//
// It is a companion to RecordUsage rather than a replacement for it, and callers
// write both: the node row is what every existing reader sums, this is what a
// reader asking about shape needs. An empty ledger is not an event — an executor
// that does not meter turns has recorded no shape, which is a different fact
// from a leaf that ran none.
func (s *Store) RecordTurnUsage(nodeID, model string, turns []TurnUsage) error {
	if nodeID == "" {
		return fmt.Errorf("record turn usage: %w: empty node id", ErrInvalid)
	}
	if len(turns) == 0 {
		return nil
	}
	for _, turn := range turns {
		if turn.Turn <= 0 {
			return fmt.Errorf("record turn usage: %w: turn %d is not numbered from one",
				ErrInvalid, turn.Turn)
		}
	}
	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("record turn usage: %w", err)
	}
	defer tx.Rollback()

	if err := requireNode(tx, nodeID); err != nil {
		return fmt.Errorf("record turn usage: %w", err)
	}
	payload := turnUsagePayload{NodeID: nodeID, Model: strings.TrimSpace(model), Turns: turns}
	seq, at, err := appendEvent(tx, nodeID, EventTurnUsageRecorded, payload)
	if err != nil {
		return fmt.Errorf("record turn usage: %w", err)
	}
	if err := applyTurnUsageView(tx, payload, seq, at); err != nil {
		return fmt.Errorf("record turn usage: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record turn usage: %w", err)
	}
	return nil
}

// TurnUsageFor is one node's journaled shape, oldest execution first and each
// execution's turns in order. A node whose executor never metered turns answers
// with nothing at all, which reads as "no shape recorded".
func (s *Store) TurnUsageFor(nodeID string) ([]TurnUsage, error) {
	rows, err := s.db.Query(`
		SELECT turn, prompt_tokens, completion_tokens, cached_tokens, sent_tokens, cost
		FROM usage_turns WHERE node_id = ? ORDER BY seq, turn`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("turn usage: %w", err)
	}
	defer rows.Close()
	var turns []TurnUsage
	for rows.Next() {
		var turn TurnUsage
		if err := rows.Scan(&turn.Turn, &turn.PromptTokens, &turn.CompletionTokens,
			&turn.CachedTokens, &turn.SentTokens, &turn.Cost); err != nil {
			return nil, fmt.Errorf("turn usage: %w", err)
		}
		turns = append(turns, turn)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("turn usage: %w", err)
	}
	return turns, nil
}

func applyTurnUsageView(tx *sql.Tx, payload turnUsagePayload, seq int64, at time.Time) error {
	stamp := formatTime(at)
	for _, turn := range payload.Turns {
		if _, err := tx.Exec(`
			INSERT INTO usage_turns (
			    seq, turn, ts, node_id, prompt_tokens, completion_tokens,
			    cached_tokens, sent_tokens, cost, model
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			seq, turn.Turn, stamp, payload.NodeID, turn.PromptTokens, turn.CompletionTokens,
			turn.CachedTokens, turn.SentTokens, turn.Cost, payload.Model); err != nil {
			return err
		}
	}
	return nil
}

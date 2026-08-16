package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const retrospectiveSchema = `
CREATE TABLE IF NOT EXISTS retrospective_watermark (
    singleton    INTEGER PRIMARY KEY CHECK (singleton = 1),
    seq          INTEGER NOT NULL REFERENCES events(seq),
    ts           TEXT NOT NULL,
    settled_jobs INTEGER NOT NULL CHECK (settled_jobs >= 0)
);
`

// The retrospective's watermark was the first of its family; the resident has
// since grown two more cursors that a restart used to forget — the settlement
// lane's place in the journal, and the notebook consolidation clock. They share
// one table and one event kind rather than each minting their own, because they
// are the same shape: a lane name, the journal position it reached, and when it
// got there.
const residentWatermarkSchema = `
CREATE TABLE IF NOT EXISTS resident_watermarks (
    lane   TEXT PRIMARY KEY,
    seq    INTEGER NOT NULL REFERENCES events(seq),
    ts     TEXT NOT NULL,
    cursor INTEGER NOT NULL DEFAULT 0
);
`

// ResidentLane names one durable resident cursor.
type ResidentLane string

const (
	// LaneSettlement is the announce/distill/fold cursor. Its cursor is the
	// last journal sequence the settle pass has already reacted to, so a
	// process that starts after another one stopped resumes at the gap rather
	// than stepping over it.
	LaneSettlement ResidentLane = "settlement"
	// LaneConsolidation paces the notebook's belief-rewriting sleep pass. It
	// carries no cursor: the event's own time is the whole watermark.
	LaneConsolidation ResidentLane = "consolidation"
)

// ResidentWatermark is one lane's durable position.
type ResidentWatermark struct {
	Lane   ResidentLane
	Seq    int64
	At     time.Time
	Cursor int64
}

type residentWatermarkPayload struct {
	Lane   ResidentLane `json:"lane"`
	Cursor int64        `json:"cursor,omitempty"`
}

// MarkResidentWatermark appends a lane watermark event and advances its row in
// the same transaction — the retrospective checkpoint's shape, keyed by lane.
func (s *Store) MarkResidentWatermark(lane ResidentLane, cursor int64) (ResidentWatermark, error) {
	if strings.TrimSpace(string(lane)) == "" {
		return ResidentWatermark{}, fmt.Errorf("mark resident watermark: %w: empty lane", ErrInvalid)
	}
	if cursor < 0 {
		return ResidentWatermark{}, fmt.Errorf("mark resident watermark: %w: negative cursor", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return ResidentWatermark{}, fmt.Errorf("mark resident watermark: %w", err)
	}
	defer tx.Rollback()

	payload := residentWatermarkPayload{Lane: lane, Cursor: cursor}
	seq, at, err := appendEvent(tx, "", EventResidentWatermarked, payload)
	if err != nil {
		return ResidentWatermark{}, fmt.Errorf("mark resident watermark: %w", err)
	}
	if err := applyResidentWatermark(tx, payload, seq, at); err != nil {
		return ResidentWatermark{}, fmt.Errorf("mark resident watermark: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return ResidentWatermark{}, fmt.Errorf("mark resident watermark: %w", err)
	}
	return ResidentWatermark{Lane: lane, Seq: seq, At: at, Cursor: cursor}, nil
}

// ResidentWatermarkFor returns one lane's latest watermark, if it has ever run.
func (s *Store) ResidentWatermarkFor(lane ResidentLane) (ResidentWatermark, bool, error) {
	watermark := ResidentWatermark{Lane: lane}
	var timestamp string
	err := s.db.QueryRow(`
		SELECT seq, ts, cursor FROM resident_watermarks WHERE lane = ?`, lane).
		Scan(&watermark.Seq, &timestamp, &watermark.Cursor)
	if errors.Is(err, sql.ErrNoRows) {
		return ResidentWatermark{}, false, nil
	}
	if err != nil {
		return ResidentWatermark{}, false, fmt.Errorf("read resident watermark: %w", err)
	}
	watermark.At, err = parseTime(timestamp)
	if err != nil {
		return ResidentWatermark{}, false, fmt.Errorf("read resident watermark: %w", err)
	}
	return watermark, true, nil
}

func applyResidentWatermark(tx *sql.Tx, payload residentWatermarkPayload, seq int64, at time.Time) error {
	if strings.TrimSpace(string(payload.Lane)) == "" {
		return fmt.Errorf("apply resident watermark: %w: empty lane", ErrInvalid)
	}
	_, err := tx.Exec(`
		INSERT INTO resident_watermarks (lane, seq, ts, cursor)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(lane) DO UPDATE SET
			seq = excluded.seq,
			ts = excluded.ts,
			cursor = excluded.cursor`,
		payload.Lane, seq, formatTime(at), payload.Cursor)
	return err
}

type retrospectiveCheckpointPayload struct {
	SettledJobs int `json:"settled_jobs"`
}

// RetrospectiveWatermark is the last durable retrospective checkpoint. At is
// the journal event time; SettledJobs is the uncapped count considered then.
type RetrospectiveWatermark struct {
	Seq         int64
	At          time.Time
	SettledJobs int
}

// CheckpointRetrospective appends a watermark event and advances its singleton
// materialized view in the same transaction.
func (s *Store) CheckpointRetrospective(settledJobs int) (RetrospectiveWatermark, error) {
	if settledJobs < 0 {
		return RetrospectiveWatermark{}, fmt.Errorf("checkpoint retrospective: %w: negative settled job count", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return RetrospectiveWatermark{}, fmt.Errorf("checkpoint retrospective: %w", err)
	}
	defer tx.Rollback()

	payload := retrospectiveCheckpointPayload{SettledJobs: settledJobs}
	seq, at, err := appendEvent(tx, "", EventRetrospectiveCheckpointed, payload)
	if err != nil {
		return RetrospectiveWatermark{}, fmt.Errorf("checkpoint retrospective: %w", err)
	}
	if err := applyRetrospectiveCheckpoint(tx, payload, seq, at); err != nil {
		return RetrospectiveWatermark{}, fmt.Errorf("checkpoint retrospective: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return RetrospectiveWatermark{}, fmt.Errorf("checkpoint retrospective: %w", err)
	}
	return RetrospectiveWatermark{Seq: seq, At: at, SettledJobs: settledJobs}, nil
}

// RetrospectiveWatermark returns the latest checkpoint, if reflection has run.
func (s *Store) RetrospectiveWatermark() (RetrospectiveWatermark, bool, error) {
	var watermark RetrospectiveWatermark
	var timestamp string
	err := s.db.QueryRow(`
		SELECT seq, ts, settled_jobs FROM retrospective_watermark WHERE singleton = 1`).
		Scan(&watermark.Seq, &timestamp, &watermark.SettledJobs)
	if errors.Is(err, sql.ErrNoRows) {
		return RetrospectiveWatermark{}, false, nil
	}
	if err != nil {
		return RetrospectiveWatermark{}, false, fmt.Errorf("read retrospective watermark: %w", err)
	}
	watermark.At, err = parseTime(timestamp)
	if err != nil {
		return RetrospectiveWatermark{}, false, fmt.Errorf("read retrospective watermark: %w", err)
	}
	return watermark, true, nil
}

func applyRetrospectiveCheckpoint(tx *sql.Tx, payload retrospectiveCheckpointPayload, seq int64, at time.Time) error {
	_, err := tx.Exec(`
		INSERT INTO retrospective_watermark (singleton, seq, ts, settled_jobs)
		VALUES (1, ?, ?, ?)
		ON CONFLICT(singleton) DO UPDATE SET
			seq = excluded.seq,
			ts = excluded.ts,
			settled_jobs = excluded.settled_jobs`,
		seq, formatTime(at), payload.SettledJobs)
	return err
}

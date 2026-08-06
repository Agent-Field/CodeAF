package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

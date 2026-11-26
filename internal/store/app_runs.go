package store

import (
	"database/sql"
	"encoding/json"
)

const eventAppRunCheckpoint EventKind = "app_run_checkpoint"

// RecordAppRunCheckpoints appends checkpoints as one transaction so recording
// a scheduled run and advancing its clock cannot be torn apart by a restart.
func (s *Store) RecordAppRunCheckpoints(checkpoints map[string]json.RawMessage) error {
	tx, err := s.beginWrite()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, payload := range checkpoints {
		if _, _, err := appendEvent(tx, key, eventAppRunCheckpoint, payload); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AppRunCheckpoints reads the last committed checkpoint for each run and clock.
func (s *Store) AppRunCheckpoints() (map[string]json.RawMessage, error) {
	rows, err := s.db.Query(`
		SELECT node_id, payload FROM events
		WHERE seq IN (SELECT MAX(seq) FROM events WHERE kind = ? GROUP BY node_id)
	`, eventAppRunCheckpoint)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	checkpoints := make(map[string]json.RawMessage)
	for rows.Next() {
		var key, payload string
		if err := rows.Scan(&key, &payload); err != nil {
			return nil, err
		}
		checkpoints[key] = json.RawMessage(payload)
	}
	return checkpoints, rows.Err()
}

// ClaimAppRun records the trigger receipt and the initial run checkpoint
// together. The data outbox remains readable if either write is interrupted.
func (s *Store) ClaimAppRun(seq int64, trigger, key string, payload json.RawMessage) (bool, error) {
	return s.withAppRunClaim(seq, trigger, func(tx *sql.Tx) error {
		_, _, err := appendEvent(tx, key, eventAppRunCheckpoint, payload)
		return err
	})
}

// ClaimSourceTrigger consumes a filtered-out source event without starting a run.
func (s *Store) ClaimSourceTrigger(seq int64, trigger string) error {
	_, err := s.withAppRunClaim(seq, trigger, func(*sql.Tx) error { return nil })
	return err
}

// AppRunClaimed lets a replay skip trigger evaluation for a committed run.
func (s *Store) AppRunClaimed(seq int64, trigger string) (bool, error) {
	var exists bool
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM app_run_receipts WHERE seq = ? AND trigger_id = ?)`, seq, trigger).Scan(&exists)
	return exists, err
}

func (s *Store) withAppRunClaim(seq int64, trigger string, record func(*sql.Tx) error) (bool, error) {
	tx, err := s.beginWrite()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`INSERT INTO app_run_receipts(seq, trigger_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, seq, trigger)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil || count == 0 {
		return false, err
	}
	if err := record(tx); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

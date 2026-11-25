package store

import (
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

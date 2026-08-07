package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// migrateCharterSchema resolves the two charter materializations joined by the
// resident branch merge. The journal remains the source of truth: when either
// older table shape is found, the union view is rebuilt from charter events.
func migrateCharterSchema(db *sql.DB) error {
	hasID, err := tableHasColumn(db, "charters", "id")
	if err != nil {
		return err
	}
	if !hasID {
		_, err := db.Exec(charterSchema)
		return err
	}

	required := []string{
		"session_id", "invariant", "watch", "sentinel_hint", "action", "rails",
		"status", "ratification", "proposal_shape", "next_due", "created_seq",
		"updated_seq", "legacy_spec", "source_command_seq", "created_at",
	}
	current := true
	for _, column := range required {
		hasColumn, columnErr := tableHasColumn(db, "charters", column)
		if columnErr != nil {
			return columnErr
		}
		current = current && hasColumn
	}
	var definition string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='charters'`).Scan(&definition); err != nil {
		return err
	}
	current = current && strings.Contains(definition, "'draft'") && strings.Contains(definition, "'proposed'")
	if current {
		_, err := db.Exec(charterSchema)
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	events, err := readEvents(tx)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
		DROP TABLE IF EXISTS charters_fts;
		DROP INDEX IF EXISTS charters_due;
		DROP INDEX IF EXISTS charters_status_created;
		ALTER TABLE charters RENAME TO charters_before_union;
	`); err != nil {
		return err
	}
	if _, err := tx.Exec(charterSchema); err != nil {
		return err
	}
	for _, event := range events {
		if !isCharterEvent(event.Kind) {
			continue
		}
		if err := replayCharterViewEvent(tx, event); err != nil {
			return fmt.Errorf("replay event %d (%s): %w", event.Seq, event.Kind, err)
		}
	}
	if _, err := tx.Exec(`DROP TABLE charters_before_union`); err != nil {
		return err
	}
	return tx.Commit()
}

func isCharterEvent(kind EventKind) bool {
	switch kind {
	case EventCharterDrafted, EventCharterRatified, EventCharterPaused,
		EventCharterRetired, EventCharterCadenceEdited, EventCharterCreated,
		EventCharterRevised, EventCharterStatusChanged, EventCharterWatchAdvanced,
		EventCharterWoken, EventSentinelChecked, EventCharterFired,
		EventCharterFiringBlocked, EventCharterFiringDeferred,
		EventCharterProposalDeclined:
		return true
	default:
		return false
	}
}

func replayCharterViewEvent(tx *sql.Tx, event Event) error {
	if event.Kind == EventCharterCreated {
		var payload charterRecord
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyCharterCreatedView(tx, payload, event.Seq, event.Time)
	}
	return replayEvent(tx, event)
}

package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// MemoryEdit is a person's conditional edit, including a shelf transfer.
// Undo deadlines share the tombstone transaction so a crash cannot split them.
type MemoryEdit struct {
	Action      string
	Text, Owner *string
	State       *string
	Version     *uint64
	Now         time.Time
}

const MemoryUndoWindow = 5 * time.Second

var ErrMemoryStale = errors.New("the memory changed; read it again before acting")
var ErrMemoryMissing = errors.New("no such memory")
var ErrMemoryConflict = errors.New("memory action conflicts with its current state")
var ErrMemoryUndoEnded = errors.New("the five-second memory undo window has ended")

// EditMemory serializes comparison and mutation against every store writer,
// including a terminal sharing the app's home. It journals through the native folds.
func (s *Store) EditMemory(id string, edit MemoryEdit) error {
	tx, err := s.beginWrite()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner, status, title, text, tags string
	var version uint64
	err = tx.QueryRow("SELECT owner,status,title,text,tags,updated_seq FROM memories WHERE id=?", id).Scan(&owner, &status, &title, &text, &tags, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrMemoryMissing
	}
	if err != nil {
		return err
	}
	if edit.State != nil && *edit.State != status || edit.Version != nil && *edit.Version != version {
		return ErrMemoryStale
	}
	if _, err := tx.Exec("CREATE TABLE IF NOT EXISTS memory_undo (id TEXT PRIMARY KEY, until_ns INTEGER NOT NULL, version INTEGER NOT NULL)"); err != nil {
		return err
	}
	switch edit.Action {
	case "patch":
		if status != MemoryActive {
			return ErrMemoryConflict
		}
		var parsedTags []string
		if err := json.Unmarshal([]byte(tags), &parsedTags); err != nil {
			return err
		}
		if edit.Text != nil {
			text = *edit.Text
		}
		title, text, parsedTags, err = validMemoryBody(title, text, parsedTags)
		if err != nil {
			return err
		}
		payload := memoryUpdatePayload{ID: id, Title: title, Text: text, Tags: parsedTags}
		if edit.Owner != nil && *edit.Owner != owner {
			if !ValidOwner(*edit.Owner) || *edit.Owner == OwnerLegacyProject {
				return fmt.Errorf("%w: invalid memory shelf", ErrInvalid)
			}
			payload.Owner, payload.PreviousOwner = *edit.Owner, owner
		}
		seq, _, err := appendEvent(tx, id, EventMemoryUpdate, payload)
		if err != nil {
			return err
		}
		if err := applyMemoryUpdate(tx, payload, seq, s.fts); err != nil {
			return err
		}
	case "forget":
		if status != MemoryActive {
			return ErrMemoryConflict
		}
		payload := memoryForgetPayload{ID: id, Owner: owner}
		seq, _, err := appendEvent(tx, id, EventMemoryForget, payload)
		if err != nil {
			return err
		}
		if err := applyMemoryForget(tx, payload, seq, s.fts); err != nil {
			return err
		}
		if err := suppressForgottenEvidence(tx, id, owner); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT OR REPLACE INTO memory_undo(id,until_ns,version) VALUES(?,?,?)", id, edit.Now.Add(MemoryUndoWindow).UnixNano(), seq); err != nil {
			return err
		}
	case "restore":
		var until int64
		var forgottenVersion uint64
		err := tx.QueryRow("SELECT until_ns,version FROM memory_undo WHERE id=?", id).Scan(&until, &forgottenVersion)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if status != MemoryForgotten || err != nil || forgottenVersion != version || edit.Now.UnixNano() >= until {
			return ErrMemoryUndoEnded
		}
		payload := memoryRestorePayload{ID: id}
		seq, _, err := appendEvent(tx, id, EventMemoryRestore, payload)
		if err != nil {
			return err
		}
		if err := applyMemoryRestore(tx, payload, seq, s.fts); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM memory_undo WHERE id=?", id); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: invalid memory action", ErrInvalid)
	}
	return tx.Commit()
}

// A person's forget also withdraws contextual evidence in the same transaction.
func suppressForgottenEvidence(tx *sql.Tx, id, owner string) error {
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM events WHERE node_id=? AND kind=? AND json_extract(payload,'$.MemoryID')=?`, contextualNode(owner), EventContextualEvidence, id).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		_, _, err := appendEvent(tx, contextualNode(owner), EventContextualMemorySuppression, struct{ MemoryID, Reason string }{id, "explicit forget"})
		return err
	}
	return nil
}

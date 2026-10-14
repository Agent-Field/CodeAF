package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// The write door. THIS IS THE ONE WAY ANYTHING LANDS IN THE MEMORY TABLE from a
// caller outside the store: /remember, the model's `remember`, what the pass
// after a turn notices, the memory place's own add and the legacy import all
// come through [Store.Write], so there is exactly one duplicate rule and exactly
// one journal for every mouth that says "keep this".
//
// [Store.AddMemory] is the raw door, kept for the demo seeder and the tests.
// Nothing that receives text from a person or a model should call it.
//
// The duplicate check is deterministic and same-owner BY CONSTRUCTION: it reads
// the writer's own owner, so "is this already remembered?" is never answered by
// another project's row.

// WriteRequest is one memory a caller wants kept.
type WriteRequest struct {
	// Owner is the blast radius, spelled as an owner (memory_owner.go). An
	// empty owner is refused — the caller that cannot name one is exactly the
	// caller the quarantine exists for.
	Owner string
	Type  string
	Text  string
	Tags  []string
	// SourceSession is the conversation that supplied the words, for the
	// memory place's card.
	SourceSession string
	// Origin is who is keeping it ([OriginPerson], [OriginModel],
	// [OriginNoticed]).
	Origin string
	// ProjectName is the name the person knows the project by, kept beside a
	// project memory so the memory place can say where it holds.
	ProjectName string
}

// WriteResult is the door's answer.
type WriteResult struct {
	// Memory is the stored row for an added write, and the row that beat this
	// one for a skipped one.
	Memory Memory
	// Outcome is [WriteOutcomeAdded] or [WriteOutcomeSkipped].
	Outcome string
}

// The two outcomes of a write.
const (
	WriteOutcomeAdded   = "added"
	WriteOutcomeSkipped = "skipped-duplicate"
)

// Write validates and deduplicates under one immediate write transaction. Exact
// folded bodies within the same owner are duplicates. Validation comes before
// the lookup so an invalid request cannot become a successful skip.
func (s *Store) Write(req WriteRequest) (WriteResult, error) {
	owner := normalizeOwner(req.Owner)
	if !ValidOwner(owner) {
		return WriteResult{}, fmt.Errorf("write memory: %w: an owner is required and %q is not one this build mints", ErrInvalid, req.Owner)
	}
	payload, err := memoryPayloadFrom(Memory{
		Owner: owner, Type: req.Type, Text: req.Text, Tags: req.Tags,
		Origin: req.Origin, ProjectName: req.ProjectName,
	})
	if err != nil {
		return WriteResult{}, fmt.Errorf("write memory: %w", err)
	}
	payload.ID = NewMemoryID()
	payload.SourceSession = strings.TrimSpace(req.SourceSession)

	tx, err := s.beginWrite()
	if err != nil {
		return WriteResult{}, fmt.Errorf("write memory: %w", err)
	}
	defer tx.Rollback()
	existing, err := writeDuplicateOn(tx, owner, payload.Text)
	if err != nil {
		return WriteResult{}, fmt.Errorf("write memory: %w", err)
	}
	result := WriteResult{Outcome: WriteOutcomeAdded}
	if existing != nil {
		if _, _, err := appendEvent(tx, "memory", EventMemorySkipped, map[string]any{"owner": owner, "text": payload.Text, "kept": existing.ID}); err != nil {
			return WriteResult{}, fmt.Errorf("write memory: %w", err)
		}
		result = WriteResult{Memory: *existing, Outcome: WriteOutcomeSkipped}
	} else {
		seq, _, err := appendEvent(tx, payload.ID, EventMemoryAdd, payload)
		if err != nil {
			return WriteResult{}, fmt.Errorf("write memory: %w", err)
		}
		if err := applyMemoryAdd(tx, payload, seq); err != nil {
			return WriteResult{}, fmt.Errorf("write memory: %w", err)
		}
		rows, err := queryMemoriesOn(tx, "WHERE id = ?", []any{payload.ID}, "", 1)
		if err != nil {
			return WriteResult{}, fmt.Errorf("write memory: %w", err)
		}
		result.Memory = rows[0]
	}
	if err := tx.Commit(); err != nil {
		return WriteResult{}, fmt.Errorf("write memory: %w", err)
	}
	return result, nil
}

// writeDuplicateOn checks bodies in the transaction that admits the new row.
// It streams the owner's active rows, so an old duplicate obeys the same exact
// rule as a new one.
func writeDuplicateOn(tx *sql.Tx, owner, text string) (*Memory, error) {
	rows, err := tx.Query("SELECT id, text FROM memories WHERE owner = ? AND status = ? ORDER BY updated_seq DESC, id DESC", owner, MemoryActive)
	if err != nil {
		return nil, err
	}
	want := NormalizeMemoryWords(text)
	var id string
	for rows.Next() {
		var candidateID, body string
		if err := rows.Scan(&candidateID, &body); err != nil {
			rows.Close()
			return nil, err
		}
		if NormalizeMemoryWords(body) == want {
			id = candidateID
			break
		}
	}
	readErr := rows.Err()
	closeErr := rows.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if id == "" {
		return nil, nil
	}
	found, err := queryMemoriesOn(tx, "WHERE id = ?", []any{id}, "", 1)
	if err != nil {
		return nil, err
	}
	return &found[0], nil
}

// NormalizeMemoryWords folds the words a duplicate question is asked in: case
// and whitespace. It is the ONE spelling of "the same words", and it is
// deliberately weaker than a similarity score — two lines that are not the same
// words are not the same memory.
func NormalizeMemoryWords(text string) string {
	return strings.Join(strings.Fields(strings.ToLower(text)), " ")
}

// journalMemoryOutcome appends one memory journal event on its own transaction.
func (s *Store) journalMemoryOutcome(kind EventKind, payload any) (int64, time.Time, error) {
	tx, err := s.beginWrite()
	if err != nil {
		return 0, time.Time{}, err
	}
	defer tx.Rollback()
	seq, at, err := appendEvent(tx, "memory", kind, payload)
	if err != nil {
		return 0, time.Time{}, err
	}
	if err := tx.Commit(); err != nil {
		return 0, time.Time{}, err
	}
	return seq, at, nil
}

// JournalMemoryFailure records that a memory write did not land, with the
// reason, so a failure is readable at the store rather than lost at the door it
// happened at.
func (s *Store) JournalMemoryFailure(via string, err error) error {
	if err == nil {
		return nil
	}
	_, _, err = s.journalMemoryOutcome(EventMemoryWriteFailed, map[string]any{
		"via":   via,
		"error": err.Error(),
	})
	if err != nil {
		return fmt.Errorf("journal memory failure: %w", err)
	}
	return nil
}

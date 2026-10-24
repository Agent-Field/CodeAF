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
	Owner      string
	Durability string
	Type       string
	Text       string
	Tags       []string
	// SourceSession is the conversation that supplied the words, for the
	// memory place's card.
	SourceSession string
	// Origin is who is keeping it ([OriginPerson], [OriginModel],
	// [OriginNoticed]).
	Origin string
	// ProjectName is the name the person knows the project by, kept beside a
	// project memory so the memory place can say where it holds.
	ProjectName string
	// Unbounded is retained for import compatibility. All valid writes are unbounded.
	Unbounded bool
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
	WriteOutcomeDropped = "dropped-ephemeral"
	WriteOutcomeAdded   = "added"
	WriteOutcomeSkipped = "skipped-duplicate"
)

// Write validates and conservatively deduplicates within the named owner in
// one transaction. Storage is unbounded; only prompt admission has a budget.
func (s *Store) Write(req WriteRequest) (WriteResult, error) {
	if req.Durability == "ephemeral" {
		return WriteResult{Outcome: WriteOutcomeDropped}, nil
	}
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
	payload.Type = memoryLintType(payload.Type, payload.Text)
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
	var addedAt time.Time
	if existing != nil {
		if err := mergeMemoryWriteOn(tx, owner, payload, existing); err != nil {
			return WriteResult{}, err
		}
		result = WriteResult{Memory: *existing, Outcome: WriteOutcomeSkipped}
	} else {
		added, at, err := addMemoryOn(tx, owner, payload, req.Unbounded)
		if err != nil {
			return WriteResult{}, fmt.Errorf("write memory: %w", err)
		}
		result.Memory, addedAt = added, at
	}
	if err := tx.Commit(); err != nil {
		return WriteResult{}, fmt.Errorf("write memory: %w", err)
	}
	if result.Outcome == WriteOutcomeAdded {
		if err := s.seal(payload.ID, EventMemoryAdd, addedAt, payload); err != nil {
			return result, fmt.Errorf("write memory: %w", err)
		}
	}
	return result, nil
}

// addMemoryOn is the add half of [Store.Write], inside its transaction: the
// room check unless the request is unbounded, the add event and its
// projection, and the row read back. It answers the event's time too, because
// the seal made after the commit carries it.
func addMemoryOn(tx *sql.Tx, owner string, payload memoryPayload, unbounded bool) (Memory, time.Time, error) {
	if !unbounded {
		if err := memoryRoomOn(tx, owner, payload.Text, ""); err != nil {
			return Memory{}, time.Time{}, err
		}
	}
	seq, at, err := appendEvent(tx, payload.ID, EventMemoryAdd, payload)
	if err != nil {
		return Memory{}, time.Time{}, err
	}
	if err := applyMemoryAdd(tx, payload, seq); err != nil {
		return Memory{}, time.Time{}, err
	}
	rows, err := queryMemoriesOn(tx, "WHERE id = ?", []any{payload.ID}, "", 1)
	if err != nil {
		return Memory{}, time.Time{}, err
	}
	return rows[0], at, nil
}

// writeDuplicateOn probes indexed SimHash bands and verifies candidates with
// ordered words so punctuation repeats merge without changing meaning.
func writeDuplicateOn(tx *sql.Tx, owner, text string) (*Memory, error) {
	sig := memorySignature(memoryWords(text))
	rows, err := tx.Query(`SELECT m.id,m.text FROM memories m JOIN memory_fingerprints f ON f.memory_id=m.id WHERE m.owner=? AND m.status=? AND (f.b0=? OR f.b1=? OR f.b2=? OR f.b3=?) ORDER BY m.updated_seq DESC,m.id DESC`, owner, MemoryActive, int(sig&65535), int((sig>>16)&65535), int((sig>>32)&65535), int(sig>>48))
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
		if NormalizeMemoryWords(body) == want || memoryNearDuplicate(body, text) {
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

func mergeMemoryWriteOn(tx *sql.Tx, owner string, payload memoryPayload, existing *Memory) error {
	if _, _, err := appendEvent(tx, "memory", EventMemorySkipped, map[string]any{"owner": owner, "text": payload.Text, "kept": existing.ID}); err != nil {
		return err
	}
	if err := memoryInteractionOn(tx, existing.ID, "merged", payload.SourceSession, payload.Origin == OriginPerson); err != nil {
		return err
	}
	if payload.Origin == OriginPerson {
		existing.Origin = OriginPerson
	}
	return nil
}

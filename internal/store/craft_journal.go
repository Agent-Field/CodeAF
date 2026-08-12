package store

import (
	"context"
	"fmt"
	"strings"
)

// A learned way of working lands in a git repository, not in this database —
// that is the whole point of internal/craft, and a second copy of a workflow's
// versions here would be one more thing to keep true. What the repository
// cannot answer is WHEN, in the journal's own ordering, the resident learned
// something: every other thing it learns (a belief, a skill, a topic merged, an
// area formed) is an event with a sequence number, and the retrospective digest
// reads exactly that interval. A craft had no seq at all, so the one moment
// worth announcing — "I worked out how to do this and I'll work this way next
// time" — was structurally unsayable in the digest.
//
// This is that one moment, journaled and nothing more: a name, whether it was a
// first version or a better one, and the commit it landed as. The file, its
// steps, its history and its ceilings stay in the repository, which remains the
// only place they live.

// EventCraftForged records that the resident distilled a way of working into
// the craft repository.
const EventCraftForged EventKind = "craft_forged"

// CraftForged is one forging on the wire. Commit is the version it landed as,
// so a reader can line the announcement up against the repository's own
// history; Refined says whether this replaced a way of working that already
// existed, which is the difference between "learned how to" and "got better
// at" in every sentence written about it.
type CraftForged struct {
	Name    string `json:"name"`
	Commit  string `json:"commit,omitempty"`
	Refined bool   `json:"refined,omitempty"`
	// Because is the evidence line the commit carries, kept short. It is what
	// lets a digest say what the new version answers without a git call.
	Because string `json:"because,omitempty"`
}

// RecordCraftForged journals one forging. There is no view to materialize —
// the repository is the view — so this writes the event and stops, which is
// why replay decodes it and does nothing else.
func (s *Store) RecordCraftForged(forged CraftForged) (int64, error) {
	forged.Name = strings.TrimSpace(forged.Name)
	forged.Commit = strings.TrimSpace(forged.Commit)
	forged.Because = bounded(strings.TrimSpace(forged.Because), MaxDigestBytes)
	if forged.Name == "" {
		return 0, fmt.Errorf("record craft forged: %w: a craft has a name", ErrInvalid)
	}

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return 0, fmt.Errorf("record craft forged: %w", err)
	}
	defer tx.Rollback()

	seq, _, err := appendEvent(tx, "", EventCraftForged, forged)
	if err != nil {
		return 0, fmt.Errorf("record craft forged: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("record craft forged: %w", err)
	}
	return seq, nil
}

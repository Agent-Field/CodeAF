package store

import (
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

// ForgedCraftNames is every way of working this brain has learned, newest
// first. The repository is still the authority on what a craft IS — its steps,
// its versions, its ceilings — but the journal is the only place a process
// without the repository open can find out which names EXIST, and that turns
// out to be the question a conversation asks.
//
// It exists because the verb triad resolves one id against everything the
// person owns (internal/head/change.go): a job, a rule, a service, a way of
// working. Three of those the store can confirm. Without this the fourth had to
// be the fall-through — which made every mistyped job id a command against a
// workflow nobody has forged.
func (s *Store) ForgedCraftNames() ([]string, error) {
	rows, err := s.db.Query(`SELECT json_extract(payload, '$.name') FROM events
		WHERE kind=? ORDER BY seq DESC`, EventCraftForged)
	if err != nil {
		return nil, fmt.Errorf("forged craft names: %w", err)
	}
	defer rows.Close()

	names := make([]string, 0, 8)
	seen := make(map[string]bool, 8)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("forged craft names: %w", err)
		}
		if name = strings.TrimSpace(name); name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("forged craft names: %w", err)
	}
	return names, nil
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

	tx, err := s.beginWrite()
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

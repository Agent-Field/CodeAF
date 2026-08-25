package store

// THE MEMORY SNAPSHOT: everything remembered, in one reading a page can afford.
//
// The surface that draws memory today reads it the one way a surface on a
// three-second clock cannot: `ListMemories("", 500)` on the keystroke, then a
// provenance lookup PER MEMORY — up to five hundred and one round trips before
// the frame returns. That survives behind a slash command that opens a panel
// once. It does not survive on a page in a tab bar.
//
// So this is the whole shape in a FIXED NUMBER OF STATEMENTS: two, whatever a
// person has remembered. One counts, grouped in SQLite where counting belongs;
// one carries the newest rows. Neither grows a round trip when the store does,
// which is the property the "one query, not N" rule is actually about.
//
// IT IS THE FIRST READER IN THIS PACKAGE THAT SEES PAST `active`. Every other
// one — GetMemories, SearchMemories, ListMemories, MemoryIndex,
// MemoryCandidates — filters `status = active` in its own WHERE, which is right
// for a router that must not be told something the person let go of, and which
// meant that from outside this package there was no way to enumerate or even
// COUNT a forgotten memory at all. A page that says "41 let go" has to be able
// to see them.

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// memorySnapshotRows is how many memories one snapshot carries when the caller
// names no number. Five hundred is what the existing panel reads and far past
// what a page draws at once; the COUNTS are exact whatever this is, so a page
// showing a shelf of a thousand still says a thousand.
const memorySnapshotRows = 500

// MemoryShelf is one scope's worth of what is remembered.
//
// THE SHELVES ARE A CLOSED THREE AND CANNOT BE MORE. `scope` on a memory is an
// enum of exactly `user`, `project` and `env`, enforced on the way in — so a
// page drawing "86 shelves" would be drawing the OTHER product's fact table,
// where scopes are free-form strings. Three is the number, and it is worth a
// page saying so plainly rather than implying an open list.
//
// THERE IS NO PROJECT DIRECTORY ON A PROJECT MEMORY, and a shelf headed
// `project:/some/path` cannot be built from this table. The row carries the word
// `project` and nothing else: no workspace column, and its source session's row
// carries no workspace either. Which project a project-scoped memory belongs to
// is a fact this store has never kept — see [MemoryShelf.Scope].
type MemoryShelf struct {
	// Scope is `user`, `project` or `env` — the raw word, for a caller that
	// filters on it. [MemoryShelfWord] is what a person reads.
	Scope string
	// Label is the shelf's heading in words a person uses.
	Label string
	// Memories are this shelf's rows, NEWEST TOUCHED FIRST, and they are a
	// SAMPLE where the snapshot's limit bit: the counts below are over the whole
	// shelf and this list may be shorter than any of them.
	Memories []Memory
	// Held, LetGo and Superseded are this shelf's whole population by status —
	// `active`, `forgotten` and `superseded` in the words screen 2d uses. Every
	// memory on the shelf is in exactly one of the three.
	Held       int
	LetGo      int
	Superseded int
	// ByType counts every memory on the shelf by its kind — `fact`,
	// `preference`, `decision`, `correction`, `project_state` — whatever its
	// status, so a section line can say what a shelf is MADE of. A kind nobody
	// has used is absent rather than zero.
	ByType map[string]int
}

// MemoryShelves is the whole snapshot: the shelves in a fixed order, and the
// machine's own totals beside them.
type MemoryShelves struct {
	// Shelves are in the order `user`, `project`, `env`, and a shelf with
	// nothing on it is NOT here — an empty shelf is not a shelf. The three
	// scopes are a closed enum, so a page that wants to name a missing one can.
	Shelves []MemoryShelf
	// The machine's totals, by the same three statuses.
	Held       int
	LetGo      int
	Superseded int
	// Shown is how many rows the shelves actually carry, and Total is how many
	// exist. Shown < Total is the snapshot's limit biting, and a page quoting a
	// figure off the rows rather than off the counts has to know.
	Shown int
	Total int
}

// MemoryShelfWord is a shelf's heading in words a person uses.
//
// It is the SHORT form of the same three meanings internal/session's
// consolidateScopeWord spells out at length ("true only inside one project") —
// that one is prose written for a model reading a listing, this one is a column
// heading. Two audiences, two lengths, one meaning; a scope this build does not
// know reads as nothing rather than as itself.
func MemoryShelfWord(scope string) string {
	switch scope {
	case MemoryScopeUser:
		return "about you"
	case MemoryScopeProject:
		return "about a project"
	case MemoryScopeEnv:
		return "about this machine"
	}
	return ""
}

// MemorySnapshot is everything remembered, shelved, counted and bounded.
//
// limit caps how many ROWS come back in total, newest touched first across the
// whole store and then dealt onto their shelves; zero or less means
// [memorySnapshotRows]. The COUNTS never depend on it.
//
// It reads every status, which is the point: a page cannot say what was let go
// of by asking a reader that filters let-go rows out.
func (s *Store) MemorySnapshot(limit int) (MemoryShelves, error) {
	if limit <= 0 {
		limit = memorySnapshotRows
	}
	// STATEMENT ONE: the census. Grouping in SQLite rather than in Go is what
	// makes the counts exact at any size — counting in Go would mean carrying
	// every body across the wire to add up three integers, which is the defect
	// internal/command's notebook read already wrote down ("there is no count
	// read behind the facts table, so a full window reports itself as one").
	rows, err := s.db.Query(`
		SELECT scope, type, status, COUNT(*)
		FROM memories
		GROUP BY scope, type, status`)
	if err != nil {
		return MemoryShelves{}, fmt.Errorf("memory snapshot: %w", err)
	}
	defer rows.Close()
	shelves := map[string]*MemoryShelf{}
	var snapshot MemoryShelves
	for rows.Next() {
		var scope, memoryType, status string
		var count int
		if err := rows.Scan(&scope, &memoryType, &status, &count); err != nil {
			return MemoryShelves{}, fmt.Errorf("memory snapshot: %w", err)
		}
		shelf := shelves[scope]
		if shelf == nil {
			shelf = &MemoryShelf{Scope: scope, Label: MemoryShelfWord(scope), ByType: map[string]int{}}
			shelves[scope] = shelf
		}
		shelf.ByType[memoryType] += count
		switch status {
		case MemoryActive:
			shelf.Held += count
			snapshot.Held += count
		case MemoryForgotten:
			shelf.LetGo += count
			snapshot.LetGo += count
		case MemorySuperseded:
			shelf.Superseded += count
			snapshot.Superseded += count
		}
		snapshot.Total += count
	}
	if err := rows.Err(); err != nil {
		return MemoryShelves{}, fmt.Errorf("memory snapshot: %w", err)
	}

	// STATEMENT TWO: the newest rows, every status, through the one reader every
	// memory read in this package goes through — so a snapshot's rows and a
	// panel's rows can never come to decode differently.
	memories, err := s.queryMemories("", nil, `updated_seq DESC, id`, limit)
	if err != nil {
		return MemoryShelves{}, fmt.Errorf("memory snapshot: %w", err)
	}
	for _, memory := range memories {
		shelf := shelves[memory.Scope]
		if shelf == nil {
			// A row whose scope the census did not see cannot happen inside one
			// transaction-free read of a quiet store, and can between two of them
			// — a memory written between the statements. It gets a shelf rather
			// than being dropped: a row on screen with no count behind it is a
			// smaller lie than a row that vanished.
			shelf = &MemoryShelf{Scope: memory.Scope, Label: MemoryShelfWord(memory.Scope), ByType: map[string]int{}}
			shelves[memory.Scope] = shelf
		}
		shelf.Memories = append(shelf.Memories, memory)
		snapshot.Shown++
	}

	// THE ORDER IS THE ENUM'S AND NOT THE MAP'S, so two draws of one store put
	// the shelves in the same places.
	for _, scope := range []string{MemoryScopeUser, MemoryScopeProject, MemoryScopeEnv} {
		if shelf := shelves[scope]; shelf != nil {
			snapshot.Shelves = append(snapshot.Shelves, *shelf)
			delete(shelves, scope)
		}
	}
	// Anything left is a scope this build does not know — a row written by a
	// later build, or by hand. It is shown, after the three, in a settled order.
	var rest []string
	for scope := range shelves {
		rest = append(rest, scope)
	}
	sort.Strings(rest)
	for _, scope := range rest {
		snapshot.Shelves = append(snapshot.Shelves, *shelves[scope])
	}
	return snapshot, nil
}

// MemoryChangedSince is the "since you left" line's two figures: how many
// memories were learned after t, and how many were let go of after it.
//
// THE TIMESTAMP IS NOT ON THE MEMORY, and that is worth a caller knowing. The
// memories table carries `created_seq` and `updated_seq` — positions in the
// journal, not instants — so both figures come from joining each memory to the
// EVENT that wrote it. Two consequences follow and neither is a defect:
//
//   - A memory whose event predates the columns joins to nothing and is counted
//     as neither, which is right: nobody knows when it was learned, and unknown
//     provenance must not become "learned just now".
//   - "let go" is read off the memory's CURRENT status and its LAST event. A
//     memory forgotten and then restored is not counted, because it is not let
//     go of any more — the line is about what stands now, not about what
//     happened.
//
// A zero t answers zeros: with no origin there is no "since", and a first look
// that declared every memory ever learned to be news would be the delta with no
// delta in it (internal/session's look.go makes the same refusal).
//
// It is ONE statement, and it does not carry a single body across the wire.
func (s *Store) MemoryChangedSince(t time.Time) (learned, letGo int, err error) {
	if t.IsZero() {
		return 0, 0, nil
	}
	// The journal's own spelling: fixed-width RFC3339 in UTC, so the comparison
	// SQLite makes on the text is the comparison a clock would make.
	floor := formatTime(t)
	// A SUM over no rows is NULL, so both land in a nullable and an empty store
	// answers zeros rather than a scan failure: a person who has remembered
	// nothing is not a failed read.
	var since, released sql.NullInt64
	err = s.db.QueryRow(`
		SELECT
			SUM(CASE WHEN created.ts >= ? THEN 1 ELSE 0 END),
			SUM(CASE WHEN memories.status = ? AND updated.ts >= ? THEN 1 ELSE 0 END)
		FROM memories
		LEFT JOIN events AS created ON created.seq = memories.created_seq
		LEFT JOIN events AS updated ON updated.seq = memories.updated_seq`,
		floor, MemoryForgotten, floor).Scan(&since, &released)
	if err != nil {
		return 0, 0, fmt.Errorf("memory changed since: %w", err)
	}
	return int(since.Int64), int(released.Int64), nil
}

// MemoryShelfTypes is a shelf's kinds in a settled order — the section line's
// own reading, so that two draws of one shelf do not name its kinds in two
// orders. Kinds this build knows come first in the order the store declares
// them; anything else follows, sorted.
func MemoryShelfTypes(shelf MemoryShelf) []string {
	var kinds []string
	seen := map[string]bool{}
	for _, kind := range []string{MemoryFact, MemoryPreference, MemoryDecision, MemoryCorrection, MemoryProjectState} {
		if shelf.ByType[kind] > 0 {
			kinds = append(kinds, kind)
			seen[kind] = true
		}
	}
	var rest []string
	for kind, count := range shelf.ByType {
		if count > 0 && !seen[kind] && strings.TrimSpace(kind) != "" {
			rest = append(rest, kind)
		}
	}
	sort.Strings(rest)
	return append(kinds, rest...)
}

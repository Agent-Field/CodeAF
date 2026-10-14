package store

// THE MEMORY SNAPSHOT: everything remembered, in one reading the memory place
// can afford on its clock.
//
// It is shelved BY OWNER — the person, this machine, each project, and the
// quarantine of lines nobody could place — because the owner is what decides
// which conversations a line is put in front of, and a page that grouped by
// anything else would be drawing a fact nobody acts on. Each shelf's lines are
// in the order a conversation reads them (oldest first, [Store.MemoriesFor]'s
// order), so the page and the model never disagree about what comes first.
//
// The lines that were let go — forgotten, or replaced by a newer line — are a
// list of their own, newest first, because the one thing a person does with
// them is put one back.
//
// THE STATEMENTS DESCRIBE ONE MOMENT: they run inside a single deferred read
// transaction ([Store.beginRead]), so a line written between them cannot leave
// the counts describing one store and the rows another.

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
)

// memorySnapshotRows is how many active memories one snapshot carries when the
// caller names no number, and memorySnapshotLetGo how many let-go ones. The
// counts are exact whatever these are.
const (
	memorySnapshotRows  = 500
	memorySnapshotLetGo = 100
)

// MemoryShelf is one owner's active memories.
type MemoryShelf struct {
	// Owner is `user`, `machine`, `project:<key>` or the quarantine.
	Owner string
	// ProjectName is the newest name any of a project shelf's lines was kept
	// under, or "" for the other shelves and for lines older than the column.
	ProjectName string
	// Held is how many active lines the owner has — exact, even when Memories
	// was cut by the snapshot's limit.
	Held int
	// Memories are the owner's active lines, oldest first.
	Memories []Memory
}

// MemoryShelves is the whole snapshot.
type MemoryShelves struct {
	// Shelves come person, machine, projects by name, quarantine last. A shelf
	// with nothing on it is not here.
	Shelves []MemoryShelf
	// LetGo is the forgotten and replaced lines, newest first.
	LetGo []Memory
	// The store's totals.
	Held       int
	LetGoCount int
}

// MemorySnapshot is everything remembered, shelved and counted. limit caps the
// active rows carried; zero or less means [memorySnapshotRows].
func (s *Store) MemorySnapshot(limit int) (MemoryShelves, error) {
	if limit <= 0 {
		limit = memorySnapshotRows
	}
	tx, err := s.beginRead()
	if err != nil {
		return MemoryShelves{}, fmt.Errorf("memory snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	counts := map[string]int{}
	rows, err := tx.Query(`SELECT owner, COUNT(*) FROM memories WHERE status = ? GROUP BY owner`, MemoryActive)
	if err != nil {
		return MemoryShelves{}, fmt.Errorf("memory snapshot: %w", err)
	}
	held := 0
	for rows.Next() {
		var owner string
		var count int
		if err := rows.Scan(&owner, &count); err != nil {
			rows.Close()
			return MemoryShelves{}, fmt.Errorf("memory snapshot: %w", err)
		}
		counts[owner] = count
		held += count
	}
	if err := closeRows(rows); err != nil {
		return MemoryShelves{}, fmt.Errorf("memory snapshot: %w", err)
	}
	var letGoCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM memories WHERE status <> ?`, MemoryActive).Scan(&letGoCount); err != nil {
		return MemoryShelves{}, fmt.Errorf("memory snapshot: %w", err)
	}

	active, err := queryMemoriesOn(tx, `WHERE status = ?`, []any{MemoryActive}, `memories.created_seq, memories.id`, limit)
	if err != nil {
		return MemoryShelves{}, fmt.Errorf("memory snapshot: %w", err)
	}
	letGo, err := queryMemoriesOn(tx, `WHERE status <> ?`, []any{MemoryActive}, `memories.updated_seq DESC, memories.id`, memorySnapshotLetGo)
	if err != nil {
		return MemoryShelves{}, fmt.Errorf("memory snapshot: %w", err)
	}

	byOwner := map[string]*MemoryShelf{}
	for owner, count := range counts {
		byOwner[owner] = &MemoryShelf{Owner: owner, Held: count}
	}
	for _, memory := range active {
		shelf := byOwner[memory.Owner]
		if shelf == nil {
			shelf = &MemoryShelf{Owner: memory.Owner}
			byOwner[memory.Owner] = shelf
		}
		shelf.Memories = append(shelf.Memories, memory)
		if name := strings.TrimSpace(memory.ProjectName); name != "" {
			shelf.ProjectName = name
		}
	}
	shelves := make([]MemoryShelf, 0, len(byOwner))
	for _, shelf := range byOwner {
		shelves = append(shelves, *shelf)
	}
	sort.SliceStable(shelves, func(i, j int) bool {
		ri, rj := memoryShelfRank(shelves[i].Owner), memoryShelfRank(shelves[j].Owner)
		if ri != rj {
			return ri < rj
		}
		ni, nj := strings.ToLower(shelves[i].ProjectName), strings.ToLower(shelves[j].ProjectName)
		if ni != nj {
			return ni < nj
		}
		return shelves[i].Owner < shelves[j].Owner
	})
	return MemoryShelves{Shelves: shelves, LetGo: letGo, Held: held, LetGoCount: letGoCount}, nil
}

// memoryShelfRank is where an owner's shelf stands on the page.
func memoryShelfRank(owner string) int {
	switch {
	case owner == OwnerUser:
		return 0
	case owner == OwnerMachine:
		return 1
	case owner == OwnerLegacyProject:
		return 3
	}
	return 2
}

func closeRows(rows *sql.Rows) error {
	readErr := rows.Err()
	closeErr := rows.Close()
	if readErr != nil {
		return readErr
	}
	return closeErr
}

// MemoryChangedSince counts what changed in the store since t: lines learned
// (created at or after t) and lines let go of (forgotten at or after t). It is
// what home's "since you left" and the Memory tab's count say.
//
// A zero t answers zeros: with no origin there is no "since".
func (s *Store) MemoryChangedSince(t time.Time) (learned, letGo int, err error) {
	if t.IsZero() {
		return 0, 0, nil
	}
	floor := formatTime(t)
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

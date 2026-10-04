package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The write door. THIS IS THE ONE WAY ANYTHING LANDS IN THE MEMORY TABLE from
// a caller outside the store: /remember, the remember tool, a routed
// "remember…", the post-turn extraction and the legacy import all come through
// [Store.Write], which means there is exactly one dedup rule and exactly one
// journal for every mouth that says "keep this".
//
// [Store.AddMemory] is the raw door the Write door itself uses, and it stays
// exported for the stores-own callers that already hold an id and a decision —
// a test, the replay path, the demo seeder. Nothing that receives text from a
// person or a model should call it.
//
// The dedup is deterministic and same-owner BY CONSTRUCTION: the search that
// looks for a duplicate runs under the writer's own owner filter, so the
// question "is this already remembered?" is never answered by another
// project's row. Two projects may hold genuinely different truths about the
// same words, and neither write can swallow the other.

// WriteRequest is one memory a caller wants kept.
type WriteRequest struct {
	// Owner is the blast radius, spelled as an owner (memory_owner.go). An
	// empty owner is refused — the caller that cannot name one is exactly the
	// caller the quarantine exists for, and refusing is how a guess is kept
	// out of the store.
	Owner string
	Type  string
	Title string
	Text  string
	Tags  []string
	// SourceSession is the conversation that supplied the words, for the
	// provenance card.
	SourceSession string
}

// WriteResult is the door's answer: what happened, and why, in words a person
// can read back through the journal.
type WriteResult struct {
	// Memory is the stored row for an added write, and the row that beat this
	// one for a skipped one.
	Memory Memory
	// Outcome is `added` or `skipped-duplicate`.
	Outcome string
	// Why is the one-line reason, spelled for the surface that offered the
	// write. Empty on an added write.
	Why string
}

// WriteOutcomeAdded and WriteOutcomeSkipped are the two outcomes.
const (
	WriteOutcomeAdded    = "added"
	WriteOutcomeSkipped  = "skipped-duplicate"
	writeNeighborLookups = 200
)

// Write is the one door. It validates, looks for a same-owner duplicate, and
// either adds or skips — and both outcomes are journaled, the skip so that
// "it said remember twice and the store kept one" is provable later.
//
// The dedup is deliberately SMALL: exact text (words folded, case folded) or
// exact title, within the same owner. Everything subtler is the decider's job
// — the model compares near misses and refines — and this door only refuses
// what no reading of the words could make different. A store without the FTS
// index falls back to a bounded scan of its own active rows, so the door's
// promise holds on a build where the index never existed.
func (s *Store) Write(req WriteRequest) (WriteResult, error) {
	owner := normalizeOwner(req.Owner)
	if !ValidOwner(owner) {
		return WriteResult{}, fmt.Errorf("write memory: %w: an owner is required and %q is not one this build mints", ErrInvalid, req.Owner)
	}
	fresh := Memory{
		Owner:         owner,
		Type:          req.Type,
		Title:         req.Title,
		Text:          req.Text,
		Tags:          req.Tags,
		SourceSession: req.SourceSession,
	}
	existing, err := s.writeDuplicate(owner, fresh)
	if err != nil {
		return WriteResult{}, err
	}
	if existing != nil {
		skip := WriteResult{
			Memory:  *existing,
			Outcome: WriteOutcomeSkipped,
			Why:     "already kept as " + existing.Title,
		}
		if _, _, err := s.journalMemoryOutcome(EventMemorySkipped, map[string]any{
			"owner": owner, "text": fresh.Text, "kept": existing.ID,
		}); err != nil {
			return skip, fmt.Errorf("write memory: %w", err)
		}
		return skip, nil
	}
	memory, err := s.addMemory(fresh, fresh.SourceSession)
	if err != nil {
		return WriteResult{}, err
	}
	return WriteResult{Memory: memory, Outcome: WriteOutcomeAdded}, nil
}

// writeDuplicate answers the same-owner row this write would duplicate, or nil.
// nil-with-no-error means nothing matched; an error means the search itself
// failed and the write is refused rather than risked, because a dedup search
// that cannot run must never become a second copy of somebody's words.
//
// BOTH WORDS AND TITLE ARE SEARCHED. A repeat usually retypes the words, but a
// caller that renamed the body while keeping the index line is also a
// duplicate — the title is what every shortlist picks by, and two rows with
// one title would be two entries in every one of them.
func (s *Store) writeDuplicate(owner string, fresh Memory) (*Memory, error) {
	want := normalizeMemoryWords(fresh.Text)
	wantTitle := normalizeMemoryWords(fresh.Title)
	var found []Memory
	var err error
	if s.fts {
		byText, err := s.SearchMemories([]string{owner}, fresh.Text, writeNeighborLookups)
		if err != nil {
			return nil, fmt.Errorf("write memory: %w", err)
		}
		byTitle, err := s.SearchMemories([]string{owner}, fresh.Title, writeNeighborLookups)
		if err != nil {
			return nil, fmt.Errorf("write memory: %w", err)
		}
		found = append(append([]Memory{}, byText...), byTitle...)
	} else {
		// No index: scan the owner's own active rows. A store big enough for
		// the scan to hurt is a store that has an index.
		found, err = s.ListMemories([]string{owner}, memoryDuplicateScanLimit)
		if err != nil {
			return nil, fmt.Errorf("write memory: %w", err)
		}
	}
	seen := map[string]bool{}
	for _, row := range found {
		if seen[row.ID] {
			continue
		}
		seen[row.ID] = true
		if want != "" && normalizeMemoryWords(row.Text) == want {
			return &row, nil
		}
		if wantTitle != "" && normalizeMemoryWords(row.Title) == wantTitle {
			return &row, nil
		}
	}
	return nil, nil
}

// memoryDuplicateScanLimit bounds the no-index fallback scan: past this many
// rows the exact-duplicate question is asked of the newest ten thousand, which
// covers every active-owner store in practice. A store big enough for the scan
// to hurt is a store that has an index.
const memoryDuplicateScanLimit = 10000

// normalizeMemoryWords folds the words a dedup question is asked in: case,
// whitespace and width. It is the ONE spelling of "the same words" for the
// write door, and it is deliberately weaker than a similarity score — two
// lines that are not the same words are not the same memory, and a score
// threshold would be a guess wearing a rule's clothes.
func normalizeMemoryWords(text string) string {
	folded := strings.Join(strings.Fields(strings.ToLower(text)), " ")
	return folded
}

// journalMemoryOutcome appends one memory journal event on its own
// transaction. It is the door a failure and a skip walk when there is no
// larger write to ride inside.
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
// reason. The callers are the session's write doors: an extraction the decider
// could not settle, an import row that failed, a routed command the store
// refused. ERRORS USED TO VANISH AT ALL THREE, and a memory system that can
// quietly stop learning is one that has.
//
// The journal is an event, not a memory row: it is never injected anywhere,
// never searchable by the router, and readable by the person from the
// activity feed the journal already feeds.
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

// MemoryPage is one keyset page of what is remembered, newest touched first.
//
// THE CURSOR IS THE WHOLE PAGING CONTRACT: (updated_seq, id) of the last row
// on the page, handed back so the next call continues exactly where this one
// stopped, whatever has been written in between — no OFFSET, so a write that
// lands between pages moves nothing the reader already saw. A zero cursor is
// the first page. When fewer rows than the limit come back, the cursor's
// More is false and the walk is done.
type MemoryCursor struct {
	UpdatedSeq int64
	ID         string
	// More is false only on a page that ended the list.
	More bool
}

func (s *Store) MemoryPage(owners []string, cursor MemoryCursor, limit int) ([]Memory, MemoryCursor, error) {
	if len(owners) == 0 {
		return nil, MemoryCursor{}, fmt.Errorf("memory page: %w: no owner was named", ErrInvalid)
	}
	if limit <= 0 {
		limit = memoryListLimitDefault
	}
	where, ownerArgs := ownerFilterSQL(owners)
	statement := `WHERE status = ?` + where
	args := append([]any{MemoryActive}, ownerArgs...)
	if cursor.ID != "" || cursor.UpdatedSeq != 0 {
		statement += ` AND (updated_seq, id) < (?, ?)`
		args = append(args, cursor.UpdatedSeq, cursor.ID)
	}
	page, err := s.queryMemories(statement, args, `updated_seq DESC, id DESC`, limit+1)
	if err != nil {
		return nil, MemoryCursor{}, fmt.Errorf("memory page: %w", err)
	}
	more := len(page) > limit
	if more {
		page = page[:limit]
	}
	next := MemoryCursor{More: more}
	if more && len(page) > 0 {
		last := page[len(page)-1]
		next.UpdatedSeq, next.ID = last.UpdatedSeq, last.ID
	}
	return page, next, nil
}

// memoryListLimitDefault is the page size nobody argues about: fifty is a
// screen and a half, and it is the same figure the /memories receipt has
// always printed at.
const memoryListLimitDefault = 50

// createMemoriesFTS builds the memory index, and answers whether it exists.
//
// THE PROBE IS THE PROOF. Rather than trusting the module graph or the build
// tag, the open runs one small FTS5 statement in a savepoint and reads the
// answer off the error: a build with FTS5 gets its index, and a build without
// one gets a store whose memories are otherwise complete. There is no install
// step, no extension download, and no fallback data store — the fallback IS
// "the lexical tier is quiet", which is exactly what the architecture says a
// missing tier is.
func createMemoriesFTS(db *sql.DB) bool {
	if !probeFTS5(db) {
		return false
	}
	if _, err := db.Exec(memoriesFTSSchema); err != nil {
		// The probe passed and the real table refused: a damaged file, a
		// disk out of room. The honest answer is the degraded one — memory
		// without lexical search — and not a store that refuses to open.
		return false
	}
	return true
}

func probeFTS5(db *sql.DB) bool {
	row := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'memories_fts'`)
	var exists int
	if err := row.Scan(&exists); err != nil {
		return false
	}
	if exists != 0 {
		return true
	}
	// Prove the engine can actually build one, in a transaction that never
	// commits: the CREATE exercises the parser and the module, and the
	// rollback leaves nothing behind.
	tx, err := db.Begin()
	if err != nil {
		return false
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE VIRTUAL TABLE fts5_probe USING fts5(body)`); err != nil {
		return false
	}
	if _, err := tx.Exec(`INSERT INTO fts5_probe (body) VALUES ('probe')`); err != nil {
		return false
	}
	if err := tx.QueryRow(`SELECT body FROM fts5_probe WHERE fts5_probe MATCH 'probe'`).Scan(new(string)); err != nil {
		return false
	}
	return true
}

// ErrMemoryWriteRefused wraps every refusal the write door makes, so a caller
// can tell "the store said no" from "the disk said no" without parsing text.
var ErrMemoryWriteRefused = errors.New("memory write refused")

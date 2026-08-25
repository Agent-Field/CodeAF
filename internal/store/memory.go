package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Memories are what one session knows and the next one would otherwise have to
// be told again.
//
// The notebook (facts.go) distils beliefs out of finished WORK: a fact is
// something the graph learned by running. A memory is something a person said,
// decided, corrected or is in the middle of — it arrives in conversation, it is
// addressed by a short title rather than found by a retrieval sweep, and it is
// carried across sessions by a router that is shown a SHORTLIST of titles —
// eight of them, ranked here in SQL ([Store.MemoryCandidates]) — and asks for
// the two or three that bear on the moment.
//
// It is event-sourced like everything else here, and for the same reason: the
// events table is truth, the memories table and memories_fts are materialized
// views refreshed inside the same write transaction as the event that changed
// them, and Rebuild reproduces both by replaying the journal. Nothing is ever
// deleted — a forgotten memory is a tombstone with its row intact, because
// "the user told me to forget this" is itself a thing worth being able to
// prove later.
const (
	// EventMemoryAdd carries a whole new memory row.
	EventMemoryAdd EventKind = "memory_add"
	// EventMemoryUpdate carries an id and the new title, text and tags. It is a
	// correction in place: the memory is still the same memory, and its id,
	// type, scope and use count survive.
	EventMemoryUpdate EventKind = "memory_update"
	// EventMemorySupersede retires one memory in favour of another in a single
	// event, because the two halves are one decision. Two events — forget the
	// old, add the new — would let a replay stop between them and leave the
	// brain believing nothing at all about the subject.
	EventMemorySupersede EventKind = "memory_supersede"
	// EventMemoryForget is a tombstone and never a deletion. The row stays for
	// audit; every view and every search excludes it from the moment this
	// lands.
	EventMemoryForget EventKind = "memory_forget"
	// EventMemoryRestore returns one forgotten memory to the active views.
	EventMemoryRestore EventKind = "memory_restore"
	// EventMemoryRanking carries a periodic SNAPSHOT of the two ranking
	// counters — how often each memory helped, and how often it was put in
	// front of a model and did not.
	//
	// It exists because those counters are now load-bearing: [Store.MemoryCandidates]
	// ranks on them, so a Rebuild that reset them to zero would not merely lose
	// telemetry, it would change which eight rows the router is shown. One event
	// per retrieval is still the wrong answer for the reason [Memory.UseCount]
	// gives — it would bury the five events that carry meaning under thousands
	// that carry none — so the journal takes a snapshot on an interval instead
	// and a replay lands on that floor rather than on nothing.
	EventMemoryRanking EventKind = "memory_ranking"
)

// The five kinds of thing worth remembering across sessions. They are separate
// because the router treats them differently — a correction outranks a fact
// about the same subject, and project_state goes stale in a way a preference
// never does.
const (
	MemoryFact         = "fact"
	MemoryPreference   = "preference"
	MemoryDecision     = "decision"
	MemoryCorrection   = "correction"
	MemoryProjectState = "project_state"
)

// A memory's scope is the blast radius of its truth: something true about the
// person everywhere, something true only inside this project, or something true
// only of this machine.
const (
	MemoryScopeUser    = "user"
	MemoryScopeProject = "project"
	MemoryScopeEnv     = "env"
)

// The three states a memory row can be in. Only active is ever visible: the
// other two are history that a view has stopped agreeing with.
const (
	MemoryActive     = "active"
	MemorySuperseded = "superseded"
	MemoryForgotten  = "forgotten"
)

// The caps are on what a memory may WEIGH, not on what it may say, and they are
// enforced in Go rather than in SQL so the refusal can name which field was too
// long instead of surfacing a constraint violation.
//
// A title is an index line — the router picks by it and never sees the body —
// so a title that needs a second line has already failed at its job. Text is
// one line of substance. Eight tags is more than any memory has
// ever needed and is a bound on nonsense rather than on expression.
const (
	MemoryTitleRunes = 80
	MemoryTextRunes  = 512
	MemoryMaxTags    = 8
)

// Memory is one durable thing known across sessions.
type Memory struct {
	ID     string
	Type   string
	Scope  string
	Title  string
	Text   string
	Tags   []string
	Status string
	// UseCount is how often this memory HELPED, and MissCount is how often it
	// was put in front of a model and bore on nothing.
	//
	// THE COUNTER MEASURES HELP, NOT INJECTION. It used to be incremented for
	// every id the router named, which credited a memory for being retrieved
	// rather than for being worth retrieving — RoMeRL (arXiv 2608.02508) names
	// that the "memory-reward trap": when several memories are co-retrieved,
	// all of them receive credit. So the two counters are written together,
	// from one confirmation after the turn, and they are the same bargain
	// internal/session's fixstore.go already keeps for a suggested fix: a
	// patch that was offered and then failed is counted AGAINST itself, or the
	// store would rank a line that has never once mattered at the top of its
	// own ranking forever.
	//
	// Neither is journaled per write, for the reason [EventMemoryRanking]
	// states; a snapshot on an interval is what carries them through a Rebuild.
	UseCount  int
	MissCount int
	// UpdatedAt is when the event that last touched this memory was journaled —
	// transaction time, resolved from updated_seq inside the same statement
	// that reads the row rather than by a second read per row.
	//
	// It is what lets a reader SHOW a memory's age. A model cannot judge
	// staleness it cannot see, and LongMemEval (ICLR 2025) measures time-aware
	// expansion as the single largest category lever it ablated (+11.3%
	// recall). Zero is unknown provenance — an old row whose event predates the
	// column — and renders as nothing.
	UpdatedAt     time.Time
	CreatedSeq    int64
	UpdatedSeq    int64
	SourceSession string
	SourceSeq     int64
}

// MemoryStub is one line of the router's shortlist: enough to decide whether a
// memory bears on the moment, and nothing more. The full text is a second call
// away on purpose — the shortlist is read every turn and the bodies are not.
type MemoryStub struct{ ID, Title, Type, Scope string }

// memories_fts is a materialized search view rather than an external-content
// table, for the same reason graph_fts and messages_fts are: replacement is
// explicit, it happens inside the event transaction that changed the memory,
// and Rebuild reproduces it by replay through the ordinary view function.
//
// Only active memories are indexed. That is what makes "excluded from every
// view and search" a property of the write rather than a filter every reader
// has to remember — a superseded or forgotten row leaves the index at the
// moment it stops being true.
const memoriesSchema = `
CREATE TABLE IF NOT EXISTS memories (
    id          TEXT PRIMARY KEY,
    type        TEXT NOT NULL,
    scope       TEXT NOT NULL,
    title       TEXT NOT NULL,
    text        TEXT NOT NULL,
    tags        TEXT NOT NULL DEFAULT '[]',
    status      TEXT NOT NULL DEFAULT 'active',
    use_count   INTEGER NOT NULL DEFAULT 0,
    miss_count  INTEGER NOT NULL DEFAULT 0,
    created_seq    INTEGER NOT NULL,
    updated_seq    INTEGER NOT NULL,
    source_session TEXT NOT NULL DEFAULT '',
    source_seq     INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS memories_status_updated ON memories (status, updated_seq DESC);
CREATE INDEX IF NOT EXISTS memories_status_scope ON memories (status, scope, updated_seq DESC);
CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
    memory_id UNINDEXED,
    title,
    text,
    tags
);
`

func migrateMemoriesSchema(db *sql.DB) error {
	for _, column := range []struct{ name, declaration string }{
		{"source_session", `ALTER TABLE memories ADD COLUMN source_session TEXT NOT NULL DEFAULT ''`},
		{"source_seq", `ALTER TABLE memories ADD COLUMN source_seq INTEGER NOT NULL DEFAULT 0`},
		{"miss_count", `ALTER TABLE memories ADD COLUMN miss_count INTEGER NOT NULL DEFAULT 0`},
	} {
		found, err := tableHasColumn(db, "memories", column.name)
		if err != nil {
			return err
		}
		if !found {
			if _, err := db.Exec(column.declaration); err != nil {
				return err
			}
		}
	}
	return nil
}

type memoryPayload struct {
	ID            string   `json:"id"`
	Type          string   `json:"type"`
	Scope         string   `json:"scope"`
	Title         string   `json:"title"`
	Text          string   `json:"text"`
	Tags          []string `json:"tags,omitempty"`
	SourceSession string   `json:"source_session,omitempty"`
}

type memoryUpdatePayload struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Text          string   `json:"text"`
	Tags          []string `json:"tags,omitempty"`
	SourceSession string   `json:"source_session,omitempty"`
}

type memorySupersedePayload struct {
	OldID         string        `json:"old_id"`
	New           memoryPayload `json:"new"`
	SourceSession string        `json:"source_session,omitempty"`
}

type memoryForgetPayload struct {
	ID            string `json:"id"`
	SourceSession string `json:"source_session,omitempty"`
}

type memoryRestorePayload struct {
	ID string `json:"id"`
}

// memoryRankingPayload is one photograph of the ranking counters. Rows whose
// counters are both zero are absent rather than listed as zeroes — that is the
// emptiness law spelled in a journal, and it is also what keeps the snapshot
// small in the store where most memories have never been retrieved.
type memoryRankingPayload struct {
	Counts []memoryRankingCount `json:"counts"`
}

type memoryRankingCount struct {
	ID   string `json:"id"`
	Use  int    `json:"use,omitempty"`
	Miss int    `json:"miss,omitempty"`
}

// NewMemoryID mints a memory's name: sortable by the millisecond it was made,
// unique by eight random bytes after it.
//
// The time prefix is fixed width on purpose. A base-36 stamp that grows a digit
// would sort every id minted before the rollover after every id minted after
// it, and an id that is only sometimes ordered is worse than one that never
// claimed to be. Nine digits carry the clock past the year 3000.
func NewMemoryID() string {
	stamp := strconv.FormatInt(time.Now().UTC().UnixMilli(), 36)
	for len(stamp) < 9 {
		stamp = "0" + stamp
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err == nil {
		return "mem_" + stamp + hex.EncodeToString(random[:])
	}
	// The same fallback NewSessionID takes: a clock reading is not a random
	// number, but it is a name, and a store that cannot mint one is worse than
	// a store whose ids are briefly guessable.
	return "mem_" + stamp + fmt.Sprintf("%016x", time.Now().UnixNano())
}

// AddMemory journals one new memory and materializes it in the same
// transaction. The returned Memory is the stored row — id minted if the caller
// left it empty, status active, both sequences set to the event that created
// it.
func (s *Store) AddMemory(m Memory) (Memory, error) {
	return s.addMemory(m, m.SourceSession)
}

func (s *Store) addMemory(m Memory, sourceSession string) (Memory, error) {
	payload, err := memoryPayloadFrom(m)
	if err != nil {
		return Memory{}, fmt.Errorf("add memory: %w", err)
	}
	if payload.ID == "" {
		payload.ID = NewMemoryID()
	}
	payload.SourceSession = strings.TrimSpace(sourceSession)
	tx, err := s.beginWrite()
	if err != nil {
		return Memory{}, fmt.Errorf("add memory: %w", err)
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM memories WHERE id = ?`, payload.ID).Scan(&exists); err != nil {
		return Memory{}, fmt.Errorf("add memory: %w", err)
	}
	if exists != 0 {
		return Memory{}, fmt.Errorf("add memory: %w: %q already exists", ErrInvalid, payload.ID)
	}
	seq, _, err := appendEvent(tx, payload.ID, EventMemoryAdd, payload)
	if err != nil {
		return Memory{}, fmt.Errorf("add memory: %w", err)
	}
	if err := applyMemoryAdd(tx, payload, seq); err != nil {
		return Memory{}, fmt.Errorf("add memory: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Memory{}, fmt.Errorf("add memory: %w", err)
	}
	return Memory{
		ID: payload.ID, Type: payload.Type, Scope: payload.Scope,
		Title: payload.Title, Text: payload.Text, Tags: payload.Tags,
		Status: MemoryActive, CreatedSeq: seq, UpdatedSeq: seq,
		SourceSession: payload.SourceSession, SourceSeq: memorySourceSeq(payload.SourceSession, seq),
	}, nil
}

// UpdateMemory corrects a memory in place. Type and scope are not arguments
// because a memory that changed either of those is a different memory and
// wants SupersedeMemory: the whole point of an update is that the router's
// existing pointers to this id stay valid.
func (s *Store) UpdateMemory(id, title, text string, tags []string) error {
	return s.UpdateMemoryFromSession(id, title, text, tags, "")
}

// UpdateMemoryFromSession records which conversation supplied the correction.
func (s *Store) UpdateMemoryFromSession(id, title, text string, tags []string, sourceSession string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("update memory: %w: id is required", ErrInvalid)
	}
	title, text, tags, err := validMemoryBody(title, text, tags)
	if err != nil {
		return fmt.Errorf("update memory: %w", err)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("update memory: %w", err)
	}
	defer tx.Rollback()

	payload := memoryUpdatePayload{ID: id, Title: title, Text: text, Tags: tags, SourceSession: strings.TrimSpace(sourceSession)}
	seq, _, err := appendEvent(tx, id, EventMemoryUpdate, payload)
	if err != nil {
		return fmt.Errorf("update memory: %w", err)
	}
	if err := applyMemoryUpdate(tx, payload, seq); err != nil {
		return fmt.Errorf("update memory: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("update memory: %w", err)
	}
	return nil
}

// SupersedeMemory retires one memory and admits its replacement as one event,
// so no replay and no reader can ever observe the gap between them.
func (s *Store) SupersedeMemory(oldID string, m Memory) (Memory, error) {
	oldID = strings.TrimSpace(oldID)
	if oldID == "" {
		return Memory{}, fmt.Errorf("supersede memory: %w: id is required", ErrInvalid)
	}
	fresh, err := memoryPayloadFrom(m)
	if err != nil {
		return Memory{}, fmt.Errorf("supersede memory: %w", err)
	}
	if fresh.ID == "" {
		fresh.ID = NewMemoryID()
	}
	if fresh.ID == oldID {
		return Memory{}, fmt.Errorf("supersede memory: %w: %q cannot supersede itself", ErrInvalid, oldID)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return Memory{}, fmt.Errorf("supersede memory: %w", err)
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM memories WHERE id = ?`, fresh.ID).Scan(&exists); err != nil {
		return Memory{}, fmt.Errorf("supersede memory: %w", err)
	}
	if exists != 0 {
		return Memory{}, fmt.Errorf("supersede memory: %w: %q already exists", ErrInvalid, fresh.ID)
	}
	fresh.SourceSession = strings.TrimSpace(m.SourceSession)
	payload := memorySupersedePayload{OldID: oldID, New: fresh, SourceSession: fresh.SourceSession}
	seq, _, err := appendEvent(tx, fresh.ID, EventMemorySupersede, payload)
	if err != nil {
		return Memory{}, fmt.Errorf("supersede memory: %w", err)
	}
	if err := applyMemorySupersede(tx, payload, seq); err != nil {
		return Memory{}, fmt.Errorf("supersede memory: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Memory{}, fmt.Errorf("supersede memory: %w", err)
	}
	return Memory{
		ID: fresh.ID, Type: fresh.Type, Scope: fresh.Scope,
		Title: fresh.Title, Text: fresh.Text, Tags: fresh.Tags,
		Status: MemoryActive, CreatedSeq: seq, UpdatedSeq: seq,
		SourceSession: fresh.SourceSession, SourceSeq: memorySourceSeq(fresh.SourceSession, seq),
	}, nil
}

// ForgetMemory tombstones a memory. It refuses a memory that is already
// forgotten rather than returning quietly: "forget that" is an instruction a
// person expects to have had an effect, and a silent success on a row that was
// already gone is the store agreeing with something that did not happen.
func (s *Store) ForgetMemory(id string) error {
	return s.ForgetMemoryFromSession(id, "")
}

// ForgetMemoryFromSession records the conversation that issued the tombstone.
func (s *Store) ForgetMemoryFromSession(id, sourceSession string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("forget memory: %w: id is required", ErrInvalid)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("forget memory: %w", err)
	}
	defer tx.Rollback()

	payload := memoryForgetPayload{ID: id, SourceSession: strings.TrimSpace(sourceSession)}
	seq, _, err := appendEvent(tx, id, EventMemoryForget, payload)
	if err != nil {
		return fmt.Errorf("forget memory: %w", err)
	}
	if err := applyMemoryForget(tx, payload, seq); err != nil {
		return fmt.Errorf("forget memory: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("forget memory: %w", err)
	}
	return nil
}

// RestoreMemory returns a forgotten memory to the active views. Superseded
// memories remain retired because their replacement is still the store's truth.
func (s *Store) RestoreMemory(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("restore memory: %w: id is required", ErrInvalid)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("restore memory: %w", err)
	}
	defer tx.Rollback()
	payload := memoryRestorePayload{ID: id}
	seq, _, err := appendEvent(tx, id, EventMemoryRestore, payload)
	if err != nil {
		return fmt.Errorf("restore memory: %w", err)
	}
	if err := applyMemoryRestore(tx, payload, seq); err != nil {
		return fmt.Errorf("restore memory: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("restore memory: %w", err)
	}
	return nil
}

// GetMemories reads full memories by id, in the order asked for.
//
// Input order is the contract because the caller is a router that has already
// decided what matters most; re-sorting its choice by anything the store knows
// would discard the one ranking in the system that saw the actual question. Ids
// that name nothing, or name something no longer active, are simply absent —
// a memory the user forgot between the index read and this call must not
// reappear because a pointer to it survived.
func (s *Store) GetMemories(ids []string) ([]Memory, error) {
	wanted := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		wanted = append(wanted, id)
	}
	if len(wanted) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(wanted)), ",")
	args := make([]any, 0, len(wanted)+1)
	for _, id := range wanted {
		args = append(args, id)
	}
	args = append(args, MemoryActive)
	found, err := s.queryMemories(
		`WHERE id IN (`+placeholders+`) AND status = ?`, args, "", 0)
	if err != nil {
		return nil, fmt.Errorf("get memories: %w", err)
	}
	byID := make(map[string]Memory, len(found))
	for _, memory := range found {
		byID[memory.ID] = memory
	}
	ordered := make([]Memory, 0, len(found))
	for _, id := range wanted {
		if memory, ok := byID[id]; ok {
			ordered = append(ordered, memory)
		}
	}
	return ordered, nil
}

// SearchMemories finds active memories by their words, best match first.
//
// Ranking is bm25 with the more recently touched memory as the tiebreak. The
// tiebreak is not a term: a search that quietly preferred recent memories would
// answer "what did we decide about pricing" with whatever was said this
// morning, which is the failure the store exists to prevent.
//
// A query nothing can be made of — punctuation, or nothing but words shorter
// than the index keeps — is a miss and not an error. ftsQueryFrom is what makes
// that safe to say: it reduces the caller's words to quoted alphanumeric terms
// joined by OR, so hostile FTS syntax never reaches MATCH and a failure here is
// a real failure worth returning.
func (s *Store) SearchMemories(query string, limit int) ([]Memory, error) {
	terms := ftsQueryFrom(query)
	if terms == "" {
		return nil, nil
	}
	memories, err := s.queryMemories(`
		JOIN memories_fts ON memories_fts.memory_id = memories.id
		WHERE memories_fts MATCH ? AND memories.status = ?`,
		[]any{terms, MemoryActive},
		`bm25(memories_fts), memories.updated_seq DESC`, limit)
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}
	return memories, nil
}

// ListMemories returns active memories newest-touched first. An empty scope
// means every scope, which is what a person means by "what do you remember".
func (s *Store) ListMemories(scope string, limit int) ([]Memory, error) {
	where := `WHERE status = ?`
	args := []any{MemoryActive}
	if scope = strings.TrimSpace(scope); scope != "" {
		where += ` AND scope = ?`
		args = append(args, scope)
	}
	memories, err := s.queryMemories(where, args, `updated_seq DESC, id`, limit)
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}
	return memories, nil
}

// MemoryIndex is the whole of what is remembered as title-sized lines: one per
// active memory, most-used first and newest-touched within a tie.
//
// IT IS NO LONGER WHAT THE ROUTER READS. Handing every title to a model on
// every message costs ~3,200 tokens at a two-hundred-memory store and grows
// linearly with what a person has remembered, so the per-turn read is
// [Store.MemoryCandidates] — a shortlist ranked here rather than a dump. This
// stays because a pass that wants to look at ALL the titles at once, off the
// person's path, wants exactly this shape and nothing bigger.
//
// It is a separate read from ListMemories rather than a projection of it
// because carrying five hundred bodies to render five hundred titles is how a
// memory store becomes the most expensive thing in the loop.
func (s *Store) MemoryIndex(limit int) ([]MemoryStub, error) {
	// THE INDEX IS A RANKING, NOT A CHRONOLOGY. Memories that have proved useful
	// lead; updated sequence only settles equal-use rows.
	statement := `
		SELECT id, title, type, scope FROM memories
		WHERE status = ?
		ORDER BY use_count DESC, updated_seq DESC, id`
	args := []any{MemoryActive}
	if limit > 0 {
		statement += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(statement, args...)
	if err != nil {
		return nil, fmt.Errorf("memory index: %w", err)
	}
	defer rows.Close()
	stubs := make([]MemoryStub, 0, 16)
	for rows.Next() {
		var stub MemoryStub
		if err := rows.Scan(&stub.ID, &stub.Title, &stub.Type, &stub.Scope); err != nil {
			return nil, fmt.Errorf("memory index: %w", err)
		}
		stubs = append(stubs, stub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory index: %w", err)
	}
	return stubs, nil
}

// MemoryCandidatesDefault is how many rows the router is shown when a caller
// does not name a number.
//
// EIGHT, AND DELIBERATELY NOT MORE. LongMemEval's own Table 10 measures k=5→10
// over long retrieval units as a SIX-POINT LOSS on a small model, and the
// injected-memory literature is unanimous that one plausible-but-wrong line is
// expensive: a single top-retrieved non-answer document costs 18–20% relative
// (Cuconasu et al., SIGIR 2024). A longer shortlist is not a safer one.
const MemoryCandidatesDefault = 8

// rrfK is Reciprocal Rank Fusion's one constant, from Cormack, Clarke and
// Buettcher (SIGIR 2009): score a document as the sum of 1/(k + rank) over
// every ranking it appears in, with k = 60.
//
// DO NOT TUNE IT. That is not deference to the paper — their own Table 1 shows
// mean average precision moving 0.24% across k from 10 to 100, so there is
// almost nothing to win, and Bruch, Gai and Ingber (ACM TOIS 2023, arXiv
// 2210.11934) measured *tuned* RRF generalising badly off the corpus it was
// tuned on (HotpotQA .675 → .621). A constant that cannot be tuned cannot be
// overfitted to one person's store.
const rrfK = 60

// MemoryCandidates is the router's shortlist: the few remembered lines most
// likely to bear on what was just typed, ranked here in SQL and handed to a
// model to REJECT rather than to search.
//
// The ranking is Reciprocal Rank Fusion over the three orderings this table
// already has, and they are chosen because they fail in different directions —
// which is the only condition under which fusion is worth anything:
//
//   - RELEVANCE, bm25(memories_fts) over the words of the message. ftsQueryFrom
//     ORs its terms rather than ANDing them, and tags are indexed alongside
//     title and text, so a partial match still ranks.
//   - IMPORTANCE, over the lines that have ACTUALLY HELPED at least once,
//     ranked by help less miss. It is what keeps a HIGH-VALUE HEAD in the pool
//     whose words appear nowhere in the message, which is the one failure mode
//     a lexical index has and cannot fix. Counting the miss inside the ordering
//     is what makes an injection that bore on nothing cost something: a line
//     that helped twice and missed ten times sinks to where its contribution is
//     smallest, the way fixstore.go sinks a patch it offered that then failed.
//   - RECENCY, updated_seq descending — the transaction-time ordering, so
//     something corrected this morning is in the pool on the strength of that
//     alone.
//
// A LIST ONLY CONTAINS ROWS IT HAS SOMETHING TO SAY ABOUT, and that is what
// makes the fusion honest rather than a weighting in disguise. RRF combines
// RANKINGS OF CANDIDATES, not total orders over a corpus: an importance list
// that ran on past its evidence — ordering the rows that have never once helped
// by how recently they changed — would be a second copy of the recency list,
// and two thirds of the score would be one signal wearing two hats. So a row
// with no retrieval history is simply absent from the importance ranking, and
// a message with no matchable words produces no lexical ranking at all.
//
// THE LIMIT BOUNDS THE POOL, NEVER THE STORE. The read it replaces was capped
// at two hundred titles, which quietly made memory two hundred and one
// unreachable forever; every active row is ranked here and the limit only says
// how many of the ranked rows are carried out.
//
// A query with nothing in it to match — punctuation, or nothing but words
// shorter than the index keeps — is not an error and not an empty answer: the
// two arithmetic orderings still rank, so a person who types "ok, do it" is
// still shown what has mattered most and what changed last.
func (s *Store) MemoryCandidates(terms string, limit int) ([]MemoryStub, error) {
	if limit <= 0 {
		limit = MemoryCandidatesDefault
	}
	// An empty MATCH is a syntax error in FTS5 rather than a miss, so the
	// lexical ranking is left out of the fusion entirely when there is nothing
	// to match with. The other two still rank every row.
	lexical := `SELECT '' AS id, 0 AS rank WHERE 0`
	args := []any{MemoryActive}
	if query := ftsQueryFrom(terms); query != "" {
		lexical = `
			SELECT id, ROW_NUMBER() OVER (ORDER BY relevance, updated_seq DESC, id) AS rank
			FROM (
				SELECT active.id AS id, bm25(memories_fts) AS relevance,
				       active.updated_seq AS updated_seq
				FROM memories_fts JOIN active ON active.id = memories_fts.memory_id
				WHERE memories_fts MATCH ?
			)`
		args = append(args, query)
	}
	statement := `
		WITH active AS (
			SELECT id, title, type, scope, use_count, miss_count, updated_seq
			FROM memories WHERE status = ?
		),
		lexical AS (` + lexical + `),
		important AS (
			SELECT id, ROW_NUMBER() OVER (
				ORDER BY use_count - miss_count DESC, use_count DESC, updated_seq DESC, id) AS rank
			FROM active WHERE use_count > 0
		),
		recent AS (
			SELECT id, ROW_NUMBER() OVER (ORDER BY updated_seq DESC, id) AS rank
			FROM active
		)
		SELECT active.id, active.title, active.type, active.scope
		FROM active
		LEFT JOIN lexical ON lexical.id = active.id
		LEFT JOIN important ON important.id = active.id
		LEFT JOIN recent ON recent.id = active.id
		ORDER BY
			COALESCE(1.0 / (? + lexical.rank), 0) +
			COALESCE(1.0 / (? + important.rank), 0) +
			COALESCE(1.0 / (? + recent.rank), 0) DESC,
			active.use_count DESC, active.updated_seq DESC, active.id
		LIMIT ?`
	args = append(args, rrfK, rrfK, rrfK, limit)
	rows, err := s.db.Query(statement, args...)
	if err != nil {
		return nil, fmt.Errorf("memory candidates: %w", err)
	}
	defer rows.Close()
	stubs := make([]MemoryStub, 0, limit)
	for rows.Next() {
		var stub MemoryStub
		if err := rows.Scan(&stub.ID, &stub.Title, &stub.Type, &stub.Scope); err != nil {
			return nil, fmt.Errorf("memory candidates: %w", err)
		}
		stubs = append(stubs, stub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory candidates: %w", err)
	}
	return stubs, nil
}

// RecordMemoryOutcome settles what a turn's injected memories actually did:
// helped names the ones that bore on the answer, unused names the ones that
// were put in front of the model and did not.
//
// BOTH HALVES OR NEITHER, in one transaction, for the reason the retrieval
// count was always written in one: a turn's accounting is one fact about that
// turn, and half of it landing is a ranking signal nobody can interpret.
//
// It writes no event, for the reason stated on [Memory.UseCount] — the
// journalled floor is [Store.SnapshotMemoryRanking]'s job. Ids that name
// nothing, or name an inactive memory, are skipped in silence: telemetry is not
// a place to raise an alarm about a stale pointer.
func (s *Store) RecordMemoryOutcome(helped, unused []string) error {
	if len(helped) == 0 && len(unused) == 0 {
		return nil
	}
	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("record memory outcome: %w", err)
	}
	defer tx.Rollback()
	for _, step := range []struct {
		ids    []string
		column string
	}{{helped, "use_count"}, {unused, "miss_count"}} {
		for _, id := range step.ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, err := tx.Exec(`
				UPDATE memories SET `+step.column+` = `+step.column+` + 1
				WHERE id = ? AND status = ?`, id, MemoryActive); err != nil {
				return fmt.Errorf("record memory outcome: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record memory outcome: %w", err)
	}
	return nil
}

// SnapshotMemoryRanking journals what the two ranking counters currently say,
// at most once per minInterval, and reports whether it wrote one.
//
// THIS IS THE SIMPLEST SCHEME THAT IS CORRECT, and the simplicity is the
// argument for it. The counters cannot be journaled per write — that is the
// most frequent write in the feature and would bury the five events that carry
// meaning. They cannot be left unjournaled either, now that
// [Store.MemoryCandidates] ranks on them: a Rebuild would silently change which
// rows the router is shown. So the journal carries a periodic photograph
// instead. A replay lands on the last photograph rather than on zero, which is
// a floor a week deep at worst and nothing anybody has to reason about.
//
// IT DOES NOT HALVE, AND THAT IS DELIBERATE. Halving old counts is plausible
// ranking hygiene and it is also exactly the mechanism no paper has ever
// ablated: MemoryBank (AAAI 2024) proposed the Ebbinghaus curve and never
// tested it, and FadeMem (arXiv 2601.18642) — the most decay-committed paper in
// the literature — attributes its own gains to fusion and conflict resolution
// and ships no "without decay" row. Surviving a rebuild is a correctness
// problem and is solved here; decay is a guess and is not.
//
// Nothing is written when every counter is still zero: a store nobody has used
// yet has no ranking to preserve, and a weekly event saying so forever is the
// journal noise this whole arrangement exists to avoid.
func (s *Store) SnapshotMemoryRanking(minInterval time.Duration) (bool, error) {
	var last string
	err := s.db.QueryRow(`SELECT ts FROM events WHERE kind = ? ORDER BY seq DESC LIMIT 1`,
		EventMemoryRanking).Scan(&last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("snapshot memory ranking: %w", err)
	}
	if last != "" {
		at, err := parseTime(last)
		if err != nil {
			return false, fmt.Errorf("snapshot memory ranking: %w", err)
		}
		if time.Since(at) < minInterval {
			return false, nil
		}
	}
	rows, err := s.db.Query(`
		SELECT id, use_count, miss_count FROM memories
		WHERE status = ? AND (use_count > 0 OR miss_count > 0)
		ORDER BY id`, MemoryActive)
	if err != nil {
		return false, fmt.Errorf("snapshot memory ranking: %w", err)
	}
	defer rows.Close()
	var payload memoryRankingPayload
	for rows.Next() {
		var count memoryRankingCount
		if err := rows.Scan(&count.ID, &count.Use, &count.Miss); err != nil {
			return false, fmt.Errorf("snapshot memory ranking: %w", err)
		}
		payload.Counts = append(payload.Counts, count)
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("snapshot memory ranking: %w", err)
	}
	if len(payload.Counts) == 0 {
		return false, nil
	}
	tx, err := s.beginWrite()
	if err != nil {
		return false, fmt.Errorf("snapshot memory ranking: %w", err)
	}
	defer tx.Rollback()
	// The event is about the whole table rather than about one memory, which is
	// why it is the only one here journaled under an empty node id.
	if _, _, err := appendEvent(tx, "", EventMemoryRanking, payload); err != nil {
		return false, fmt.Errorf("snapshot memory ranking: %w", err)
	}
	// Applied on the live path too, though it is a no-op there: a write path
	// that skipped the apply function would be a second answer to what the
	// event means, and Rebuild's answer is the one that has to be right.
	if err := applyMemoryRanking(tx, payload); err != nil {
		return false, fmt.Errorf("snapshot memory ranking: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("snapshot memory ranking: %w", err)
	}
	return true, nil
}

// MemoryRecord reads one memory by id whatever its status, which is the read
// that makes the tombstone an audit trail instead of a claim.
//
// Every other read here is active-only by design; without this one, "the row
// stays for audit" would be true of the table and false of the package, and
// nothing outside the store could ever answer "what did that memory say before
// it was replaced".
func (s *Store) MemoryRecord(id string) (Memory, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Memory{}, false, nil
	}
	memories, err := s.queryMemories(`WHERE id = ?`, []any{id}, "", 0)
	if err != nil {
		return Memory{}, false, fmt.Errorf("read memory: %w", err)
	}
	if len(memories) == 0 {
		return Memory{}, false, nil
	}
	return memories[0], true, nil
}

// MemoryProvenance names the conversation and journal instant that last wrote
// a memory. Old events carry no source, which is unknown provenance rather than
// an error.
func (s *Store) MemoryProvenance(id string) (sessionID, sessionTitle string, writtenAt time.Time, err error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", "", time.Time{}, nil
	}
	var timestamp string
	err = s.db.QueryRow(`
		SELECT m.source_session, COALESCE(s.title, ''), COALESCE(e.ts, '')
		FROM memories m
		LEFT JOIN sessions s ON s.id = m.source_session
		LEFT JOIN events e ON e.seq = m.source_seq
		WHERE m.id = ?`, id).Scan(&sessionID, &sessionTitle, &timestamp)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", time.Time{}, nil
	}
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("memory provenance: %w", err)
	}
	if timestamp != "" {
		writtenAt, err = parseTime(timestamp)
		if err != nil {
			return "", "", time.Time{}, fmt.Errorf("memory provenance: %w", err)
		}
	}
	return sessionID, sessionTitle, writtenAt, nil
}

// memoryQuerier is whatever a memory read runs against: the pool, or ONE open
// transaction. It exists so that [Store.MemorySnapshot] can take its census and
// its rows inside a single read snapshot without a second decoder growing up
// beside [Store.queryMemories] — *sql.DB and *sql.Tx already share this method.
type memoryQuerier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// queryMemories is the single reader every memory read goes through, so no two
// of them can come to disagree about how a row decodes.
func (s *Store) queryMemories(where string, args []any, order string, limit int) ([]Memory, error) {
	return queryMemoriesOn(s.db, where, args, order, limit)
}

// queryMemoriesOn is queryMemories against a caller's own handle.
func queryMemoriesOn(db memoryQuerier, where string, args []any, order string, limit int) ([]Memory, error) {
	// The timestamp is a correlated lookup by primary key rather than a join in
	// the FROM clause, because the FROM clause is the caller's: SearchMemories
	// passes its own JOIN onto memories_fts in the same fragment, and a reader
	// that rewrote it would be a reader with two shapes. One statement, one
	// rowid seek per row carried out, and no second read from Go.
	statement := `
		SELECT memories.id, memories.type, memories.scope, memories.title,
		       memories.text, memories.tags, memories.status, memories.use_count,
		       memories.miss_count, memories.created_seq, memories.updated_seq,
		       memories.source_session, memories.source_seq,
		       COALESCE((SELECT ts FROM events WHERE seq = memories.updated_seq), '')
		FROM memories ` + where
	if order != "" {
		statement += ` ORDER BY ` + order
	}
	if limit > 0 {
		statement += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := db.Query(statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	memories := make([]Memory, 0, 16)
	for rows.Next() {
		var memory Memory
		var tags, updatedAt string
		if err := rows.Scan(&memory.ID, &memory.Type, &memory.Scope, &memory.Title,
			&memory.Text, &tags, &memory.Status, &memory.UseCount, &memory.MissCount,
			&memory.CreatedSeq, &memory.UpdatedSeq, &memory.SourceSession, &memory.SourceSeq,
			&updatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(tags), &memory.Tags); err != nil {
			return nil, err
		}
		if updatedAt != "" {
			// AN UNREADABLE TIMESTAMP IS AN UNKNOWN AGE, NOT A FAILED READ. The
			// row is the memory; a journal entry this package cannot parse must
			// not be a reason a person's memory stops being readable.
			if at, err := parseTime(updatedAt); err == nil {
				memory.UpdatedAt = at
			}
		}
		memories = append(memories, memory)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return memories, nil
}

// applyMemoryAdd materializes one added memory. It is shared by the write path
// and by Rebuild, so a replayed brain and a live one cannot differ.
func applyMemoryAdd(tx *sql.Tx, payload memoryPayload, seq int64) error {
	tags, err := memoryTagsJSON(payload.Tags)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO memories (id, type, scope, title, text, tags, status, use_count, created_seq, updated_seq, source_session, source_seq)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?)`,
		payload.ID, payload.Type, payload.Scope, payload.Title, payload.Text,
		tags, MemoryActive, seq, seq, payload.SourceSession, memorySourceSeq(payload.SourceSession, seq)); err != nil {
		return err
	}
	return refreshMemoryFTS(tx, payload.ID)
}

func applyMemoryUpdate(tx *sql.Tx, payload memoryUpdatePayload, seq int64) error {
	tags, err := memoryTagsJSON(payload.Tags)
	if err != nil {
		return err
	}
	result, err := tx.Exec(`
		UPDATE memories SET title = ?, text = ?, tags = ?, updated_seq = ?, source_session = ?, source_seq = ?
		WHERE id = ? AND status = ?`,
		payload.Title, payload.Text, tags, seq, payload.SourceSession, memorySourceSeq(payload.SourceSession, seq), payload.ID, MemoryActive)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("%w: update targets missing or inactive memory %q", ErrInvalid, payload.ID)
	}
	return refreshMemoryFTS(tx, payload.ID)
}

func applyMemorySupersede(tx *sql.Tx, payload memorySupersedePayload, seq int64) error {
	result, err := tx.Exec(`
		UPDATE memories SET status = ?, updated_seq = ?, source_session = ?, source_seq = ?
		WHERE id = ? AND status = ?`,
		MemorySuperseded, seq, payload.SourceSession, memorySourceSeq(payload.SourceSession, seq), payload.OldID, MemoryActive)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("%w: supersession targets missing or inactive memory %q", ErrInvalid, payload.OldID)
	}
	if err := refreshMemoryFTS(tx, payload.OldID); err != nil {
		return err
	}
	return applyMemoryAdd(tx, payload.New, seq)
}

func applyMemoryForget(tx *sql.Tx, payload memoryForgetPayload, seq int64) error {
	result, err := tx.Exec(`
		UPDATE memories SET status = ?, updated_seq = ?, source_session = ?, source_seq = ?
		WHERE id = ? AND status <> ?`,
		MemoryForgotten, seq, payload.SourceSession, memorySourceSeq(payload.SourceSession, seq), payload.ID, MemoryForgotten)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("%w: nothing to forget under %q", ErrInvalid, payload.ID)
	}
	return refreshMemoryFTS(tx, payload.ID)
}

// applyMemoryRanking restores the counters one snapshot recorded. It is shared
// by the write path and by Rebuild, so a replayed brain and a live one cannot
// come to rank the same store differently.
//
// A count naming a memory that has since been forgotten or superseded matches
// nothing and is skipped: the snapshot is a photograph of a moment, and the
// moment is allowed to have passed.
func applyMemoryRanking(tx *sql.Tx, payload memoryRankingPayload) error {
	for _, count := range payload.Counts {
		id := strings.TrimSpace(count.ID)
		if id == "" {
			continue
		}
		if _, err := tx.Exec(`
			UPDATE memories SET use_count = ?, miss_count = ?
			WHERE id = ? AND status = ?`, count.Use, count.Miss, id, MemoryActive); err != nil {
			return err
		}
	}
	return nil
}

func memorySourceSeq(sourceSession string, seq int64) int64 {
	if strings.TrimSpace(sourceSession) == "" {
		return 0
	}
	return seq
}

func applyMemoryRestore(tx *sql.Tx, payload memoryRestorePayload, seq int64) error {
	result, err := tx.Exec(`UPDATE memories SET status = ?, updated_seq = ? WHERE id = ? AND status = ?`,
		MemoryActive, seq, payload.ID, MemoryForgotten)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("%w: restore targets missing or non-forgotten memory %q", ErrInvalid, payload.ID)
	}
	return refreshMemoryFTS(tx, payload.ID)
}

// memoryTagsJSON is why a memory with no tags reads back the same way after a
// rebuild as it did before one.
//
// The payloads spell tags `omitempty`, so a memory that has none carries no
// tags field in its event at all — and json.Marshal of the nil slice a replay
// decodes writes the string "null", where the live write had written "[]".
// Every read then hands back a nil slice on one path and an empty one on the
// other, for the same memory, depending only on whether the store had been
// rebuilt. The journal is truth; it must not also be a source of variation.
func memoryTagsJSON(tags []string) (string, error) {
	if tags == nil {
		tags = []string{}
	}
	encoded, err := json.Marshal(tags)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// refreshMemoryFTS replaces one index row from the authoritative memories view,
// inside the event transaction, so searchable memory can never get ahead of or
// lag behind the event that changed it.
//
// The status filter in the SELECT is what makes exclusion structural: a
// superseded or forgotten row matches nothing, so the DELETE stands and the row
// is out of every search from that instant — no reader has to remember to
// filter it, and none can forget.
func refreshMemoryFTS(tx *sql.Tx, id string) error {
	if _, err := tx.Exec(`DELETE FROM memories_fts WHERE memory_id = ?`, id); err != nil {
		return err
	}
	_, err := tx.Exec(`
		INSERT INTO memories_fts (memory_id, title, text, tags)
		SELECT id, title, text, replace(replace(replace(tags, '[', ''), ']', ''), '"', '')
		FROM memories WHERE id = ? AND status = ?`, id, MemoryActive)
	return err
}

// memoryPayloadFrom is the one gate every written memory passes through.
func memoryPayloadFrom(m Memory) (memoryPayload, error) {
	memoryType := strings.ToLower(strings.TrimSpace(m.Type))
	if !validMemoryType(memoryType) {
		return memoryPayload{}, fmt.Errorf("%w: unknown memory type %q", ErrInvalid, m.Type)
	}
	scope := strings.ToLower(strings.TrimSpace(m.Scope))
	if !validMemoryScope(scope) {
		return memoryPayload{}, fmt.Errorf("%w: unknown memory scope %q", ErrInvalid, m.Scope)
	}
	title, text, tags, err := validMemoryBody(m.Title, m.Text, m.Tags)
	if err != nil {
		return memoryPayload{}, err
	}
	return memoryPayload{
		ID:    strings.TrimSpace(m.ID),
		Type:  memoryType,
		Scope: scope,
		Title: title,
		Text:  text,
		Tags:  tags,
	}, nil
}

// validMemoryBody enforces the three caps and the one thing a memory may not be
// without: something to say. Lengths are counted in runes rather than bytes,
// because a cap measured in bytes refuses a shorter sentence for being written
// in a different language.
func validMemoryBody(title, text string, tags []string) (string, string, []string, error) {
	title = strings.TrimSpace(title)
	text = strings.TrimSpace(text)
	if text == "" {
		return "", "", nil, fmt.Errorf("%w: a memory with no text says nothing", ErrInvalid)
	}
	if utf8.RuneCountInString(title) > MemoryTitleRunes {
		return "", "", nil, fmt.Errorf("%w: title is %d characters, the limit is %d",
			ErrInvalid, utf8.RuneCountInString(title), MemoryTitleRunes)
	}
	if utf8.RuneCountInString(text) > MemoryTextRunes {
		return "", "", nil, fmt.Errorf("%w: text is %d characters, the limit is %d",
			ErrInvalid, utf8.RuneCountInString(text), MemoryTextRunes)
	}
	kept := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag = strings.ToLower(strings.TrimSpace(tag)); tag != "" {
			kept = append(kept, tag)
		}
	}
	if len(kept) > MemoryMaxTags {
		return "", "", nil, fmt.Errorf("%w: %d tags, the limit is %d",
			ErrInvalid, len(kept), MemoryMaxTags)
	}
	return title, text, kept, nil
}

func validMemoryType(memoryType string) bool {
	switch memoryType {
	case MemoryFact, MemoryPreference, MemoryDecision, MemoryCorrection, MemoryProjectState:
		return true
	}
	return false
}

func validMemoryScope(scope string) bool {
	switch scope {
	case MemoryScopeUser, MemoryScopeProject, MemoryScopeEnv:
		return true
	}
	return false
}

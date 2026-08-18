package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
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
// carried across sessions by a router that reads an index of titles and asks
// for the two or three that bear on the moment.
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
// A title is an index line — the router reads hundreds of them at once and
// picks by them, so a title that needs a second line has already failed at its
// job. Text is one line of substance. Eight tags is more than any memory has
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
	// UseCount is retrieval telemetry and is deliberately NOT journaled: it is
	// incremented by the router as it hands memories to a model, which happens
	// far more often than anything else here and would otherwise write one
	// event per read into an append-only journal. Rebuild therefore resets it
	// to zero, exactly as it does the notebook's use counters, and nothing
	// ranks on it — a number the journal cannot reproduce may inform a person
	// reading the table, never a retrieval deciding what a model sees.
	UseCount   int
	CreatedSeq int64
	UpdatedSeq int64
}

// MemoryStub is one line of the router's index: enough to decide whether a
// memory bears on the moment, and nothing more. The full text is a second call
// away on purpose — the index is read in full every turn and the bodies are
// not.
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
    created_seq INTEGER NOT NULL,
    updated_seq INTEGER NOT NULL
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

type memoryPayload struct {
	ID    string   `json:"id"`
	Type  string   `json:"type"`
	Scope string   `json:"scope"`
	Title string   `json:"title"`
	Text  string   `json:"text"`
	Tags  []string `json:"tags,omitempty"`
}

type memoryUpdatePayload struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Text  string   `json:"text"`
	Tags  []string `json:"tags,omitempty"`
}

type memorySupersedePayload struct {
	OldID string        `json:"old_id"`
	New   memoryPayload `json:"new"`
}

type memoryForgetPayload struct {
	ID string `json:"id"`
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
	payload, err := memoryPayloadFrom(m)
	if err != nil {
		return Memory{}, fmt.Errorf("add memory: %w", err)
	}
	if payload.ID == "" {
		payload.ID = NewMemoryID()
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
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
	}, nil
}

// UpdateMemory corrects a memory in place. Type and scope are not arguments
// because a memory that changed either of those is a different memory and
// wants SupersedeMemory: the whole point of an update is that the router's
// existing pointers to this id stay valid.
func (s *Store) UpdateMemory(id, title, text string, tags []string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("update memory: %w: id is required", ErrInvalid)
	}
	title, text, tags, err := validMemoryBody(title, text, tags)
	if err != nil {
		return fmt.Errorf("update memory: %w", err)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("update memory: %w", err)
	}
	defer tx.Rollback()

	payload := memoryUpdatePayload{ID: id, Title: title, Text: text, Tags: tags}
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
	tx, err := s.db.BeginTx(context.Background(), nil)
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
	payload := memorySupersedePayload{OldID: oldID, New: fresh}
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
	}, nil
}

// ForgetMemory tombstones a memory. It refuses a memory that is already
// forgotten rather than returning quietly: "forget that" is an instruction a
// person expects to have had an effect, and a silent success on a row that was
// already gone is the store agreeing with something that did not happen.
func (s *Store) ForgetMemory(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("forget memory: %w: id is required", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("forget memory: %w", err)
	}
	defer tx.Rollback()

	payload := memoryForgetPayload{ID: id}
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

// MemoryIndex is the router's whole view of what is remembered: one title-sized
// line per active memory, newest-touched first.
//
// It is a separate read from ListMemories rather than a projection of it
// because it is the one read that happens every turn. Carrying five hundred
// bodies to render five hundred titles is how a memory store becomes the most
// expensive thing in the loop.
func (s *Store) MemoryIndex(limit int) ([]MemoryStub, error) {
	statement := `
		SELECT id, title, type, scope FROM memories
		WHERE status = ?
		ORDER BY updated_seq DESC, id`
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

// BumpMemoryUse counts one retrieval of each named memory, in one transaction
// so a router that hands three memories to a model either counts all three or
// none.
//
// It writes no event, for the reason stated on Memory.UseCount: this is the
// most frequent write in the whole feature and journaling it would bury the
// four events that carry meaning under thousands that carry none. Ids that name
// nothing, or name an inactive memory, are skipped in silence — telemetry is
// not a place to raise an alarm about a stale pointer.
func (s *Store) BumpMemoryUse(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("bump memory use: %w", err)
	}
	defer tx.Rollback()
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, err := tx.Exec(`
			UPDATE memories SET use_count = use_count + 1
			WHERE id = ? AND status = ?`, id, MemoryActive); err != nil {
			return fmt.Errorf("bump memory use: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("bump memory use: %w", err)
	}
	return nil
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

// queryMemories is the single reader every memory read goes through, so no two
// of them can come to disagree about how a row decodes.
func (s *Store) queryMemories(where string, args []any, order string, limit int) ([]Memory, error) {
	statement := `
		SELECT memories.id, memories.type, memories.scope, memories.title,
		       memories.text, memories.tags, memories.status, memories.use_count,
		       memories.created_seq, memories.updated_seq
		FROM memories ` + where
	if order != "" {
		statement += ` ORDER BY ` + order
	}
	if limit > 0 {
		statement += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	memories := make([]Memory, 0, 16)
	for rows.Next() {
		var memory Memory
		var tags string
		if err := rows.Scan(&memory.ID, &memory.Type, &memory.Scope, &memory.Title,
			&memory.Text, &tags, &memory.Status, &memory.UseCount,
			&memory.CreatedSeq, &memory.UpdatedSeq); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(tags), &memory.Tags); err != nil {
			return nil, err
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
		INSERT INTO memories (id, type, scope, title, text, tags, status, use_count, created_seq, updated_seq)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
		payload.ID, payload.Type, payload.Scope, payload.Title, payload.Text,
		tags, MemoryActive, seq, seq); err != nil {
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
		UPDATE memories SET title = ?, text = ?, tags = ?, updated_seq = ?
		WHERE id = ? AND status = ?`,
		payload.Title, payload.Text, tags, seq, payload.ID, MemoryActive)
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
		UPDATE memories SET status = ?, updated_seq = ?
		WHERE id = ? AND status = ?`,
		MemorySuperseded, seq, payload.OldID, MemoryActive)
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
		UPDATE memories SET status = ?, updated_seq = ?
		WHERE id = ? AND status <> ?`,
		MemoryForgotten, seq, payload.ID, MemoryForgotten)
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

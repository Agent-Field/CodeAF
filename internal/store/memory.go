package store

import (
	"cmp"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/redact"
)

// Memories are what one conversation knows and the next one would otherwise
// have to be told again: a preference, a standing rule, a correction, a fact
// about the person, their machine or their project.
//
// Memories are admitted to a bounded prompt cache by activation and context.
// Explicit person pins always ride; other rows remain inspectable and searchable.
// Event telemetry feeds the cached practice trace rather than remaining write-only.
//
// The notebook (facts.go) is a different thing: it distils beliefs out of
// finished WORK. A memory is something a person said or had noticed for them.
//
// It is event-sourced like everything else here, and for the same reason: the
// events table is truth, the memories table is a materialized view refreshed
// inside the same write transaction as the event that changed it, and Rebuild
// reproduces it by replaying the journal. Nothing is ever deleted — a forgotten
// memory is a tombstone with its row intact, which is what lets the memory place
// put a forgotten line back.
const (
	// EventMemoryAdd carries a whole new memory row.
	EventMemoryAdd EventKind = "memory_add"
	// EventMemoryUpdate carries an id and the new title, text and tags: the
	// person correcting the wording of a line they can see.
	EventMemoryUpdate EventKind = "memory_update"
	// EventMemorySupersede retires one memory in favour of another in a single
	// event, because the two halves are one decision. Two events — forget the
	// old, add the new — would let a replay stop between them and leave the
	// brain believing nothing at all about the subject.
	EventMemorySupersede EventKind = "memory_supersede"
	// EventMemoryForget is a tombstone and never a deletion.
	EventMemoryForget EventKind = "memory_forget"
	// EventMemoryRestore returns one forgotten or replaced memory to the active
	// view.
	EventMemoryRestore EventKind = "memory_restore"
	// EventMemorySkipped journals a write the duplicate door refused. A skip is
	// an outcome, not a silence: the person said "remember X" twice, and the
	// record of both says so.
	EventMemorySkipped EventKind = "memory_skipped"
	// EventMemoryWriteFailed journals a write that did not land, so a failure is
	// readable at the store rather than lost at whichever door it happened.
	EventMemoryWriteFailed EventKind = "memory_write_failed"
	// EventMemoryRehomed journals a quarantine row's move to a provable owner.
	EventMemoryRehomed EventKind = "memory_rehomed"
	// EventMemoryMoved journals the person moving a line to another owner — from
	// this project to everywhere, say — from the memory place. It is the general
	// case of a re-home and carries the project's name with it, so the place can
	// still say which project a line holds in after the move.
	EventMemoryMoved EventKind = "memory_moved"
)

// The kinds describe durable knowledge and transient project state. Admission
// depends on practice and context; the kind remains available for inspection.
const (
	MemoryFact         = "fact"
	MemoryPreference   = "preference"
	MemoryDecision     = "decision"
	MemoryCorrection   = "correction"
	MemoryProjectState = "project_state"
)

// A memory's scope is the blast radius of its truth: something true about the
// person everywhere, something true only inside one project, or something true
// only of this machine. SCOPE IS DERIVED FROM OWNER (memory_owner.go) and the
// column stays because old rows and old payloads carry it.
const (
	MemoryScopeUser    = "user"
	MemoryScopeProject = "project"
	MemoryScopeEnv     = "env"
)

// The three states a memory row can be in. Only active is ever put in front of
// a model; the other two are history the memory place can put back.
const (
	MemoryActive     = "active"
	MemorySuperseded = "superseded"
	MemoryForgotten  = "forgotten"
)

// Who saved a memory. It is shown beside every line in the memory place and in
// every receipt, because a line somebody else decided to keep about you is a
// line you should be able to recognise as such. Empty is a row written before
// the column existed, and renders as nothing.
const (
	// OriginPerson is the person keeping it themselves: /remember, /always, or
	// the memory place's own add.
	OriginPerson = "person"
	// OriginModel is the model's own `remember`, usually because the person
	// asked it to in words.
	OriginModel = "model"
	// OriginNoticed is the pass after a turn noticing something durable in what
	// the person said and keeping it — always with a line in the conversation
	// saying so.
	OriginNoticed = "noticed"
)

// The caps are on what a memory may WEIGH. A title is the first words of the
// text and is what a list draws when the text will not fit; text is one line of
// substance. Eight tags is a bound on nonsense rather than on expression.
const (
	MemoryTitleRunes = 80
	MemoryTextRunes  = 512
	MemoryMaxTags    = 8
)

// Memory is one durable thing known across conversations.
type Memory struct {
	ID            string
	OwnerFan      int
	TopicStrength map[string]int
	TopicFans     map[string]int
	Rehearsals    int
	Activation    float64
	ActivationAt  time.Time
	// Owner is WHO the memory belongs to — the whole of the permission model
	// (memory_owner.go). Every read that can put a memory in front of a model
	// filters on it; two projects may hold genuinely different truths about
	// the same words, and never see each other's.
	Owner  string
	Type   string
	Scope  string
	Title  string
	Text   string
	Tags   []string
	Status string
	// Origin is who saved it: [OriginPerson], [OriginModel] or
	// [OriginNoticed], or "" for a row older than the column.
	Origin string
	// ProjectName is the name a person knows the project by — the repository's
	// name, or the folder's — written beside a project memory when it is kept.
	// The owner key is an opaque hash on purpose, and this is what lets the
	// memory place say which project a line holds in without reversing it.
	ProjectName string
	// UpdatedAt is when the event that last touched this memory was journaled.
	// Zero is unknown and renders as nothing.
	UpdatedAt time.Time
	// Rule preserves an earlier explicit pin in the always_on column. These
	// lines bypass activation and always ride, just like current person pins.
	Rule          bool
	CreatedSeq    int64
	UpdatedSeq    int64
	SourceSession string
	SourceSeq     int64
}

// Legacy telemetry columns remain compatible with earlier builds. Imported
// use_count supplies an undated practice baseline; new learning is journaled
// through memory interactions. always_on preserves earlier explicit pins.
const memoriesSchema = `
CREATE TABLE IF NOT EXISTS memories (
    id          TEXT PRIMARY KEY,
    owner       TEXT NOT NULL DEFAULT 'user',
    type        TEXT NOT NULL,
    scope       TEXT NOT NULL,
    title       TEXT NOT NULL,
    text        TEXT NOT NULL,
    tags        TEXT NOT NULL DEFAULT '[]',
    status      TEXT NOT NULL DEFAULT 'active',
    created_seq    INTEGER NOT NULL,
    updated_seq    INTEGER NOT NULL,
    source_session TEXT NOT NULL DEFAULT '',
    source_seq     INTEGER NOT NULL DEFAULT 0,
    origin         TEXT NOT NULL DEFAULT '',
    project_name   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS memories_status_updated ON memories (status, updated_seq DESC);
-- THE OWNER INDEX IS NOT HERE. owner is a migration column: a database written
-- before owners existed already has this table, so CREATE TABLE IF NOT EXISTS
-- leaves it alone, and an index declared here would be created against a column
-- that does not exist yet — which is exactly the open that failed with
-- "no such column: owner" on every store already on disk. It is created by
-- [migrateMemoriesOwner], beside the column it indexes.
`

// migrateMemoriesSchema brings a store written by an earlier build up to this
// one's shape: the columns it added, and the indexes and search table it no
// longer maintains taken away.
//
// THE DROPS ARE NOT TIDINESS. Two of the retired indexes were over the whole
// events journal — every event any part of codeaf writes paid to maintain them.
// An older engine still serving a workspace re-creates an index it misses on
// its next open, so dropping one costs it nothing.
//
// THE SEARCH TABLE (memories_fts) IS NOT DROPPED IN THIS RELEASE, because an
// older engine still serving a workspace writes and reads it on every memory
// change and recall, and would fail each one with "no such table" until it
// restarted. This build neither reads nor refreshes it; a later release drops
// it once no engine of the older build can still be running.
func migrateMemoriesSchema(db *sql.DB) error {
	for _, column := range []struct{ name, declaration string }{
		{"source_session", `ALTER TABLE memories ADD COLUMN source_session TEXT NOT NULL DEFAULT ''`},
		{"source_seq", `ALTER TABLE memories ADD COLUMN source_seq INTEGER NOT NULL DEFAULT 0`},
		{"origin", `ALTER TABLE memories ADD COLUMN origin TEXT NOT NULL DEFAULT ''`},
		{"project_name", `ALTER TABLE memories ADD COLUMN project_name TEXT NOT NULL DEFAULT ''`},
		{"always_on", `ALTER TABLE memories ADD COLUMN always_on INTEGER NOT NULL DEFAULT 0`},
	} {
		if err := addColumnOnce(db, "memories", column.name, column.declaration); err != nil {
			return err
		}
	}
	for _, statement := range []string{
		`DROP INDEX IF EXISTS events_contextual_approved`,
		`DROP INDEX IF EXISTS events_contextual_memory`,
		`DROP INDEX IF EXISTS memories_always_active`,
		`DROP INDEX IF EXISTS memories_status_scope`,
	} {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

// addColumnOnce adds one column inside one immediate transaction, asking
// whether it exists INSIDE that transaction: two processes opening one old
// store at once must not both read "absent" and both alter.
func addColumnOnce(db *sql.DB, table, name, declaration string) error {
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	found, err := tableHasColumnOn(tx, table, name)
	if err != nil {
		return err
	}
	if !found {
		if _, err := tx.Exec(declaration); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type memoryPayload struct {
	ID            string   `json:"id"`
	Owner         string   `json:"owner,omitempty"`
	Type          string   `json:"type"`
	Scope         string   `json:"scope"`
	Title         string   `json:"title"`
	Text          string   `json:"text"`
	Tags          []string `json:"tags,omitempty"`
	SourceSession string   `json:"source_session,omitempty"`
	Origin        string   `json:"origin,omitempty"`
	ProjectName   string   `json:"project_name,omitempty"`
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
	Owner         string `json:"owner,omitempty"`
	ID            string `json:"id"`
	SourceSession string `json:"source_session,omitempty"`
}

type memoryRestorePayload struct {
	ID string `json:"id"`
}

type memoryMovedPayload struct {
	ID          string `json:"id"`
	Owner       string `json:"owner"`
	ProjectName string `json:"project_name,omitempty"`
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

// MemoryTitleOf is a memory's title, derived from its text: the first line, cut
// at a word boundary inside [MemoryTitleRunes]. It is THE ONE derivation — the
// write door, an edit and a replacement all call it — so a line whose wording
// was corrected can never keep the title of what it used to say.
func MemoryTitleOf(text string) string {
	text = strings.TrimSpace(text)
	if index := strings.IndexAny(text, "\r\n"); index >= 0 {
		text = strings.TrimSpace(text[:index])
	}
	if utf8.RuneCountInString(text) <= MemoryTitleRunes {
		return text
	}
	runes := []rune(text)
	cut := MemoryTitleRunes - 1
	for index := cut; index > MemoryTitleRunes/2; index-- {
		if unicode.IsSpace(runes[index]) {
			cut = index
			break
		}
	}
	return strings.TrimSpace(string(runes[:cut])) + "…"
}

// AddMemory journals one new memory and materializes it in the same
// transaction, with no duplicate check. It is the seeder's door; everything a
// person or a model says goes through [Store.Write], which checks.
func (s *Store) AddMemory(m Memory) (Memory, error) {
	payload, err := memoryPayloadFrom(m)
	if err != nil {
		return Memory{}, fmt.Errorf("add memory: %w", err)
	}
	if payload.ID == "" {
		payload.ID = NewMemoryID()
	}
	payload.SourceSession = strings.TrimSpace(m.SourceSession)
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
	return memoryFromPayload(payload, seq), nil
}

// memoryFromPayload is the row a just-applied add or supersede left behind.
func memoryFromPayload(payload memoryPayload, seq int64) Memory {
	return Memory{
		ID: payload.ID, Owner: payload.Owner, Type: payload.Type, Scope: payload.Scope,
		Title: payload.Title, Text: payload.Text, Tags: payload.Tags,
		Status: MemoryActive, Origin: payload.Origin, ProjectName: payload.ProjectName,
		CreatedSeq: seq, UpdatedSeq: seq,
		SourceSession: payload.SourceSession, SourceSeq: memorySourceSeq(payload.SourceSession, seq),
	}
}

// UpdateMemory corrects the wording of an active memory in place and re-derives
// its title from the new words ([MemoryTitleOf]); its tags, owner and origin
// stay. It acts BY ID with no owner filter, which is a contract and not an
// oversight: its one caller is the person's own memory place, which may manage
// any row it is showing.
func (s *Store) UpdateMemory(id, text string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("update memory: %w: id is required", ErrInvalid)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("update memory: %w", err)
	}
	defer tx.Rollback()
	var rawTags, owner string
	if err := tx.QueryRow(`SELECT tags, owner FROM memories WHERE id = ? AND status = ?`, id, MemoryActive).Scan(&rawTags, &owner); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("update memory: %w: %q is not an active memory", ErrInvalid, id)
		}
		return fmt.Errorf("update memory: %w", err)
	}
	var tags []string
	if err := json.Unmarshal([]byte(rawTags), &tags); err != nil {
		return fmt.Errorf("update memory: %w", err)
	}
	title, text, tags, err := validMemoryBody(MemoryTitleOf(redact.Secrets(text)), text, tags)
	if err != nil {
		return fmt.Errorf("update memory: %w", err)
	}
	// A WORDING ITS OWNER ALREADY KEEPS WOULD BE ONE THING SAID TWICE.
	if kept, err := memoryDuplicateOn(tx, owner, text, id); err != nil || kept != "" {
		return fmt.Errorf("update memory: %w", cmp.Or(err, ErrMemoryKept))
	}
	// A LONGER WORDING IS A BIGGER LINE, and it is measured like a new one.
	if err := memoryRoomOn(tx, owner, text, id); err != nil {
		return fmt.Errorf("update memory: %w", err)
	}
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

// SupersedeMemoryForOwners retires one memory and admits its replacement as one
// event, under the constraint that the old row is active and owned by one of the
// named owners — the store-level guard that keeps a caller holding a foreign id
// from retiring another project's memory. The replacement keeps the target's
// owner, and is measured against the bound with the old line already gone.
//
// WHEN THE REPLACEMENT'S WORDS ARE ALREADY KEPT for that owner, nothing new is
// admitted: the old line is let go, the line already there stands, and the
// answer is that line with added false.
func (s *Store) SupersedeMemoryForOwners(owners []string, oldID string, m Memory) (Memory, bool, error) {
	oldID = strings.TrimSpace(oldID)
	fresh, err := supersedePayloadFor(owners, oldID, m)
	if err != nil {
		return Memory{}, false, fmt.Errorf("supersede memory: %w", err)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return Memory{}, false, fmt.Errorf("supersede memory: %w", err)
	}
	defer tx.Rollback()
	if err := supersedeTargetOn(tx, owners, oldID, fresh.Owner); err != nil {
		return Memory{}, false, fmt.Errorf("supersede memory: %w", err)
	}
	kept, err := memoryDuplicateOn(tx, fresh.Owner, fresh.Text, oldID)
	if err != nil {
		return Memory{}, false, fmt.Errorf("supersede memory: %w", err)
	}
	if kept != "" {
		// The words are already kept there: the old line goes, that one stands.
		if err := forgetOn(tx, oldID, fresh.Owner, fresh.SourceSession); err != nil {
			return Memory{}, false, fmt.Errorf("supersede memory: %w", err)
		}
		found, err := queryMemoriesOn(tx, "WHERE id = ?", []any{kept}, "", 1)
		if err == nil {
			err = tx.Commit()
		}
		if err != nil {
			return Memory{}, false, fmt.Errorf("supersede memory: %w", err)
		}
		return found[0], false, nil
	}
	if err := memoryRoomOn(tx, fresh.Owner, fresh.Text, oldID); err != nil {
		return Memory{}, false, fmt.Errorf("supersede memory: %w", err)
	}
	payload := memorySupersedePayload{OldID: oldID, New: fresh, SourceSession: fresh.SourceSession}
	seq, _, err := appendEvent(tx, fresh.ID, EventMemorySupersede, payload)
	if err == nil {
		err = applyMemorySupersede(tx, payload, seq)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		return Memory{}, false, fmt.Errorf("supersede memory: %w", err)
	}
	return memoryFromPayload(fresh, seq), true, nil
}

// supersedePayloadFor checks a replacement's arguments and builds the row it
// would admit, before any transaction is opened.
func supersedePayloadFor(owners []string, oldID string, m Memory) (memoryPayload, error) {
	if len(owners) == 0 {
		return memoryPayload{}, fmt.Errorf("%w: no owner was named", ErrInvalid)
	}
	if oldID == "" {
		return memoryPayload{}, fmt.Errorf("%w: id is required", ErrInvalid)
	}
	fresh, err := memoryPayloadFrom(m)
	if err != nil {
		return memoryPayload{}, err
	}
	if fresh.ID == "" {
		fresh.ID = NewMemoryID()
	}
	if fresh.ID == oldID {
		return memoryPayload{}, fmt.Errorf("%w: %q cannot supersede itself", ErrInvalid, oldID)
	}
	fresh.SourceSession = strings.TrimSpace(m.SourceSession)
	return fresh, nil
}

// supersedeTargetOn checks that the line being replaced is active, is one the
// caller's owners may see, and holds where its replacement will.
func supersedeTargetOn(tx *sql.Tx, owners []string, oldID, freshOwner string) error {
	where, ownerArgs := ownerFilterSQL(owners)
	var oldOwner string
	err := tx.QueryRow(`SELECT owner FROM memories WHERE id = ? AND status = ?`+where,
		append([]any{oldID, MemoryActive}, ownerArgs...)...).Scan(&oldOwner)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %q is not an active memory this conversation can see", ErrInvalid, oldID)
	}
	if err != nil {
		return err
	}
	if oldOwner != freshOwner {
		return fmt.Errorf("%w: replacement must keep the target owner", ErrInvalid)
	}
	return nil
}

// forgetOn lets one line go inside a caller's own transaction — the doors that
// find the words they were about to make active already kept, and let the
// duplicate go rather than keep it twice.
func forgetOn(tx *sql.Tx, id, owner, sourceSession string) error {
	payload := memoryForgetPayload{ID: id, Owner: owner, SourceSession: sourceSession}
	seq, _, err := appendEvent(tx, id, EventMemoryForget, payload)
	if err != nil {
		return err
	}
	return applyMemoryForget(tx, payload, seq)
}

// ForgetMemory tombstones a memory by id. It refuses a memory that is already
// forgotten rather than returning quietly: "forget that" is an instruction a
// person expects to have had an effect.
func (s *Store) ForgetMemory(id string) error {
	return s.forgetMemory(nil, id, "")
}

// ForgetMemoryForOwners tombstones only a row owned by the named owners.
func (s *Store) ForgetMemoryForOwners(owners []string, id, sourceSession string) error {
	if len(owners) == 0 {
		return fmt.Errorf("forget memory: %w: no owner was named", ErrInvalid)
	}
	return s.forgetMemory(owners, id, sourceSession)
}

func (s *Store) forgetMemory(owners []string, id, sourceSession string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("forget memory: %w: id is required", ErrInvalid)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("forget memory: %w", err)
	}
	defer tx.Rollback()
	if owners != nil {
		if err := requireMemoryOwners(tx, owners, id); err != nil {
			return fmt.Errorf("forget memory: %w", err)
		}
	}
	var rowOwner string
	if err := tx.QueryRow("SELECT owner FROM memories WHERE id = ?", id).Scan(&rowOwner); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("forget memory: %w: missing memory", ErrInvalid)
		}
		return fmt.Errorf("forget memory: %w", err)
	}
	payload := memoryForgetPayload{ID: id, Owner: rowOwner, SourceSession: strings.TrimSpace(sourceSession)}
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

// RestoreMemory puts a forgotten or replaced memory back in the active view.
// A replaced line comes back BESIDE its replacement — the person putting it back
// decides which of the two to keep — except when the caller is undoing the
// replacement itself, which forgets the replacement first.
func (s *Store) RestoreMemory(id string) error { return s.restoreMemory(nil, id) }

// RestoreMemoryForOwners restores only a row owned by the named owners.
func (s *Store) RestoreMemoryForOwners(owners []string, id string) error {
	if len(owners) == 0 {
		return fmt.Errorf("restore memory: %w: no owner was named", ErrInvalid)
	}
	return s.restoreMemory(owners, id)
}

func requireMemoryOwners(tx *sql.Tx, owners []string, id string) error {
	where, args := ownerFilterSQL(owners)
	var count int
	if err := tx.QueryRow("SELECT COUNT(*) FROM memories WHERE id = ?"+where, append([]any{id}, args...)...).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("%w: memory is outside the named owners", ErrInvalid)
	}
	return nil
}

func (s *Store) restoreMemory(owners []string, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("restore memory: %w: id is required", ErrInvalid)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("restore memory: %w", err)
	}
	defer tx.Rollback()
	if owners != nil {
		if err := requireMemoryOwners(tx, owners, id); err != nil {
			return fmt.Errorf("restore memory: %w", err)
		}
	}
	// A LINE PUT BACK IS A LINE MADE ACTIVE, and it is measured like a new one.
	var owner, text string
	if err := tx.QueryRow(`SELECT owner, text FROM memories WHERE id = ?`, id).Scan(&owner, &text); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("restore memory: %w: missing memory", ErrInvalid)
		}
		return fmt.Errorf("restore memory: %w", err)
	}
	// AND WORDS ALREADY KEPT THERE ARE NOT KEPT TWICE: the line already active
	// stands, and this one stays let go.
	if kept, err := memoryDuplicateOn(tx, owner, text, id); err != nil || kept != "" {
		return fmt.Errorf("restore memory: %w", cmp.Or(err, ErrMemoryKept))
	}
	if err := memoryRoomOn(tx, owner, text, id); err != nil {
		return fmt.Errorf("restore memory: %w", err)
	}
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

// MoveMemory gives one active memory a new owner — the person moving a line
// from this project to everywhere, or a quarantined line into the project they
// are standing in — and records the project's name with it when the new owner
// is a project. Like [Store.UpdateMemory] it acts by id for the person's own
// Memories tab. The line is measured against every block its new owner
// reaches, with its old place already counted as gone.
//
// WHEN THE NEW OWNER ALREADY HOLDS THE SAME WORDS, the line is not moved on top
// of them: it is let go, the line already there stands, and merged is true.
func (s *Store) MoveMemory(id, owner, projectName string) (merged bool, err error) {
	id = strings.TrimSpace(id)
	owner = normalizeOwner(owner)
	if id == "" {
		return false, fmt.Errorf("move memory: %w: id is required", ErrInvalid)
	}
	if !ValidOwner(owner) || owner == OwnerLegacyProject {
		return false, fmt.Errorf("move memory: %w: owner %q is not one a line can be moved to", ErrInvalid, owner)
	}
	if OwnerKindOf(owner) != OwnerKindProject {
		projectName = ""
	}
	tx, err := s.beginWrite()
	if err != nil {
		return false, fmt.Errorf("move memory: %w", err)
	}
	defer tx.Rollback()
	merged, err = moveMemoryOn(tx, id, owner, strings.TrimSpace(projectName))
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		return false, fmt.Errorf("move memory: %w", err)
	}
	return merged, nil
}

// moveMemoryOn is the move inside its transaction: an active line only, onto
// words the new owner does not already hold, and only with room for it.
func moveMemoryOn(tx *sql.Tx, id, owner, projectName string) (bool, error) {
	var oldOwner, text string
	if err := tx.QueryRow(`SELECT owner, text FROM memories WHERE id = ? AND status = ?`, id, MemoryActive).Scan(&oldOwner, &text); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, fmt.Errorf("%w: %q is not an active memory", ErrInvalid, id)
		}
		return false, err
	}
	kept, err := memoryDuplicateOn(tx, owner, text, id)
	if err != nil {
		return false, err
	}
	if kept != "" {
		return true, forgetOn(tx, id, oldOwner, "")
	}
	if err := memoryRoomOn(tx, owner, text, id); err != nil {
		return false, err
	}
	payload := memoryMovedPayload{ID: id, Owner: owner, ProjectName: projectName}
	seq, _, err := appendEvent(tx, id, EventMemoryMoved, payload)
	if err != nil {
		return false, err
	}
	return false, applyMemoryMoved(tx, payload, seq)
}

// MemoriesFor is the complete active catalog for the named owners: person,
// machine, then project, oldest first within each. Prompt admission instead
// uses AssembleMemoryBlock so reading the catalog never changes the ranking.
//
// An empty owners list is an error and not "everything": this read feeds what a
// model is told, and "unscoped" would be the leak the owner column closes.
func (s *Store) MemoriesFor(owners []string) ([]Memory, error) {
	if len(owners) == 0 {
		return nil, fmt.Errorf("memories for: %w: no owner was named", ErrInvalid)
	}
	where, ownerArgs := ownerFilterSQL(owners)
	// The arguments follow the placeholders: the status, the owner list, and
	// then the two owners the ORDER BY ranks first.
	args := append(append([]any{MemoryActive}, ownerArgs...), OwnerUser, OwnerMachine)
	memories, err := s.queryMemories(`WHERE status = ?`+where, args,
		`CASE memories.owner WHEN ? THEN 0 WHEN ? THEN 1 ELSE 2 END, memories.created_seq, memories.id`, 0)
	if err != nil {
		return nil, fmt.Errorf("memories for: %w", err)
	}
	return memories, nil
}

// ActiveMemoryCount is how many active lines ride some conversation: every
// owner's but the quarantine's, which no conversation is told.
func (s *Store) ActiveMemoryCount() (int, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM memories WHERE status = ? AND owner <> ?`, MemoryActive, OwnerLegacyProject).Scan(&count); err != nil {
		return 0, fmt.Errorf("count memories: %w", err)
	}
	return count, nil
}

// ListMemories returns active memories newest-touched first. The owners list is
// the only filter, and an empty one is deliberate: this is the person's own
// audit read and the demo seeder's, never a retrieval.
func (s *Store) ListMemories(owners []string, limit int) ([]Memory, error) {
	where, ownerArgs := ownerFilterSQL(owners)
	args := append([]any{MemoryActive}, ownerArgs...)
	memories, err := s.queryMemories(`WHERE status = ?`+where, args, `updated_seq DESC, id`, limit)
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}
	return memories, nil
}

// MemoryRecord reads one memory by id whatever its status.
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
// transaction. *sql.DB and *sql.Tx already share this method.
type memoryQuerier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// queryMemories is the single reader every memory read goes through, so no two
// of them can come to disagree about how a row decodes.
func (s *Store) queryMemories(where string, args []any, order string, limit int) ([]Memory, error) {
	return queryMemoriesOn(s.db, where, args, order, limit)
}

// queryMemoriesOn is queryMemories against a caller's own handle. The timestamp
// is a correlated lookup by primary key, one rowid seek per row carried out.
func queryMemoriesOn(db memoryQuerier, where string, args []any, order string, limit int) ([]Memory, error) {
	statement := `
		SELECT memories.id, memories.owner, memories.type, memories.scope, memories.title,
		       memories.text, memories.tags, memories.status, memories.origin,
		       memories.project_name, memories.created_seq, memories.updated_seq,
		       memories.source_session, memories.source_seq, memories.always_on,
		       COALESCE((SELECT ts FROM events WHERE seq = memories.updated_seq), ''), COALESCE((SELECT mass FROM memory_activation WHERE memory_id=memories.id),1), COALESCE((SELECT rehearsals FROM memory_activation WHERE memory_id=memories.id),0), COALESCE((SELECT at FROM memory_activation WHERE memory_id=memories.id),0), COALESCE((SELECT json_group_object(t.topic,f.n) FROM memory_topics t JOIN memory_topic_fans f ON f.topic=t.topic WHERE t.memory_id=memories.id),'{}'),COALESCE((SELECT n FROM memory_owner_counts WHERE owner=memories.owner),1),COALESCE((SELECT json_group_object(topic,n) FROM memory_associations WHERE memory_id=memories.id),'{}')
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
		var tags, updatedAt, topicFans, topicStrength string
		var activationAt int64
		if err := rows.Scan(&memory.ID, &memory.Owner, &memory.Type, &memory.Scope, &memory.Title,
			&memory.Text, &tags, &memory.Status, &memory.Origin, &memory.ProjectName,
			&memory.CreatedSeq, &memory.UpdatedSeq, &memory.SourceSession, &memory.SourceSeq,
			&memory.Rule, &updatedAt, &memory.Activation, &memory.Rehearsals, &activationAt, &topicFans, &memory.OwnerFan, &topicStrength); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(tags), &memory.Tags); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(topicFans), &memory.TopicFans)
		_ = json.Unmarshal([]byte(topicStrength), &memory.TopicStrength)
		memory.ActivationAt = time.Unix(activationAt, 0)
		if updatedAt != "" {
			// AN UNREADABLE TIMESTAMP IS AN UNKNOWN AGE, NOT A FAILED READ.
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
	// A PAYLOAD WITH NO OWNER IS A JOURNAL WRITTEN BEFORE THE COLUMN EXISTED,
	// and it is answered the way the migration answers its rows.
	payload.Owner = ownerForReplay(payload)
	tags, err := memoryTagsJSON(payload.Tags)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		INSERT INTO memories (id, owner, type, scope, title, text, tags, status, created_seq, updated_seq, source_session, source_seq, origin, project_name)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		payload.ID, payload.Owner, payload.Type, payload.Scope, payload.Title, payload.Text,
		tags, MemoryActive, seq, seq, payload.SourceSession, memorySourceSeq(payload.SourceSession, seq),
		payload.Origin, payload.ProjectName)
	if err != nil {
		return err
	}
	return indexMemoryOn(tx, payload.ID, payload.Text, payload.Tags, payload.SourceSession)
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
	if err := requireOneRow(result, "update targets missing or inactive memory", payload.ID); err != nil {
		return err
	}
	return indexMemoryOn(tx, payload.ID, payload.Text, payload.Tags, payload.SourceSession)
}

func applyMemorySupersede(tx *sql.Tx, payload memorySupersedePayload, seq int64) error {
	var pinned bool
	if err := tx.QueryRow(`SELECT (always_on != 0 OR origin='person') FROM memories WHERE id=?`, payload.OldID).Scan(&pinned); err != nil {
		return err
	}

	result, err := tx.Exec(`
		UPDATE memories SET status = ?, updated_seq = ?, source_session = ?, source_seq = ?
		WHERE id = ? AND status = ?`,
		MemorySuperseded, seq, payload.SourceSession, memorySourceSeq(payload.SourceSession, seq), payload.OldID, MemoryActive)
	if err != nil {
		return err
	}
	if err := requireOneRow(result, "supersession targets missing or inactive memory", payload.OldID); err != nil {
		return err
	}
	if err := applyMemoryAdd(tx, payload.New, seq); err != nil {
		return err
	}
	if pinned {
		_, err := tx.Exec(`UPDATE memories SET always_on=1 WHERE id=?`, payload.New.ID)
		return err
	}
	return nil
}

func applyMemoryForget(tx *sql.Tx, payload memoryForgetPayload, seq int64) error {
	_, err := tx.Exec(`
		UPDATE memories SET status = ?, updated_seq = ?, source_session = ?, source_seq = ?
		WHERE id = ? AND status <> ?`,
		MemoryForgotten, seq, payload.SourceSession, memorySourceSeq(payload.SourceSession, seq), payload.ID, MemoryForgotten)
	if err != nil {
		return err
	}
	// A TOMBSTONE FOR A ROW THIS STORE NEVER HAD IS A NO-OP ON REPLAY — an
	// earlier build's sync could journal one ahead of its row — and a tombstone
	// for a row that is already forgotten is refused by the live door before it
	// is journaled, so neither needs an error here.
	return nil
}

func memorySourceSeq(sourceSession string, seq int64) int64 {
	if strings.TrimSpace(sourceSession) == "" {
		return 0
	}
	return seq
}

func applyMemoryRestore(tx *sql.Tx, payload memoryRestorePayload, seq int64) error {
	result, err := tx.Exec(`UPDATE memories SET status = ?, updated_seq = ? WHERE id = ? AND status IN (?, ?)`,
		MemoryActive, seq, payload.ID, MemoryForgotten, MemorySuperseded)
	if err != nil {
		return err
	}
	return requireOneRow(result, "restore targets missing or active memory", payload.ID)
}

func applyMemoryMoved(tx *sql.Tx, payload memoryMovedPayload, seq int64) error {
	owner := normalizeOwner(payload.Owner)
	if !ValidOwner(owner) {
		return fmt.Errorf("%w: moved event names owner %q", ErrInvalid, payload.Owner)
	}
	result, err := tx.Exec(`UPDATE memories SET owner = ?, scope = ?, project_name = ?, updated_seq = ? WHERE id = ?`,
		owner, OwnerScopeOf(owner), payload.ProjectName, seq, payload.ID)
	if err != nil {
		return err
	}
	return requireOneRow(result, "move targets missing memory", payload.ID)
}

// requireOneRow turns "the statement touched nothing" into the error a live
// write and a replay must both refuse with.
func requireOneRow(result sql.Result, why, id string) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("%w: %s %q", ErrInvalid, why, id)
	}
	return nil
}

// memoryTagsJSON is why a memory with no tags reads back the same way after a
// rebuild as it did before one: the payloads spell tags `omitempty`, and
// json.Marshal of the nil slice a replay decodes would write "null" where the
// live write had written "[]".
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

// memoryPayloadFrom is the one gate every written memory passes through.
//
// OWNER IS THE AUTHORITY AND SCOPE IS DERIVED from it (memory_owner.go): a
// caller that names only the old scope word is answered as a legacy write —
// `user` and `env` map to their owners, and a bare `project` maps to the
// QUARANTINE, because a caller that cannot name the project's key has just
// demonstrated that it cannot prove the row belongs anywhere.
func memoryPayloadFrom(m Memory) (memoryPayload, error) {
	memoryType := strings.ToLower(strings.TrimSpace(m.Type))
	if memoryType == "" {
		memoryType = MemoryFact
	}
	if !validMemoryType(memoryType) {
		return memoryPayload{}, fmt.Errorf("%w: unknown memory type %q", ErrInvalid, m.Type)
	}
	owner := normalizeOwner(m.Owner)
	if owner == "" {
		switch strings.ToLower(strings.TrimSpace(m.Scope)) {
		case MemoryScopeUser:
			owner = OwnerUser
		case MemoryScopeEnv:
			owner = OwnerMachine
		case MemoryScopeProject:
			owner = OwnerLegacyProject
		default:
			return memoryPayload{}, fmt.Errorf("%w: unknown memory scope %q", ErrInvalid, m.Scope)
		}
	}
	if !ValidOwner(owner) {
		return memoryPayload{}, fmt.Errorf("%w: unknown memory owner %q", ErrInvalid, m.Owner)
	}
	title := m.Title
	if strings.TrimSpace(title) == "" {
		title = MemoryTitleOf(redact.Secrets(m.Text))
	}
	title, text, tags, err := validMemoryBody(title, m.Text, m.Tags)
	if err != nil {
		return memoryPayload{}, err
	}
	if owner == OwnerLegacyProject && !hasTag(tags, legacyTag) {
		// THE QUARANTINE CARRIES ITS MARK, wherever it is read back.
		tags = append(tags, legacyTag)
	}
	projectName := strings.TrimSpace(m.ProjectName)
	if OwnerKindOf(owner) != OwnerKindProject {
		projectName = ""
	}
	return memoryPayload{
		ID:          strings.TrimSpace(m.ID),
		Owner:       owner,
		Type:        memoryType,
		Scope:       OwnerScopeOf(owner),
		Title:       title,
		Text:        text,
		Tags:        tags,
		Origin:      validMemoryOrigin(m.Origin),
		ProjectName: projectName,
	}, nil
}

// validMemoryOrigin keeps the origin column to the three words this build
// spells; anything else is unknown, which renders as nothing.
func validMemoryOrigin(origin string) string {
	switch origin = strings.TrimSpace(origin); origin {
	case OriginPerson, OriginModel, OriginNoticed:
		return origin
	}
	return ""
}

// validMemoryBody is the ONE normalization every memory mouth shares. It
// redacts first and then enforces the caps, so a credential cannot ride a
// title, a body or a tag through a door that forgot to redact. Lengths are
// counted in runes, because a cap measured in bytes refuses a shorter sentence
// for being written in a different language.
func validMemoryBody(title, text string, tags []string) (string, string, []string, error) {
	title = redact.Secrets(strings.TrimSpace(title))
	text = redact.Secrets(strings.TrimSpace(text))
	if text == "" {
		return "", "", nil, fmt.Errorf("%w: a memory with no text says nothing", ErrInvalid)
	}
	if utf8.RuneCountInString(title) > MemoryTitleRunes {
		return "", "", nil, fmt.Errorf("%w: title is %d characters, the limit is %d",
			ErrInvalid, utf8.RuneCountInString(title), MemoryTitleRunes)
	}
	if utf8.RuneCountInString(text) > MemoryTextRunes {
		return "", "", nil, fmt.Errorf("%w: that is %d characters, and a memory is at most %d",
			ErrInvalid, utf8.RuneCountInString(text), MemoryTextRunes)
	}
	kept := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag = redact.Secrets(strings.ToLower(strings.TrimSpace(tag))); tag != "" {
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

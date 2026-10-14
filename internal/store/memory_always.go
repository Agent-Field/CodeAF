package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// A RULE IS A MEMORY MARKED ALWAYS. "Always use tabs here", "never touch the
// public API" — a sentence that governs work nobody has done yet is not
// something to be recalled when a router judges it relevant; it is put in front
// of every conversation turn and every task brief at its owner's scope, in a
// stable order, with no ages (docs/design/automations/DESIGN.md, "Rules become
// memories marked always"). A memory without the flag is exactly what it was.
//
// THE FLAG IS A COLUMN AND AN EVENT, like everything else in this table. The add
// and the supersede that create a row carry it in their payloads ([memoryPayload]),
// and a change to it on a row that already exists is one event of its own,
// [EventMemoryAlways], applied by the same function on the live path, the
// replay and the sync fold — so a rebuilt brain, a folded one and a live one
// cannot disagree about which lines are rules.
//
// THE SCOPES ARE MEMORY'S OWN OWNERS: the person, this machine, one project.
// There is no rule for one conversation only and no "not here" exception, and
// the quarantine is never a rule's home — a row nobody can prove the project of
// is never put in front of anything ([OwnerLegacyProject]).

// EventMemoryAlways carries one row's flag, set or cleared. It names the owner
// so a receiving store can check the row it lands on is the row it was meant
// for, and it never moves updated_seq: a rule is the same memory before and
// after, and a toggle that re-dated it would reorder the router's recency
// ranking and the place's pages over a change no words underwent.
const EventMemoryAlways EventKind = "memory_always"

// WriteOutcomePromoted is the write door's third answer: the words were already
// remembered, the request asked for them always, and the row that was there is
// the rule now rather than a second row beside it.
const WriteOutcomePromoted = "promoted"

// memoryAlwaysPayload is one flag change. Always is spelled without omitempty
// because false is a statement here — the rule was taken back — and not the
// absence of one.
type memoryAlwaysPayload struct {
	ID     string `json:"id"`
	Owner  string `json:"owner,omitempty"`
	Always bool   `json:"always"`
}

// memoriesAlwaysIndexDDL is the one spelling of the rules index. It is PARTIAL
// over exactly the rows [Store.AlwaysMemories] reads — active and always — and
// ordered the way that read orders within an owner, so the read every turn
// opens with is a seek over the handful of rules rather than a walk of the
// owner's memories. It lives in the migration and not in [memoriesSchema], for
// the reason that schema's own comment gives.
const memoriesAlwaysIndexDDL = `CREATE INDEX IF NOT EXISTS memories_always_active ON memories (owner, created_seq) WHERE status = 'active' AND always_on = 1`

// migrateMemoriesAlways gives a store written before rules existed its column
// and its index, once, in one immediate transaction.
//
// THE COLUMN IS ASKED ABOUT INSIDE THE TRANSACTION THAT ADDS IT, which is the
// whole of what makes two processes upgrading one store at the same moment
// safe: the transaction is IMMEDIATE ([Open]'s `_txlock`), so the second waits
// for the first to commit and then reads the column the first one made, rather
// than both reading "absent" off the pool and the second dying on a duplicate
// column. The index waits for the owner column too, which [migrateMemoriesOwner]
// has settled by the time [Open] calls this.
func migrateMemoriesAlways(db *sql.DB) error {
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("migrate memory rules: %w", err)
	}
	defer tx.Rollback()
	found, err := tableHasColumnOn(tx, "memories", "always_on")
	if err != nil {
		return fmt.Errorf("migrate memory rules: %w", err)
	}
	if !found {
		if _, err := tx.Exec(`ALTER TABLE memories ADD COLUMN always_on INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("migrate memory rules: %w", err)
		}
	}
	if _, err := tx.Exec(memoriesAlwaysIndexDDL); err != nil {
		return fmt.Errorf("migrate memory rules: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate memory rules: %w", err)
	}
	return nil
}

// keepsTheRule is the flag a REPLACEMENT carries: the retired row's, or the
// replacement's own when it asked for one.
//
// A RULE SAID BETTER IS STILL A RULE. A supersession is the same memory in
// better words, so a replacement that dropped the flag would quietly take a
// rule out of every conversation the person set it over — through a door the
// person never touched. And a write that asked for always and settled as the
// replacement of a recalled line asked for a rule; the line it lands as is one.
func keepsTheRule(retired, asked bool) bool { return retired || asked }

// SetMemoryAlways sets or clears one row's flag BY ID, with no owner filter.
//
// It is the raw door, for the one caller [Store.UpdateMemory] reserves raw doors
// for: the person's own surface, which may manage any row it is showing (the
// memory place's `a`). A caller holding an id from a model or from the wire
// goes through [Store.SetMemoryAlwaysForOwners] instead.
func (s *Store) SetMemoryAlways(id string, always bool) error {
	return s.setMemoryAlways(nil, id, always)
}

// SetMemoryAlwaysForOwners sets or clears one row's flag only when that row is
// owned by one of the named owners — the same store-level guard the update and
// supersede doors keep, so an id a decider named or a stale pointer carried
// cannot make another project's memory a rule here.
func (s *Store) SetMemoryAlwaysForOwners(owners []string, id string, always bool) error {
	if len(owners) == 0 {
		return fmt.Errorf("set memory rule: %w: no owner was named", ErrInvalid)
	}
	return s.setMemoryAlways(owners, id, always)
}

// setMemoryAlways is both doors. owners is nil for the raw one.
//
// AN UNCHANGED FLAG WRITES NOTHING. Setting a rule that is already a rule is
// not history, so it journals no event — the same law the fold keeps about a
// no-op — and answers success, because what was asked for is what stands.
func (s *Store) setMemoryAlways(owners []string, id string, always bool) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("set memory rule: %w: id is required", ErrInvalid)
	}
	tx, err := s.beginWrite()
	if err != nil {
		return fmt.Errorf("set memory rule: %w", err)
	}
	defer tx.Rollback()
	if owners != nil {
		if err := requireMemoryOwners(tx, owners, id); err != nil {
			return fmt.Errorf("set memory rule: %w", err)
		}
	}
	var owner, status string
	var current bool
	if err := tx.QueryRow(`SELECT owner, status, always_on FROM memories WHERE id = ?`, id).Scan(&owner, &status, &current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("set memory rule: %w: missing memory", ErrInvalid)
		}
		return fmt.Errorf("set memory rule: %w", err)
	}
	if status != MemoryActive {
		// A LINE THAT IS NOT HELD IS NOT IN FRONT OF ANYTHING, so it cannot be
		// made a rule: the rule would claim a place it does not have until a
		// restore nobody has asked for.
		return fmt.Errorf("set memory rule: %w: only a memory that is still held can be a rule", ErrInvalid)
	}
	if always && owner == OwnerLegacyProject {
		return fmt.Errorf("set memory rule: %w: nothing proves which project this memory belongs to, so it cannot be a rule", ErrInvalid)
	}
	if current == always {
		return nil
	}
	payload := memoryAlwaysPayload{ID: id, Owner: owner, Always: always}
	if _, _, err := appendEvent(tx, id, EventMemoryAlways, payload); err != nil {
		return fmt.Errorf("set memory rule: %w", err)
	}
	if err := applyMemoryAlways(tx, payload); err != nil {
		return fmt.Errorf("set memory rule: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("set memory rule: %w", err)
	}
	return nil
}

// applyMemoryAlways materializes one flag change. It is shared by the live
// doors, Rebuild and the sync fold, so the three cannot come to mean different
// things by the same event.
//
// IT TOUCHES ONLY THE COLUMN. updated_seq and the provenance stay where they
// were ([EventMemoryAlways] says why), and the search index is untouched because
// the words are.
func applyMemoryAlways(tx *sql.Tx, payload memoryAlwaysPayload) error {
	result, err := tx.Exec(`UPDATE memories SET always_on = ? WHERE id = ? AND status = ?`,
		payload.Always, strings.TrimSpace(payload.ID), MemoryActive)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("%w: a rule change targets missing or inactive memory %q", ErrInvalid, payload.ID)
	}
	return nil
}

// AlwaysMemories is every rule in force for the named owners: active, always,
// never the quarantine, in ONE deterministic order.
//
// THE ORDER IS THE BLOCK'S BYTE STABILITY. What this returns is rendered into
// the first message of every request ([internal/session]'s always block), and
// one byte moved there re-prices the whole conversation at the uncached rate.
// So the order is a function of the rows alone — the person's rules first, then
// this machine's, then the project's, each owner's oldest first, the id settling
// a tie — and never of a clock, a ranking counter or the order SQLite happened
// to read them in.
//
// THE SETTLED ONES LEAD within each owner, which is the same choice the standing
// section made for the same reason: a rule that has stood for months is the
// house rule, and the one made this morning is already in the conversation that
// made it. When a caller's cap bites, it is the newest that wait.
//
// An empty owners list is refused, as on every read that can put a memory in
// front of a model; limit zero or less is every rule.
func (s *Store) AlwaysMemories(owners []string, limit int) ([]Memory, error) {
	if len(owners) == 0 {
		return nil, fmt.Errorf("always memories: %w: no owner was named", ErrInvalid)
	}
	kept := make([]string, 0, len(owners))
	for _, owner := range owners {
		if owner = normalizeOwner(owner); owner != OwnerLegacyProject {
			kept = append(kept, owner)
		}
	}
	if len(kept) == 0 {
		// THE QUARANTINE ALONE HOLDS NO RULES. Handing [ownerFilterSQL] an empty
		// list would mean every owner, which is the janitor's answer and never
		// this read's.
		return nil, nil
	}
	where, args := ownerFilterSQL(kept)
	memories, err := s.queryMemories(
		`WHERE memories.status = '`+MemoryActive+`' AND memories.always_on = 1`+where+` AND memories.owner <> '`+OwnerLegacyProject+`'`,
		args,
		`CASE WHEN memories.owner = '`+OwnerUser+`' THEN 0 WHEN memories.owner = '`+OwnerMachine+`' THEN 1 ELSE 2 END, memories.created_seq, memories.id`,
		limit)
	if err != nil {
		return nil, fmt.Errorf("always memories: %w", err)
	}
	return memories, nil
}

// commitPromotedWrite is the write door's promotion: the words were already
// remembered as an ordinary memory and this write asked for them always, so the
// row that was there becomes the rule — one event, in the write's own
// transaction — rather than a second row with the same words.
//
// When provenance was supplied it lands on the row that was promoted, exactly
// as a skip's does ([commitDuplicateWrite]).
func commitPromotedWrite(tx *sql.Tx, owner string, evidence *ContextualEvidence, existing *Memory) (WriteResult, ContextualEvidence, error) {
	var committed ContextualEvidence
	payload := memoryAlwaysPayload{ID: existing.ID, Owner: owner, Always: true}
	if _, _, err := appendEvent(tx, existing.ID, EventMemoryAlways, payload); err != nil {
		return WriteResult{}, committed, fmt.Errorf("write memory: %w", err)
	}
	if err := applyMemoryAlways(tx, payload); err != nil {
		return WriteResult{}, committed, fmt.Errorf("write memory: %w", err)
	}
	if evidence != nil {
		evidence.MemoryID = existing.ID
		if evidence.Owner == "" {
			evidence.Owner = owner
		}
		if err := validateContextualEvidence(*evidence); err != nil {
			return WriteResult{}, committed, fmt.Errorf("write memory: %w", err)
		}
		var err error
		if committed, err = appendContextualEvidenceTx(tx, *evidence); err != nil {
			return WriteResult{}, committed, fmt.Errorf("write memory: %w", err)
		}
	}
	promoted := *existing
	promoted.Always = true
	return WriteResult{Memory: promoted, Outcome: WriteOutcomePromoted, Why: "already kept as " + existing.Title + ", and always now"}, committed, nil
}

// foldMemoryAlways applies a flag change that arrived from another store. It
// acts only on an ACTIVE row that is already here, under the owner the row
// itself carries: an event naming a different owner than the row it lands on is
// about some other row and changes nothing, and the receiver's policy is asked
// about the row's own owner, as it is for a forget. A rule cannot be folded onto
// the quarantine any more than it can be set there.
func foldMemoryAlways(tx *sql.Tx, event Event, seq int64, fts bool, allow func(owner string) bool) (bool, error) {
	var payload memoryAlwaysPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return false, err
	}
	rowOwner, active := memoryRowOwner(tx, payload.ID)
	if !active {
		return false, nil
	}
	if payload.Owner != "" && normalizeOwner(payload.Owner) != rowOwner {
		return false, nil
	}
	if allow != nil && !allow(rowOwner) {
		return false, nil
	}
	if payload.Always && rowOwner == OwnerLegacyProject {
		return false, nil
	}
	var current bool
	if err := tx.QueryRow(`SELECT always_on FROM memories WHERE id = ?`, payload.ID).Scan(&current); err != nil {
		return false, err
	}
	if current == payload.Always {
		// ALREADY SO. A re-delivered rule is a skip, never a second event.
		return false, nil
	}
	return true, applyMemoryAlways(tx, payload)
}

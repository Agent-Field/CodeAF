package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// Owners are who a memory belongs to, and they are the whole of the
// permission model: a memory has exactly one owner, and every read that can
// put a memory in front of a model filters on owner equality. There is no
// audience list and no visibility knob to forget — sharing is a later, explicit
// copy with a new owner.
//
// The kinds a build mints today:
//
//	OwnerUser            the person, every project, every machine the store reaches
//	OwnerMachine         this machine only — an env-scoped memory's owner
//	OwnerProject(key)    one project; the key is [gitidentity.ProjectKey]'s output
//	OwnerLegacyProject   scope=project rows whose project nobody can prove
//
// OwnerLegacyProject is the quarantine. The old schema kept a `project` scope
// with no project identity on the row, so the true owner is unknowable from
// the row alone — and attributing one project's memory to the person at large,
// or to a guessed project, would widen what can see it. Quarantined rows are
// never injected, never deduplicated against, and never matched by a forget
// query; the person re-homes or lets go of them from the memory place, and a
// door that can prove a row's source session lived in a workspace can re-home
// it to that project ([Store.RehomeMemories]).
//
// team:<id> owners are deliberately ABSENT, not deferred in a comment: no door
// mints one yet, and a capability that cannot work is absent rather than
// broken. Sharing with a team arrives with the sync work, as an explicit copy.
const (
	OwnerUser          = "user"
	OwnerMachine       = "machine"
	OwnerLegacyProject = "project:legacy"

	// legacyTag marks a row the migration quarantined. It survives re-homing
	// on purpose: a memory's source is never cleaned off it, and a person who
	// wants to know which rows came in blind can search for the tag.
	legacyTag = "legacy-project"
)

// OwnerProject spells a project owner from a project key. The key itself is
// opaque — a hash minted by internal/gitidentity — and is never a path: a path
// moves, a hash of a normalized remote does not.
func OwnerProject(key string) string {
	return "project:" + strings.TrimSpace(key)
}

// OwnerKindOf answers the kind word of an owner: `user`, `machine`, `project`
// or "" for an owner this build does not understand.
func OwnerKindOf(owner string) string {
	switch {
	case owner == OwnerUser || owner == OwnerMachine:
		return owner
	case strings.HasPrefix(owner, "project:"):
		if key := strings.TrimPrefix(owner, "project:"); key != "" {
			return "project"
		}
	}
	return ""
}

// OwnerScopeOf answers the legacy scope word a row's owner spells, which is
// what the `scope` column and the snapshot's shelf grouping keep reading. The
// mapping is fixed by the column's history: `user`, `project` and `env` are
// the three words the schema has always carried, and this is where owner and
// scope are kept from drifting apart — a write derives one from the other.
func OwnerScopeOf(owner string) string {
	switch OwnerKindOf(owner) {
	case OwnerUser:
		return MemoryScopeUser
	case OwnerMachine:
		return MemoryScopeEnv
	case "project":
		return MemoryScopeProject
	}
	return MemoryScopeUser
}

// ValidOwner answers whether owner is one this build can write. An empty owner
// is not valid here — every caller that has one names it, and the fallback for
// an empty owner lives at the call sites that know what an empty MEANS, not in
// the validator where it would silently widen.
func ValidOwner(owner string) bool {
	return OwnerKindOf(owner) != ""
}

// OwnerLegacyTag is the marker a quarantined row carries, for the reader that
// names it.
const OwnerLegacyTag = legacyTag

// normalizeOwner is the one place an owner is cleaned before it is compared or
// written: trimmed, and matched as invalid when it is empty.
func normalizeOwner(owner string) string {
	return strings.TrimSpace(owner)
}

// ownerFilterSQL builds the owner clause the owner-keyed reads share. An empty
// owners list means "no filter" and is the janitor's answer only — the callers
// that must never see across owners assert the list is non-empty before they
// call, and this function is not where that is decided.
func ownerFilterSQL(owners []string) (string, []any) {
	if len(owners) == 0 {
		return "", nil
	}
	cleaned := make([]string, 0, len(owners))
	for _, owner := range owners {
		if owner = normalizeOwner(owner); ValidOwner(owner) {
			cleaned = append(cleaned, owner)
		}
	}
	if len(cleaned) == 0 {
		// EVERY OWNER NAMED WAS INVALID. That is a caller bug, and the honest
		// answer is "nothing visible" rather than "everything visible": a
		// filter that cannot be understood must never degrade to no filter.
		return ` AND 1 = 0`, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(cleaned)), ",")
	args := make([]any, 0, len(cleaned))
	for _, owner := range cleaned {
		args = append(args, owner)
	}
	return ` AND memories.owner IN (` + placeholders + `)`, args
}

// migrateMemoriesOwner gives every memory row an owner, once, from the only
// authority the old rows carry:
//
//   - scope=user  → owner=user      (already the person's; nothing moves)
//   - scope=env   → owner=machine   (this machine's own truth, renamed)
//   - scope=project → owner=project:legacy, tagged legacy-project
//
// THE PROJECT ROWS ARE THE POINT AND THEY ARE NEVER GUESSED. The old scope
// said "true in a project" and carried no project, so the only honest owners
// are "this machine" (never widens) and "unknown" (never injects). Rows whose
// source session is later provable to a workspace are re-homed by the door
// that can do the proving ([Store.RehomeMemories]); everything else stays in
// quarantine, visible to the person and to nobody's model.
//
// It is idempotent — a row that already has an owner is left alone — and it is
// written in one transaction with the column it backfills.
func migrateMemoriesOwner(db *sql.DB) error {
	found, err := tableHasColumn(db, "memories", "owner")
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("migrate memory owners: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`ALTER TABLE memories ADD COLUMN owner TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("migrate memory owners: %w", err)
	}
	// The column lands empty and is filled from scope in one pass each, so no
	// row is ever left with an owner the validator would refuse.
	if _, err := tx.Exec(`UPDATE memories SET owner = ? WHERE scope = ? AND owner = ''`,
		OwnerUser, MemoryScopeUser); err != nil {
		return fmt.Errorf("migrate memory owners: %w", err)
	}
	if _, err := tx.Exec(`UPDATE memories SET owner = ? WHERE scope = ? AND owner = ''`,
		OwnerMachine, MemoryScopeEnv); err != nil {
		return fmt.Errorf("migrate memory owners: %w", err)
	}
	rows, err := tx.Query(`SELECT id, tags FROM memories WHERE scope = ? AND owner = ''`,
		MemoryScopeProject)
	if err != nil {
		return fmt.Errorf("migrate memory owners: %w", err)
	}
	type quarantined struct {
		id   string
		tags []string
	}
	var pending []quarantined
	for rows.Next() {
		var id, tags string
		if err := rows.Scan(&id, &tags); err != nil {
			rows.Close()
			return fmt.Errorf("migrate memory owners: %w", err)
		}
		var parsed []string
		if err := json.Unmarshal([]byte(tags), &parsed); err != nil {
			parsed = nil
		}
		pending = append(pending, quarantined{id: id, tags: parsed})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("migrate memory owners: %w", err)
	}
	rows.Close()
	for _, row := range pending {
		if !hasTag(row.tags, legacyTag) {
			row.tags = append(row.tags, legacyTag)
		}
		encoded, err := json.Marshal(row.tags)
		if err != nil {
			return fmt.Errorf("migrate memory owners: %w", err)
		}
		if _, err := tx.Exec(`UPDATE memories SET owner = ?, tags = ? WHERE id = ?`,
			OwnerLegacyProject, string(encoded), row.id); err != nil {
			return fmt.Errorf("migrate memory owners: %w", err)
		}
	}
	// Anything still empty is a scope this build does not know, written by a
	// later or a hand-edited store. It is quarantined too — the same answer,
	// for the same reason: an owner nobody can prove never injects.
	if _, err := tx.Exec(`UPDATE memories SET owner = ? WHERE owner = ''`, OwnerLegacyProject); err != nil {
		return fmt.Errorf("migrate memory owners: %w", err)
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS memories_owner_active ON memories (owner, updated_seq DESC) WHERE status = 'active'`); err != nil {
		return fmt.Errorf("migrate memory owners: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate memory owners: %w", err)
	}
	return nil
}

// RehomeMemories moves quarantined rows to a provable owner. It is the one
// door out of quarantine, and it takes ids rather than a scope: a pass that
// re-homes EVERYTHING quarantined to one project would be exactly the guess
// the migration refused to make.
//
// Only rows that ARE quarantined move — an id naming a row in any other owner
// is left untouched, because a re-home that could strip an owner it disagrees
// with would be a second, wider permission model hiding in a janitor's name.
func (s *Store) RehomeMemories(ids []string, owner string) (int, error) {
	owner = normalizeOwner(owner)
	if !ValidOwner(owner) {
		return 0, fmt.Errorf("rehome memories: %w: owner %q is not one this build mints", ErrInvalid, owner)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	tx, err := s.beginWrite()
	if err != nil {
		return 0, fmt.Errorf("rehome memories: %w", err)
	}
	defer tx.Rollback()
	moved := 0
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		result, err := tx.Exec(`UPDATE memories SET owner = ?, scope = ? WHERE id = ? AND owner = ?`,
			owner, OwnerScopeOf(owner), id, OwnerLegacyProject)
		if err != nil {
			return 0, fmt.Errorf("rehome memories: %w", err)
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("rehome memories: %w", err)
		}
		moved += int(changed)
	}
	// A re-home is a permission change and it is journaled as one: the event
	// says which rows moved and where, so a later sync fold can agree about it.
	if moved > 0 {
		if _, _, err := appendEvent(tx, "memory", EventMemoryRehomed, map[string]any{
			"ids": ids, "owner": owner,
		}); err != nil {
			return 0, fmt.Errorf("rehome memories: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("rehome memories: %w", err)
	}
	return moved, nil
}

// QuarantinedMemories lists the rows still in quarantine, whatever their
// status, so the one surface that offers re-homing can show them. This is a
// person's audit read and never a model's: nothing here is injected anywhere.
func (s *Store) QuarantinedMemories(limit int) ([]Memory, error) {
	memories, err := s.queryMemories(`WHERE owner = ?`, []any{OwnerLegacyProject}, `updated_seq DESC, id`, limit)
	if err != nil {
		return nil, fmt.Errorf("quarantined memories: %w", err)
	}
	return memories, nil
}

// ListProjectMemories lists every project-owned row — every project's rows
// AND the quarantine's — for the person's own project shelf. It is the read
// the surface's `project` scope word means: a person auditing their memory
// sees all of their projects, because "which project is this surface in" is
// not a question an audit page answers (a session's retrieval answers that,
// scoped to its own project, and the session reads are where the scoping
// lives). The quarantine's rows are here too: they are project rows whose
// project nobody proved, and hiding them from the person would be hiding the
// one place re-homing starts from.
func (s *Store) ListProjectMemories(limit int) ([]Memory, error) {
	memories, err := s.queryMemories(`WHERE owner LIKE ?`, []any{"project:%"}, `updated_seq DESC, id`, limit)
	if err != nil {
		return nil, fmt.Errorf("list project memories: %w", err)
	}
	return memories, nil
}

func hasTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}

// applyRehomeOne moves one id out of quarantine to owner, and answers whether
// it moved. It is the one place the fold and the replay both re-home through,
// so a remote fold and a journal replay cannot disagree about what a rehomed
// event means. A row is moved ONLY while its current owner is the quarantine —
// whatever its status, matching the live door, which re-homes forgotten rows
// too ([QuarantinedMemories] lists them whatever their status) — and the
// journal sequence stamps the row's updated_seq so the move is visible to
// pagination like any other touch.
func applyRehomeOne(tx *sql.Tx, id, owner string, seq int64) (bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return false, nil
	}
	result, err := tx.Exec(`UPDATE memories SET owner = ?, scope = ?, updated_seq = ?
		WHERE id = ? AND owner = ?`,
		owner, OwnerScopeOf(owner), seq, id, OwnerLegacyProject)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return changed == 1, nil
}

// ownerForReplay answers the owner an event payload carries, or derives it the
// way the migration derives one from a bare scope word. It is the replay
// path's answer to "what did this row belong to before owners existed", and it
// is the same rule [memoryPayloadFrom] uses so a replayed row and a live row
// cannot disagree.
func ownerForReplay(payload memoryPayload) string {
	if owner := normalizeOwner(payload.Owner); ValidOwner(owner) {
		return owner
	}
	switch strings.ToLower(strings.TrimSpace(payload.Scope)) {
	case MemoryScopeEnv:
		return OwnerMachine
	case MemoryScopeProject:
		return OwnerLegacyProject
	default:
		return OwnerUser
	}
}

// applyMemoryRehomed reproduces a re-home during replay. It moves an id ONLY
// while that id is still quarantined, exactly what the live door would have
// moved: a re-home that could strip an owner it disagrees with would be a
// second, wider permission model hiding in a replay helper. The restriction is
// also what makes replay exact — the live door only ever moved quarantined
// rows, so a journal's rehomed events replay onto rows that are in quarantine
// when the event reaches them.
func applyMemoryRehomed(tx *sql.Tx, raw json.RawMessage, seq int64) error {
	var payload struct {
		IDs   []string `json:"ids"`
		Owner string   `json:"owner"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	if !ValidOwner(payload.Owner) {
		return fmt.Errorf("%w: rehomed event names owner %q", ErrInvalid, payload.Owner)
	}
	for _, id := range payload.IDs {
		if _, err := applyRehomeOne(tx, id, payload.Owner, seq); err != nil {
			return err
		}
	}
	return nil
}

package data

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

const residencyMigration = `
	CREATE TABLE type_residency (
		type_id TEXT PRIMARY KEY REFERENCES types(id),
		definition TEXT NOT NULL,
		mode TEXT NOT NULL,
		checkpoint TEXT NOT NULL,
		checked_at TEXT NOT NULL
	);
	CREATE TABLE residency_actions (
		type_id TEXT NOT NULL REFERENCES types(id),
		epoch INTEGER NOT NULL,
		receipt TEXT NOT NULL,
		at TEXT NOT NULL,
		PRIMARY KEY(type_id, epoch)
	);
`

type ResidencyMode string

const (
	ResidencyLive   ResidencyMode = "live"
	ResidencyMirror ResidencyMode = "mirror"
	ResidencyNative ResidencyMode = "native"
)

// ResidencyBinding pins the reviewed source and its reversible fields to a
// stable type. The private names never come from an app version or a record.
type ResidencyBinding struct {
	App, Connection, Read string
	Key                   ExternalKey
	Writable              []string
}

type TypeResidency struct {
	Mode       ResidencyMode    `json:"mode"`
	Epochs     TypeEpochs       `json:"epochs"`
	Binding    ResidencyBinding `json:"-"`
	Checkpoint string           `json:"-"`
	CheckedAt  time.Time        `json:"checked_at,omitzero"`
}

type ResidencyRow struct {
	Key      ExternalKey
	Revision string
	Claims   map[string]SourceFieldClaim
	Deleted  bool
}

type ResidencySnapshot struct {
	Rows       []ResidencyRow
	Checkpoint string
	ObservedAt time.Time
}

type ResidencyReplay struct {
	Key      ExternalKey
	Revision string
	Set      map[string]any
}

type ResidencyPlan struct {
	From         ResidencyMode `json:"from"`
	To           ResidencyMode `json:"to"`
	Rows         int           `json:"rows"`
	Conflicts    int           `json:"conflicts"`
	ReplayRows   int           `json:"replay_rows"`
	Epochs       TypeEpochs    `json:"epochs"`
	Confirmation string        `json:"confirmation"`
}

func residencyState(q querier, id string) (TypeResidency, error) {
	state := TypeResidency{}
	var definition, checked string
	err := q.QueryRow(`SELECT COALESCE(r.mode, 'native'), COALESCE(r.definition, ''), COALESCE(r.checkpoint, ''),
        COALESCE(r.checked_at, ''), COALESCE(e.mapping, 0), COALESCE(e.residency, 0)
        FROM types t LEFT JOIN type_residency r ON r.type_id = t.id LEFT JOIN type_epochs e ON e.type_id = t.id
        WHERE t.id = ?`, id).Scan(&state.Mode, &definition, &state.Checkpoint, &checked, &state.Epochs.Mapping, &state.Epochs.Residency)
	if err != nil || definition == "" {
		return state, err
	}
	if err := json.Unmarshal([]byte(definition), &state.Binding); err != nil {
		return state, err
	}
	state.CheckedAt, err = time.Parse(stamp, checked)
	return state, err
}

// Residency reports the active data route through the caller's type grant.
func (s *Store) Residency(sc Scope, typ string) (TypeResidency, error) {
	t, err := s.schema.Load().allow(sc, typ, false)
	if err != nil {
		return TypeResidency{}, err
	}
	return residencyState(s.rdb, t.ID)
}

func (w *writeTx) saveResidency(t *Type, state TypeResidency) error {
	_, err := w.tx.Exec(`INSERT INTO type_residency VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(type_id) DO UPDATE SET definition = excluded.definition,
		mode = excluded.mode, checkpoint = excluded.checkpoint, checked_at = excluded.checked_at`,
		t.ID, canonicalJSON(state.Binding), state.Mode, state.Checkpoint, state.CheckedAt.UTC().Format(stamp))
	return err
}

// DeclareResidencies opens the live routes as one registration. The callback
// stores the private declaration and credential before the writer publishes it.
func (s *Store) DeclareResidencies(bindings map[string]ResidencyBinding, register func() error) error {
	return s.write(func(w *writeTx) error {
		for _, alias := range sortedKeys(bindings) {
			binding := bindings[alias]
			t, err := w.schema.allow(Scope{App: binding.App}, alias, true)
			if err != nil {
				return err
			}
			if t.HasRowPolicy() {
				return errorf(CodeNotAllowed, "a row-policy type needs per-person live access")
			}
			state, err := residencyState(w.tx, t.ID)
			if err != nil {
				return err
			}
			if state.Binding.App != "" {
				return errorf(CodeInvalid, "the type already has a residency binding")
			}
			var existing bool
			if err := w.tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM records WHERE type_id = ? AND deleted_at IS NULL)`, t.ID).Scan(&existing); err != nil {
				return err
			}
			if existing {
				return errorf(CodeNotAllowed, "a live binding needs an empty type; move its native records first")
			}
			state.Mode, state.Binding = ResidencyLive, binding
			if err := w.saveResidency(t, state); err != nil {
				return err
			}
			next := state.Epochs
			next.Mapping++
			next.Residency++
			if err := w.advanceEpochs(t.ID, state.Epochs, next); err != nil {
				return err
			}
		}
		return register()
	})
}

// Cutover checks and, with a matching confirmation, commits the same plan.
// A source transaction fences the snapshot and performs conditional reverse
// replay while the local writer serializes record edits and epoch changes.
func (s *Store) Cutover(ctx context.Context, binding ResidencyBinding, alias string, to ResidencyMode, quiesced bool, snapshot ResidencySnapshot,
	confirmation string, replay func([]ResidencyReplay) (ResidencySnapshot, error)) (ResidencyPlan, error) {
	var plan ResidencyPlan
	err := s.write(func(w *writeTx) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		t, err := w.schema.allow(Scope{App: binding.App}, alias, true)
		if err != nil {
			return err
		}
		pub, err := readPublication(w.tx, binding.App)
		if err != nil || pub.Archived || pub.Deleted {
			return errorf(CodeNotAllowed, "cut-over needs a kept app")
		}
		if t.HasRowPolicy() {
			return errorf(CodeNotAllowed, "a row-policy type needs per-person live access")
		}
		state, err := residencyState(w.tx, t.ID)
		if err != nil {
			return err
		}
		if state.Binding.App == "" {
			state.Mode = ResidencyLive
			state.Binding = binding
		}
		if canonicalJSON(state.Binding) != canonicalJSON(binding) {
			return errorf(CodeStale, "the type uses a different source binding")
		}
		if !residencyTransition(state.Mode, to) {
			return errorf(CodeInvalid, "cannot move from %s to %s", state.Mode, to)
		}
		if (to == ResidencyNative || state.Mode == ResidencyNative) && !quiesced {
			return errorf(CodeNotAllowed, "pause upstream writers before changing write authority")
		}
		plan = ResidencyPlan{From: state.Mode, To: to, Epochs: state.Epochs}
		revisions, reverse, err := w.inspectResidency(t, state, snapshot.Rows, &plan)
		if err != nil {
			return err
		}
		plan.ReplayRows = len(reverse)
		// The approval covers the application version, exact rows, local
		// revisions, both epochs and any native edits to replay.
		plan.Confirmation = fmt.Sprintf("%x", sha256.Sum256([]byte(canonicalJSON([]any{
			plan, binding, pub.Version, pub.Metadata, snapshot.Rows, revisions, reverse,
		}))))
		if confirmation == "" {
			return nil
		}
		if confirmation != plan.Confirmation || plan.Conflicts > 0 {
			return errorf(CodeStale, "the cut-over plan changed or has conflicts; preview it again")
		}
		next := state.Epochs
		next.Mapping++
		next.Residency++
		if err := w.advanceEpochs(t.ID, state.Epochs, next); err != nil {
			return err
		}
		plan.Epochs = next
		state.Mode, state.Epochs = to, next
		if plan.From == ResidencyNative {
			snapshot, err = replay(reverse)
			if err != nil {
				return err
			}
		}
		state.Checkpoint, state.CheckedAt = snapshot.Checkpoint, snapshot.ObservedAt
		if to == ResidencyLive {
			if err := w.retireMirrorRows(t, binding.Key); err != nil {
				return err
			}
		}
		if to != ResidencyLive {
			if err := w.applyResidencyRows(t, state, snapshot.Rows); err != nil {
				return err
			}
		}
		if err := w.saveResidency(t, state); err != nil {
			return err
		}
		if _, err := w.tx.Exec(`INSERT INTO residency_actions VALUES (?, ?, ?, ?)`,
			t.ID, next.Residency, canonicalJSON(plan), w.now.UTC().Format(stamp)); err != nil {
			return err
		}
		if err := w.invalidate(EventMappingChanged, t.ID, binding.Key.Binding); err != nil {
			return err
		}
		return ctx.Err()
	})
	return plan, err
}

// A live route keeps the crosswalk and audit receipts. Retiring the mirror
// clears copied values and marks its record shells absent without recycling IDs.
func (w *writeTx) retireMirrorRows(t *Type, key ExternalKey) error {
	args := []any{t.ID, key.Binding, key.Object, key.Generation}
	members := `SELECT entity_id FROM external_keys WHERE binding = ? AND object = ? AND generation = ?`
	members = members + " UNION SELECT " + identityRefSQL(t, "entity_id") +
		" FROM external_keys WHERE binding = ? AND object = ? AND generation = ?"
	args = append(args, key.Binding, key.Object, key.Generation)
	if _, err := w.tx.Exec(`UPDATE records SET rev = rev + 1, data_json = '{}', meta_json = '{}',
        deleted_at = COALESCE(deleted_at, updated_at) WHERE type_id = ? AND id IN (`+members+`)`, args...); err != nil {
		return err
	}
	if _, err := w.tx.Exec(`UPDATE history SET before_json = NULL, after_json = NULL
        WHERE type_id = ? AND record IN (`+members+`)`, args...); err != nil {
		return err
	}
	_, err := w.tx.Exec(`DELETE FROM source_claims WHERE binding = ? AND object = ? AND generation = ?`, key.Binding, key.Object, key.Generation)
	return err
}

func residencyTransition(from, to ResidencyMode) bool {
	switch from {
	case ResidencyLive:
		return to == ResidencyMirror
	case ResidencyMirror:
		return to == ResidencyNative || to == ResidencyLive
	case ResidencyNative:
		return to == ResidencyLive || to == ResidencyMirror
	}
	return false
}

func (w *writeTx) inspectResidency(t *Type, state TypeResidency, rows []ResidencyRow, plan *ResidencyPlan) (map[string]int64, []ResidencyReplay, error) {
	revisions := map[string]int64{}
	var replay []ResidencyReplay
	comparable := map[string]bool{}
	for _, row := range rows {
		if !row.Deleted {
			plan.Rows++
		}
		id, _, err := findIdentity(w.tx, row.Key)
		if err != nil {
			return nil, nil, err
		}
		if id == "" {
			if state.Mode == ResidencyNative {
				plan.Conflicts++
			}
			continue
		}
		record, err := loadRecord(w.tx, w.schema, id)
		if err != nil {
			return nil, nil, err
		}
		if record == nil || record.DeletedAt != "" {
			if state.Mode == ResidencyNative && !row.Deleted {
				plan.Conflicts++
			}
			continue
		}
		if comparable[id] {
			plan.Conflicts++
			continue
		}
		comparable[id] = true
		revisions[id] = record.Rev
		if state.Mode != ResidencyNative {
			for name := range record.Meta.By {
				value := record.Data[name]
				claim, mapped := row.Claims[name]
				if !mapped || ((record.Meta.By[name] == ByPerson || record.Meta.By[name] == ByImport) &&
					canonicalJSON(value) != canonicalJSON(claim.Value)) {
					plan.Conflicts++
					break
				}
			}
			continue
		}
		entry, conflict, err := w.reverseResidencyRow(t, state, record, row)
		if err != nil {
			return nil, nil, err
		}
		if conflict {
			plan.Conflicts++
		} else if len(entry.Set) > 0 {
			replay = append(replay, entry)
		}
	}
	err := eachRow(w.tx, `SELECT id FROM records WHERE type_id = ? AND deleted_at IS NULL`, []any{t.ID}, func(scan func(...any) error) error {
		var id string
		if err := scan(&id); err != nil {
			return err
		}
		if !comparable[id] {
			plan.Conflicts++
		}
		return nil
	})
	return revisions, replay, err
}

func (w *writeTx) reverseResidencyRow(t *Type, state TypeResidency, r *record, row ResidencyRow) (ResidencyReplay, bool, error) {
	out := ResidencyReplay{Key: row.Key, Revision: row.Revision, Set: map[string]any{}}
	var version string
	if err := w.tx.QueryRow(`SELECT source_revision FROM source_observations WHERE binding = ? AND object = ? AND key = ? AND generation = ?`,
		row.Key.args()...).Scan(&version); err != nil {
		return out, false, err
	}
	if row.Deleted {
		return out, true, nil
	}
	for _, name := range state.Binding.Writable {
		field := t.field(name)
		claim, exists := row.Claims[name]
		if field == nil || !exists {
			return out, true, nil
		}
		if canonicalJSON(claim.Value) == canonicalJSON(r.Data[name]) {
			continue
		}
		if version != row.Revision {
			return out, true, nil
		}
		out.Set[name] = r.Data[name]
	}
	// Treat any upstream edit during native authority as a conflict, except
	// when every source field already equals the native row after a retry.
	if version != row.Revision || len(out.Set) > 0 {
		for name, claim := range row.Claims {
			if _, replayed := out.Set[name]; !replayed && canonicalJSON(claim.Value) != canonicalJSON(r.Data[name]) {
				return out, true, nil
			}
		}
	}
	return out, false, nil
}

// SyncResidency commits claims and the source checkpoint together. The
// checkpoint compare prevents a slow poll overwriting a newer observation.
func (s *Store) SyncResidency(sc Scope, alias string, expected TypeResidency, snapshot ResidencySnapshot) error {
	return s.write(func(w *writeTx) error {
		t, err := w.schema.allow(sc, alias, true)
		if err != nil {
			return err
		}
		if t.HasRowPolicy() {
			return errorf(CodeNotAllowed, "a row-policy type needs per-person live access")
		}
		state, err := residencyState(w.tx, t.ID)
		if err != nil {
			return err
		}
		if pub, err := readPublication(w.tx, sc.App); err != nil || pub.Archived || pub.Deleted {
			return errorf(CodeNotAllowed, "sync needs a kept app")
		}
		if state.Mode != ResidencyMirror || expected.Epochs != state.Epochs || expected.Checkpoint != state.Checkpoint {
			return errorf(CodeStale, "the mirror advanced during sync")
		}
		if err := w.applyResidencyRows(t, state, snapshot.Rows); err != nil {
			return err
		}
		state.Checkpoint, state.CheckedAt = snapshot.Checkpoint, snapshot.ObservedAt
		if err := w.saveResidency(t, state); err != nil {
			return err
		}
		if w.cursor != 0 {
			return w.invalidate(EventFreshnessChanged, t.ID, state.Binding.Key.Binding)
		}
		return nil
	})
}

func (w *writeTx) applyResidencyRows(t *Type, state TypeResidency, rows []ResidencyRow) error {
	common := t.Common
	for _, row := range rows {
		id, err := w.linkIdentity(common, row.Key, "")
		if err != nil {
			return err
		}
		event := fmt.Sprintf("residency:%d:%x", state.Epochs.Residency, sha256.Sum256([]byte(row.Revision)))
		o := SourceObservation{Key: row.Key, Event: event, Revision: row.Revision, Epochs: state.Epochs,
			mappingVersion: state.Epochs.Mapping, Claims: row.Claims, NativeValues: !row.Deleted, restore: true}
		if row.Deleted {
			o.Claims = nil
		}
		if err := o.validate(); err != nil {
			return err
		}
		if _, changed, err := w.observeSource(o); err != nil {
			return err
		} else if changed && row.Deleted {
			if err := w.removeMirrorRow(t, id, o); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *writeTx) removeMirrorRow(t *Type, id string, o SourceObservation) error {
	before, err := loadRecord(w.tx, w.schema, id)
	if err != nil || before == nil || before.DeletedAt != "" {
		return err
	}
	sc := Scope{Actor: "engine", sync: true, sourceEvent: o.Event, sourceKey: o.Key}
	if _, err := w.newBatch(sc, OpSync, t); err != nil {
		return err
	}
	after := before.clone()
	after.Rev++
	after.UpdatedAt = w.now.UTC().Format(stamp)
	after.DeletedAt = after.UpdatedAt
	if err := w.save(t, after, false, sc); err != nil {
		return err
	}
	return w.history(t, sc, OpSync, "", before, after)
}

func (w *writeTx) guardResidencyWrite(t *Type, r *record, insert bool, sc Scope) error {
	if sc.sync || t.Common == "" {
		return nil
	}
	state, err := residencyState(w.tx, t.ID)
	if err != nil || state.Binding.App == "" {
		return err
	}
	if state.Mode != ResidencyNative {
		return errorf(CodeNotAllowed, "source-owned records change through an approved source Action")
	}
	if insert || r.DeletedAt != "" {
		return errorf(CodeNotAllowed, "creating and deleting records is fenced during the rollback window")
	}
	before, err := loadRecord(w.tx, w.schema, r.ID)
	if err != nil {
		return err
	}
	for _, field := range changedFields(before, r) {
		if !slices.Contains(state.Binding.Writable, field) {
			return fieldErr(field, "this field has no reverse mapping during the rollback window")
		}
	}
	return nil
}

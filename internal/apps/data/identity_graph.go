package data

import (
	"database/sql"
	"encoding/json"
	"errors"
)

const (
	OpIdentityMerge = "identity.merge"
	OpIdentitySplit = "identity.split"
)

// IdentityMerge identifies the survivor and the revision a split must check.
type IdentityMerge struct {
	ID        string `json:"id"`
	Canonical string `json:"canonical"`
	Alias     string `json:"alias"`
	Revision  int64  `json:"revision"`
}

func identityRoot(q querier, id string) (string, error) {
	var root string
	err := q.QueryRow("SELECT "+identityRefSQL(nil, "?"), id).Scan(&root)
	return root, err
}

// identityRefSQL follows redirects without rewriting stored references. The
// expression comes from the query compiler or a bound parameter.
func identityRefSQL(target *Type, value string) string {
	if target != nil && !target.identifiable() {
		return value
	}
	return `(WITH RECURSIVE chain(member) AS (
		VALUES(` + value + `) UNION ALL SELECT a.canonical FROM chain c JOIN identity_aliases a ON a.alias = c.member
	) SELECT member FROM chain WHERE NOT EXISTS (SELECT 1 FROM identity_aliases WHERE alias = chain.member))`
}

func (t *Type) identifiable() bool {
	return t.Common == CommonPerson || t.Common == CommonOrganization
}

// identityMembersSQL expands known canonical IDs to their stored aliases once
// per filter, keeping recursive lookups off the outer query's row loop.
func identityMembersSQL(target *Type, roots string) string {
	if target != nil && !target.identifiable() {
		return roots
	}
	return "WITH RECURSIVE " + identityMembersCTE("matches", roots) + " SELECT member FROM matches"
}

func identityMembersCTE(name, roots string) string {
	return name + "(member) AS (" + roots + " UNION ALL SELECT a.alias FROM " + name +
		" m JOIN identity_aliases a ON a.canonical = m.member)"
}

func identityOwner(sc Scope) bool {
	return sc.App == "" && !sc.below() && !sc.Flow
}

func allowIdentity(sc Scope, draft bool) error {
	if draft || !identityOwner(sc) {
		return errorf(CodeNotAllowed, "identity decisions require the published space's owner")
	}
	return nil
}

// MergeIdentity explicitly chooses the surviving record. Both revisions are
// required so a review cannot absorb changes the reviewer did not see.
func (s *Store) MergeIdentity(sc Scope, canonical, alias string, canonicalRev, aliasRev int64) (*IdentityMerge, error) {
	if err := allowIdentity(sc, s.draft != ""); err != nil {
		return nil, err
	}
	var result *IdentityMerge
	err := s.write(func(w *writeTx) error {
		t, a, b, err := w.identityPair(canonical, alias, canonicalRev, aliasRev)
		if err != nil {
			return err
		}
		result, err = w.mergeIdentity(sc, t, a, b, MatchEvidence{Signals: []string{"owner selected both records"}})
		if err != nil {
			return err
		}
		return w.retireIdentityReviews(sc, t, a.ID, b.ID)
	})
	return result, err
}

func (w *writeTx) identityPair(left, right string, leftRev, rightRev int64) (*Type, *record, *record, error) {
	if !validID(left) || !validID(right) || left == right || leftRev < 1 || rightRev < 1 {
		return nil, nil, nil, errorf(CodeInvalid, "choose two distinct record IDs and their revisions")
	}
	a, err := loadRecord(w.tx, w.schema, left)
	if err != nil {
		return nil, nil, nil, err
	}
	b, err := loadRecord(w.tx, w.schema, right)
	if err != nil {
		return nil, nil, nil, err
	}
	if a == nil || b == nil || a.DeletedAt != "" || b.DeletedAt != "" || a.Type != b.Type {
		return nil, nil, nil, errorf(CodeStale, "both records must still be live and of the same type")
	}
	t := w.schema.typeNamed(a.Type)
	if !t.identifiable() || t.Ledger {
		return nil, nil, nil, errorf(CodeInvalid, "choose records bound to Person or Organization")
	}
	if a.Rev != leftRev || b.Rev != rightRev {
		return nil, nil, nil, errorf(CodeStale, "a record changed after the identity review")
	}
	return t, a, b, nil
}

func (w *writeTx) mergeIdentity(sc Scope, t *Type, a, b *record, evidence MatchEvidence) (*IdentityMerge, error) {
	next := a.clone()
	for _, name := range t.Order {
		f := t.Fields[name]
		if f.Computed != "" || f.Sequence != nil || t.Lifecycle != nil && t.Lifecycle.Field == name {
			continue
		}
		aKind, bKind := a.Meta.By[name], b.Meta.By[name]
		bValue, present := b.Data[name]
		_, aPresent := a.Data[name]
		// The survivor's person-entered value or clear wins. The alias can
		// supply a person's value ahead of a source, or fill a missing field.
		if personValue(aKind) || (!present && bKind == "") || (aPresent || aKind != "") && !personValue(bKind) {
			continue
		}
		if present {
			next.Data[name] = bValue
		} else {
			delete(next.Data, name)
		}
		next.Meta.By[name] = bKind
		next.Meta.copySource(name, b.Meta)
	}
	changed := changedFields(a, next)
	compute(t, next, changed, w.schema.person)
	if err := w.s.checkFrozen(t, b, nil, sc, ""); err != nil {
		return nil, err
	}
	if err := w.s.checkFrozen(t, a, changed, sc, ""); err != nil {
		return nil, err
	}
	id, err := w.newBatch(sc, OpIdentityMerge, t)
	if err != nil {
		return nil, err
	}
	hidden := b.clone()
	hidden.Rev++
	hidden.UpdatedAt = w.now.UTC().Format(stamp)
	hidden.DeletedAt = hidden.UpdatedAt
	if err := w.save(t, hidden, false, sc); err != nil {
		return nil, err
	}
	if err := w.checkRecord(t, next, changed); err != nil {
		return nil, err
	}
	next.Rev++
	next.UpdatedAt = hidden.UpdatedAt
	if err := w.save(t, next, false, sc); err != nil {
		return nil, err
	}
	if _, err := w.tx.Exec(`INSERT INTO identity_merges(id,type_id,canonical,alias,before_json,canonical_rev,alias_rev,evidence,actor,at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, id, t.ID, a.ID, b.ID, canonicalJSON([]string{t.encodeState(a), t.encodeState(next)}),
		next.Rev, hidden.Rev, canonicalJSON(evidence), w.schema.actor(sc), next.UpdatedAt); err != nil {
		return nil, err
	}
	if _, err := w.tx.Exec(`INSERT INTO identity_aliases VALUES(?,?,?)`, b.ID, a.ID, id); err != nil {
		return nil, err
	}
	if err := w.history(t, sc, OpIdentityMerge, b.ID, a, next); err != nil {
		return nil, err
	}
	if err := w.history(t, sc, OpIdentityMerge, a.ID, b, hidden); err != nil {
		return nil, err
	}
	return &IdentityMerge{ID: id, Canonical: a.ID, Alias: b.ID, Revision: next.Rev}, nil
}

// UnmergeIdentity restores the alias and moves its original claims and
// references back with it. Edits to unrelated survivor fields are preserved.
func (s *Store) UnmergeIdentity(sc Scope, id string, rev int64) error {
	if err := allowIdentity(sc, s.draft != ""); err != nil {
		return err
	}
	if !validID(id) || rev < 1 {
		return errorf(CodeInvalid, "choose a merge ID and the current survivor revision")
	}
	return s.write(func(w *writeTx) error {
		var typeID, aID, bID, raw string
		var aliasRev int64
		err := w.tx.QueryRow(`SELECT type_id, canonical, alias, before_json, alias_rev FROM identity_merges
			WHERE id = ? AND split_at = ''`, id).Scan(&typeID, &aID, &bID, &raw, &aliasRev)
		if errors.Is(err, sql.ErrNoRows) {
			return errorf(CodeNotFound, "there is no active merge with that ID")
		}
		if err != nil {
			return err
		}
		t := w.schema.byID[typeID]
		if t == nil {
			return errorf(CodeNotFound, "the merge's type is unavailable")
		}
		a, err := w.live(t, aID)
		if err != nil {
			return err
		}
		b, err := loadRecord(w.tx, w.schema, bID)
		if err != nil {
			return err
		}
		var later bool
		if err := w.tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM identity_merges
			WHERE canonical = ? AND id > ? AND split_at = '')`, aID, id).Scan(&later); err != nil {
			return err
		}
		if a.Rev != rev || b == nil || b.Rev != aliasRev || later {
			return errorf(CodeStale, "check both records and split any newer merge on this survivor first")
		}
		return w.splitIdentity(sc, t, a, b, id, raw)
	})
}

func (w *writeTx) splitIdentity(sc Scope, t *Type, a, b *record, id, raw string) error {
	var states []string
	if err := json.Unmarshal([]byte(raw), &states); err != nil {
		return err
	}
	if len(states) != 2 {
		return errorf(CodeInvalid, "the merge history is incomplete")
	}
	old, merged := t.decodeState(states[0]), t.decodeState(states[1])
	next := a.clone()
	for _, name := range t.Order {
		if t.Fields[name].Computed != "" {
			continue
		}
		fromAlias, err := sourceBelongsToAlias(w.tx, a.Meta.Sources[name], b.ID)
		if err != nil {
			return err
		}
		if !fromAlias && (sameIdentityField(name, old.Data, old.Meta, merged.Data, merged.Meta) ||
			!sameIdentityField(name, a.Data, a.Meta, merged.Data, merged.Meta)) {
			// A later direct edit or refresh from the survivor stays with it.
			continue
		}
		if v, ok := old.Data[name]; ok {
			next.Data[name] = v
		} else {
			delete(next.Data, name)
		}
		if k := old.Meta.By[name]; k != "" {
			next.Meta.By[name] = k
		} else {
			delete(next.Meta.By, name)
		}
		next.Meta.copySource(name, old.Meta)
	}
	changed := changedFields(a, next)
	compute(t, next, changed, w.schema.person)
	if err := w.s.checkFrozen(t, a, changed, sc, ""); err != nil {
		return err
	}
	if _, err := w.newBatch(sc, OpIdentitySplit, t); err != nil {
		return err
	}
	next.Rev++
	next.UpdatedAt = w.now.UTC().Format(stamp)
	if err := w.checkRecord(t, next, changed); err != nil {
		return err
	}
	if err := w.save(t, next, false, sc); err != nil {
		return err
	}
	restored := b.clone()
	restored.Rev++
	restored.UpdatedAt = next.UpdatedAt
	restored.DeletedAt = ""
	compute(t, restored, nil, w.schema.person)
	if err := w.checkRecord(t, restored, nil); err != nil {
		return err
	}
	if err := w.save(t, restored, false, sc); err != nil {
		return err
	}
	if _, err := w.tx.Exec(`DELETE FROM identity_aliases WHERE merge_id = ?`, id); err != nil {
		return err
	}
	if _, err := w.tx.Exec(`UPDATE identity_merges SET split_by = ?, split_at = ? WHERE id = ?`, w.schema.actor(sc), next.UpdatedAt, id); err != nil {
		return err
	}
	if err := w.history(t, sc, OpIdentitySplit, id, a, next); err != nil {
		return err
	}
	return w.history(t, sc, OpIdentitySplit, id, b, restored)
}

func sourceBelongsToAlias(q querier, key ExternalKey, alias string) (bool, error) {
	if key == (ExternalKey{}) {
		return false, nil
	}
	var found bool
	err := q.QueryRow(`SELECT EXISTS(SELECT 1 FROM external_keys WHERE binding = ? AND object = ? AND key = ?
		AND generation = ? AND entity_id IN (`+identityMembersSQL(nil, "SELECT ?")+`))`, append(key.args(), alias)...).Scan(&found)
	return found, err
}

func sameIdentityField(name string, a map[string]any, am recordMeta, b map[string]any, bm recordMeta) bool {
	return canonicalJSON(a[name]) == canonicalJSON(b[name]) && am.By[name] == bm.By[name] && am.Sources[name] == bm.Sources[name]
}

// rejectedIdentity checks whole clusters so a third copy cannot accidentally
// reunite two records the owner already told us to keep separate.
func rejectedIdentity(q querier, a, b string) (bool, error) {
	var rejected bool
	err := q.QueryRow(`WITH RECURSIVE `+identityMembersCTE("left_group", "SELECT ?")+", "+identityMembersCTE("right_group", "SELECT ?")+`
		SELECT EXISTS(SELECT 1 FROM identity_matches WHERE decision = 'separate' AND
			((left_id IN left_group AND right_id IN right_group) OR (right_id IN left_group AND left_id IN right_group)))`,
		a, b).Scan(&rejected)
	return rejected, err
}

func (s *Store) groupExternalKeys(id string) ([]ExternalKey, error) {
	out := []ExternalKey{}
	members := identityMembersSQL(nil, "SELECT "+identityRefSQL(nil, "?"))
	err := eachRow(s.rdb, `SELECT binding, object, key, generation FROM external_keys
		WHERE entity_id IN (`+members+`) ORDER BY binding, object, key, generation`, []any{id}, func(scan func(...any) error) error {
		var key ExternalKey
		if err := scan(&key.Binding, &key.Object, &key.Key, &key.Generation); err != nil {
			return err
		}
		out = append(out, key)
		return nil
	})
	return out, err
}

// refreshIdentityAlias keeps native source edits on their original side of a
// merge. The alias remains hidden until a split explicitly restores it.
func (w *writeTx) refreshIdentityAlias(t *Type, sc Scope, id string, values map[string]any) error {
	r, err := loadRecord(w.tx, w.schema, id)
	if err != nil {
		return err
	}
	if r == nil || r.DeletedAt == "" {
		return errorf(CodeStale, "the original source alias is unavailable")
	}
	set, err := w.prepareSet(t, values, prepared{})
	if err != nil {
		return err
	}
	applySet(r, set, sc, OpSync)
	data, meta := canonicalJSON(t.encode(r.Data, r.unknown)), canonicalJSON(t.encodeMeta(r.Meta))
	_, err = w.tx.Exec(`UPDATE records SET data_json = ?, meta_json = ? WHERE id = ?`, data, meta, id)
	return err
}

// filterAliasProjection keeps the existing source winner on overlapping fields.
// An alias can still refresh its own contributions and fill missing fields.
func (w *writeTx) filterAliasProjection(id, origin string, values map[string]any) error {
	r, err := loadRecord(w.tx, w.schema, id)
	if err != nil {
		return err
	}
	if r == nil || r.DeletedAt != "" {
		return errorf(CodeNotFound, "the survivor is unavailable")
	}
	for name := range values {
		key, set := r.Meta.Sources[name]
		if !set {
			continue
		}
		same, err := sourceBelongsToAlias(w.tx, key, origin)
		if err != nil {
			return err
		}
		if !same {
			delete(values, name)
		}
	}
	return nil
}

package data

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// SourceFieldClaim keeps provenance alongside the value even when a person
// holds the canonical field clear. Claims are private to engine callers.
type SourceFieldClaim struct {
	Path  string
	Value any
	Class string
}

// SourceObservation carries one source revision and the mapping and ACL
// generations under which it was read. ExpectedRevision compares the source
// token independently of the canonical record revision.
type SourceObservation struct {
	Key              ExternalKey
	Event            string
	Revision         string
	ExpectedRevision *string
	Epochs           TypeEpochs
	mappingVersion   int64
	ACLRef           string
	ACLEpoch         int64
	CauseChain       []string
	Claims           map[string]SourceFieldClaim

	// NativeValues explicitly copies values with native visibility. Sources
	// with inherited permissions must leave claims private for the policy
	// layer to resolve.
	NativeValues bool
}

// StoredSourceClaim is the latest observation of one stable field ID.
type StoredSourceClaim struct {
	SourceFieldClaim
	Revision       string
	Epochs         TypeEpochs
	MappingVersion int64 `json:",omitempty"`
	ACLRef         string
	ACLEpoch       int64
	ObservedAt     time.Time
	ByKind         string
	CauseChain     []string
}

// SourceState is the current source proof and its field contributions.
// ACL-only observations replace the proof even when no values changed.
type SourceState struct {
	Revision   string
	ACLRef     string
	ACLEpoch   int64
	ObservedAt time.Time
	Claims     map[string]StoredSourceClaim
}

// SourceState reads the source proof and contributions from one snapshot.
// Only trusted engine adapters may call it.
func (s *Store) SourceState(key ExternalKey) (*SourceState, error) {
	tx, err := s.rdb.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	state := &SourceState{Claims: map[string]StoredSourceClaim{}}
	var at string
	err = tx.QueryRow(`SELECT source_revision, acl_ref, acl_epoch, observed_at FROM source_observations
		WHERE binding = ? AND object = ? AND key = ? AND generation = ?`, key.args()...).
		Scan(&state.Revision, &state.ACLRef, &state.ACLEpoch, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errorf(CodeNotFound, "the source key has no observations")
	}
	if err != nil {
		return nil, err
	}
	if state.ObservedAt, err = time.Parse(stamp, at); err != nil {
		return nil, err
	}
	err = eachRow(tx, `SELECT field_id, claim_json FROM source_claims
		WHERE binding = ? AND object = ? AND key = ? AND generation = ?`, key.args(), func(scan func(...any) error) error {
		var id, raw string
		if err := scan(&id, &raw); err != nil {
			return err
		}
		var claim StoredSourceClaim
		if err := json.Unmarshal([]byte(raw), &claim); err != nil {
			return err
		}
		state.Claims[id] = claim
		return nil
	})
	if err != nil {
		return nil, err
	}
	return state, nil
}

// ObserveSource records claims, dedupe and invalidation in the writer. An
// explicitly native contribution also goes through the canonical write rules.
func (s *Store) ObserveSource(o SourceObservation) (string, error) {
	if err := o.validate(); err != nil {
		return "", err
	}
	var id string
	err := s.write(func(w *writeTx) error {
		var err error
		id, _, err = w.observeSource(o)
		return err
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

func (o SourceObservation) validate() error {
	if !o.Key.valid() || o.Event == "" || len(o.Event) > 200 || o.Revision == "" || len(o.Revision) > 1024 || o.ACLEpoch < 0 {
		return errorf(CodeInvalid, "an observation needs a source key, event and revision")
	}
	if o.NativeValues && (o.ACLRef != "" || o.ACLEpoch != 0) {
		return errorf(CodeNotAllowed, "an ACL-bound observation cannot supply native values")
	}
	return nil
}

func (w *writeTx) observeSource(o SourceObservation) (string, bool, error) {
	var typeID string
	origin, typeID, err := findIdentityOrigin(w.tx, o.Key)
	if err != nil {
		return "", false, err
	}
	if origin == "" {
		return "", false, errorf(CodeNotFound, "the source key has no canonical identity")
	}
	id, err := identityRoot(w.tx, origin)
	if err != nil {
		return "", false, err
	}
	t := w.schema.byID[typeID]
	res, err := w.tx.Exec(`INSERT OR IGNORE INTO source_events VALUES(?, ?, ?, ?, ?)`, append(o.Key.args(), o.Event)...)
	if err != nil {
		return "", false, err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return id, false, err
	}
	epochs, err := readEpochs(w.tx, typeID)
	if err != nil {
		return "", false, err
	}
	if epochs != o.Epochs {
		return "", false, errorf(CodeStale, "the source observation uses old type epochs")
	}
	policyChanged, values, err := w.saveObservation(t, o)
	if err != nil {
		return "", false, err
	}
	sc := Scope{Actor: "engine", sync: true, CauseChain: o.CauseChain, sourceEvent: o.Event, sourceKey: o.Key}
	if o.NativeValues && len(values) > 0 {
		if origin != id {
			if err := w.refreshIdentityAlias(t, sc, origin, values); err != nil {
				return "", false, err
			}
		}
		if err := w.projectSource(t, sc, id, values); err != nil {
			return "", false, err
		}
	}
	if w.change == nil {
		w.change = &Change{At: w.now, ByKind: BySync, CauseChain: o.CauseChain, SourceEvent: o.Event}
	}
	if policyChanged {
		return id, true, w.invalidate(EventPolicyChanged, t.ID, o.Key.Binding)
	}
	if w.cursor == 0 {
		return id, true, w.invalidate(EventFreshnessChanged, t.ID, o.Key.Binding)
	}
	return id, true, nil
}

func (w *writeTx) saveObservation(t *Type, o SourceObservation) (bool, map[string]any, error) {
	var previous, aclRef string
	var aclEpoch int64
	err := w.tx.QueryRow(`SELECT source_revision, acl_ref, acl_epoch FROM source_observations
		WHERE binding = ? AND object = ? AND key = ? AND generation = ?`, o.Key.args()...).Scan(&previous, &aclRef, &aclEpoch)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, nil, err
	}
	if o.ExpectedRevision != nil && *o.ExpectedRevision != previous || o.ACLEpoch < aclEpoch {
		return false, nil, errorf(CodeStale, "the source revision or ACL generation has changed")
	}
	_, err = w.tx.Exec(`INSERT INTO source_observations VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(binding, object, key, generation) DO UPDATE SET source_revision = excluded.source_revision,
		acl_ref = excluded.acl_ref, acl_epoch = excluded.acl_epoch, observed_at = excluded.observed_at`,
		append(o.Key.args(), o.Revision, o.ACLRef, o.ACLEpoch, w.now.UTC().Format(stamp))...)
	if err != nil {
		return false, nil, err
	}
	values := make(map[string]any, len(o.Claims))
	for _, name := range sortedKeys(o.Claims) {
		claim := o.Claims[name]
		f := t.field(name)
		if f == nil {
			f = t.byID[name]
		}
		if f == nil || claim.Path == "" || len(claim.Path) > 1024 {
			return false, nil, fieldErr(name, "a source claim needs a known field and source path")
		}
		if _, duplicate := values[f.Name]; duplicate {
			return false, nil, fieldErr(name, "a source observation names the same field twice")
		}
		values[f.Name] = claim.Value
		stored := StoredSourceClaim{SourceFieldClaim: claim, Revision: o.Revision, Epochs: o.Epochs, MappingVersion: o.mappingVersion,
			ACLRef: o.ACLRef, ACLEpoch: o.ACLEpoch, ObservedAt: w.now.UTC(), ByKind: BySync, CauseChain: o.CauseChain}
		raw, err := json.Marshal(stored)
		if err != nil {
			return false, nil, err
		}
		if _, err := w.tx.Exec(`INSERT INTO source_claims VALUES(?, ?, ?, ?, ?, ?)
			ON CONFLICT(binding, object, key, generation, field_id) DO UPDATE SET claim_json = excluded.claim_json`,
			append(o.Key.args(), f.ID, string(raw))...); err != nil {
			return false, nil, err
		}
	}
	return aclRef != o.ACLRef || aclEpoch != o.ACLEpoch, values, nil
}

func (w *writeTx) projectSource(t *Type, sc Scope, id string, values map[string]any) error {
	set, err := w.prepareSet(t, values, prepared{})
	if err != nil {
		return err
	}
	before, err := loadRecord(w.tx, w.schema, id)
	if err != nil {
		return err
	}
	if before == nil {
		if _, err := w.newBatch(sc, OpSync, t); err != nil {
			return err
		}
		_, err := w.createOneAs(t, sc, set, OpSync, id)
		return err
	}
	if before.DeletedAt != "" || before.Type != t.Name {
		return errorf(CodeNotFound, "the canonical record is unavailable")
	}
	if t.Ledger {
		return errorf(CodeLedger, "a source observation cannot change a ledger entry")
	}
	after := before.clone()
	applySet(after, set, sc, OpSync)
	changed := changedFields(before, after)
	compute(t, after, changed, w.schema.person)
	if after.key(t) == before.key(t) {
		return nil
	}
	if err := w.s.checkFrozen(t, before, changed, sc, ""); err != nil {
		return err
	}
	if err := w.checkRecord(t, after, changed); err != nil {
		return err
	}
	if _, err := w.newBatch(sc, OpSync, t); err != nil {
		return err
	}
	after.Rev++
	after.UpdatedAt = w.now.UTC().Format(stamp)
	if err := w.save(t, after, false, sc); err != nil {
		return err
	}
	return w.history(t, sc, OpSync, "", before, after)
}

package data

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/google/cel-go/cel"
)

// A LIFECYCLE OWNS ITS STATE FIELD. A type may declare a process: an enum
// field whose values are its states, and named transitions between them
// ("Submit for review", "Mark won"). Once it does, nothing writes that field
// directly: a create starts the record in the first state, an update naming
// the field is refused, and [Store.Transition] is the only way the state
// moves. A transition checks, in the writer's transaction, that the record
// is in a state it starts from, that its guard holds (required fields,
// open work done, a sign-off on this very version), that the writer is a
// kind the transition allows, and that the record is the revision the caller
// saw. It is recorded in history with the actor and the reason, and its
// effects (a work item, a timer, a run) are handed to the engine through the
// outbox in the same transaction.
//
// An import or a seeded sample brings records in the state they are already
// in; that is moving data, not moving work, and is recorded as an import.

// lifecycle is a compiled [LifecycleDef].
type lifecycle struct {
	LifecycleDef
	byID   map[string]*transition
	states []string
}

// transition is one compiled [TransitionDef].
type transition struct {
	TransitionDef
	guard *program
}

// compile checks the lifecycle against the merged type and compiles its
// guards.
func (lc *lifecycle) compile(t *Type, env *cel.Env) []error {
	var problems []error
	f := t.field(lc.Field)
	if f == nil || f.Type != TypeEnum {
		return []error{fmt.Errorf("%s's lifecycle needs %q to be an enum field", t.Name, lc.Field)}
	}
	lc.states = f.Values
	lc.byID = map[string]*transition{}
	for _, def := range lc.Transitions {
		tr := &transition{TransitionDef: def}
		if len(tr.By) == 0 {
			tr.By = []string{ByPerson, ByFlow}
		}
		if !slices.Contains(lc.states, tr.To) {
			problems = append(problems, fmt.Errorf("%s transition %s goes to %q, which %s no longer has", t.Name, tr.ID, tr.To, lc.Field))
		}
		if tr.Guard != "" {
			p, err := compile(env, tr.Guard)
			if err != nil {
				problems = append(problems, fmt.Errorf("%s transition %s: %v", t.Name, tr.ID, err))
				continue
			}
			tr.guard = p
		}
		lc.byID[tr.ID] = tr
	}
	return problems
}

// initial is the state a new record starts in.
func (lc *lifecycle) initial(t *Type) string {
	if lc.Initial != "" {
		return lc.Initial
	}
	if len(lc.states) > 0 {
		return lc.states[0]
	}
	return ""
}

// names lists the transitions, for a refusal that says what to do instead.
func (lc *lifecycle) names() string {
	var out []string
	for _, tr := range lc.Transitions {
		label := tr.Label
		if label == "" {
			label = tr.ID
		}
		out = append(out, label)
	}
	return strings.Join(out, ", ")
}

// TransitionState is one transition a record may take from its state, as a
// state bar draws it: whether its guard holds now (Ready) and whether the
// scope asking may fire it (Allowed).
type TransitionState struct {
	ID      string
	Label   string
	To      string
	Allowed bool
	Ready   bool
	Blocked string
}

// TransitionOptions are a transition's choices: the revision the caller saw
// (refused as stale when the record has moved on), the reason kept on the
// history row, and, from a person only, the reason for moving a frozen
// record.
type TransitionOptions struct {
	Rev      int64
	Reason   string
	Unfreeze string
}

// Transition fires one named transition on one record.
func (s *Store) Transition(sc Scope, typ, id, name string, opts TransitionOptions) (*WriteResult, error) {
	return s.transitionRecords(sc, name, []TransitionRecord{{RecordRef: RecordRef{Type: typ, ID: id}, Rev: opts.Rev}}, opts, false)
}

// TransitionRecord is one selected row and the revision the caller saw.
type TransitionRecord struct {
	RecordRef
	Rev int64
}

// TransitionBatch moves the selected records in one transaction and one
// history batch. Every selected type must declare the named move.
func (s *Store) TransitionBatch(sc Scope, name string, records []TransitionRecord, opts TransitionOptions) (*WriteResult, error) {
	return s.transitionRecords(sc, name, records, opts, true)
}

func (s *Store) transitionRecords(sc Scope, name string, records []TransitionRecord, opts TransitionOptions, requireRev bool) (*WriteResult, error) {
	if len(records) == 0 || len(records) > IDsMax {
		return nil, errorf(CodeInvalid, "select between 1 and %d records", IDsMax)
	}
	if len(opts.Reason) > 2000 {
		return nil, errorf(CodeInvalid, "a reason is at most 2000 characters")
	}
	seen := map[string]bool{}
	for _, ref := range records {
		switch {
		case !validID(ref.ID):
			return nil, errorf(CodeInvalid, "%q is not a record id", ref.ID)
		case seen[ref.ID]:
			return nil, errorf(CodeInvalid, "record %s appears more than once", ref.ID)
		case requireRev && ref.Rev <= 0:
			return nil, errorf(CodeInvalid, "expected_rev is required for record %s", ref.ID)
		}
		seen[ref.ID] = true
	}
	type move struct {
		typ        *Type
		transition *transition
		before     *record
	}
	var rows []Row
	var batch string
	err := s.writeAs(sc, func(w *writeTx) error {
		moves := make([]move, 0, len(records))
		var batchType *Type
		for i, ref := range records {
			t, err := w.schema.allow(sc, ref.Type, true)
			if err != nil {
				return err
			}
			if i == 0 {
				batchType = t
			} else if batchType != t {
				batchType = nil
			}
			tr, err := allowedTransition(sc, t, name)
			if err != nil {
				return err
			}
			row, err := w.transitionReady(sc, t, tr, ref, opts)
			if err != nil {
				return err
			}
			moves = append(moves, move{t, tr, row})
		}
		cause := sc
		if cause.Cause == "" {
			cause.Cause = "transition:" + name
		}
		var err error
		batch, err = w.newBatch(cause, OpTransition, batchType)
		if err != nil {
			return err
		}
		rows = make([]Row, 0, len(moves))
		for _, m := range moves {
			next, err := w.applyTransition(cause, m.typ, m.transition, m.before, opts.Reason)
			if err != nil {
				return err
			}
			rows = append(rows, s.row(m.typ, next, sc, false))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &WriteResult{Batch: batch, Rows: rows, Held: []Held{}}, nil
}

func allowedTransition(sc Scope, t *Type, name string) (*transition, error) {
	if t.Ledger {
		return nil, errorf(CodeLedger, "%s entries are never changed; reverse the entry instead", t.Name)
	}
	lc := t.Lifecycle
	if lc == nil {
		return nil, errorf(CodeLifecycle, "%s records have no lifecycle", t.Name)
	}
	tr := lc.byID[name]
	if tr == nil {
		return nil, errorf(CodeLifecycle, "%s has no transition %q; it has %s", t.Name, name, lc.names())
	}
	kind := sc.kind(OpTransition)
	if !slices.Contains(tr.By, kind) {
		return nil, &Error{Code: CodeNotAllowed, Need: "transition." + tr.ID + "." + kind, Message: actorBlocked(tr, kind)}
	}
	return tr, nil
}

func (w *writeTx) transitionReady(sc Scope, t *Type, tr *transition, ref TransitionRecord, opts TransitionOptions) (*record, error) {
	before, err := w.liveFor(sc, t, ref.ID)
	if err != nil {
		return nil, err
	}
	if ref.Rev != 0 && before.Rev != ref.Rev {
		return nil, staleTransition(t, ref.Rev, before.Rev)
	}
	field := t.Lifecycle.Field
	if sc.below() && (before.Meta.By[field] == ByPerson || before.Meta.By[field] == ByImport) {
		return nil, errorf(CodeNotAllowed, "%s was set by the person", field)
	}
	from, _ := before.Data[field].(string)
	if !startsFrom(tr, from) {
		return nil, &Error{Code: CodeLifecycle, Field: field, Message: fmt.Sprintf("this %s is %s; %s starts from %s", t.Name, orNone(from), label(tr), strings.Join(tr.From, " or "))}
	}
	ready, err := w.guardHolds(t, tr, before)
	if err != nil {
		return nil, err
	}
	if !ready {
		return nil, errorf(CodeLifecycle, "%s", guardBlocked(tr))
	}
	if err := w.s.checkFrozen(t, before, []string{field}, sc, opts.Unfreeze); err != nil {
		return nil, err
	}
	return before, nil
}

func (w *writeTx) applyTransition(sc Scope, t *Type, tr *transition, before *record, reason string) (*record, error) {
	field := t.Lifecycle.Field
	from, _ := before.Data[field].(string)
	next := before.clone()
	next.Data[field] = tr.To
	next.Meta.LastTransition = tr.ID
	next.Meta.By[field] = sc.kind(OpTransition)
	compute(t, next, changedFields(before, next), w.schema.person)
	if err := w.checkRecord(t, next, []string{field}); err != nil {
		return nil, err
	}
	next.UpdatedAt = w.now.UTC().Format(stamp)
	next.Rev++
	if err := w.save(t, next, false, sc); err != nil {
		return nil, err
	}
	note := tr.ID
	if reason != "" {
		note += ": " + reason
	}
	if err := w.history(t, sc, OpTransition, note, before, next); err != nil {
		return nil, err
	}
	if err := w.assign(sc, t, next, tr.ID); err != nil {
		return nil, err
	}
	effects := tr.Effects
	if effects == nil {
		effects = []map[string]any{}
	}
	return next, w.emit(outboxRow{event: EventTransition, typeID: t.ID, record: before.ID, payload: map[string]any{
		"type": t.Name, "id": before.ID, "transition": tr.ID, "label": label(tr), "from": from, "to": tr.To,
		"effects": effects,
	}})
}

// Transitions lists the transitions one record may take from its state, each
// with whether its guard holds now and whether sc may fire it.
func (s *Store) Transitions(sc Scope, typ, id string) ([]TransitionState, error) {
	sch := s.schema.Load()
	t, err := sch.allow(sc, typ, false)
	if err != nil {
		return nil, err
	}
	out := []TransitionState{}
	if t.Lifecycle == nil {
		return out, nil
	}
	r, err := loadRecord(s.rdb, sch, id)
	if err != nil {
		return nil, err
	}
	if r == nil || r.Type != t.Name || r.DeletedAt != "" {
		return nil, errorf(CodeNotFound, "there is no %s record %s", t.Name, id)
	}
	w := &writeTx{s: s, schema: sch, now: s.now()}
	return w.transitionsOn(s.rdb, sc, t, r)
}

func (w *writeTx) transitionsOn(q querier, sc Scope, t *Type, r *record) ([]TransitionState, error) {
	out := []TransitionState{}
	if t.processField() == nil {
		return out, nil
	}
	_, writeErr := w.schema.allow(sc, t.Name, true)
	kind := sc.kind(OpTransition)
	from, _ := r.Data[t.Lifecycle.Field].(string)
	for _, def := range t.Lifecycle.Transitions {
		tr := t.Lifecycle.byID[def.ID]
		if tr == nil || !startsFrom(tr, from) {
			continue
		}
		ready, err := w.guardHoldsOn(q, t, tr, r)
		if err != nil {
			return nil, err
		}
		state := TransitionState{ID: tr.ID, Label: label(tr), To: tr.To, Ready: ready, Allowed: writeErr == nil && !t.Ledger && slices.Contains(tr.By, kind)}
		switch {
		case t.Ledger:
			state.Blocked = fmt.Sprintf("%s entries can only be reversed", t.Name)
		case writeErr != nil:
			if refusal, ok := AsError(writeErr); ok {
				state.Blocked = refusal.Message
			} else {
				return nil, writeErr
			}
		case !state.Allowed:
			state.Blocked = actorBlocked(tr, kind)
		case !ready:
			state.Blocked = guardBlocked(tr)
		default:
			if err := w.s.checkFrozen(t, r, []string{t.Lifecycle.Field}, sc, ""); err != nil {
				if refusal, ok := AsError(err); ok {
					state.Blocked = refusal.Message
				} else {
					return nil, err
				}
			}
		}
		out = append(out, state)
	}
	return out, nil
}

// processField is the state field when the process can show it.
func (t *Type) processField() *Field {
	if t.Lifecycle == nil {
		return nil
	}
	field := t.field(t.Lifecycle.Field)
	if field == nil || field.Masked {
		return nil
	}
	return field
}

func actorBlocked(tr *transition, kind string) string {
	return fmt.Sprintf("%s cannot be fired by %s", label(tr), kindWords(kind))
}

func guardBlocked(tr *transition) string {
	if strings.TrimSpace(tr.Guard) == "signed()" {
		return "An approval signature is required."
	}
	return fmt.Sprintf("Finish the required steps before you can choose %s.", label(tr))
}

func staleTransition(t *Type, expected, actual int64) error {
	return errorf(CodeStale, "this %s has changed since revision %d; it is at %d", t.Name, expected, actual)
}

// startsFrom says whether tr may fire from state from.
func startsFrom(tr *transition, from string) bool {
	return len(tr.From) == 0 || slices.Contains(tr.From, "*") || slices.Contains(tr.From, from)
}

// guardHolds evaluates tr's guard over r inside the writer's transaction.
func (w *writeTx) guardHolds(t *Type, tr *transition, r *record) (bool, error) {
	return w.guardHoldsOn(w.tx, t, tr, r)
}

// guardHoldsOn evaluates tr's guard over r, reading sign-offs and open work
// through q. A guard reads signed() (a sign-off is bound to this version of
// the record) and open_work() (work items on it still open).
func (w *writeTx) guardHoldsOn(q querier, t *Type, tr *transition, r *record) (bool, error) {
	if tr.guard == nil {
		return true, nil
	}
	values := r.values()
	signed, err := currentApproval(q, t, r)
	if err != nil {
		return false, err
	}
	var open int
	if err := q.QueryRow(`SELECT count(*) FROM work_items WHERE record = ? AND status = ?`, r.ID, WorkOpen).Scan(&open); err != nil {
		return false, err
	}
	values["_signed"] = signed
	values["_open_work"] = float64(open)
	values["_checklists"] = completedChecklists(t, r)
	return tr.guard.evalBool(t, values, nil), nil
}

// versionHash is a record's version as a sign-off binds it: the hash of its
// stored values. Any change to a value makes a new version, and a sign-off on
// the old one no longer counts.
func versionHash(t *Type, r *record) string {
	encoded := canonicalJSON(t.encode(r.Data, r.unknown))
	if len(r.Meta.Checklists) != 0 {
		encoded += "\n" + canonicalJSON(r.Meta.Checklists)
	}
	sum := sha256.Sum256([]byte(encoded))
	return hex.EncodeToString(sum[:])
}

// label is a transition as a person reads it.
func label(tr *transition) string {
	if tr.Label != "" {
		return tr.Label
	}
	return tr.ID
}

// kindWords is a writer kind in a refusal.
func kindWords(kind string) string {
	switch kind {
	case ByAI:
		return "an agent"
	case ByFlow:
		return "an automation"
	}
	return "a person"
}

func orNone(state string) string {
	if state == "" {
		return "in no state"
	}
	return state
}

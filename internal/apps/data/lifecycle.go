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
	if !validID(id) {
		return nil, errorf(CodeInvalid, "%q is not a record id", id)
	}
	if len(opts.Reason) > 2000 {
		return nil, errorf(CodeInvalid, "a reason is at most 2000 characters")
	}
	var t *Type
	var after *record
	var batch string
	err := s.write(func(w *writeTx) error {
		after, batch = nil, ""
		var err error
		if t, err = w.schema.allow(sc, typ, true); err != nil {
			return err
		}
		lc := t.Lifecycle
		if lc == nil {
			return &Error{Code: CodeLifecycle, Message: fmt.Sprintf("%s records have no lifecycle", t.Name)}
		}
		tr := lc.byID[name]
		if tr == nil {
			return &Error{Code: CodeLifecycle, Message: fmt.Sprintf("%s has no transition %q; it has %s", t.Name, name, lc.names())}
		}
		kind := sc.kind(OpTransition)
		if !slices.Contains(tr.By, kind) {
			return &Error{Code: CodeNotAllowed, Need: "transition." + tr.ID + "." + kind, Message: fmt.Sprintf("%s cannot be fired by %s", label(tr), kindWords(kind))}
		}
		before, err := w.live(t, id)
		if err != nil {
			return err
		}
		if opts.Rev != 0 && before.Rev != opts.Rev {
			return &Error{Code: CodeStale, Message: fmt.Sprintf("this %s has changed since revision %d; it is at %d", t.Name, opts.Rev, before.Rev)}
		}
		from, _ := before.Data[lc.Field].(string)
		if !startsFrom(tr, from) {
			return &Error{Code: CodeLifecycle, Field: lc.Field, Message: fmt.Sprintf("this %s is %s; %s starts from %s", t.Name, orNone(from), label(tr), strings.Join(tr.From, " or "))}
		}
		ready, err := w.guardHolds(t, tr, before)
		if err != nil {
			return err
		}
		if !ready {
			return &Error{Code: CodeLifecycle, Message: fmt.Sprintf("%s cannot happen yet: %s does not hold", label(tr), tr.Guard)}
		}
		if err := w.s.checkFrozen(t, before, []string{lc.Field}, sc, opts.Unfreeze); err != nil {
			return err
		}
		next := before.clone()
		next.Data[lc.Field] = tr.To
		next.Meta.By[lc.Field] = kind
		compute(t, next, changedFields(before, next), w.schema.person)
		if err := w.checkRecord(t, next, []string{lc.Field}); err != nil {
			return err
		}
		if sc.Cause == "" {
			sc.Cause = "transition:" + tr.ID
		}
		if batch, err = w.newBatch(sc, OpTransition, t); err != nil {
			return err
		}
		next.UpdatedAt = w.now.UTC().Format(stamp)
		next.Rev++
		if err := w.save(t, next, false); err != nil {
			return err
		}
		note := tr.ID
		if opts.Reason != "" {
			note += ": " + opts.Reason
		}
		if err := w.history(t, sc, OpTransition, note, before, next); err != nil {
			return err
		}
		after = next
		effects := tr.Effects
		if effects == nil {
			effects = []map[string]any{}
		}
		return w.emit(outboxRow{event: EventTransition, typeID: t.ID, record: id, payload: map[string]any{
			"type": t.Name, "id": id, "transition": tr.ID, "label": label(tr), "from": from, "to": tr.To,
			"reason": opts.Reason, "effects": effects,
		}})
	})
	if err != nil {
		return nil, err
	}
	return &WriteResult{Batch: batch, Rows: []Row{s.row(t, after, sc, false)}, Held: []Held{}}, nil
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
	_, writeErr := sch.allow(sc, typ, true)
	from, _ := r.Data[t.Lifecycle.Field].(string)
	w := &writeTx{s: s, schema: sch, now: s.now()}
	for _, def := range t.Lifecycle.Transitions {
		tr := t.Lifecycle.byID[def.ID]
		if tr == nil || !startsFrom(tr, from) {
			continue
		}
		ready, err := w.guardHoldsOn(s.rdb, t, tr, r)
		if err != nil {
			return nil, err
		}
		out = append(out, TransitionState{ID: tr.ID, Label: label(tr), To: tr.To, Ready: ready,
			Allowed: writeErr == nil && slices.Contains(tr.By, sc.kind(OpTransition))})
	}
	return out, nil
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
	var signed, open int
	if err := q.QueryRow(`SELECT count(*) FROM signoffs WHERE record = ? AND version_hash = ?`, r.ID, versionHash(t, r)).Scan(&signed); err != nil {
		return false, err
	}
	if err := q.QueryRow(`SELECT count(*) FROM work_items WHERE record = ? AND status = ?`, r.ID, WorkOpen).Scan(&open); err != nil {
		return false, err
	}
	values["_signed"] = signed > 0
	values["_open_work"] = float64(open)
	return tr.guard.evalBool(t, values, nil), nil
}

// versionHash is a record's version as a sign-off binds it: the hash of its
// stored values. Any change to a value makes a new version, and a sign-off on
// the old one no longer counts.
func versionHash(t *Type, r *record) string {
	sum := sha256.Sum256([]byte(canonicalJSON(t.encode(r.Data, r.unknown))))
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

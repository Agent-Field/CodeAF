package decide

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// NextTime is the person's instruction for this kind after a reversal.
type NextTime string

const (
	NextAsk  NextTime = "ask"
	NextKeep NextTime = "keep"
)

// ErrNotUndoable lets the action owner refuse a reversal that is no longer possible.
var ErrNotUndoable = errors.New("This decision cannot be undone.")

// Dependent describes real work that referenced a decision. Uses contains direct
// references, so discovering dependents never walks or changes their descendants.
type Dependent struct {
	ID    string    `json:"id"`
	Kind  string    `json:"kind"`
	Title string    `json:"title,omitempty"`
	At    time.Time `json:"at"`
	Uses  []string  `json:"uses"`
}

const (
	DependentTask     = "task"
	DependentDecision = "decision"
)

// OverturnResult lists affected work without acting on it. Offers require a
// separate explicit choice; returning them never notifies or pauses anything.
type OverturnResult struct {
	DecisionID string      `json:"decisionId"`
	Refusal    string      `json:"refusal,omitempty"`
	Mode       Mode        `json:"mode,omitempty"`
	Dependents []Dependent `json:"dependents,omitempty"`
	Summary    string      `json:"summary,omitempty"`
	Offers     []string    `json:"offers,omitempty"`
}

// UndoAction executes the stored opaque handle in the engine that created it.
// It must be idempotent by token: a disk failure after the action may need a retry.
// It must not re-enter this Store, whose lock prevents concurrent double undo.
type UndoAction func(context.Context, Undo) error

// ReadDependents reads a snapshot of tasks and decisions from their real owners.
// It runs before the ledger lock so a reader can safely consult the same store.
type ReadDependents func(context.Context) ([]Dependent, error)

// Overturner joins the ledger to the engine's undo and dependency doors. It owns
// no task executor, consent store or guessed dependency data.
type Overturner struct {
	store      *Store
	undo       UndoAction
	dependents ReadDependents
}

// NewOverturner requires both engine doors so absent support cannot look like a
// successful reversal or an empty dependency list.
func NewOverturner(store *Store, undo UndoAction, dependents ReadDependents) (*Overturner, error) {
	if store == nil || undo == nil || dependents == nil {
		return nil, fmt.Errorf("%w: overturn needs a store, undo and dependency reader", ErrInvalid)
	}
	return &Overturner{store: store, undo: undo, dependents: dependents}, nil
}

// Overturn reverses only the named decision, then resets only its subject kind.
// Keep restarts learning; ask always asks. The undo must succeed before either
// the reversal time or mode is persisted. Dependents are offered, never changed.
func (o *Overturner) Overturn(ctx context.Context, decisionID string, next NextTime) (OverturnResult, error) {
	result := OverturnResult{DecisionID: decisionID}
	if decisionID == "" || (next != NextAsk && next != NextKeep) {
		return result, fmt.Errorf("%w: overturn needs a decision and next=ask|keep", ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	// A known refusal does not depend on the availability of the work reader.
	// Recheck under the write lock below because another caller may reverse it.
	var decision *Decision
	if err := o.store.view(func(c *doc) {
		for _, d := range c.Decisions {
			if d.ID == decisionID {
				decision = &d
				break
			}
		}
	}); err != nil {
		return result, err
	}
	if decision == nil {
		return result, ErrNotFound
	}
	if decision.OverturnedAt == nil && (!decision.Reversible || decision.Undo.Token == "") {
		result.Refusal = ErrNotUndoable.Error()
		return result, nil
	}
	candidates, err := o.dependents(ctx)
	if err != nil {
		return result, err
	}
	err = o.store.update(func(c *doc) error {
		for i := range c.Decisions {
			d := &c.Decisions[i]
			if d.ID != decisionID {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.OverturnedAt == nil {
				if !d.Reversible || d.Undo.Token == "" {
					result.Refusal = ErrNotUndoable.Error()
					return ErrNotUndoable
				}
				if err := o.undo(ctx, d.Undo); err != nil {
					if errors.Is(err, ErrNotUndoable) {
						result.Refusal = err.Error()
					}
					return err
				}
				at := o.store.now()
				d.OverturnedAt = &at
				mode := ModeLearning
				if next == NextAsk {
					mode = ModeAsk
				}
				c.Modes[KindKey(d.AskKind, d.Subject)] = KindState{Mode: mode}
			}
			// A repeated request reports the first result without undoing again or
			// erasing learning collected since that reversal.
			result.Mode = c.Modes[KindKey(d.AskKind, d.Subject)].Mode
			result.Dependents = directDependents(*d, candidates)
			result.Summary = dependentSummary(result.Dependents)
			if len(result.Dependents) > 0 {
				result.Offers = []string{"notify", "pause"}
			}
			return nil
		}
		return ErrNotFound
	})
	if errors.Is(err, ErrNotUndoable) {
		return result, nil
	}
	if err != nil {
		return OverturnResult{DecisionID: decisionID}, err
	}
	return result, nil
}

func directDependents(d Decision, candidates []Dependent) []Dependent {
	var out []Dependent
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate.ID == "" || candidate.ID == d.ID && candidate.Kind == DependentDecision || !candidate.At.After(d.At) {
			continue
		}
		if candidate.Kind != DependentTask && candidate.Kind != DependentDecision {
			continue
		}
		references := false
		for _, id := range candidate.Uses {
			if id == d.ID {
				references = true
				break
			}
		}
		key := candidate.Kind + ":" + candidate.ID
		if !references || seen[key] {
			continue
		}
		seen[key] = true
		candidate.Uses = append([]string(nil), candidate.Uses...)
		out = append(out, candidate)
	}
	return out
}

func dependentSummary(dependents []Dependent) string {
	tasks, decisions := 0, 0
	for _, d := range dependents {
		if d.Kind == DependentTask {
			tasks++
		} else {
			decisions++
		}
	}
	var parts []string
	for _, count := range []struct {
		n    int
		name string
	}{{tasks, "task"}, {decisions, "decision"}} {
		if count.n == 0 {
			continue
		}
		name := count.name
		if count.n != 1 {
			name += "s"
		}
		parts = append(parts, fmt.Sprintf("%d %s", count.n, name))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " and ") + " used this"
}

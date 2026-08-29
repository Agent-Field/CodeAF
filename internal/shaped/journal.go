package shaped

import (
	"context"
	"fmt"
)

// ── LEAVING A RECORD ──────────────────────────────────────────────────────────
//
// FAILSAFE.md clauses 3 and 4: a fail-safe that fires without saying so is a
// fail-safe nobody can autopsy, and a run that repaired itself twice looks
// exactly like a run that never had to. Every repair this seam performs is
// therefore announced to a journal the caller installs on the context — one line
// for a person watching, one row in the store for whoever reads it afterwards.
//
// It rides the context for the reason provider.WithCall does: only the door that
// opened the run knows where a record belongs, and only the pass making the call
// knows what happened. A call made without a journal is not an error and not a
// special case — every path that has no store is one — and Repaired is simply
// never reached.

// RepairKind is what the seam did.
type RepairKind string

const (
	// RepairContinued: the answer was cut at the ceiling with the object still
	// open, and the rest of it was asked for.
	RepairContinued RepairKind = "continued"
	// RepairReasked: the reply was not an object at all, and it was asked again
	// with the format contract and its own words quoted back.
	RepairReasked RepairKind = "reasked"
	// RepairFailed: the repairs are spent and the caller is getting a fault.
	RepairFailed RepairKind = "failed"
)

// Repair is one thing the seam did to get an answer, as the record keeps it.
type Repair struct {
	Lane    string     `json:"lane"`
	Model   string     `json:"model,omitempty"`
	Kind    RepairKind `json:"kind"`
	Round   int        `json:"round,omitempty"`
	Spent   int        `json:"spent,omitempty"`
	Ceiling int        `json:"ceiling,omitempty"`
}

// Words is the repair as a person reads it, in the register the headless
// stream's other narration lines use: what happened, then what was done about
// it. No machinery vocabulary — a reader is told the answer was cut off, never
// that a finish reason was length.
func (r Repair) Words() string {
	switch r.Kind {
	case RepairContinued:
		return "answer cut at the ceiling — continued"
	case RepairReasked:
		return "the answer was not readable — asked again"
	case RepairFailed:
		return "the answer was still not readable — giving up on it"
	default:
		return string(r.Kind)
	}
}

// Line is the whole narration line for one repair, subject included.
func (r Repair) Line() string {
	if r.Lane == "" {
		return r.Words()
	}
	return fmt.Sprintf("%s: %s", r.Lane, r.Words())
}

// Journal is where repairs go. One method, because a journal that could be asked
// questions would be a journal callers started reading during a request.
type Journal interface {
	Repaired(Repair)
}

// JournalFunc lets a plain function be a journal, which is what a test and a
// one-line wiring both want.
type JournalFunc func(Repair)

func (f JournalFunc) Repaired(repair Repair) { f(repair) }

type journalContextKey struct{}

// WithJournal names where repairs made under ctx are recorded.
func WithJournal(ctx context.Context, journal Journal) context.Context {
	if journal == nil {
		return ctx
	}
	return context.WithValue(ctx, journalContextKey{}, journal)
}

// JournalFrom returns the journal in force for ctx, nil when none was installed.
func JournalFrom(ctx context.Context) Journal {
	journal, _ := ctx.Value(journalContextKey{}).(Journal)
	return journal
}

// note records one repair, and does nothing at all when nobody is listening.
func note(ctx context.Context, repair Repair) {
	if journal := JournalFrom(ctx); journal != nil {
		journal.Repaired(repair)
	}
}

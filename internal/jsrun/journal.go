package jsrun

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// THE INCREMENTAL JOURNAL WRITER, and the run's ledger, which are one object
// because they are one fact.
//
// [exec.Spend] summed across a run's journal entries IS the run's ledger — the
// contract says so and this is where that stops being a claim: every host call
// writes its entry and folds its spend through the same method, so there is no
// second piece of arithmetic that could disagree with the first. A surface that
// added up the journal and a surface that read RunResult.Spend get the same
// number because they are reading the same additions.
//
// NOTHING HERE LOCKS, which is the law internal/subharness/usage.go states about
// itself and internal/exec/journal.go restates about [exec.Spend]. This runtime
// earns it the plain way: THE PROGRAM RUNS ON ONE GOROUTINE AND HOST CALLS ARE
// SERIAL BY CONSTRUCTION. goja calls a bound Go function synchronously, on the
// goroutine running the script, and this package binds nothing that starts
// another one — there is no setTimeout, no worker, no job queue a program can
// reach. The only other goroutine a run has is the fuel watchdog (fuel.go),
// which touches the interpreter through [goja.Runtime.Interrupt] — atomic by
// goja's own design — and touches this recorder not at all. A runtime that ever
// ran host calls concurrently owns this type's safety along with everything else
// it changed about running a program.
type recorder struct {
	journal exec.Journal
	env     exec.Env

	// seq counts from one within a run, so a reader can order entries without a
	// clock.
	seq int
	// steps is how many host calls have been made. It is the operation budget's
	// meter; see [Fuel] for why an operation is a host call here.
	steps int
	// spend is the running ledger.
	spend exec.Spend
	// report is the last thing log() said, which is what a person reads as the
	// account of the run. The LAST one rather than all of them: the journal
	// already has every row, and RunResult.Report is one line.
	report string
}

// write puts one entry in the journal, numbering it and stamping it. It is the
// only door — a caller that filled Seq or At itself would be the second place
// this run's ordering was decided.
func (r *recorder) write(entry exec.JournalEntry) {
	r.seq++
	entry.Seq = r.seq
	if entry.At.IsZero() {
		entry.At = time.Now()
	}
	r.spend.Add(entry.Spend)
	// A journal that cannot be written is a run nobody can watch, not a run that
	// should stop. The work is the point; the record is how it is watched.
	_ = exec.Record(r.journal, entry)
}

// deopt records that this run needed a closer look and the work is going the
// long way.
//
// IT IS WRITTEN AS A log() ROW ON PURPOSE. The six call names in
// internal/exec/journal.go are a closed vocabulary — a bundle spells its doors
// with them and a surface filters a journal by them — and a seventh invented
// here would be a word only this package knew. A deopt is exactly what log() is
// for: one visible progress row, in a person's words, costing nothing.
func (r *recorder) deopt(why string) {
	r.write(exec.JournalEntry{Call: exec.CallLog, Note: why})
}

// encode writes a value down for the journal. A value that will not marshal is
// recorded as absent rather than as a failure: the journal's job is to say the
// call happened, and losing an unencodable argument is a smaller loss than
// losing the entry.
func encode(value any) json.RawMessage {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return raw
}

// journalKey is how a run's journal reaches a runner that was handed only a
// context, an input and an [exec.Env].
type journalKey struct{}

// WithJournal puts this run's journal on the context.
//
// THE CONTEXT CARRIES IT BECAUSE THE JOURNAL IS A FACT ABOUT ONE RUN.
// [exec.Runner.Run] takes exactly one other per-run thing — the input — and the
// runner itself is per-BUNDLE and outlives any run of it, so a journal field on
// [Bundle] would be a per-run value living on a shared object. The door lane
// wraps the context it already builds for cancellation.
func WithJournal(ctx context.Context, journal exec.Journal) context.Context {
	if journal == nil {
		return ctx
	}
	return context.WithValue(ctx, journalKey{}, journal)
}

// journalFor finds the journal for this run, the context first and the
// [exec.Env] second.
//
// An Env that also writes the run's feed is the shape the door lane is likely to
// build — a room is one object that both spends and displays — so an Env that
// happens to implement [exec.Journal] is taken at its word. A run with neither
// is a run nobody is watching, which is a real case and not an error:
// [exec.Record] is nil-safe for exactly this.
func journalFor(ctx context.Context, env exec.Env) exec.Journal {
	if journal, ok := ctx.Value(journalKey{}).(exec.Journal); ok && journal != nil {
		return journal
	}
	if journal, ok := env.(exec.Journal); ok && journal != nil {
		return journal
	}
	return nil
}

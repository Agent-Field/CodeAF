package session

import (
	"context"
	"strings"

	"github.com/Agent-Field/codeaf/internal/effort"
)

// WHAT ONE HAND-OFF TO A PROGRAM ASKS OF ITS RUN BEYOND THE BRIEF.
//
// A hand-off is decided in the turn that proposes it and started by a door
// several calls away from it (task.go's commitProposalToRun, then the run
// road's start doors). The two things that ride between them for a program and
// for nothing else travel on the context, the way the crew's own wish does
// ([crewWish]), so no door in between grows an argument only one caller fills.

// programWish is one hand-off's asks of its program's run.
type programWish struct {
	// thinking is the rung the conversation asked the program's working model
	// for (`thinking` on the proposal), [effort.None] for the program's own
	// default.
	thinking effort.Rung
	// carry is the branch an earlier run of the same line left its work on,
	// which this run takes up rather than starting again from the person's
	// checkout ([programCarryOf]); nil for a line's first run.
	carry *programCarry
}

// programCarry is the branch a run carries on, as its earlier run's row wrote
// it down ([runCopyOf]): the branch, the repository it is in, and where the
// person's checkout stood when the line began.
//
// Fresh says the run takes the work up on a new branch of its own cut from
// that one's tip rather than on that branch itself, because the earlier run's
// work passed: its branch is left holding exactly what passed.
//
// Snapshot is the commit the line's first run carried the person's uncommitted
// changes in on ([ProgramFolder.Snapshot]), which a run that carries the
// branch on counts its work from.
type programCarry struct {
	Branch    string
	Root      string
	Home      string
	Start     string
	Snapshot  string
	Untracked []string
	Fresh     bool
}

type programWishKey struct{}

// withProgramWish hands a start door one hand-off's program wish; an empty
// wish is no value.
func withProgramWish(ctx context.Context, wish programWish) context.Context {
	if wish.thinking == effort.None && wish.carry == nil {
		return ctx
	}
	return context.WithValue(ctx, programWishKey{}, wish)
}

func programWishOf(ctx context.Context) programWish {
	wish, _ := ctx.Value(programWishKey{}).(programWish)
	return wish
}

// programCarryOf is the branch a hand-off to program carries on: the one the
// line's last run left its work on, when this hand-off is the next run of that
// line ([Agent.programAttemptOf]) and the same program's. Nil otherwise.
//
// A SENT-BACK RUN FINISHES THE WORK, IT DOES NOT START IT AGAIN. codeaf sends a
// program back to work that did not stand, with a brief sharpened by what
// failed; the work it is sent back to is on the earlier run's branch, and a
// run cut fresh from the person's checkout would not have it. Before programs
// worked in a copy of their own, that branch was simply the one left checked
// out; it is checked out nowhere now, so the line's own record names it.
//
// A BRANCH WHOSE WORK PASSED IS NEVER WRITTEN AGAIN. The next hand-off to the
// same program before the person speaks may be the rest of that work, which
// needs it, or new work queued behind it; either way it starts from that
// branch's tip on a new branch of its own ([programCarry.Fresh]), so the
// passed branch still holds exactly what passed.
func (a *Agent) programCarryOf(prior *programOutcome, program string) *programCarry {
	if prior == nil || prior.program != program {
		return nil
	}
	record := a.runRowCopy(prior.row)
	if record == nil || strings.TrimSpace(record.Branch) == "" || strings.TrimSpace(record.Root) == "" {
		return nil
	}
	return &programCarry{Branch: record.Branch, Root: record.Root, Home: record.Home, Start: record.HomeSha, Snapshot: record.Snapshot,
		Untracked: append([]string(nil), record.Untracked...), Fresh: prior.verdict == programPassed}
}

// programEffort is the rung a program's working model is asked for, as its
// crew hands it over ([delegate.Crew.Effort]): the one the conversation chose
// for this hand-off, else the one written on the person's working seat
// (`model:high`), else nothing, which leaves the program on its own default.
//
// THE CONVERSATION'S CHOICE OUTRANKS THE SEAT because it is the nearer hand: it
// was made for this piece of work, today, by a model that read it, where the
// seat's rung was written once for every task. A person who wants a particular
// rung says so in the conversation, and that is how the choice is made.
func programEffort(thinking effort.Rung, seat string) string {
	if thinking.Valid() {
		return thinking.String()
	}
	if rung, ok := effort.Parse(seat); ok && rung.Valid() {
		return rung.String()
	}
	return ""
}

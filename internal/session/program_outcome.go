package session

// A PROGRAM'S ENDING IS FOR CODEAF TO ACT ON, NOT FOR THE PERSON TO DECODE.
//
// senior-dev ends with a status — its change passed the project's own build and
// tests, nothing finished checking it, it did not pass, it hit a ceiling, it
// broke — and until this file that status was a sentence on a row and in the
// conversation's record, read by the model only if the person happened to ask
// something next. The owner asked on 2026-09-25 that the status inform codeaf
// rather than the person: the chat reads it the moment the run ends, checks
// what it can, sends senior-dev back with a sharper brief when the work did not
// stand, and tells the person where the work is in one plain summary.
//
// So every program run's landing WAKES a bounded turn ([Agent.deliverBeltRunLanding])
// carrying the ending as a fact ([programVerdict]) and the one line of what to
// do about it, under a role page that says the whole playbook
// (prompts/program-outcome.md) — the per-event vehicle, which costs the fixed
// prefix nothing.
//
// TWO BOUNDS ARE CODE, NOT ADVICE. codeaf sends a program back on its own at
// most [programAutoRetries] times in a line of runs on one piece of work, and
// never after a run that ended on a dollar or time ceiling: that re-run spends
// more of the person's money, so it waits for their word ([Agent.programRetryRefusal]).
// A hand-off the person asks for in their own turn is theirs, and starts the
// count again.

import (
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
)

// programVerdict is how a program's run came out, as codeaf acts on it.
type programVerdict string

const (
	// programPassed is finished work the program's own run of the project's
	// build and tests passed.
	programPassed programVerdict = "passed"
	// programUnverified is finished work nothing finished checking.
	programUnverified programVerdict = "unverified"
	// programFailed is work the program handed in that does not pass, or work
	// it did not finish.
	programFailed programVerdict = "failed"
	// programLimit is a run that stopped on a ceiling: the program's own, or a
	// dollar or time limit the person set.
	programLimit programVerdict = "limit"
	// programCrashed is a run that broke — the program crashed, or ended
	// without an ending of its own.
	programCrashed programVerdict = "crashed"
)

// programAutoRetries is how many times codeaf sends a program back to one
// piece of work on its own, after the first run: the owner's cap.
const programAutoRetries = 2

// programOutcomeCallCeiling bounds the turn a program's landing wakes. It is
// wider than a settle turn's ([settleCallCeiling]), because this turn may run
// the project's checks on the program's branch and hand the work back, and
// narrow enough that a turn cannot become an unbounded session of its own.
const programOutcomeCallCeiling = 16

// programOutcomeWindow is how long that turn has: a project's test suite has
// to fit in it.
const programOutcomeWindow = 15 * time.Minute

// programVerdictOf reads how a program's run came out off the run's summary:
// a limit first (the person's own or the program's ceiling), then the
// program's own unfinished ending, then the word it finished on.
func programVerdictOf(summary RunSummary) programVerdict {
	if summary.Limit != "" {
		return programLimit
	}
	if ended := summary.Program; ended != nil {
		switch ended.Status {
		case delegate.StatusBudget:
			return programLimit
		case delegate.StatusCrashed:
			return programCrashed
		}
		return programFailed
	}
	if summary.Outcome == beltRunOutcomeDone {
		if summary.ProgramVerdict == "pass" {
			return programPassed
		}
		return programUnverified
	}
	// A run that did not finish and carried no ending of the program's own:
	// it exited without one, or the road under it failed.
	return programCrashed
}

// programAttempt is one run's place in a line of runs on one piece of work.
type programAttempt struct {
	// attempt counts the runs in the line, 1 for the first.
	attempt int
	// auto counts the runs in it codeaf started on its own after an ending.
	auto int
}

// programOutcome is a program run's ending as the turn it wakes holds it
// ([Agent.programOutcomeNow]): which run, what it came to, and where in its
// line it stands.
type programOutcome struct {
	row     uint64
	program string
	verdict programVerdict
	programAttempt
}

// programOutcomeNote is the note a program's landing wakes the conversation
// with: the landing's own line, then the ending as a fact and the one thing to
// do about it now.
func programOutcomeNote(outcome programOutcome, line string, costUSD float64) string {
	var b strings.Builder
	b.WriteString(line)
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "[%s ended — for you to act on] task %d · %s · run %d", outcome.program, outcome.row, outcome.verdict, outcome.attempt)
	if costUSD > 0 {
		fmt.Fprintf(&b, " · $%.2f", costUSD)
	}
	b.WriteString("\n")
	b.WriteString(programNextStep(outcome))
	return b.String()
}

// programNextStep is what to do about one ending, in one sentence the model
// reads with the playbook it expands.
func programNextStep(o programOutcome) string {
	left := programAutoRetries - o.auto
	switch o.verdict {
	case programPassed:
		return "Its change passed the project's own checks. Check the result against what was asked, then tell the person in one short summary where the work is and offer to merge it."
	case programUnverified:
		return "Nothing finished checking its change. Run the project's checks on its branch yourself, then act on what they show as you would on a pass or a failure."
	case programLimit:
		return "It stopped on a limit, so another run spends more of the person's money: do not hand it back. Tell the person briefly what is done and what is left, and ask whether to spend more."
	}
	if left <= 0 {
		return fmt.Sprintf("It has been sent back %d times already, which is the most codeaf does on its own: do not hand it back. Tell the person plainly what still does not work, where the work is, and what you would try next.", programAutoRetries)
	}
	if o.verdict == programCrashed {
		return fmt.Sprintf("It broke rather than finished. If the cause looks passing (a network or provider failure), hand the same work to %s again; otherwise tell the person plainly. You may send it back %d more time%s on your own.", o.program, left, plural(left))
	}
	return fmt.Sprintf("Its work does not stand yet. Read what failed; fix a small gap on its branch yourself, or hand the work back to %s with a brief sharpened by what failed. You may send it back %d more time%s on your own.", o.program, left, plural(left))
}

// rememberProgramOutcomeLocked keeps a program's ending for the turn it
// arrives in, so a hand-off that turn makes is known as a re-attempt of it.
// The caller holds a.mu.
func (a *Agent) rememberProgramOutcomeLocked(user userMessage) {
	if user.programOutcome != nil {
		outcome := *user.programOutcome
		a.programOutcomeNow = &outcome
	}
}

// programRetryRefusal is why a hand-off to a program made in the turn a
// program's ending woke may not go, and "" when it may. It is the code half
// of the playbook's two bounds.
func (a *Agent) programRetryRefusal(via string) string {
	if strings.TrimSpace(via) == "" {
		return ""
	}
	a.mu.Lock()
	now := a.programOutcomeNow
	a.mu.Unlock()
	if now == nil {
		return ""
	}
	switch {
	case now.verdict == programLimit:
		return fmt.Sprintf("task %d stopped on a limit, and another %s run spends more of the person's money: ask the person first, and hand it over only on their word", now.row, via)
	case now.auto >= programAutoRetries:
		return fmt.Sprintf("%s has been sent back to this work %d times already, the most codeaf does on its own: tell the person where the work stands and let them decide", now.program, programAutoRetries)
	}
	return ""
}

// programAttemptOf is the place in its line of a hand-off to a program made
// now: the next run of the line the turn's ending belongs to, counted as
// codeaf's own, or the first run of a new line when the person's turn made it.
func (a *Agent) programAttemptOf() programAttempt {
	a.mu.Lock()
	defer a.mu.Unlock()
	if now := a.programOutcomeNow; now != nil {
		return programAttempt{attempt: now.attempt + 1, auto: now.auto + 1}
	}
	return programAttempt{attempt: 1}
}

// keepProgramAttempt writes down a started hand-off's place in its line, by
// the row the run is published under.
func (a *Agent) keepProgramAttempt(row uint64, attempt programAttempt) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.programAttempts == nil {
		a.programAttempts = map[uint64]programAttempt{}
	}
	a.programAttempts[row] = attempt
}

// programAttemptFor is a run's place in its line, the first run of one when
// nothing was written down (a typed `/senior-dev`, or a conversation reopened
// since).
func (a *Agent) programAttemptFor(row uint64) programAttempt {
	a.mu.Lock()
	defer a.mu.Unlock()
	if attempt, ok := a.programAttempts[row]; ok {
		return attempt
	}
	return programAttempt{attempt: 1}
}

// programLandingNote is a program run's landing as the note that wakes the
// conversation to act on it ([programOutcomeNote]), owing the person's
// question when the hand-off carried one.
func (a *Agent) programLandingNote(run *beltRun, summary RunSummary, line string) userMessage {
	outcome := programOutcome{
		row: run.row, program: programName(run.delegate), verdict: programVerdictOf(summary),
		programAttempt: a.programAttemptFor(run.row),
	}
	text := programOutcomeNote(outcome, line, a.beltRunSpent(run.row))
	document := userText(text)
	if task := run.store.Task(run.root); landingOwesAnswer(task) {
		document = owedLandingDocument(task, text)
	}
	note := wakeNote(document.text())
	note.landingQuestion, note.landingOutcome = document.landingQuestion, document.landingOutcome
	note.batch = false
	note.settle, note.settleCeiling, note.settleWindow = true, programOutcomeCallCeiling, programOutcomeWindow
	note.settlePrompt = programOutcomePrompt
	note.programOutcome = &outcome
	return note
}

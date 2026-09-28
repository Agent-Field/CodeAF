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
	"encoding/json"
	"fmt"
	"os"
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

// rememberProgramOutcomeLocked keeps a program's ending until the person next
// speaks, so later automatic turns still know a hand-off is a re-attempt.
// The caller holds a.mu.
func (a *Agent) rememberProgramOutcomeLocked(user userMessage) {
	if user.programOutcome != nil {
		outcome := *user.programOutcome
		a.programOutcomeNow = &outcome
		a.programHold = &outcome
		a.saveProgramHoldLocked()
	}
}

// programRetryRefusal is why an automatic hand-off before the person's next
// message may not go, and "" when it may. It is the code half of the playbook's
// two bounds.
func (a *Agent) programRetryRefusal(via string) string {
	if strings.TrimSpace(via) == "" {
		return ""
	}
	a.mu.Lock()
	now := a.programHold
	fault := a.programHoldErr
	a.mu.Unlock()
	if fault != "" {
		return fmt.Sprintf("%s cannot be sent back automatically: its hand-off history could not be saved (%s)", via, fault)
	}
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
	if now := a.programHold; now != nil {
		return programAttempt{attempt: now.attempt + 1, auto: now.auto + 1}
	}
	return programAttempt{attempt: 1}
}

// keepProgramAttempt writes down a started hand-off's place in its line, by
// the row the run is published under.
func (a *Agent) keepProgramAttempt(row uint64, attempt programAttempt) *programOutcome {
	a.mu.Lock()
	defer a.mu.Unlock()
	var prior *programOutcome
	if a.programHold != nil {
		copy := *a.programHold
		prior = &copy
	}
	if a.programAttempts == nil {
		a.programAttempts = map[uint64]programAttempt{}
	}
	a.programAttempts[row] = attempt
	if a.programHold != nil {
		next := *a.programHold
		next.row = row
		next.programAttempt = attempt
		a.programHold = &next
		a.saveProgramHoldLocked()
	}
	return prior
}

// rollbackProgramAttempt returns a refused start's place to the count, because
// only a run that actually started can use one of the automatic hand-offs.
func (a *Agent) rollbackProgramAttempt(row uint64, prior *programOutcome) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.programAttempts, row)
	if a.programHold == nil || a.programHold.row != row {
		return
	}
	if prior == nil {
		a.clearProgramHoldLocked()
		return
	}
	copy := *prior
	a.programHold = &copy
	a.saveProgramHoldLocked()
}

// rollbackFailedProgramStart returns the count only when a proposed program
// never started; ordinary tasks have no program attempt to return.
func (a *Agent) rollbackFailedProgramStart(via *delegate.Delegate, row uint64, prior *programOutcome, err error) {
	if via != nil && err != nil {
		a.rollbackProgramAttempt(row, prior)
	}
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
	a.mu.Lock()
	a.programHold = &outcome
	a.saveProgramHoldLocked()
	a.mu.Unlock()
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

type programHoldRecord struct {
	Row     uint64         `json:"row"`
	Program string         `json:"program"`
	Verdict programVerdict `json:"verdict"`
	Attempt int            `json:"attempt"`
	Auto    int            `json:"auto"`
}

// programHoldPath keeps the automatic hand-off bound beside the conversation
// journal, so a reopen does not make another unattended run newly legal.
func (a *Agent) programHoldPath() string {
	if a.config.SessionFile == "" {
		return ""
	}
	return a.config.SessionFile + ".program-handoff.json"
}

func (a *Agent) restoreProgramHold() {
	path := a.programHoldPath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		a.programHoldErr = err.Error()
		return
	}
	var record programHoldRecord
	if err := json.Unmarshal(data, &record); err != nil {
		a.programHoldErr = err.Error()
		return
	}
	a.programHold = &programOutcome{row: record.Row, program: record.Program, verdict: record.Verdict,
		programAttempt: programAttempt{attempt: record.Attempt, auto: record.Auto}}
}

// saveProgramHoldLocked stores the whole bound atomically. A failed write
// refuses further automatic runs until the person's next message resets it.
func (a *Agent) saveProgramHoldLocked() {
	path := a.programHoldPath()
	if path == "" || a.programHold == nil {
		return
	}
	o := a.programHold
	data, err := json.Marshal(programHoldRecord{Row: o.row, Program: o.program, Verdict: o.verdict,
		Attempt: o.attempt, Auto: o.auto})
	if err == nil {
		err = os.WriteFile(path+".tmp", data, 0o600)
	}
	if err == nil {
		err = os.Rename(path+".tmp", path)
	}
	if err != nil {
		a.programHoldErr = err.Error()
	} else {
		a.programHoldErr = ""
	}
}

func (a *Agent) clearProgramHoldLocked() {
	a.programHold, a.programHoldErr = nil, ""
	if path := a.programHoldPath(); path != "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			a.programHoldErr = err.Error()
		}
	}
}

// programPageNote is what a program run's own page keeps as its ending: that
// the ending went to the conversation, and where the work is. THE PROGRAM'S
// STATUS IS NOT ON IT. The page's notes used to carry the whole outcome line —
// the program's status sentence, what its model claimed, what it observed —
// which is the account codeaf acts on, not one a person reads; the chat's reply
// says what came of the work, and the program's own words are on its actions.
func programPageNote(program string, landing RunLanding) string {
	said := program + "'s ending went to the chat"
	if line := beltLandingLine(landing); line != "" {
		said += " · " + line
	}
	return said
}

// programLimitLine is written to the conversation before its wake is tried.
// The same conversation limit may refuse that wake, so the model cannot be
// the only messenger of the ending.
func programLimitLine(run *beltRun, summary RunSummary, landing RunLanding) string {
	if programVerdictOf(summary) != programLimit {
		return ""
	}
	kind := summary.Limit
	if kind == "" && summary.Program != nil {
		// senior-dev calls its own elapsed ceiling "wall", while the run
		// supervisor reports the same cause as a time limit.
		reason := strings.ToLower(summary.Program.Reason)
		if strings.Contains(reason, "wall") || strings.Contains(reason, "time") {
			kind = RunLimitTime
		}
	}
	scope, limit := "the run's", ""
	if kind == RunLimitTime {
		if run.conversationTimeLimit {
			scope = "the conversation's"
		}
		limit = (delegate.Ceilings{Hours: run.timeCeiling}).TimeWord()
	} else {
		if run.conversationCostLimit {
			scope = "the conversation's"
		}
		limit = fmt.Sprintf("$%.2f", run.costCeiling)
	}
	spent := summary.USD
	if spent <= 0 {
		spent = run.spent
	}
	spentWord := "spent no metered dollars"
	if spent > 0 {
		spentWord = fmt.Sprintf("spent $%.2f", spent)
	}
	where := "in the folder " + run.ground
	if landing.Branch != "" {
		where = "on branch " + landing.Branch + " in " + run.ground
	}
	return fmt.Sprintf("%s stopped at %s %s limit · %s · its work is %s", programName(run.delegate), scope, limit, spentWord, where)
}

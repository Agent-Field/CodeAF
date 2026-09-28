package tui3

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// ── WHAT THIS CONVERSATION IS SPENDING, INCLUDING THE WORK IT STARTED ────────
//
// The money segment used to say what the conversation's own books said, and a
// task's money only reaches those books when the task CLOSES (internal/session's
// foldTaskUsage). On the run that produced issue #145 that meant `$2.53` on the
// row for two hours while the tree under it burned $51.05 — the true figure
// reachable only by widening the task column and reading the parent row. The
// smaller number standing alone is the whole defect; a person glances at that
// row precisely while the work is running.
//
// SO THE SEGMENT SHOWS THE SUBTREE TOTAL, WHICH IS ONE FIGURE AND NOT TWO. It is
// the rule the task column already draws by — a parent row carries what its
// whole family cost — applied to the conversation as the root of its own tree.
// One figure rather than `$2.53 + $51 in tasks` because this is a live row whose
// segments must not grow and shrink under a person's eye, because the row is
// already the most crowded thing on the screen, and because the question it is
// glanced at to answer is "what is this costing me", which has one answer. The
// SPLIT is what `/cost` is for, and `/cost` is a note somebody asks for and
// reads once.
//
// THE FIGURE COMES OFF THE LEDGER THE LIMITS ARE READ FROM, and no second
// accumulator exists anywhere. Every model call writes one line there AS IT IS
// MADE (issue #269 — it used to be written at the turn's seal, so an interrupted
// turn's money reached no surface but the meter), a fold writes none, and a
// node's line names the conversation the work is rooted in
// ([session.UsageLine.Root]) — so the tree is one sum over rows that are each
// there exactly once, running or closed.
//
// AND THE SUM IS MADE ONCE, in [session.UsageTree], which hands back the one
// [session.Receipt] every surface here reads. This file chooses what to SHOW;
// it never chooses what to add.
//
// ON THE FRAME CLOCK IT IS READ ONLY WHILE THERE IS WORK TO READ ABOUT. A
// conversation that has never started a task never touches the file there: a
// stat three times a second for a number that cannot move is the shape PERF.md
// exists to catch. The places that ask for the figure on purpose — the Spending
// tab, /cost, /status — read it every time, because a late receipt moves the
// ledger with no work running at all ([app.treeLines] says how).

// readTreeSpend takes one reading of the ledger for this conversation's tree.
//
// It is a TAIL READ. [session.UsageCache] keeps what it has already parsed and
// reads only what has been appended since, which is what makes this callable on
// the frame clock: a ledger with a year of spending in it is walked once, and
// every reading after that costs a stat and the handful of rows a running tree
// has written since the last frame.
func (a *app) readTreeSpend() {
	self := a.selfSessionID()
	if self == "" {
		return
	}
	lines, known := a.treeLines()
	if !known {
		// A ledger nobody could read, or a far one that has not answered yet, is
		// a figure this surface does not have, and what it does about that is
		// keep the figure it had — never say a smaller number, and never say
		// anything about the file to somebody who is mid-sentence.
		return
	}
	a.tree = session.UsageTree(lines, self)
}

// treeLines is the ledger the tree is summed over, through THE SAME TWO DOORS
// [app.usageSince] goes through and in the same order: the seam first, the file
// on this disk only where there is none.
//
// THE SEAM IS NOT OPTIONAL, AND ITS ABSENCE HERE WAS A WRONG BILL. A window on
// the engine host — which is what a bare `codeaf` opens — carries the seam and
// no ledger path, so this reading used to stop at its first line and the
// `this one` receipt fell back to the conversation's own books, which learn
// about a call only when the frame clock asks the agent. A cut errand's
// receipt that the provider answered after the turn had ended (a naming call
// past its patience, a route judge the turn finished in front of) is banked
// into the ledger under this conversation, and nothing asks the agent again
// once the frames stop. So `today` counted it and `this one` did not, on a
// machine holding one conversation (the tagged suite's
// one_figure_on_every_spend_surface). The day's own reading was moved onto this
// seam for the same reason and this one was left behind (settingspend.go's
// [app.readDayCost]).
//
// THE FLOORS DIFFER AND BOTH ARE DELIBERATE. The file is read with none: a
// conversation held open across midnight has spent what it has spent, and a
// figure that reset under a person's eye would be this row answering a question
// about the DAY with the word for the conversation. The seam is asked for the
// spend place's own fortnight instead, because a floor under what it holds makes
// the link fetch the far machine's whole ledger again on every beat
// (cmd/codeaf's hostLedger). What that costs is stated rather than hidden: over
// a connection, a conversation's rows older than [spendWindowDays] are not in
// the tree, and [app.spendShown]'s maximum leaves such a conversation on its
// own books.
func (a *app) treeLines() ([]session.UsageLine, bool) {
	if a.ledger != nil {
		lines, _, known := a.ledger(session.LastDays(a.now(), spendWindowDays).From)
		return lines, known
	}
	if strings.TrimSpace(a.usageLedger) == "" {
		return nil, false
	}
	a.treeCache.Path = a.usageLedger
	// The error is dropped and the lines kept, for [session.UsageCache.Read]'s
	// reason: a torn last line costs that line and never the figure.
	lines, _ := a.treeCache.Read(time.Time{})
	return lines, true
}

// spendShown is the figure the money segment, the phone deck, /status and /cost
// all draw. ONE FUNCTION, because a bill that read one way on the row and
// another way in the note would be the surface disagreeing with itself about the
// only number on it a person acts on.
//
// IT IS THE LARGER OF THE TWO READINGS AND NEVER THEIR SUM. The conversation's
// own books already hold every closed node's tally, folded in as it closed, and
// the ledger holds those same calls under the node that made them — so adding
// the two would count a finished task twice. Taking the larger is exact in both
// directions that matter: while a tree is running the ledger is ahead, because
// the books have not been told yet; once everything has closed the two agree.
//
// AND IT NEVER GOES BACKWARDS, which is the other half of why it is a maximum.
// A conversation resumed from a journal older than the ledger — or on a machine
// whose ledger was moved — has books the file cannot account for, and a figure
// that dropped when a person opened yesterday's work would be worse than the
// figure that was too small.
func (a *app) spendShown() float64 { return spendOf(a.cost, a.tree.Folded()) }

// spendOf is [app.spendShown]'s rule on its own, the larger of a
// conversation's books and its tree on the ledger, so the wall's tile for a
// conversation this window holds behind the front says the figure its status
// line would (wallspend.go), by the same arithmetic and not a copy of it.
func spendOf(books, tree float64) float64 {
	if tree > books {
		return tree
	}
	return books
}

// spendSplit is what /cost prints under the total: what the conversation itself
// spent, and what its work spent.
//
// IT ANSWERS NOTHING AT ALL UNLESS BOTH HALVES ARE REAL AND THE LEDGER IS THE
// FIGURE BEING SHOWN. A conversation that has started no work has no split to
// state — the total IS the conversation — a half that is zero would be `$0.00`
// printed into a note, which the emptiness law forbids outright, and a
// conversation whose books run ahead of the ledger has a total the two halves
// would not add up to, which is the one thing a split must never be. Saying
// nothing in all three cases is the same rule: this surface draws a division
// only where it knows one.
func (a *app) spendSplit() (conversation, tasks float64, ok bool) {
	if a.tree.Children <= 0 || a.tree.Direct <= 0 || a.tree.Folded() < a.cost {
		return 0, 0, false
	}
	return a.tree.Direct, a.tree.Children, true
}

// selfSessionID is this conversation's own 16-hex id: the name of the folder its
// journal sits in, which is how the id is written down on disk (internal/
// session's place.go mints the id and names the folder after it).
//
// IT IS THE TASK SHEET'S OWN DERIVATION ([app.taskSheetSelfRow]) and not a
// second one, so the two surfaces can never disagree about which conversation
// this is. A session with no journal — and the legacy flat layout, where the
// folder is the workspace and not an id — answers with something no ledger line
// names, which sums to nothing and leaves [app.spendShown] on the books alone.
func (a *app) selfSessionID() string { return sessionIDOf(a.file) }

// sessionIDOf is the conversation id a journal at file is kept under: the name
// of its folder. "" for no file.
func sessionIDOf(file string) string {
	file = strings.TrimSpace(file)
	if file == "" {
		return ""
	}
	return filepath.Base(filepath.Dir(file))
}

package tui3

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
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
// accumulator exists anywhere. Every model call writes one line there where it
// was made, a fold writes none, and a node's line now names the conversation the
// work is rooted in ([session.UsageLine.Root]) — so the tree is one sum over
// rows that are each there exactly once, running or closed.
//
// IT IS READ ONLY WHILE THERE IS WORK TO READ ABOUT. A conversation that has
// never started a task never touches the file: its own books are the whole
// truth, and a stat three times a second for a number that cannot move is the
// shape PERF.md exists to catch.

// readTreeSpend takes one reading of the ledger for this conversation's tree.
//
// It is a TAIL READ. [session.UsageCache] keeps what it has already parsed and
// reads only what has been appended since, which is what makes this callable on
// the frame clock: a ledger with a year of spending in it is walked once, and
// every reading after that costs a stat and the handful of rows a running tree
// has written since the last frame.
func (a *app) readTreeSpend() {
	if strings.TrimSpace(a.usageLedger) == "" {
		return
	}
	self := a.selfSessionID()
	if self == "" {
		return
	}
	a.treeCache.Path = a.usageLedger
	// The error is dropped for [session.ReadUsage]'s reason at the other call
	// sites: a ledger that cannot be read is a figure this surface does not
	// have, and what it does about that is keep the figure it had — never say a
	// smaller number, and never say anything about the file to somebody who is
	// mid-sentence.
	//
	// THE FLOOR IS THE ZERO TIME, on purpose. A conversation held open across
	// midnight has spent what it has spent, and a figure that reset under a
	// person's eye would be this row answering a question about the DAY with the
	// word for the conversation. The day's own reading is a different row on a
	// different page (settingspend.go).
	lines, _ := a.treeCache.Read(time.Time{})
	a.tree = session.UsageTree(lines, self)
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
func (a *app) spendShown() float64 {
	if total := a.tree.TotalUSD(); total > a.cost {
		return total
	}
	return a.cost
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
	if a.tree.TasksUSD <= 0 || a.tree.ConversationUSD <= 0 || a.tree.TotalUSD() < a.cost {
		return 0, 0, false
	}
	return a.tree.ConversationUSD, a.tree.TasksUSD, true
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
func (a *app) selfSessionID() string {
	file := strings.TrimSpace(a.file)
	if file == "" {
		return ""
	}
	return filepath.Base(filepath.Dir(file))
}

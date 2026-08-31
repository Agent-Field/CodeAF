package tui3

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE MINUTES AFTER THE WORKER STOPS TALKING ──────────────────────────────
//
// A task node has three lives and one state. Its worker writes the code, a check
// reads what the worker left, and a repair round closes the gaps the check named
// — and the engine calls all three of those `running`, correctly, because
// nothing landed and nothing was undone between them (session's task_audit.go).
//
// SO THE ROW WENT QUIET. The worker's last line scrolls past, the check spends
// four minutes reading a tree, a repair round spends another six rewriting it,
// and this surface drew for all of that exactly what it drew for a node between
// two calls: a clock. The person watching has no way to tell that from work that
// hung, and in the run this file exists for they concluded it had.
//
// The engine now says which life it is in ([session.EventTaskPhase]) and this
// file is where the words are. THE WORDS ARE THE SURFACE'S, not the engine's:
// the wire carries `checking` and `repairing`, which are the same three words
// the pulse file on disk carries, and what a person reads is written here, once,
// for every place that draws it.
//
// NONE OF THE MACHINERY IS IN THEM. There is a gate, and rounds, and a judgement
// — and a person watching their own work has no use for any of that. What they
// have a use for is that the work is being looked at, that it is being finished
// rather than abandoned, how far through that it is, and what was found. So:
// "checking what it left", "closing gaps · round 1 of 1", and the finding under
// it in the checker's own sentence.

const (
	// taskCheckingWord is a node under the check that reads what its worker
	// left. It says what is being read rather than what is doing the reading:
	// the reader is machinery and the work is the person's.
	taskCheckingWord = "checking what it left"
	// taskClosingWord opens a repair round. "closing gaps" is what the round
	// does; the round numbers follow it, because how far through it is is the
	// second thing anybody watching a second minute of one wants.
	taskClosingWord = "closing gaps"
)

// taskPhaseLine is what a person reads for one of a node's three lives, and ""
// for the ordinary one.
//
// WORKING DRAWS NOTHING AT ALL. A node getting on with the work is what this
// column has always drawn — its call, its gap, its clock — and a row that added
// "working" to that would be the surface narrating its own default. That is the
// emptiness law on this line: the phase is drawn only where it is news.
//
// AND THE ROUNDS ARE DRAWN ONLY WHEN THERE ARE ROUNDS. A check has none and says
// none, rather than saying "round 0 of 0" — the same law, one clause down.
func taskPhaseWords(phase string, round, rounds int) string {
	switch phase {
	case session.TaskPhaseChecking:
		return taskCheckingWord
	case session.TaskPhaseRepairing:
		if round > 0 && rounds > 0 {
			return taskClosingWord + railSep + "round " + itoa(round) + " of " + itoa(rounds)
		}
		return taskClosingWord
	}
	return ""
}

// taskPhaseLine is [taskPhaseWords] for a node this surface is holding.
//
// It is the second reader rather than the first because home reads the same
// words off a project row that carries the phase and not the rounds
// (session's [session.TaskIndexEntry]), and the two must not be two spellings.
func taskPhaseLine(node *taskNode) string {
	if node == nil {
		return ""
	}
	return taskPhaseWords(node.phase, node.phaseRound, node.phaseRounds)
}

// railPhase is the under-block a node wears while it is being checked or while a
// round is closing what the check found.
//
// IT OUTRANKS THE GAP LINE AND SAYS MORE THAN IT DID. [app.railMending] draws
// "finishing · <gap>" off the same repair round, from the one field that existed
// before the phase did; this says the same gap with the two facts that were
// missing — that a round is what is closing it, and which round of how many. A
// node that has a gap and no phase still gets the older row, which is every node
// running under a build whose engine does not send the phase.
//
// THE FINDING TAKES THE SECOND ROW, which spends the whole block
// ([railUnderRows]) and leaves no room for the telemetry. That is the trade made
// deliberately: the clock and the bill are true at every moment of a run and a
// person can read them a second later, while "the check did not accept this, and
// here is what it said" is the one thing on this surface that explains why work
// somebody thought was finished is being done again.
func (a *app) railPhase(node *taskNode, width int) []string {
	line := fit(taskPhaseLine(node), width)
	if line == "" {
		return nil
	}
	rows := []string{a.pal.dim(line)}
	if finding := fit(node.phaseFinding, width); finding != "" && len(rows) < railUnderRows {
		rows = append(rows, a.pal.dim(finding))
	}
	return rows
}

// taskPhaseMoved folds one phase move onto the node it is about.
//
// IT OPENS NOTHING. A phase is news about work a row already exists for — an
// update put it there and an update will land it — so a move naming a node this
// surface has never heard of is dropped rather than drawn as a task with no
// title, no state and no clock. That is the same rule the pilot lane is held to,
// and it is what keeps this event from being a second way to create a row.
//
// AND IT IS COPIED WHOLE, INCLUDING ITS ABSENCE. The engine sends `working` on
// the way out of a check and out of a repair round, and everything the check or
// the round put on this row goes with it: a finding left standing under a node
// that is back at work would be this column explaining a present that has passed.
func (a *app) taskPhaseMoved(ev session.Event) {
	move := ev.TaskPhase
	if move == nil {
		return
	}
	node := a.tasks[move.ID]
	if node == nil {
		return
	}
	node.phase, node.phaseRound, node.phaseRounds = move.Phase, move.Round, move.Rounds
	node.phaseFinding = strings.TrimSpace(move.Text)
}

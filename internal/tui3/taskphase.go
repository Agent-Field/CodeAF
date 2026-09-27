package tui3

import (
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE MINUTES WHEN THE WORKER IS NOT THE ONE WORKING ──────────────────────
//
// A task node has several lives and one state. Its worker writes the code, a
// check reads what the worker left, a repair round closes the gaps the check
// named, and a reading decides whether the work is handed out in parts — and the
// engine calls every one of those `running`, correctly, because nothing landed
// and nothing was undone between them (session's task_audit.go, task_divide.go).
//
// SO THE ROW WENT QUIET. The worker's last line scrolls past, the check spends
// four minutes reading a tree, a repair round spends another six rewriting it,
// and this surface drew for all of that exactly what it drew for a node between
// two calls: a clock. The person watching has no way to tell that from work that
// hung, and in the run this file exists for they concluded it had. The sizing
// reading is the same defect at the OTHER end of a node's life — thirteen
// measured seconds on a card that has only just appeared, before its worker has
// said one word.
//
// The engine now says which life it is in ([session.EventTaskPhase]) and this
// file is where the words are. THE WORDS ARE THE SURFACE'S, not the engine's:
// the wire carries `checking`, `repairing` and `sizing`, which are the same
// words the pulse file on disk carries, and what a person reads is written here,
// once, for every place that draws it.
//
// NONE OF THE MACHINERY IS IN THEM. There is a gate, and rounds, and a judgement
// — and a person watching their own work has no use for any of that. What they
// have a use for is that the work is being looked at, that it is being finished
// rather than abandoned, how far through that it is, and what was found. So:
// "checking what it left", "closing gaps · round 1 of 1", "sizing the work", and
// the finding under it in the checker's own sentence.
//
// AND WHEN A LIFE IS WAITING ON A REQUEST, THE REQUEST IS DRAWN. The sizing
// reading of 2026-09-11 thought for 219 seconds under a row that said the model's
// id and nothing else, which is the phase word's own defect one level down: the
// row said what the node was doing and not whether anything was happening. So
// the engine sends the request with the phase ([session.TaskPhaseNotice.Call])
// and this file draws it the way the rest of the surface draws a call: what it
// is doing in the provider's own words — `first word`, `thinking`, `writing` —
// how long it has been out, what has come back (↓, the token column's own
// figure) and the machine answering. Every piece is drawn only when it is known.

const (
	// taskCheckingWord is a node under the check that reads what its worker
	// left. It says what is being read rather than what is doing the reading:
	// the reader is machinery and the work is the person's.
	taskCheckingWord = "checking what it left"
	// taskClosingWord opens a repair round. "closing gaps" is what the round
	// does; the round numbers follow it, because how far through it is is the
	// second thing anybody watching a second minute of one wants.
	taskClosingWord = "closing gaps"
	// taskSizingWord is the reading that decides whether the work is handed out
	// in parts, and how (session's task_divide.go).
	//
	// IT SAYS THE WORK AND NOT THE DIVISION. "reviewing the division" is the
	// harness's own name for a call it is making; what is actually being settled
	// is how big this job is and how many hands it wants, and a person who has
	// just watched a task appear and do nothing wants that in the words they
	// would use. It carries no numbers, because there are none yet — how many
	// parts there are is what this reading is deciding, and the roster says so
	// afterwards when they exist.
	taskSizingWord = "sizing the work"
)

// taskPhaseLine is what a person reads for one of a node's lives, and "" for the
// ordinary one.
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
	case session.TaskPhaseSizing:
		return taskSizingWord
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
//
// AND A LIFE WAITING ON A REQUEST CARRIES IT ON THE PHASE'S OWN ROW, where the
// word is short and leaves the cells a figure needs: `sizing the work · thinking
// 41s · ↓ 4,465 · deepinfra`, shedding from the right as the column narrows and
// never shedding the word ([app.callFields] ranks the rest). The model's name
// stays on the second row inside the ladder's sentence, because which model was
// asked is the sentence's to say, and a figure beside a clipped id is two halves
// of nothing.
//
// THE CLOCK HERE IS THE NODE'S OWN AND THE ROOM'S IS THE SURFACE'S, and that is
// deliberate rather than an oversight. This row is drawn against [app.taskNow],
// which FREEZES while somebody is standing in the node's room — a number
// climbing in the corner of the screen is pressure applied to a person who has
// already gone to look — so while it is frozen the row says the phase and leaves
// the request's figures out rather than draw them stopped; the room's own row
// ([app.roomCallRow]) counts on [app.now], because inside the room the seconds
// this request has been out are exactly what they went there to see.
func (a *app) railPhase(node *taskNode, width int) []string {
	word := taskPhaseLine(node)
	line := fit(word, width)
	if line == "" {
		return nil
	}
	if call := node.phaseCall; call != nil && node.froze.IsZero() {
		fields := append([]rowField{rowSay(word)}, a.callFields(call, a.taskNow(node))...)
		if said := rowLed(fields, width); said != "" {
			line = said
		}
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
// the way out of a check, a repair round and a sizing reading, and everything
// the stage that is ending put on this row goes with it: a finding left standing
// under a node that is back at work would be this column explaining a present
// that has passed.
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
	node.phaseCall = move.Call
}

// callPhaseWords is internal/provider's account of where one request is, said in
// the words the phase clock already speaks for the conversation's own requests
// (phase.go's [phaseFields]). It is a table because it is a translation between
// two vocabularies the provider owns, and a switch here would be a third.
var callPhaseWords = map[provider.CallPhase]provider.Phase{
	provider.CallStarted:  provider.PhaseFirstWord,
	provider.CallPaced:    provider.PhasePaced,
	provider.CallThinking: provider.PhaseThinking,
	provider.CallWriting:  provider.PhaseWriting,
}

// callFields is one live request as the ranked facts a row is fitted with
// (rowfit.go): how long it has been out, led by what it is doing; what has come
// back; and the machine answering. The row it joins supplies its own lead — the
// phase word on the rail, the ladder's sentence in the room ([app.roomCallRow]).
//
// THE CLOCK IS THE REQUEST'S AGE, from the moment it went out, and it is spelled
// the way [phaseFields] spells a clock: in tenths while nothing has come back,
// because the difference between 1.2s and 3.1s is all those seconds say, and in
// whole seconds once the model is working. A narrow row keeps the figure and lets
// the word go.
//
// ↓ IS [session.TaskCall.Received], thought and answer together, because both
// are the model's writing and the bill counts both — the token column's own rule
// ([modelWrote]). The machine is spelled by [phaseServing], the one place a
// machine answering is spelled, so it reads the same here as on the status line.
//
// IT IS NOT [phaseFields], AND THE TWO DIFFER IN WHAT THEY ARE HANDED RATHER
// THAN IN WHAT THEY BELIEVE. That function draws the conversation's own turn off
// a [PhaseNews], which carries a phase's own start and, on a pacing wait, the
// router's `Retry-After` — so it can say `paced · retry in 6s`, a real moment
// this build will act at. A request reported through [provider.CallProgress]
// carries neither: one moment (when it went out) and no deadline at all. So this
// row says `paced 6s` — how long the park has lasted, which is the only true
// thing there is to say about it here — and a countdown invented from nothing
// would be the one thing phase.go's own header refuses. What it keeps that the
// status line's paced arm drops is the MACHINE, because a task row is otherwise
// silent: out here a person has the model segment beside the clock, and in a
// node's row the machine answering is news.
func (a *app) callFields(call *session.TaskCall, now time.Time) []rowField {
	phase := callPhaseWords[call.Phase]
	word := string(phase)
	clock := rowSay(word)
	if !call.Started.IsZero() {
		since := max(now.Sub(call.Started), 0)
		spelled := countUpWord(since)
		if phaseWaiting(phase) {
			spelled = tookWord(since)
		}
		if spelled != "" {
			clock = rowSay(strings.TrimSpace(word+" "+spelled), spelled)
		}
	}
	return []rowField{
		clock,
		rowSay(a.tokenDownWord(call.Received())),
		phaseServing(PhaseNews{Lane: call.Served}),
	}
}

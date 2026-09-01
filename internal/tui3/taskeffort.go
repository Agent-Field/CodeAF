package tui3

// ── ONE TASK'S PLACE ON THE EFFORT LADDER ───────────────────────────────────
//
// A task is a whole session of its own with its own workers, and the rung it
// runs at is its own fact (internal/session's [Agent.SetTaskEffort], which rides
// the checkpoint). This file is the surface's half: what `ctrl+v` is aimed at
// while a person is standing on a task, and the clause the room states the rung
// in. The scope rules for every other surface are in effortscope.go.

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/effort"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// taskEffortDoor is the fifth door onto a node, beside the four in room.go: the
// rung its workers think at, read and moved.
//
// IT IS ITS OWN INTERFACE for the reason [taskModelDoor] is: a capability is
// asserted, never required. An engine that streams nodes but has never heard of
// the ladder keeps its rooms, states no rung, names no key in the legend, and is
// in the honest degraded state rather than the broken one.
type taskEffortDoor interface {
	// TaskEffort is the rung set on one task, "" for a task nobody has set one
	// on and for a task this session does not have.
	TaskEffort(id uint64) string
	// SetTaskEffort moves it. It refuses a node whose run is over, in the
	// engine's own words.
	SetTaskEffort(id uint64, rung string) error
}

// taskEffortDoors is that door under this surface, when it has one.
func (a *app) taskEffortDoors() (taskEffortDoor, bool) {
	door, ok := a.agent.(taskEffortDoor)
	return door, ok
}

// taskEffortUnavailableWord is what a surface with no such door says, in the
// shape [taskModelUnavailableWord] says the same thing about the model.
const taskEffortUnavailableWord = "changing how hard a task thinks is unavailable — this session has no door onto it"

// effortTaskHere is the node `ctrl+v` is aimed at while a person is standing on
// a task, and false when they are not standing on one.
//
// THE ROSTER'S HOLD OUTRANKS THE ROOM, and that is the dispatcher's own order
// rather than a second opinion: [app.railKey] is read before [app.roomKey]
// (app.go), on the rule that explicit focus outranks ambient place — a room is
// where a person happens to be, the roster is what they just asked for and
// walked a cursor across. So a rung moved while the roster holds the keyboard
// moves the ROW UNDER THE CURSOR, which is the row wearing the ground, and never
// the page behind it. `x` resolves the other way round (stop.go's
// [app.stopHere]) because its own key is read one rung ABOVE the roster; each of
// the two agrees with the dispatcher it is reached through, which is the only
// way a person can predict either.
//
// A RUN'S PAGE IS NOT A TASK. An adaptive run is a fleet rather than one node —
// the same reason [app.roomModelMovable] refuses it — and there is nothing there
// for one rung to be about.
func (a *app) effortTaskHere() (*taskNode, bool) {
	if node := a.railFocusNode(); node != nil {
		return node, true
	}
	if a.room == nil || a.orchOpen() {
		return nil, false
	}
	node, ok := a.tasks[a.room.id]
	return node, ok && node != nil
}

// taskRung is the rung one node is set to, and [effort.None] for one nobody has
// set — which every surface below draws as nothing at all.
func (a *app) taskRung(id uint64) effort.Rung {
	door, ok := a.taskEffortDoors()
	if !ok {
		return effort.None
	}
	// A word this build cannot parse reads as absence, which is what every other
	// reader of a stored rung does with one (internal/effort's Parse).
	rung, _ := effort.Parse(door.TaskEffort(id))
	return rung
}

// cycleTaskEffort is `ctrl+v` on a task: one step up the wheel, on the node
// under the person's hand, and a line in the conversation saying so.
//
// THE NOTE IS THE RECORD OF A DECISION and is left where every other change to
// one task's machinery is written down (room.go's [app.retargetTask] says `task
// 7 · model · <id>` for the same reason and in the same shape). The rung and the
// id are the facts and the `·` labels are not, so the pair steps up and the
// scaffolding stays dim (payload.go).
//
// AND IT SAYS WHEN, WITHOUT CLAIMING MORE THAN IS TRUE. A worker already running
// keeps the rung it started on — the engine latches it with the model at the top
// of a turn — so a node that is RUNNING is told, in the note, that the next call
// is the one that takes it. A queued node is told nothing extra, because its
// first call has not happened yet and there is nothing to caveat.
func (a *app) cycleTaskEffort() bool {
	node, ok := a.effortTaskHere()
	if !ok {
		return false
	}
	door, open := a.taskEffortDoors()
	if !open {
		a.note(taskEffortUnavailableWord)
		return true
	}
	next := effortNext(a.taskRung(node.id))
	if err := door.SetTaskEffort(node.id, next.String()); err != nil {
		// THE ENGINE'S OWN SENTENCE IS KEPT on a refusal, the way a stop's and a
		// retarget's are: "task 7 is done, not running" is the answer, and a
		// surface that swallowed it would leave a person pressing the key again.
		a.note(err.Error())
		return true
	}
	word := taskIDWord(node.id) + " · " + strings.TrimSpace(effortClauseWord) + " · " + next.String()
	if node.state == session.TaskRunning && !node.stopped {
		word += " · " + taskEffortNextCallWord
	}
	a.noteFacts(word, taskIDWord(node.id), next.String())
	a.touch()
	return true
}

// taskEffortNextCallWord is that caveat, in the words the manual uses for it.
const taskEffortNextCallWord = "its next call takes it"

// taskEffortClause is the rung as the top bar and the roster's telemetry row
// state it, and "" for a node nobody has set one on.
//
// IT DOES NOT WEAR THE EMPHASIS THE CARDS ON HOME WEAR, and that is a property
// of the two lines rather than a second opinion about the rung. Both are built
// PLAIN and painted once, whole (topbar.go's [app.topBarWord] states the rule:
// a hue nested inside a hue ends at the inner one's reset and the rest of the
// line falls back mid-sentence), so there is no way to lift the rung out of them
// without lifting the clock and the cost with it. What this surface emphasizes a
// task decision with is the note — the same line a retarget leaves, with the
// rung painted as the datum in it (payload.go) — and the clause behind it is
// furniture from the moment it appears.
func (a *app) taskEffortClause(node *taskNode) string {
	if node == nil {
		return ""
	}
	return effortClause(a.taskRung(node.id))
}

// taskRungMovable reports that the node under the cursor is one the engine would
// take a rung for — which is exactly the set of moments naming the chord in a
// legend is honest (render.go's A HINT MAY ONLY NAME A KEY THAT WORKS).
//
// It is the engine's own gate read from outside: a running or queued node in
// this session's graph, with a door to ask. A settled node's rung is a fact
// about what happened and the engine refuses to edit it; a node belonging to an
// adaptive run is not in the graph this door reaches at all.
// railHoldHintWord is [railHoldHint] with the rung's chord named in it while the
// row under the cursor can take one, and [railHoldHint] itself otherwise.
//
// AND A ROW THAT IS ASKING TAKES THE WHOLE LINE. A node that landed `needs your
// look` is the surface standing still waiting for a person, and the three words
// that answer it were reachable only from inside the node's room — so the `!`
// summoned somebody to a column that told them nothing about what to press
// (tasksettle.go's [app.railSettleKey] is the other half). While the cursor is
// on such a row the slot says the answers and nothing else: the move keys are
// still there, they are still the keys a person already knows, and the one thing
// they do not know is the one thing this line is for.
//
// It is spelled from [roomSettleHint], so the roster, the card and the room
// cannot name three different letters for one question.
func (a *app) railHoldHintWord() string {
	if a.railSettleCard() != nil {
		return roomSettleHint + " · esc"
	}
	if !a.taskRungMovable(a.railFocusNode()) {
		return a.chords.say(railHoldHint)
	}
	return a.chords.say(railHoldKeys + " · " + effortKeyClause + " · esc")
}

func (a *app) taskRungMovable(node *taskNode) bool {
	if node == nil || node.run != "" {
		return false
	}
	if node.state != session.TaskRunning && node.state != session.TaskQueued {
		return false
	}
	_, ok := a.taskEffortDoors()
	return ok
}

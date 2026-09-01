package tui3

// ctrl+g — SEND THE RUNNING COMMAND TO THE BACKGROUND.
//
// Watching `go test ./...` grind through minute three used to leave a person
// two choices, and both of them threw the work away: keep watching until the
// harness's own timeout killed it, or press esc, which killed it sooner. There
// was no third answer, because there was nothing under the surface that could
// take a running process and keep it.
//
// Now there is (internal/session's promote.go), and this file is one keypress on
// top of it. The call becomes a job — same registry, same log file, same row in
// `jobs list`, same exit note into the conversation at the next step boundary —
// and the turn carries on with the tool result the engine handed back. NOTHING
// IS KILLED AND NOTHING IS RESTARTED, which is the whole distinction between
// this key and the one beside it.
//
// ── THE KEY, AND WHY THIS ONE ──
//
// ctrl+b is copy mode and esc is the interrupt, and neither is for sale. ctrl+g
// already closes and restores the task column; while a foreground command can
// be kept, this reading wins, and with none the column keeps the key. It is
// plain BEL so every terminal on every platform delivers it, and it needs no
// Option-as-Meta bargain the way alt+b would. Read it as "go on" — let the
// command keep running and get on with the turn.
//
// ── THE KEY IS ABSENT WHEN IT CANNOT WORK ──
//
// A capability that cannot work is absent, not broken. With no promotable call
// running the key does nothing at all and falls through to the ordinary "does
// this key carry text" — it is not a key that answers "there is nothing to
// background", because a key that only ever refuses is a key that taught
// somebody a gesture and then took it away.
//
// ── WHAT THE ROW SAYS AFTERWARDS ──
//
// One dim fragment in the row's stat slot, `job 3`, which is where consent's
// "allowed"/"denied" already lives: dim, trailing, about the call rather than
// in it. The row keeps being one row. The engine's own sentence — `still running
// as job 3; log at …` — is the tool RESULT, so it is in the expansion where
// every other result is, and the person who wants the path opens the call.

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

const backgroundKeepWord = "click to background"

// promoteAgent is the engine's promotion door, asserted rather than added to
// [Agent] for [stopAgent]'s reason exactly: a surface driven by a scripted agent
// that has never heard of background jobs must stay representable, and it simply
// has no key.
type promoteAgent interface {
	// PromoteCall sends a running foreground bash call to the background and
	// answers with the line naming the job it became. False means there is
	// nothing to promote — the call finished, or the turn was interrupted, or
	// the id names nothing.
	PromoteCall(callID string) (string, bool)
}

func (a *app) promoteDoor() (promoteAgent, bool) {
	if a.agent == nil {
		return nil, false
	}
	doors, ok := a.agent.(promoteAgent)
	return doors, ok
}

// promotableRow is the index in the live conversation of the call ctrl+g would
// send to the background, or -1 when there is none.
//
// It is the OLDEST running foreground bash call with an id, which is the one a
// person watching has been waiting on longest. A batch can hold several, and
// picking the newest would background the `cd` that started a moment ago and
// leave the build the person is actually staring at exactly where it was.
//
// A row with no callID cannot be promoted and is skipped rather than guessed at:
// the id is the whole handle, and a surface that promoted "some running bash"
// would be a surface that backgrounded a different command from the one under
// the cursor.
func (a *app) promotableRow() int {
	for i := range a.entries {
		if a.promotableEntry(i) {
			return i
		}
	}
	return -1
}

// promotableEntry is the row-local form of [app.promotableRow]: whether this
// exact entry can be kept now. The key wants the oldest such row; the pointer
// wants the one under the hand, and sharing this question keeps their absences
// identical.
func (a *app) promotableEntry(i int) bool {
	if a.state != stateWorking {
		return false
	}
	if _, ok := a.promoteDoor(); !ok {
		return false
	}
	if i < 0 || i >= len(a.entries) {
		return false
	}
	e := &a.entries[i]
	if e.kind != entryTool || e.tool != "bash" || e.status != toolRunning {
		return false
	}
	if e.callID == "" || e.bg != "" || !e.ended.IsZero() || e.ran > 0 {
		return false
	}
	// A BACKGROUND CALL HAS NOTHING TO PROMOTE, and the surface knows which is
	// which by the same read the countdown does: [app.toolLimit] is zero for a
	// call that asked for background:true, because that call was a job from the
	// first instant.
	return a.toolLimit(e) > 0
}

// backgroundRunning is ctrl+g. It reports whether it took the key, so the router
// can fall through when there was nothing to background.
func (a *app) backgroundRunning() bool {
	at := a.promotableRow()
	if at < 0 {
		return false
	}
	return a.backgroundEntry(at)
}

// backgroundEntry keeps one exact row. It is shared by the oldest-call key and
// the row-local pointer door so the engine call and the mark cannot drift.
func (a *app) backgroundEntry(at int) bool {
	if !a.promotableEntry(at) {
		return false
	}
	doors, ok := a.promoteDoor()
	if !ok {
		return false
	}
	line, promoted := doors.PromoteCall(a.entries[at].callID)
	if !promoted {
		// The call ended between the frame and the keypress. Nothing is wrong
		// and nothing is said: its result is already on its way.
		return false
	}
	a.markBackground(&a.entries[at], line)
	a.touch()
	return true
}

// keepPress is a click on the narrow clause inside one tool row. A stale
// target is still consumed: the call may have finished between drawing and the
// press, and opening its expansion would be a different gesture from the one
// the person made.
func (a *app) keepPress(x int, r row) bool {
	if !r.keep.holds(x) || !a.hoveringKeep(r.entry) && !a.hoveringEntry(r.entry) {
		return false
	}
	a.backgroundEntry(r.entry)
	return true
}

// markBackground writes the one row fact every promotion door produces.
func (a *app) markBackground(e *entry, line string) {
	if e == nil {
		return
	}
	e.bg = backgroundWord(line)
	e.stale = true
}

// learnBackground reads the engine's promotion result. It asks for the exact
// lead before parsing the job word, so ordinary command output that happens to
// mention a job cannot relabel its row.
func (a *app) learnBackground(e *entry, output string) bool {
	if e == nil || e.tool != "bash" || !strings.HasPrefix(output, session.BashPromotedLead) {
		return false
	}
	a.markBackground(e, output)
	return true
}

// backgroundWord is the fragment the row shows, taken from the engine's own
// sentence rather than composed here: `still running as job 3; log at …`
// becomes `job 3`. One source of truth for the id, and no second opinion about
// what a promoted call is called.
//
// A sentence this surface cannot read falls back to the one word it is sure of,
// because a row that has plainly changed and says nothing about it is worse than
// a row that says the least true thing there is to say.
func backgroundWord(line string) string {
	at := strings.Index(line, "job ")
	if at < 0 {
		return "backgrounded"
	}
	id := line[at+len("job "):]
	if end := strings.IndexAny(id, "; \t,"); end >= 0 {
		id = id[:end]
	}
	if strings.TrimSpace(id) == "" {
		return "backgrounded"
	}
	return "job " + id
}

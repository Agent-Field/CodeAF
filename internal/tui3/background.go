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
// was unbound (keys.md said so in as many words), it is plain BEL so every
// terminal on every platform delivers it, and it needs no Option-as-Meta
// bargain the way alt+b would. Read it as "go on" — let the command keep
// running and get on with the turn.
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

import "strings"

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
	if a.state != stateWorking {
		return -1
	}
	if _, ok := a.promoteDoor(); !ok {
		return -1
	}
	for i := range a.entries {
		e := &a.entries[i]
		if e.kind != entryTool || e.tool != "bash" || e.status != toolRunning {
			continue
		}
		if e.callID == "" || e.bg != "" || !e.ended.IsZero() || e.ran > 0 {
			continue
		}
		// A BACKGROUND CALL HAS NOTHING TO PROMOTE, and the surface knows which
		// is which by the same read the countdown does: [toolLimit] is zero for
		// a call that asked for background:true, because that call was a job
		// from the first instant.
		if toolLimit(e) <= 0 {
			continue
		}
		return i
	}
	return -1
}

// backgroundRunning is ctrl+g. It reports whether it took the key, so the router
// can fall through when there was nothing to background.
func (a *app) backgroundRunning() bool {
	at := a.promotableRow()
	if at < 0 {
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
	a.entries[at].bg = backgroundWord(line)
	a.entries[at].stale = true
	a.touch()
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

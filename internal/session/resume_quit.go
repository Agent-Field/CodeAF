package session

import "sync"

// ── a reply the app was closed in the middle of ─────────────────────────────
//
// The desktop app's quit does not end a reply: it stops it where it stands, and
// the chat reads "Interrupted · Continue" when the app opens again. NOTHING
// CARRIES ON BY ITSELF AFTER A RESTART, because a chat that resumed unasked
// spent money nobody was there to see (OWNER-DECISIONS 2026-11-17). The app
// keeps the list of what it stopped (cmd/codeaf's appquit.go) and opens such a
// chat with [Config.HoldInterruptedWork]; this is the engine's half: the held
// recovery, and the one turn the person's Continue carries on.
//
// IT IS A WAKE, NOT A MESSAGE. The words that start it are the session's own,
// written in its lane rather than as the person's ([userMessage.authored]),
// and the turn reaches every window the way any turn the conversation starts
// itself does, so a surface draws it as a turn with nothing typed in front of
// it.

// ResumeAfterQuitNote is what the model reads at the front of the turn that
// carries on after the app was closed in the middle of its reply.
const ResumeAfterQuitNote = "The app was closed while you were in the middle of answering the person's last message, and it is open again. Carry on from where you stopped: do not redo steps that already finished, and finish the answer."

// ResumeAfterCloseNote also warns that an abruptly ended tool may have left
// effects without a result. Replaying a legal transcript does not prove that
// the step finished, so the model must check before relying on it.
const ResumeAfterCloseNote = "The app closed unexpectedly while you were in the middle of answering the person's last message, and it is open again. Carry on from where you stopped without redoing steps that already finished. A step that was running when the app closed may not have finished: check its result before relying on it, then finish the answer."

// ResumeAfterClose starts the same private wake as ResumeAfterQuit, with the
// additional instruction needed when no shutdown could finish the last step.
func (a *Agent) ResumeAfterClose() bool {
	return a.ResumeAfterCloseKey("")
}

// ResumeAfterCloseKey correlates only the turn that takes this private note.
// A running turn is left alone: recovery must never credit older work.
func (a *Agent) ResumeAfterCloseKey(key string) bool {
	a.ContinueInterruptedWork()
	note := wakeNote(ResumeAfterCloseNote)
	note.closeKey = key
	note.batch = false
	return a.enqueueNote(note)
}

// ResumeAfterQuit starts the turn that carries on after the app was closed in
// the middle of a reply, and reports whether it was queued. A turn already
// running takes the words at its next step instead, as any wake does.
func (a *Agent) ResumeAfterQuit() bool {
	a.ContinueInterruptedWork()
	note := wakeNote(ResumeAfterQuitNote)
	note.batch = false
	return a.enqueueNote(note)
}

// consumeCloseNotesLocked runs only at the request drain, never at turn-end.
// Merely adopting a buffered turn or queueing behind a spending rail pays nothing.
func (a *Agent) consumeCloseNotesLocked(hub *eventHub, consumed []userMessage) {
	if hub == nil || !a.running {
		return
	}
	for _, note := range consumed {
		if note.closeKey != "" {
			hub.send(Event{Kind: EventCloseContinued, Text: note.closeKey})
		}
	}
}

// ── the recovery a held open keeps back ─────────────────────────────────────

// heldKind names one piece of recovery a held open kept back.
type heldKind int

const (
	// heldBelt is an interrupted task run [Agent.recoverBeltRun] did not resume.
	heldBelt heldKind = iota
	// heldFrontier is the task frontier [Agent.recoverTasks] did not turn.
	heldFrontier
)

// heldRecovery is what [Config.HoldInterruptedWork] kept back at construction,
// released exactly once by [Agent.ContinueInterruptedWork].
type heldRecovery struct {
	mu       sync.Mutex
	belt     bool
	frontier bool
	released bool
}

// holdRecovered reports whether this piece of recovery must wait for the
// person, and remembers that it did. After the release it never holds again,
// so the second pass through recovery does what the first would have done.
func (a *Agent) holdRecovered(kind heldKind) bool {
	if !a.config.HoldInterruptedWork {
		return false
	}
	h := &a.heldWork
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.released {
		return false
	}
	switch kind {
	case heldBelt:
		h.belt = true
	case heldFrontier:
		h.frontier = true
	}
	return true
}

// HeldInterruptedWork reports whether this conversation is holding work the
// last process left in the middle, waiting for the person's Continue.
func (a *Agent) HeldInterruptedWork() bool {
	h := &a.heldWork
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.released && (h.belt || h.frontier)
}

// ContinueInterruptedWork carries on what a held open kept back: the
// interrupted task run is recovered as an ordinary open would have recovered
// it, and the frontier is turned. It runs ONCE; a second call, or a call on a
// conversation that held nothing, reports false and does nothing. The reply the
// quit stopped is not part of it: that is [Agent.ResumeAfterQuit]'s turn.
func (a *Agent) ContinueInterruptedWork() bool {
	h := &a.heldWork
	h.mu.Lock()
	if h.released {
		h.mu.Unlock()
		return false
	}
	h.released = true
	belt, frontier := h.belt, h.frontier
	h.mu.Unlock()
	if frontier {
		if g := a.tasker(); g != nil {
			g.runFrontier()
		}
	}
	if belt {
		a.recoverBeltRun()
	}
	return belt || frontier
}

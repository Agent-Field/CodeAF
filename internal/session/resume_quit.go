package session

// ── a reply the app was closed in the middle of ─────────────────────────────
//
// The desktop app's quit does not end a reply: it stops it where it stands and,
// when the app opens again, the conversation carries on by itself. The app
// keeps the list of what it stopped (cmd/codeaf's appquit.go); this is the
// engine's half, the one turn that carries on.
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
	note := wakeNote(ResumeAfterCloseNote)
	note.batch = false
	return a.enqueueNote(note)
}

// ResumeAfterQuit starts the turn that carries on after the app was closed in
// the middle of a reply, and reports whether it was queued. A turn already
// running takes the words at its next step instead, as any wake does.
func (a *Agent) ResumeAfterQuit() bool {
	note := wakeNote(ResumeAfterQuitNote)
	note.batch = false
	return a.enqueueNote(note)
}

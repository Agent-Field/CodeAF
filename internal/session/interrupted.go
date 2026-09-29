package session

// interruptedNoteOpening is the first line of the note that tells the model which
// tool calls a crash cut off. It is its own note, like the team's, because it says
// something nothing else does and is said once: the transcript ends at the last
// finished call, so without it the model would read a call that never returned as
// one that did.
const interruptedNoteOpening = "A note from the session, not from the person: tool calls that were cut off before this conversation resumed. Facts, not requests."

// landInterruptedLocked puts the cut-off calls in front of the model and then
// counts them as dealt with: the person read the same calls when the
// conversation opened, so the record has nothing left to hold open. Callers hold
// a.mu.
func (a *Agent) landInterruptedLocked() {
	cut := a.config.Interrupted
	if cut == nil {
		return
	}
	note := cut.Note()
	if note == "" {
		return
	}
	a.landNoteLocked(interruptedNoteOpening, note)
	_ = cut.Close()
}

package tui3

import "time"

// ── A TEAM EDIT IS SAID DONE ONLY WHEN THE STORE TOOK IT ────────────────────
//
// Every team edit is made to what this window holds at once and written to the
// store a moment later, off the loop ([app.teamEdit], [app.teamsWrite]). The
// notices that say an edit happened (`Made beta · 1` on the wall, `Organized ·
// 1 new team` beside Organize, `harbor is closed` and a move's words on the
// teams page) used to be drawn at the keystroke, so a write the store refused
// (a lock another process held, a file that would not read, a far machine whose
// teams kept changing) still said it was done, and the refusal went into a note
// in the conversation behind the wall, where nobody was looking. THE NOTICE NOW
// WAITS FOR THE WRITE THAT CARRIED ITS EDIT, and a refusal takes the notice's
// place, in the warning ink, with the store's reason. The edit itself stays in
// this window, as it always did.

// teamWriteSaid ties one notice to the edit it reports: the edit's number
// ([teamsDisk.seq]), whether its write is still out, and why the store refused
// it, "" when it took it.
type teamWriteSaid struct {
	seq     int
	pending bool
	why     string
}

// teamWriteWatch is the notice tie for the edit [app.teamEdit] just queued, or,
// when err says the edit never reached the queue, that refusal at once.
func (a *app) teamWriteWatch(err error) teamWriteSaid {
	if err != nil {
		return teamWriteSaid{why: err.Error()}
	}
	return teamWriteSaid{seq: a.teamsDisk.seq, pending: true}
}

// said reports whether the notice may be drawn at all: its write is back.
func (s teamWriteSaid) said() bool { return !s.pending }

// settle takes one write's answer, and reports whether it answered this
// notice's edit.
func (s *teamWriteSaid) settle(w teamsWrote) bool {
	if !s.pending {
		return false
	}
	for _, seq := range w.seqs {
		if seq != s.seq {
			continue
		}
		s.pending = false
		if err := w.refused[seq]; err != nil {
			s.why = err.Error()
		}
		return true
	}
	return false
}

// teamsWriteSettled hands one write's answer to every notice waiting on it.
// A notice's clock starts when its write comes back, not when it was made, so
// a slow write over a connection still shows its notice for the whole time.
func (a *app) teamsWriteSettled(w teamsWrote) {
	now := a.now()
	if a.wall.madeSaid.settle(w) {
		a.wall.madeAt = now
	}
	if a.wall.org.said.settle(w) {
		a.wall.org.doneAt = now
	}
	if a.tp.undo.said.settle(w) {
		a.tp.undo.at = now
		a.tp.top = teamsTopCache{}
	}
	if a.tmove.undo.said.settle(w) {
		a.tmove.undo.at = now
		if why := a.tmove.undo.said.why; why != "" {
			a.tmove.undo.word = teamNotSaved("the move", why)
		}
		a.tp.top = teamsTopCache{}
	}
	a.touch()
}

// teamNotSaved is a refusal in the words that take its notice's place.
func teamNotSaved(name, why string) string {
	s := "not saved"
	if name != "" {
		s = name + " was not saved"
	}
	if why != "" {
		s += " · " + why
	}
	return s
}

// teamSaidWithin reports whether a settled notice made at at is still inside
// its time d at now.
func teamSaidWithin(at, now time.Time, d time.Duration) bool {
	return !at.IsZero() && now.Sub(at) >= 0 && now.Sub(at) < d
}

package tui

import "strings"

// Nothing typed here is ever thrown away by the machine. Three doors used to do
// exactly that: sending destroyed the text, escape wiped the draft with no way
// back, and three overlays reset the composer on their way open. Each of them
// keeps its words now — the ring below, the stash beside it, and the overlay
// slot — and the arrows are how they come back.
const inputHistoryLimit = 50

// rememberSubmission puts a sent turn in the ring. The ring is in-memory and
// session-local on purpose: it is the composer's undo, not a second transcript.
func (m *Model) rememberSubmission(body string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return
	}
	m.inputRecall = 0
	m.draftStash = ""
	if len(m.inputHistory) > 0 && m.inputHistory[len(m.inputHistory)-1] == body {
		return
	}
	m.inputHistory = append(m.inputHistory, body)
	if len(m.inputHistory) > inputHistoryLimit {
		m.inputHistory = append([]string(nil), m.inputHistory[len(m.inputHistory)-inputHistoryLimit:]...)
	}
}

// stashDraft is what escape does instead of destroying the draft: the words step
// aside where the up arrow reaches them, ahead of everything already sent.
func (m *Model) stashDraft(draft string) {
	if strings.TrimSpace(draft) == "" {
		return
	}
	m.draftStash = draft
	m.inputRecall = 0
}

// recallable is the walk the arrows take, oldest first: everything sent, then
// the draft escape stashed, which is the newest thing the composer held.
func (m *Model) recallable() []string {
	if m.draftStash == "" {
		return m.inputHistory
	}
	return append(append([]string(nil), m.inputHistory...), m.draftStash)
}

// recallInput walks that list. It reports whether it took the key: an empty
// list leaves the arrows to the thread, which is what they did before anything
// had been sent.
func (m *Model) recallInput(older bool) bool {
	entries := m.recallable()
	if len(entries) == 0 {
		return false
	}
	step := m.inputRecall
	if older {
		if step >= len(entries) {
			// The oldest line is the end of the walk, not a door back to
			// scrolling: the arrow stops here rather than moving the thread out
			// from under a draft that is on screen.
			return true
		}
		step++
	} else {
		if step == 0 {
			return false
		}
		step--
	}
	m.inputRecall = step
	if step == 0 {
		m.input.Reset()
	} else {
		m.input.SetValue(entries[len(entries)-step])
	}
	m.paletteDismissed = false
	m.syncPalette()
	m.setSize(m.width, m.height)
	return true
}

// recalling says a recall walk is under way, so the arrows keep belonging to it
// while the composer holds the line they put there.
func (m *Model) recalling() bool { return m.inputRecall > 0 }

// setDraft puts words back in the composer; SetValue leaves the caret at the
// end, which is where a person carries on typing.
func (m *Model) setDraft(draft string) {
	m.input.SetValue(draft)
	m.inputRecall = 0
	m.paletteDismissed = false
	m.syncPalette()
	m.setSize(m.width, m.height)
}

// borrowDraft is what an overlay does with the composer it is about to use for
// its own filter. A slash command is not a draft — it is the door being opened —
// so it is dropped rather than kept, and a stash already waiting is never
// overwritten by an overlay opening on an empty line.
func (m *Model) borrowDraft() {
	draft := m.input.Value()
	m.input.Reset()
	m.inputRecall = 0
	if strings.TrimSpace(draft) == "" || slashCommandDraft(draft) {
		return
	}
	m.overlayDraft = draft
}

// dropSlashCommand clears the command that just ran and leaves an ordinary
// draft where it is. It is for the doors that answer without opening anything:
// nothing borrowed the composer, so nothing may empty it either.
func (m *Model) dropSlashCommand() {
	if slashCommandDraft(m.input.Value()) {
		m.input.Reset()
		m.inputRecall = 0
	}
}

func slashCommandDraft(draft string) bool {
	return strings.HasPrefix(strings.TrimSpace(draft), "/")
}

// returnDraft gives it back when the overlay closes, unless something has been
// typed in the meantime — that is the person's newer draft and it wins.
func (m *Model) returnDraft() {
	draft := m.overlayDraft
	m.overlayDraft = ""
	if draft == "" || strings.TrimSpace(m.input.Value()) != "" {
		return
	}
	m.setDraft(draft)
}

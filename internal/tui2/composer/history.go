package composer

import "strings"

// The draft ring (7.2) and the esc stash (8.2.21), together. Nothing typed
// here is ever thrown away: a sent line goes in the ring, an esc against a
// non-empty draft goes in the stash, and ↑ from an empty draft walks the
// ring oldest-first with the stash as the newest entry — so the very next ↑
// after an esc is what brings the words back (see doc.go).

// historyLimit bounds the ring so a long session's composer does not grow
// its history forever. 50 matches internal/tui/draft.go's inputHistoryLimit
// — not reused code, the same reasonable number arrived at independently.
const historyLimit = 50

// remember puts a sent turn in the ring. Consecutive duplicate sends collapse
// to one entry, and remembering always clears the stash: a line that was
// just sent is not also a pending "restore this" — the recall ring already
// holds it.
func (m *Model) remember(sent string) {
	sent = strings.TrimSpace(sent)
	if sent == "" {
		return
	}
	m.historyStash = ""
	m.historyStep = 0
	if n := len(m.history); n > 0 && m.history[n-1] == sent {
		return
	}
	m.history = append(m.history, sent)
	if len(m.history) > historyLimit {
		m.history = append([]string(nil), m.history[len(m.history)-historyLimit:]...)
	}
}

// stash is the esc law's non-destructive half: a non-empty draft moves here
// instead of vanishing. It overwrites any previous stash on purpose — esc
// twice in a row without a send between them means the first stash was never
// restored, so it is superseded, not stacked; nothing is lost either way
// because the draft that produced it is also gone from the screen only once
// it is safely here.
func (m *Model) stash(draft string) {
	m.historyStash = draft
	m.historyStep = 0
}

// recallList is the walk order: everything sent, oldest first, then the
// stash if one is waiting — the newest thing the composer ever held.
func (m *Model) recallList() []string {
	if m.historyStash == "" {
		return m.history
	}
	return append(append([]string(nil), m.history...), m.historyStash)
}

// recallOlder is ↑ on an empty draft: it reports whether it took the key. An
// empty ring leaves ↑ to whatever else wants it (there is nothing to recall,
// so this is not the composer's keystroke to consume).
func (m *Model) recallOlder() bool {
	entries := m.recallList()
	if len(entries) == 0 {
		return false
	}
	if m.historyStep >= len(entries) {
		// Already at the oldest line: stop here rather than reporting
		// unhandled, so the key does not leak to whatever is behind the
		// composer (a transcript scroll, say) while a recalled draft is on
		// screen.
		return true
	}
	m.historyStep++
	m.setValue(entries[len(entries)-m.historyStep])
	return true
}

// recallNewer is ↓ on an empty draft, walking back toward the present. At
// step 0 (not recalling) it reports false, same reasoning as recallOlder.
func (m *Model) recallNewer() bool {
	if m.historyStep == 0 {
		return false
	}
	m.historyStep--
	if m.historyStep == 0 {
		m.reset()
		return true
	}
	entries := m.recallList()
	m.setValue(entries[len(entries)-m.historyStep])
	return true
}

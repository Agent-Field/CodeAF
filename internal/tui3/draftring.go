package tui3

import (
	"strings"
	"time"
)

// THE KILL RING: the last few sentences the box LOST, newest first.
//
// ↑ walks what you SENT (recall.go); until now nothing anywhere kept what you
// only TYPED. A ctrl+u that went one word too far, a conversation switched
// away from and closed before its half-written sentence ever came back — all
// of them were gone, and the only way back was to type the words again. So
// every whole-box clear goes through [app.noteKilled] BEFORE the words go, and
// the ring is where they land: the same ↑ walk visits them in front of the
// sent history, drawn dim because they were never sent, and /drafts lists
// them.
//
// TEN, because the ring's job is the last mistake, not a journal. A person
// reaching for it wants the sentence they lost a moment ago, and a hundred
// entries would be a second history with none of history's bookkeeping —
// the cap is what keeps the walk's first steps being the answer.

// draftRingSize is how many killed drafts the ring keeps.
const draftRingSize = 10

// draftEntry is one killed draft: the words as they stood in the box, and when
// they were lost — the moment is what /drafts reads an age from.
type draftEntry struct {
	text []rune
	at   time.Time
}

// draftRing is the kill ring itself: entries[0] is the newest loss.
type draftRing struct {
	entries []draftEntry
}

// push records a killed draft.
//
// AN EMPTY OR WHITESPACE-ONLY BOX RECORDS NOTHING, on the draft file's own
// law (draft.go): there are no words to come back to, and an entry that drew
// nothing would be the emptiness law broken from inside.
//
// A REPEAT OF THE NEWEST ENTRY ONLY REFRESHES ITS MOMENT. The words are copied
// because the box rewrites its own rune slice in place, and the dedupe is
// because a switch away and back stashes and restores the same sentence: each
// pass would otherwise be one more identical entry, and three switches would
// fill the walk with copies of one sentence.
func (r *draftRing) push(t []rune) {
	if strings.TrimSpace(string(t)) == "" {
		return
	}
	if len(r.entries) > 0 && string(r.entries[0].text) == string(t) {
		r.entries[0].at = time.Now()
		return
	}
	r.entries = append([]draftEntry{{text: append([]rune(nil), t...), at: time.Now()}}, r.entries...)
	if len(r.entries) > draftRingSize {
		r.entries = r.entries[:draftRingSize]
	}
}

// drop removes the entry at a newest-first position — the walk's own index, so
// a consumed draft comes out by the same number it was browsed by.
func (r *draftRing) drop(i int) {
	if i < 0 || i >= len(r.entries) {
		return
	}
	r.entries = append(r.entries[:i], r.entries[i+1:]...)
}

// list is the ring as a walk or a page reads it, newest first.
func (r *draftRing) list() []draftEntry {
	return append([]draftEntry(nil), r.entries...)
}

// consume forgets the NEWEST entry with these words and reports whether there
// was one. It is how a sent draft leaves the ring: the sentence lives in the
// history now, and a walk that still dimmed it would be calling a sent
// sentence unsent. The match is on the trimmed words, because enter reads the
// box trimmed and the ring keeps it exactly as it stood.
func (r *draftRing) consume(text string) bool {
	text = strings.TrimSpace(text)
	for i, entry := range r.entries {
		if strings.TrimSpace(string(entry.text)) == text {
			r.drop(i)
			return true
		}
	}
	return false
}

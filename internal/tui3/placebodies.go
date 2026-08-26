package tui3

// The two overlays the router promoted to places, and what promotion costs.
//
// The standing list and the memory list were ≤12-row overlays drawn UNDER the
// draft, inside the conversation's chrome. As places they take the frame whole,
// which buys them the pulse, the tab bar, the composer and one key grammar — and
// costs them the two things being modal had bought:
//
//   - THEIR BARE LETTERS. `p` `s` `n` on standing and `u` `e` on memory were
//     bare because "no draft is under this list for a letter to fall through
//     into"; a place has a composer, so every letter belongs to it and the verbs
//     moved onto the `→` strip (verbstrip.go). Memory gains a real fix from
//     that: `u` was matched ahead of the filter, so the letter could not be
//     TYPED, and a search for a word containing a `u` lost it.
//   - `tab`. It cycled memory's scope; it is the way to the next place now, and
//     the scope moved to `alt+s`, which is the class a view belongs to.
//
// Both keep their bodies exactly as they drew them. What changed is the frame
// around them and the keyboard, which is the whole of what this wave claimed.
//
// WHAT IS LEFT IN THIS FILE IS THE SHARED FOOT AND NOTHING ELSE. A place's own
// frame lives in that place's own file — `place_memory.go`, `place_spend.go`,
// `place_search.go` — because a router file that knew how one place draws is
// the shape ARCHITECTURE.md exists to retire.

// placeHeadRows is how many rows every place spends before its body: the pulse,
// the tab bar, the rule, and the blank under it (pages.go's [placeFrame]).
//
// IT IS A CONSTANT AND THE POINTER DEPENDS ON IT. A press arrives as a row of
// the terminal and has to become a row of the body, and the only honest way to
// subtract the head is to have exactly one number for how tall the head is —
// which is also why the tab bar took the blank row home used to draw rather than
// being added under it. A fifth head row is a change to this constant and to
// nothing else.
const placeHeadRows = 4

// placeNote is the one line a place says about what it is holding, drawn under
// the rule and above the composer (pages.go's [placeFrame] states the law).
//
// It is where the counts, the filter lines and an open editor's label go — the
// facts that are about the WHOLE body rather than about the row under the
// cursor, and that would be a lie if they scrolled with it.
func (a *app) placeNote(width int) []string {
	pl := a.showing()
	if pl == nil {
		return nil
	}
	return pl.note(a, width)
}

// ── a place with nothing of its own to draw ─────────────────────────────────

// teachMeasure is how wide a paragraph of this surface's own prose may run. It
// is the same measure the welcome box and the refusal blocks read at — a line
// long enough to hold a whole clause and short enough that the eye finds the
// next one without hunting.
const teachMeasure = 76

// placeTeachProse wraps one paragraph of a place's own explanation to the
// reading measure and dims it.
//
// THE PROSE IS NARROWER THAN THE FRAME. A sentence run out to two hundred
// columns is a sentence nobody's eye can return from, so the paragraph is held
// to a reading measure and the rest of the width is left as air. It takes the
// reading ladder's DIM tier — it is the surface talking about itself, which is
// the whole of what dim means here.
func placeTeachProse(text string, width int, pal palette) []string {
	measure := width - 2
	if measure > teachMeasure {
		measure = teachMeasure
	}
	lines := wrap(text, measure)
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, pal.dim(line))
	}
	return out
}

// placeTeachRows puts a place's teaching prose in the body's own column and pads
// it out to the room the frame reserved. The prose hangs from the top the way
// every list on this surface does, and answers the pointer with nothing.
func placeTeachRows(lines []string, room int) []placeRow {
	rows := make([]placeRow, 0, room)
	for _, line := range lines {
		if len(rows) >= room {
			break
		}
		rows = append(rows, placeRow{text: " " + line, hit: -1})
	}
	for len(rows) < room {
		rows = append(rows, placeRow{text: "", hit: -1})
	}
	return rows
}

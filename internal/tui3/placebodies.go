package tui3

import "strings"

// ── THE SHARED FOOT, AND THE HEAD THE POINTER COUNTS FROM ───────────────────
//
// What the frame draws for EVERY place and no place draws for itself: how tall
// the head is, the one line a place may say about what it is holding, and the
// prose a place with nothing in it teaches instead of a body.
//
// The frame itself is pages.go's ([placeFrame]) and a place's own rows are that
// place's file's — this is the part that is the same on all seven, and a router
// file that knew how one place draws is the shape ARCHITECTURE.md exists to
// retire.
//
// ── WHAT PROMOTION COST THE TWO OVERLAYS ────────────────────────────────────
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
// around them and the keyboard, which is the whole of what that wave claimed.

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
	rows := pl.note(a, width)
	// AND THE ONE LINE THE FRAME ITSELF MAY ADD, under the place's own: a Mac
	// whose Option key is composing accents instead of sending meta, caught by the
	// character that arrived where a chord was aimed (chords.go). It goes in the
	// note slot because it is a fact about the whole frame rather than about the
	// row under the cursor, and it is the frame's rather than any one place's
	// because every place binds the same chords.
	if line := a.chordNote(width); line != "" {
		rows = append(rows, line)
	}
	return rows
}

// placeTray is the attachment tray drawn over a place's box, and it is home's
// alone: home is the one place whose box starts a conversation, so it is the one
// place a dropped picture can be waiting on (imagepaste.go's one door).
//
// IT IS THE CHIPS AND NOTHING ELSE, where the conversation's own row also
// carries the picked harness and the thinking dial ([app.chipStrip]). Both of
// those are things the CHAT composer does to the next turn, and neither is a
// thing home's box can do — and the dial in particular records the columns a
// press is resolved against, which on a screen that hit-tests against its own
// two maps (homemouse.go) would be a click answering for a row this frame never
// drew. So the tray here says what is being carried and offers no target.
//
// THE ERRAND'S TRAY IS DRAWN WHERE THE ERRAND'S BOX IS, which is this same row:
// [placeHome.box] hands the composer the pane's line while the pane holds the
// keyboard, and the cargo above it has to be that line's cargo
// (homeexchange.go).
func (a *app) placeTray(width int) []string {
	if !a.at(pageHome) || a.home.phone {
		return nil
	}
	chips := a.chips
	if ex := a.paneExchange(); ex != nil && ex.focused {
		chips = ex.chips
	}
	if len(chips) == 0 {
		return nil
	}
	painted := make([]string, 0, len(chips))
	for _, label := range chipLabels(chips, a.pal) {
		painted = append(painted, a.pal.dim(label))
	}
	return []string{" " + fit(strings.Join(painted, chipGap), width-2)}
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

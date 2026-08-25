package tui3

import (
	tea "charm.land/bubbletea/v2"
)

// teachPlace is a place that has nothing of its own to draw yet, drawing what
// it is FOR instead.
//
// AN ALMOST-EMPTY PAGE IS THE BEST TEACHER ON THE MACHINE (SCREEN 1f). Nobody
// arrives at spend or search by accident — you walk into them from the tab bar,
// from `alt+5`, or by typing the word — and that arrival is the one moment a
// person is asking "what is this". So the place answers, in three sentences of
// dim prose in the body's own column, and says nothing else at all.
//
// IT IS NOT A PLACEHOLDER AND IT MUST NOT PRETEND TO BE ONE. There is no "coming
// soon", no greyed-out list, no empty table with headings over it — a capability
// that cannot work is absent rather than broken (CLAUDE.md), and a page that
// draws the furniture of a feature it does not have is the worst version of
// broken, because it looks like a bug rather than like a plan. The prose is
// true today: it describes what the place is for, and the wave that fills it
// deletes this body and keeps the tab.
type teachPlace struct {
	open bool
	// at is which place this body is standing in for, so one struct serves both
	// and a third costs a line in [page.explain] and nothing here.
	at page
}

func (t *teachPlace) close() { *t = teachPlace{} }

// teachFrame draws a place whose body is its own explanation.
//
// The prose sits in the body's column with one blank row under the head, hangs
// from the top the way every list on this surface does, and takes the reading
// ladder's DIM tier — it is the surface talking about itself, which is the whole
// of what dim means here.
func (a *app) teachFrame(width, height int) ([]string, []int, int, int) {
	// THE PLACES THAT HAVE GROWN BODIES DRAW THEM, AND FALL BACK HERE WHEN THEY
	// HAVE NOTHING TO SAY. Search always has something — its own three sentences
	// while the box is empty (searchplace.go) — and spend has a body exactly when
	// the ledger has a priced line inside the window it is showing. A machine
	// that has spent nothing therefore still meets the teaching, which is the one
	// arrival this whole file was written for.
	switch a.teach.at {
	case pageSearch:
		return a.searchFrame(width, height)
	case pageSpend:
		// It asks the TOTAL rather than drawing the body to see whether it is
		// empty, because drawing it twice a frame to answer one question is the
		// kind of waste a still page does not notice until it is on a clock.
		if a.spend.reading.totals.USD > 0 {
			return a.spendFrame(width, height)
		}
	}
	lines, hits, caretX, caretY := placeFrame(a, width, height, -1,
		func(width, room int) []placeRow[int] {
			rows := make([]placeRow[int], 0, room)
			// THE PROSE IS NARROWER THAN THE FRAME. A sentence run out to two
			// hundred columns is a sentence nobody's eye can return from, so the
			// paragraph is held to a reading measure and the rest of the width is
			// left as air.
			measure := width - 2
			if measure > teachMeasure {
				measure = teachMeasure
			}
			for _, line := range wrap(a.teach.at.explain(), measure) {
				if len(rows) >= room {
					break
				}
				rows = append(rows, placeRow[int]{text: " " + a.pal.dim(line), hit: -1})
			}
			for len(rows) < room {
				rows = append(rows, placeRow[int]{text: "", hit: -1})
			}
			return rows
		})
	return lines, hits, caretX, caretY
}

// teachMeasure is how wide a paragraph of this surface's own prose may run. It
// is the same measure the welcome box and the refusal blocks read at — a line
// long enough to hold a whole clause and short enough that the eye finds the
// next one without hunting.
const teachMeasure = 76

// teachKey is every key on a place whose only body is its own explanation.
//
// It is almost entirely the router's ([app.placeKey]), which is the point: the
// grammar is the same here as everywhere, and what a place has not built yet
// changes what is on the screen rather than what the keyboard means. `esc` is
// the way back to the conversation, and every printable key goes to the
// composer — so a person can arrive at spend, read what it is for, and send off
// the task they came to ask about without leaving.
func (a *app) teachKey(msg tea.KeyPressMsg) tea.Cmd {
	// EACH PLACE'S OWN HANDLER IS READ FIRST, and each of them opens with the
	// router exactly as this one does — so the grammar is one grammar however
	// many of these places grow bodies.
	switch a.teach.at {
	case pageSpend:
		return a.spendKey(msg)
	case pageSearch:
		return a.searchKey(msg)
	}
	if cmd, took := a.placeKey(msg); took {
		return cmd
	}
	switch msg.String() {
	case "esc":
		// ONE LAYER AT A TIME, the rule every place on this surface follows: a
		// box with something in it is cleared first, and the second esc leaves.
		if box := a.placeBox(); box != nil && !box.empty() {
			box.reset()
			a.touch()
			return nil
		}
		a.teach.close()
		a.touch()
		return nil
	case "enter":
		// THE COMPOSER'S OWN ROAD. There is no list here to open, so enter is
		// what the hint line says it is: talk about it, in a conversation.
		return a.placeTalk()
	}
	if box := a.placeBox(); box != nil {
		listNavigate(msg, box, func(int) {}, func() {}, teachMeasure)
		a.touch()
	}
	return nil
}

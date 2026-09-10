package tui3

// ── A QUESTION HOME ASKS ABOUT A ROW ON ITS OWN LIST ─────────────────────────
//
// Home takes the frame WHOLE (view.go's [app.placeFrameNow]), so the question
// block pinned above the message box is not on the screen at all while somebody
// is standing here. Up to now that meant home could not ASK anything: the two
// doors on this screen that end something — moving a conversation out of another
// window, and answering a standing card — each grew their own way of asking, and
// each of them was a different set of keys for the same act.
//
// This is the seam that ends that. Home holds ONE question at a time, draws it
// with the block's own renderer, and routes its keys through the block's own
// router. Nothing about the grammar is re-decided here.
//
// ── THE ROWS, EXACTLY AS THEY ARE DRAWN ─────────────────────────────────────
//
// Inside the card beside the list, where the state band already says what the
// other window is doing:
//
//	?  Move this conversation here?
//	     its reply stops there; its tasks come here
//	     1  move it here
//	   ▸ 2  leave it there
//	   [esc] leave it there · [←→] pick
//
// The cursor starts on `leave it there`, which is stop.go's law and the reason
// this seam was worth building: `enter` is the key people press to make a
// question go away, so the answer under it has to be the one that loses nothing.
//
// And on a frame too narrow for a card at all (below [homeCardMin]) the same
// question is home's foot line, in one row, cut by the same fitter every other
// long sentence on this screen is cut by ([app.homeAskFoot]):
//
//	Move this conversation here? · 1 move it here · 2 leave it there · its reply stops there; its tasks come here · esc leave it there
//
// ── THE FOUR THINGS IT PROMISES ─────────────────────────────────────────────
//
//   - ONE AT A TIME. Home is a list of rows and a card about the row under the
//     cursor; a second question would be a card about two things. Raising one
//     drops whatever was there.
//   - IT IS NOT ON THE BLOCK. [app.questions] is the conversation's queue, drawn
//     above the message box, and a card home raised about a row on home is not a
//     thing to meet after walking away from home. Answering it or walking off
//     the row takes it down.
//   - IT NEVER TAKES A KEY IT HAS NOT DRAWN. The band is only drawn for the row
//     the cursor is on, so the card is taken down the moment the cursor moves —
//     which is the same guard the block spends its `shown` stamp on.
//   - AND IT ANSWERS THROUGH THE CLOSURE, never through a door. Everything home
//     asks about is something THIS PROGRAM does ([questionShown.local]), so
//     there is no engine to tell and no receipt to write: the act is the receipt
//     (question.go's [app.recordQuestion] says the same about every card a
//     person raises themselves).

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// raiseHomeAsk puts one question on home's card, replacing whatever was there.
func (a *app) raiseHomeAsk(q questionShown) {
	if q.question.Asked.IsZero() {
		q.question.Asked = a.now()
	}
	// THE STAMP IS WRITTEN AT THE RAISE AND NOT AT THE DRAW, which is the one
	// place this differs from the block. The block's stamp exists to catch a
	// keystroke aimed at whatever was on screen a quarter of a second ago; home's
	// card is raised BY a keystroke somebody just made, on the row they are
	// looking at, so there is no earlier screen for the next key to have been
	// meant for ([app.questionSettled] reads it either way).
	q.shown = a.now().Add(-questionSettle - 1)
	a.home.ask = &q
	a.touch()
}

// dropHomeAsk takes it down without answering it, which is what walking off the
// row means.
func (a *app) dropHomeAsk() {
	if a.home.ask == nil {
		return
	}
	a.home.ask = nil
	a.touch()
}

// homeAsking is the question home is holding, when it is holding one.
func (a *app) homeAsking() (questionShown, bool) {
	if a.home.ask == nil {
		return questionShown{}, false
	}
	return *a.home.ask, true
}

// homeAskRows is the card, drawn by the block's own renderer.
//
// IT IS [app.questionCardRows] AND NOT A SECOND DRAWING OF THE SAME FACTS. The
// mark, the head, the reason under it, the answers in a column with what each
// one costs, the cursor, and the offer row are all the block's — so a person who
// has learnt one card on this surface has learnt this one.
func (a *app) homeAskRows(width int) []string {
	ask, ok := a.homeAsking()
	if !ok || width < 1 {
		return nil
	}
	return a.questionCardRows(ask, width)
}

// homeAskFoot is the whole question on ONE ROW, for the frames that have no card
// to put it on.
//
// BELOW [homeCardMin] THERE IS NO CARD AT ALL (homebridge.go's ladder), and that
// is where every long sentence home has ever had to say has been said: the foot.
// So the question says itself there, built out of its own words — the head, what
// answering costs, then the answers and the way out through the block's own
// [app.questionHintOn] — and home's fitter drops whole clauses off the end of it
// (places.go states that law, and narrow_test.go pins it).
//
// IT IS NOT SAID BESIDE A CARD. The card is the question; a foot repeating it
// would be one decision drawn twice on one screen, which is the defect the whole
// block exists to end. The caller checks the tier.
func (a *app) homeAskFoot() string {
	ask, ok := a.homeAsking()
	if !ok {
		return ""
	}
	// THE CLAUSES ARE IN RANK ORDER AND THE WAY OUT IS LAST, which is what home's
	// fitter reads a sentence as ([hintFit]): it protects the final clause and
	// then drops the one BESIDE it, working backwards. So the least valuable
	// thing on the line has to sit next to `esc` and the most valuable at the
	// front — the question, its answers, then what answering costs. A narrow
	// terminal is left with the question and a key, which is the least a question
	// can be and still be one.
	parts := []string{strings.TrimSpace(ask.question.Head)}
	way := ""
	if hint := a.questionHintOn(ask); hint != "" {
		if at := strings.LastIndex(hint, railSep); at >= 0 {
			parts = append(parts, hint[:at])
			way = hint[at+len(railSep):]
		} else {
			way = hint
		}
	}
	if reason := strings.TrimSpace(ask.question.Reason); reason != "" {
		parts = append(parts, reason)
	}
	if way != "" {
		parts = append(parts, way)
	}
	return strings.Join(parts, railSep)
}

// sayHomeAsk puts that sentence on the foot, and does nothing on a frame that
// has a card to draw the question properly on.
func (a *app) sayHomeAsk() {
	if a.homeTierNow() != homeTierList {
		return
	}
	if word := a.homeAskFoot(); word != "" {
		a.home.say(word, "")
	}
}

// homeAskKey routes one keypress at that card and reports whether it took it.
//
// IT ROUTES THROUGH THE BLOCK'S OWN ROUTER ([app.questionKeyOn]), which is the
// whole reason this seam exists: `1` and `2` move the cursor rather than
// answering, `←→` walk it, `enter` takes what it is on, and `esc` is the answer
// that loses nothing. Home decides none of that.
func (a *app) homeAskKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	ask, ok := a.homeAsking()
	if !ok {
		return nil, false
	}
	return a.questionKeyOn(ask, msg)
}

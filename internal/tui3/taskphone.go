package tui3

// TASKS, REACHED BY A THUMB.
//
// The roster and the record were built for a keyboard and they show it at
// [tierPhone]: the page under `ctrl+t` names its verbs in a dim sentence at the
// foot, and the card a row opens does the same. Neither is a thing a finger can
// do.
//
// A third surface used to be on this list — the task strip's own phone form, one
// full-width `▸ 4 tasks · 2 running` door in place of a row of chips a thumb
// cannot land between. The strip is gone (ISSUE-126) and that door went with it:
// the deck's rows carry the live set at this width now (statusdeck.go), and the
// page is `ctrl+t` and the rail.
//
// Nothing new is invented here — the three doors already exist and every one of
// them is reachable with a key. What changes is their SHAPE on a phone:
//
//   - THE STRIP IS ONE DOOR, AND IT LOOKS LIKE ONE. A row of chips three cells
//     apart is a keyboard's idea of a tab bar: a thumb cannot land between two
//     of them, and the only door there — a press on the empty half — was
//     invisible. "Not accessible after some tabs" is a person who could not find
//     it. So at this tier the strip is a SINGLE full-width row that says what is
//     there and that it opens — `▸ 3 tasks · 1 running` — the fold glyph the
//     rest of the phone UI folds with ([glyphShut]), the count the chips would
//     have carried, and the most urgent state among them. The whole row is one
//     tap target, and it opens the roster PAGE ([app.showTaskPlace]) — the
//     scrollable list of task cards this file also shapes, not the overlay
//     column a keyboard drives.
//
//   - THE ROSTER PAGE IS A LIST OF CARDS WITH A WAY BACK. Its rows already open
//     on one press ([app.taskSheetPress]); at this tier each is a two-line CARD
//     a thumb goes into (the name and its state on top, what it did and how long
//     ago under it — the reading draws it, tasksplace.go), the list SCROLLS to
//     keep the cursor's card whole ([tasksTop] walks it in lines rather than in
//     rows), and its foot is a `‹ back` bar ([phoneBar]) instead of a key legend
//     — so a person leaves by tapping, no keyboard anywhere in the flow.
//
//   - THE CARD'S VERBS BECOME BANDS. `esc back · ↑↓ scroll · m puts it in your
//     message` is a sentence about keys; at this tier the two things it names
//     that a finger can do — going back, and putting the task in your message —
//     are the bar's targets instead, in home's own bar shape ([phoneBar]).
//
// So the whole flow is a thumb's: conversation → tap the `▸ tasks` door → the
// scrollable list of task cards → tap a card → the task record with its
// `‹ back` → back to the list → `‹ back` to the conversation. Two backs, both
// bands, mouse motion ignored on the glass the way home ignores it.

// taskPhoneMentionWord is the card's second target, and it is the same words the
// key line has always used for the same act — one gesture, one spelling.
const taskPhoneMentionWord = "m puts it in your message"

// taskCardPhone reports whether the record card is being drawn at [tierPhone].
func taskCardPhone(width int) bool { return layoutTier(width) == tierPhone }

// taskCardBar is the card's foot at this tier: the way back, and the mention.
//
// TWO TARGETS AND NOT THREE. The bar carries what a thumb can do and the card
// has exactly two of those — the scroll is the screen itself, and a target
// saying `↑↓ scroll` would be a target that does nothing when it is pressed.
func (a *app) taskCardBar(width int) (string, []hudSpan) {
	back := homeSheetBackWord
	if a.pal.ascii {
		back = homeSheetBackASCII
	}
	return phoneBar(width, []string{back, taskPhoneMentionWord}, a.pal)
}

// taskCardBarPress resolves a press on that bar and reports whether it took it.
// It is asked only on the foot row, which the frame reports along with the rows.
func (a *app) taskCardBarPress(x int) bool {
	width, _ := a.size()
	_, spans := a.taskCardBar(width)
	for i, span := range spans {
		if !span.holds(x) {
			continue
		}
		if i == 0 {
			a.closeTaskRecord()
			return true
		}
		// The mention is the card's own key, taken through the same path so the
		// two gestures can never mean two things ([app.taskCardKey]).
		a.taskCardKey("m")
		return true
	}
	return false
}

// ── THE ROSTER PAGE, AS CARDS ───────────────────────────────────────────────

// taskSheetPhoneIndent is where a card's second line hangs: two cells in from
// the label, so the tail reads as belonging under the name rather than as a row
// of its own. It is measured from the row's content, which the place's own
// two-cell lead ([tasksBareLead]) already sits in front of.
const taskSheetPhoneIndent = 2

// taskSheetBar is the roster page's foot at [tierPhone]: a `‹ back` band a thumb
// leaves by, in place of the key legend a keyboard reads ([tasksPlace.hint]).
// It is the record card's own bar shape ([phoneBar]) — one target here, because
// filtering the page is done by typing and there is no toggle to give a band to.
func (a *app) taskSheetBar(width int) (string, []hudSpan) {
	back := homeSheetBackWord
	if a.pal.ascii {
		back = homeSheetBackASCII
	}
	return phoneBar(width, []string{back}, a.pal)
}

// taskSheetBarPress resolves a press on that bar and reports whether it took it.
// The one target closes the page, which drops a person back to the conversation.
func (a *app) taskSheetBarPress(x int) bool {
	width, _ := a.size()
	_, spans := a.taskSheetBar(width)
	for i, span := range spans {
		if !span.holds(x) {
			continue
		}
		if i == 0 {
			a.closeTaskSheet()
		}
		return true
	}
	return false
}

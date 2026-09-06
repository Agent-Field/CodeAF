package tui3

import (
	tea "charm.land/bubbletea/v2"
)

// TASKS, REACHED BY A THUMB.
//
// The roster and the record were built for a keyboard and they show it at
// [tierPhone]: the strip is a row of chips three cells apart, the page under
// `ctrl+t` names its verbs in a dim sentence at the foot, and the card a row
// opens does the same. None of those is a thing a finger can do.
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
// AND OVER ANOTHER WINDOW'S WORK IT CARRIES ONE. There is nothing landed for a
// mention to point at, so the chip is absent rather than drawn dead — the same
// answer the wide foot gives ([taskAwayCardKeys] says why), on the tier where a
// dead chip is worst: a thumb has no hover to discover with and finds out by
// pressing.
func (a *app) taskCardBar(width int) (string, []hudSpan) {
	back := homeSheetBackWord
	if a.pal.ascii {
		back = homeSheetBackASCII
	}
	if a.taskSheet.awayOwner.on {
		return phoneBar(width, []string{back}, a.pal)
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

// ── THE PHONE STRIP IS ONE DOOR ─────────────────────────────────────────────

// The count word on the door, singular and plural. One task and three tasks
// reach the roster the same way at this tier, so the door is drawn for either —
// `▸ 1 task · …` is still a door.
const (
	stripDoorTaskWord  = "task"
	stripDoorTasksWord = "tasks"
)

// stripPhoneDoor is the strip at [tierPhone]: not a row of chips but a SINGLE
// full-width door into the roster. It answers the door's text and whether this
// tier draws one at all.
//
// IT REUSES THE STRIP'S OWN NUMBERS AND INVENTS NO STATE. The live set is
// [app.stripNodes] — the same running, needs-you and idle nodes the chips would
// be — so "N tasks" is how many chips there would have been and the tail is the
// most urgent of them in the roster's own word. A frame whose only live thing is
// a running sub-harness has no task nodes, so this draws nothing and the chip
// keeps the row: there is a name there a person must still be able to reach.
func (a *app) stripPhoneDoor(width int) (string, bool) {
	if layoutTier(width) != tierPhone {
		return "", false
	}
	nodes := a.stripNodes()
	if len(nodes) == 0 {
		return "", false
	}
	glyph := glyphShut
	if a.pal.ascii {
		glyph = glyphShutASCII
	}
	word := stripDoorTasksWord
	if len(nodes) == 1 {
		word = stripDoorTaskWord
	}
	line := a.pal.ink(glyph + " " + itoa(len(nodes)) + " " + word)
	if state := a.stripPhoneState(); state != "" {
		line += a.pal.dim(railSep + state)
	}
	return fit(line, width), true
}

// stripPhoneState is the most urgent thing among the live nodes, in the same
// word the roster heads its sections with — `1 running`, or `2 needs you`, or
// `3 idle`. A decision comes first because this compact summary has no other
// row on which to show it; the expanded strip still names each running task.
func (a *app) stripPhoneState() string {
	members := a.railMembers()
	if n := len(members[railAttention]); n > 0 {
		return itoa(n) + " " + railGroupWords[railAttention]
	}
	for _, g := range stripOrder {
		if n := len(members[g]); n > 0 {
			return itoa(n) + " " + railGroupWords[g]
		}
	}
	return ""
}

// stripPhonePress is the row's one gesture at [tierPhone]: it opens the roster
// PAGE and reads the project's record in behind it, exactly the door
// [app.taskSheetKeyPress] opens on its key. It answers whether it took the press.
//
// IT IS THE PAGE AND NOT THE OVERLAY COLUMN. The page is the surface this lane
// shaped into cards a thumb goes into, with a `‹ back` bar at its foot
// (taskview.go); the overlay column ([app.railTake]) is a keyboard list under a
// key legend, and a door that landed there would open the very thing this tier
// is built to leave behind.
func (a *app) stripPhonePress(width int) (tea.Cmd, bool) {
	if layoutTier(width) != tierPhone {
		return nil, false
	}
	return a.showPage(pageTasks), true
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

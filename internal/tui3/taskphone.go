package tui3

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
//   - THE STRIP IS ONE DOOR. A chip opens that piece of work's room, as it
//     always has; a press anywhere else on the row opens the ROSTER, which at
//     this width is the page over the whole body (task.go's [app.railTake]).
//     The strip is two rows of very small chips at forty-four columns, and a
//     press that missed one used to mean nothing at all.
//
//   - THE ROSTER'S ROWS ALREADY OPEN ON ONE PRESS ([app.taskSheetPress]), which
//     is the gesture this tier wanted anyway, so they are untouched.
//
//   - THE CARD'S VERBS BECOME BANDS. `esc back · ↑↓ scroll · m puts it in your
//     message` is a sentence about keys; at this tier the two things it names
//     that a finger can do — going back, and putting the task in your message —
//     are the bar's targets instead, in home's own bar shape ([phoneBar]).

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

// stripOpensRoster reports whether a press on the task strip that missed every
// chip is the door to the roster. It is the phone's answer and only the phone's:
// on a wide frame the roster has a column of its own beside the conversation,
// and a press on the strip's empty half would open a page over a list that is
// already on screen.
func stripOpensRoster(width int) bool { return layoutTier(width) == tierPhone }

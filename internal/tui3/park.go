package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// THE PARKED MESSAGE: what plain enter does while an answer is still coming.
//
// THE DEFECT THIS FILE EXISTS FOR. A person watched a long answer stream, typed
// "do much more of a deep research please" and pressed enter — and their
// sentence was drawn INTO THE MIDDLE OF THE ANSWER, sandwiched between two
// paragraphs of the same flowing reply. It read as though the model had quoted
// them mid-thought. Worse, the words often went nowhere: enter sent the line
// straight to the session as steering, and the session's steering only reaches
// the model AT A STEP BOUNDARY — so a message typed at a turn whose last request
// has already gone out lands in the transcript with nothing left to answer it.
//
// So plain enter no longer sends while a turn is open. IT PARKS: the message is
// held HERE, on the surface, in its own block between the answer and the box,
// and it goes when the answer is finished — as a turn of its own, which is a
// turn the model always answers. Held on the surface rather than handed to the
// session is what makes the other three things possible: it can still be edited,
// it can still be taken back, and esc can send it early.
//
//	enter    park it. The answer keeps streaming, the box is clear again.
//	esc      stop the answer and send what is parked, now.
//	↑        with an empty box, pull the parked message back in to edit it.
//	click    the same, on the block itself.
//
// ONE AT A TIME, in the order they were typed — the session's own law for its
// follow-up queue (internal/session's agent.go), said about this queue: each
// finished turn sends exactly one parked message, and the rest wait for the end
// of the turn that one starts. A drain that started three turns at once, or
// spliced three sentences into one message, would be a decision nobody made.
//
// ctrl+q is still its own key and still means something else: a follow-up is
// handed to the SESSION the moment it is typed (followup.go), with no take-backs
// and no editing. A parked message is still yours until it goes.

// parked is one message typed while a turn was open: the words, and the
// pictures that were in the tray with them.
//
// The chips travel with it because the tray is emptied when enter is pressed —
// the person has moved on from attaching — and a parked message that lost its
// pictures on the way would make them go and find the files again.
type parked struct {
	text  string
	chips []chip
}

// parking reports whether plain enter parks rather than sends.
//
// It asks the STATE rather than the stream, and the difference matters for
// exactly the seconds render.go's [app.waitingWords] is about: a turn that has
// been submitted and has no channel back from the provider yet is a turn that is
// open, and a message typed into that gap belongs behind it just as much as one
// typed mid-paragraph.
func (a *app) parking() bool { return a.state == stateWorking }

// park holds one message until the answer is over. It is [app.submit]'s door
// with the sending left out: the tray is spent, the draft is remembered and the
// draft file is done with, exactly as a sent message spends them, because from
// the person's side they have said the thing — it is only the model that has not
// heard it yet.
func (a *app) park(text string) tea.Cmd {
	text = strings.TrimSpace(text)
	chips := append([]chip(nil), a.chips...)
	if text == "" && len(chips) == 0 {
		return nil
	}
	a.chips = nil
	a.parks = append(a.parks, parked{text: text, chips: chips})
	a.follow()
	a.touch()
	return nil
}

// sendParked sends the oldest parked message, if there is one and if nothing
// else has taken the turn.
//
// It runs at every stream close, AFTER the follow-up queue is offered the same
// moment (app.go's streamClosedMsg): a follow-up was handed to the session
// before this message was parked in the ordinary case, and a message that
// jumped a queue the person filled first would be this surface reordering their
// sentences. Whichever starts, the rest stay parked and go at the next close.
func (a *app) sendParked() tea.Cmd {
	if a.stream != nil || len(a.parks) == 0 {
		return nil
	}
	next := a.parks[0]
	a.parks = a.parks[1:]
	if len(next.chips) > 0 {
		// The tray is refilled for exactly as long as the submit takes to read
		// it, because [app.submitImages] is the door and the tray is what it
		// reads. It empties the tray itself.
		//
		// IT HOLDS THE PARKED MESSAGE'S OWN PICTURES AND NOTHING ELSE, and what
		// was attached while it waited is put back afterwards. The parked
		// sentence says `[image #1]` about the first picture IT was written with
		// (imagepaste.go), so a tray that still carried something attached
		// during the wait would renumber the person's own words underneath them
		// — and would spend, on a message they had already sent, pictures they
		// were plainly still composing with.
		held := a.chips
		a.chips = next.chips
		cmd := a.submitImages(next.text)
		a.chips = held
		return cmd
	}
	return a.submit(next.text)
}

// dropParked forgets everything parked and says so, because the person typed
// those words. It is the one thing on this queue that loses a message, so it is
// called only where the CONVERSATION IS REPLACED under it — /new (app.go's
// [app.renew]) and a session opened from the welcome box — where a message
// parked against a reply that no longer exists has nowhere to go and no turn end
// coming to send it.
func (a *app) dropParked() {
	n := len(a.parks)
	if n == 0 {
		return
	}
	a.parks = nil
	if n == 1 {
		a.note("1 waiting message dropped")
	} else {
		a.note(itoa(n) + " waiting messages dropped")
	}
	a.touch()
}

// recallParked pulls the NEWEST parked message back into the box and reports
// whether there was one.
//
// The newest rather than the oldest: it is the thing the person just typed and
// the thing a typo is in, and ↑ over an empty box means "the last thing I said"
// everywhere else on this surface (recall.go).
//
// The words are not re-remembered on the way in — enter already put them in the
// recall history when it parked them — and the pictures go back on the tray they
// came off, so what comes back is the message exactly as it was.
func (a *app) recallParked() bool {
	if len(a.parks) == 0 {
		return false
	}
	last := a.parks[len(a.parks)-1]
	a.parks = a.parks[:len(a.parks)-1]
	a.input.setText(last.text)
	a.chips = append(a.chips, last.chips...)
	a.stick = true
	a.touch()
	return true
}

// recallParkedAt pulls ONE parked message back by its position in the queue,
// which is what a click on its block asks for: the pointer named a message, so
// the pointer's answer is that message and not the newest one.
func (a *app) recallParkedAt(i int) bool {
	if i < 0 || i >= len(a.parks) {
		return false
	}
	one := a.parks[i]
	a.parks = append(a.parks[:i], a.parks[i+1:]...)
	a.input.setText(one.text)
	a.chips = append(a.chips, one.chips...)
	a.stick = true
	a.touch()
	return true
}

// ── the block ───────────────────────────────────────────────────────────────
//
// IT IS DRAWN WHERE IT WILL LAND AND NOWHERE ELSE: under everything that has
// already happened, above the box it was typed into. That position is the whole
// answer to the defect — a message pinned below the conversation cannot be
// spliced into the middle of it, whatever the stream does next.
//
// It wears the person's own hue and the person's own glyph, because it is the
// person's own message; the dim line under it says what is going to happen to it
// and which keys change that.

// parkedHint is the dim line under the block, in the three pieces it is trimmed
// down through on a narrow frame. Each piece is dropped from the right, because
// what the message is DOING outranks what you can do about it.
var parkedHint = []string{"waits for this answer", "esc stops and sends", "↑ or click to edit"}

// parkedHeight is how many rows the block takes: the messages, then the one dim
// line. Zero when nothing is parked, which is every frame of an ordinary
// conversation.
func (a *app) parkedHeight() int {
	width, _ := a.size()
	return len(a.parkedRows(width))
}

// parkedRows draws the block.
func (a *app) parkedRows(width int) []string {
	if len(a.parks) == 0 || width < 4 {
		return nil
	}
	out := make([]string, 0, len(a.parks)+1)
	for at, p := range a.parks {
		// THE WHOLE MESSAGE LIGHTS, NOT THE ROW THE POINTER IS ON. The press pulls
		// that message back into the box whole ([app.parkPress]), so what a person is
		// about to act on is the block and not the line — and a sentence that wrapped
		// over three rows with one of them banded would read as three things
		// (hover.go: the set that lights is the set the press acts on).
		hot := a.hoveringParked(at)
		for i, line := range wrap(userLine(p.text, p.chips, a.pal), width-2) {
			lead := "  "
			if i == 0 {
				lead = a.pal.accent(a.pal.youGlyph())
			}
			text := lead + a.pal.accent(line)
			if hot {
				text = a.hoverRow(text, width)
			}
			out = append(out, text)
		}
	}
	return append(out, a.pal.dim(fit("  "+parkedWord(len(a.parks), width-2), width)))
}

// parkedWord is the dim line's sentence, trimmed to what fits. The count is
// only spelled when there is more than one message waiting — one message
// counted is a number that says nothing the block above it does not.
func parkedWord(n, width int) string {
	pieces := append([]string(nil), parkedHint...)
	if n > 1 {
		pieces[0] = itoa(n) + " wait for this answer"
	}
	for len(pieces) > 1 {
		// MEASURED, NOT COUNTED. The last piece opens with "↑", which is three
		// bytes and one cell, and a budget spent in bytes drops a piece that fits.
		line := strings.Join(pieces, " · ")
		if ansi.StringWidth(line) <= width {
			return line
		}
		pieces = pieces[:len(pieces)-1]
	}
	return pieces[0]
}

// parkPress is a click on the block: the message that row belongs to comes back
// into the box, exactly as ↑ brings it back, and the block loses that row.
//
// It answers by ROW ALONE. Every other column-aware target on this surface
// shares its line with something else; this one is a sentence in the person's
// own hue with nothing beside it, so anywhere along it is the same gesture. A
// press on the dim line under the block is marked with nothing and falls through
// untouched, because that line is a statement rather than a message.
func (a *app) parkPress(y int) (tea.Cmd, bool) {
	if len(a.parks) == 0 {
		return nil, false
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeParked {
		return nil, false
	}
	if !a.recallParkedAt(mark.index) {
		return nil, false
	}
	return a.edited(), true
}

// parkedMark is the pointer's answer for one row of the block: which parked
// message that row belongs to, so a click can pull that one back. The dim line
// at the foot belongs to no message and answers to nothing.
func (a *app) parkedMark(row, width int) chromeRow {
	if len(a.parks) == 0 {
		return chromeRow{}
	}
	at := 0
	for i, p := range a.parks {
		height := len(wrap(userLine(p.text, p.chips, a.pal), width-2))
		if height < 1 {
			height = 1
		}
		if row >= at && row < at+height {
			return chromeRow{kind: chromeParked, index: i}
		}
		at += height
	}
	return chromeRow{}
}

package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// THE MARGIN: ONE COLUMN, TWO SECTIONS, AND A DOOR AT THE FOOT OF EACH.
//
// The column on the right has been this conversation's WORK and nothing else
// (task.go's rail). Everything else standing over the conversation — the orders
// that keep being true while nobody is looking — lived on a page a person had to
// know the name of, behind a count on the status row (standdoor.go). Two things
// govern a conversation and only one of them was on the frame.
//
// So the column carries both, under two dim labels in the words the product
// already uses:
//
//	tasks
//	⠙ ◆ Fix nil-map           #7
//	+ /task
//
//	standing
//	◦ keep the tests green
//	◦ never touch the API   everywhere
//	+ /standing
//
// ── THE LAWS THIS FILE APPLIES ──
//
//   - THE LABELS ARE GEOGRAPHY AND THE ROWS ARE NEWS. A label draws wherever
//     the column does, empty or not, which is home's own STABLE GEOGRAPHY law
//     (docs/HOME-BRIDGE.md) applied to the one other place on this surface that
//     is a map; the ROWS under it obey the emptiness law exactly as they always
//     have. A map that redraws itself is not a map, and a person cannot learn
//     that standing orders live here if the place only exists once one does.
//
//   - THE `+` ROW TYPES, IT DOES NOT ARM. Pressing it puts the slash word and a
//     space in the draft and hands the keyboard back — visible, deletable text,
//     no mode, no form. The mechanism a person can see is the mechanism they can
//     learn: the next time they want one they type the word themselves.
//
//   - TWO ROWS OF PERMANENT INK AND NO MORE. The `+` rows are the whole of what
//     this file adds to a column that is otherwise the session's own news, and
//     the labels are two words. Anything else worth saying about an order is on
//     the order's page, one press away.
//
//   - THE GLYPH CARRIES THE HUE (docs/HOME-BRIDGE.md's colour law). `▲` wears
//     the question violet on the one row where a person is genuinely being waited
//     on, the breathing `●` wears the muted voice the spinner already speaks in,
//     and everything else — the title, the scope tail, the labels — is calm.
//
//   - A SCOPE TAIL ONLY WHERE THE SCOPE IS NOT THE DEFAULT. An order made in a
//     chat governs the project ([standing.AltitudeProject] is the zero value), so
//     saying so on every row would be printing the default in the one column with
//     no room to spare. The two that are not the default say which they are
//     ([standScopeTail]).
//
// TASKS NEVER CARRY A SCOPE MARK. A task is this conversation's, always; scope is
// a standing order's concept and belongs to nothing else on this column.

// The margin's own words. Both labels are words this product already uses for
// these two things, and both are quoted in internal/manual/chat's pages exactly
// as they are spelled here.
const (
	// marginTasksWord and marginStandWord are the two section labels: dim,
	// lowercase, and the same two nouns the rest of the product calls these
	// things — the roster is tasks, and what stands is standing.
	marginTasksWord = "tasks"
	marginStandWord = "standing"
	// marginTaskType and marginStandType are what a `+` row TYPES, trailing space
	// and all. They are the commands themselves rather than a spelling of them,
	// because the row is teaching the command: what lands in the box is what a
	// person would have typed.
	marginTaskType  = "/task "
	marginStandType = "/standing "
	// marginDoorMark is what a door row leads with. A `+` is the one mark on this
	// surface that already means "one more of these" (the strip's own +N counts
	// what it could not draw), and it is what keeps the row from reading as a
	// third task.
	marginDoorMark = "+ "
)

// marginTitleFloor is how little room a standing row's title may be left with
// before the scope tail gives way. It is [railTitleFloor] and not a second
// number: the two rows are the same width, and a title that survives a task's
// handle survives an order's reach.
const marginTitleFloor = railTitleFloor

// marginDoorWord is one door row as it is drawn.
func marginDoorWord(typed string) string { return marginDoorMark + strings.TrimSpace(typed) }

// ── what stands here ────────────────────────────────────────────────────────

// marginStanding is the orders governing this conversation, in the order the
// column reads them: what has a next occasion first, then what merely holds.
//
// WHY THAT ORDER. A reminder, a routine and a watch are all going to DO
// something, and when is the question a person glancing at a column has; a rule
// is true and does nothing at all ([standHoldsWord] is the whole of its status
// everywhere else on this surface). So the things with an occasion coming lead,
// and the rules sit under them — which is the same reading order the page's
// shelves have, one question up.
//
// THE SEAM'S OWN ORDER IS KEPT INSIDE EACH HALF. StandingHere answers
// conversation, project, machine, recent first within a shelf (internal/session's
// standing_orders.go), and a column that sorted again would be a second
// authority on a question the engine has already answered.
//
// IT IS CACHED ON HOME'S OWN BEAT, for [app.keepingCount]'s reason and it is the
// same reason: this is asked at LAYOUT, the layout is asked twice a frame, the
// frame turns thirty times a second while a turn is running, and the store is a
// directory of documents. A reading three seconds old is a reading that is right
// — nothing standing changes faster than that — and the glyph that moves is
// resolved on the paint clock ([app.marginGlyph]).
func (a *app) marginStanding() []StandingItemView {
	agent, ok := a.agent.(standingCountAgent)
	if !ok {
		// A SURFACE WHOSE ENGINE CANNOT BE ASKED HAS NO SECTION AT ALL — not an
		// empty one. A capability that cannot work is absent, not broken.
		return nil
	}
	if now := a.now(); !a.standRailAt.IsZero() && now.Sub(a.standRailAt) < keepEvery {
		return a.standRail
	}
	stand, _ := agent.StandingHere()
	occasions := make([]StandingItemView, 0, len(stand))
	var holds []StandingItemView
	for _, item := range stand {
		mark, running := a.standRunning(item.ID)
		view := StandingItemView{Item: item, Running: running, Mark: mark}
		if item.When.Kind == standing.WhenHold {
			holds = append(holds, view)
			continue
		}
		occasions = append(occasions, view)
	}
	a.standRail, a.standRailAt = append(occasions, holds...), a.now()
	return a.standRail
}

// marginStandingShows reports whether the column carries the standing section at
// all — which is whether this surface's engine can be asked what stands here.
func (a *app) marginStandingShows() bool {
	_, ok := a.agent.(standingCountAgent)
	return ok
}

// ── the rows ────────────────────────────────────────────────────────────────

// marginHead is the label the TASKS section opens with, and it is one line
// whatever is under it ([app.marginRows] says why a label is not news).
func (a *app) marginHead(width int) []railLine {
	return []railLine{{text: a.pal.dim(fit(marginTasksWord, width)), entry: -1}}
}

// marginRows is everything the column draws UNDER this conversation's work: the
// tasks section's own door, then the standing section whole.
//
// The blank line between the two is the separation this surface always uses —
// whitespace, never a rule, which is the design law a border would break — and
// it is counted like every other line, because a row the layout drew and did not
// count is a row the conversation pays for twice (task.go's [app.railView]).
func (a *app) marginRows(width int) []railLine {
	out := []railLine{{
		text:  a.marginDoorLine(marginTaskType, width),
		entry: -1,
		door:  marginTaskType,
	}}
	if !a.marginStandingShows() {
		return out
	}
	out = append(out,
		railLine{entry: -1},
		railLine{text: a.pal.dim(fit(marginStandWord, width)), entry: -1})
	for _, view := range a.marginStanding() {
		out = append(out, railLine{
			text:  a.marginStandRow(view, width),
			entry: -1,
			stand: view.Item.ID,
		})
	}
	return append(out, railLine{
		text:  a.marginDoorLine(marginStandType, width),
		entry: -1,
		door:  marginStandType,
	})
}

// marginDoorLine is one of the column's two doors — `+ /task` and `+ /standing`
// — as it is drawn.
//
// THE `+` IS THE CONTROL AND THE WORDS ARE THE LABEL, which is the same split
// the standing column's own door already makes ([app.railDoorLine]): the chord
// is a sentence for the hand that types, and the mark is what the hand that
// points presses. So the mark takes the accent for exactly as long as the
// pointer is on the row, and the words stay dim throughout.
//
// This is the second half of the emphasis law, which these two rows had been
// drawing only the first half of: the pointer raised the whole row's ground
// (task.go's [app.railRows]) and nothing in the row's own lead answered, so the
// louder cue was the quieter one and the mark said the same thing on every
// frame. It is the accent BUDGET that makes it safe — the mark is lit only while
// the pointer is on it, so the column never carries two.
func (a *app) marginDoorLine(typed string, width int) string {
	mark := a.pal.dim(marginDoorMark)
	if a.hoveringMarginDoor(typed) {
		mark = a.pal.accent(marginDoorMark)
	}
	word := strings.TrimSpace(typed)
	return mark + a.pal.dim(fit(word, max(0, width-ansi.StringWidth(marginDoorMark))))
}

// marginStandRow is one order as one line: the glyph every aforge surface agrees
// on, what the order is called, and its reach when its reach is not the default.
//
// THE TITLE IS THE MUTED VOICE and never the ink. A task that is running is the
// news on this column and wears the ink for it (task.go's [app.railTitle]); an
// order is a thing that is quietly true, which is exactly what the muted voice is
// for — and the one row that IS asking for something says so with its glyph.
//
// THE TAIL GIVES WAY FIRST, which is this column's own trade for the same shape:
// a name a person recognizes the order by outranks a word about its reach
// (task.go's [app.railEntryRows] drops a handle on the same terms, and
// [standFitNote] makes the same call on home's rows).
func (a *app) marginStandRow(view StandingItemView, width int) string {
	glyph := a.marginGlyph(view)
	room := width - ansi.StringWidth(a.marginPlainGlyph(view)) - 1
	tail := standScopeTail(view.Item.Level())
	if tail != "" && room-ansi.StringWidth(tail)-1 < marginTitleFloor {
		tail = ""
	}
	if tail != "" {
		room -= ansi.StringWidth(tail) + 1
	}
	if room < 1 {
		return glyph
	}
	title := fit(strings.TrimSpace(view.Item.Title()), room)
	line := glyph + " " + a.pal.muted(title)
	if tail == "" {
		return line
	}
	if pad := room - ansi.StringWidth(title) + 1; pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return line + a.pal.dim(tail)
}

// marginGlyph is the row's one coloured cell, and it BREATHES exactly where the
// status row's chip breathes (homestanding.go's [app.keepingWord]): while a pass
// has this order in its hands the cell is the spinner, on [spinnerStep]'s own
// grid so two moving things on one frame never beat against each other, and it is
// a still mark every other moment.
//
// THE VIOLET IS SPENT ON ONE STATE AND NO OTHER. `▲` is a person being waited on
// (docs/HOME-BRIDGE.md's colour law), which on this column is the only row
// somebody has to do something about; the breathing cell takes the muted voice
// the spinner already speaks in, and a still order is dim like every other fact
// this column reports.
func (a *app) marginGlyph(view StandingItemView) string {
	if view.Running {
		// The linear tier's objection to a spinner is the one it makes everywhere:
		// a claim repeated thirty times a second is heard thirty times a second by
		// a surface being read aloud. A still mark makes it once.
		if a.linear || a.pal.ascii {
			return a.pal.muted(glyphRunASCII)
		}
		return a.pal.muted(tokens.Spinner(a.paints / spinnerStep))
	}
	glyph := a.marginPlainGlyph(view)
	if view.Item.NeedsPerson != "" {
		return a.pal.askBold(glyph)
	}
	return a.pal.dim(glyph)
}

// marginPlainGlyph is that same cell with no hue on it, which is what the row's
// arithmetic measures: a painted glyph carries escape sequences and a budget
// spent on those would cut the title short by the width of a colour.
func (a *app) marginPlainGlyph(view StandingItemView) string {
	if view.Running {
		if a.linear || a.pal.ascii {
			return glyphRunASCII
		}
		return tokens.Spinner(a.paints / spinnerStep)
	}
	return standGlyph(view.Item, false, false, a.pal.ascii)
}

// ── the pointer ─────────────────────────────────────────────────────────────

// marginPress answers a press on one of the margin's own lines, and reports
// whether it took it. The rows this column has always had are answered above it
// (room.go's [app.railPress]); what is left here is a standing order and the two
// doors.
func (a *app) marginPress(line railLine) bool {
	switch {
	case line.door != "":
		a.marginType(line.door)
		return true
	case line.stand != "":
		// THE ROW OPENS THE PAGE ON ITSELF. Everything a person can do to an order
		// is a key on that page (standingpage.go), and a column two cells from a
		// paragraph is not the place to grow a second set of them.
		a.openStandingAt(line.stand)
		return true
	}
	return false
}

// marginType is what a `+` row does: the slash word into the draft, the keyboard
// back to the box.
//
// IT GOES AT THE HEAD OF THE LINE AND KEEPS WHAT WAS THERE. A slash is only a
// command as the first thing on a line (input.go's [app.enterLine]), so a word
// dropped at the caret would be a command that never ran — and a person half-way
// through saying what the work is has said the useful half already: `fix the
// flaky test` with `/task ` put in front of it is exactly the line they were
// going to type.
//
// PRESSING IT TWICE IS PRESSING IT ONCE. The word is already there, and a draft
// reading `/task /task ` is the gesture arguing with itself.
func (a *app) marginType(typed string) {
	// THE ROSTER GIVES THE KEYBOARD BACK, because the whole point of the row is
	// that the person is now typing: a column still holding the keys would eat the
	// first letter of the sentence they just asked for (task.go's [app.railTake]).
	a.railTake(false)
	a.closeLists()
	draft := strings.TrimLeft(string(a.input.value), " ")
	if !strings.HasPrefix(draft, typed) {
		draft = typed + draft
	}
	a.input.setText(draft)
	a.touch()
}

// marginDoorAt and marginStandAt are the pointer's half of the two answers
// above: the set that LIGHTS has to be the set the press acts on, or a row
// brightens and then does nothing (hover.go's own law). Both resolve through the
// layout, because where a line was drawn is a fact only the layout has (task.go's
// [app.railDoorAt] makes the same bargain one line down).
func (a *app) marginDoorAt(x, y int) (string, bool) {
	line, ok := a.marginLineAt(x, y)
	if !ok || line.door == "" {
		return "", false
	}
	return line.door, true
}

func (a *app) marginStandAt(x, y int) (string, bool) {
	line, ok := a.marginLineAt(x, y)
	if !ok || line.stand == "" {
		return "", false
	}
	return line.stand, true
}

func (a *app) marginLineAt(x, y int) (railLine, bool) {
	if !a.railAt(x, y) || a.railSeamAt(x, y) {
		return railLine{}, false
	}
	return a.railLineAt(y)
}

// hoveringMarginDoor and hoveringMarginStand are what the paint asks: is THIS
// line the one under the pointer. They are asked by word and by id rather than
// by screen row for [hoverAt]'s own reason — the column is rebuilt every frame,
// and a hover stored as a row of it would follow the scroll instead of the thing.
func (a *app) hoveringMarginDoor(typed string) bool {
	return a.hot.kind == hoverMarginDoor && a.hot.key == typed
}

func (a *app) hoveringMarginStand(id string) bool {
	return a.hot.kind == hoverMarginStand && a.hot.key == id
}

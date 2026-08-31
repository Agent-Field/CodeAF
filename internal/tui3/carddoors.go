package tui3

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE CARD'S DOORS, AND THE POINTER ON THEM ───────────────────────────────
//
// THE RIGHT COLUMN HAS DOORS AND NOTHING ON IT SAID SO. A fold line opens the
// band it is the foot of, and a work row opens that task's record — both of
// them presses a person can make and neither of them a thing the card drew any
// differently from the sentences around it. On a terminal that is worse than on
// a page: there is no underline, no cursor change, no affordance at all except
// the one the surface paints, so a card whose doors look exactly like its prose
// is a card that hides every door it has. The owner met it as a task row that
// opened a record when pressed and lit nothing when pointed at.
//
// So the line under the pointer takes THE GROUND LADDER'S CURSOR STEP
// ([palette.cursor], styles.go — the same background the left column's hovered
// row wears, and the same one every list, page and card on this surface lights
// with through [app.hoverRow]). One ink for "the pointer is here", everywhere.
//
// AND WHAT LIGHTS IS EXACTLY WHAT A PRESS ACTS ON, which is hover.go's law and
// the reason this file holds ONE registry rather than two. The fold lines and
// the work rows used to be recorded separately — a fold registry for
// [app.bandFoldAt] and a task registry for [app.cardTaskAt] — and a hover built
// on top of that would have been a third and a fourth ad-hoc map, drifting away
// from the press one wave at a time. There is one list of what the card painted
// that a press acts on; the press reads it by KIND, and the pointer reads it
// whole. A line the card draws that answers nothing — `▸ 3 more tasks`, which
// names the tasks place instead of opening (place_home.go's [app.homeCardWork])
// — records no door and therefore lights not at all, which is the useful half:
// a hover style everything wears says nothing about what can be pressed.
//
// AND THE DOORS ARE RECORDED BY THE DRAW. The card is assembled band by band,
// drops whole bands on a short frame and folds its own work rows at
// [homeCardTasks], so a row number computed a second time would name a different
// row exactly when a person could not tell why. The registry is written where
// the fold has already decided what is on the frame, and it dies with the frame
// that wrote it.

// cardDoorKind is what a door on the card DOES when it is pressed. It is the
// one thing the two kinds do not share, which is why the registry carries it
// rather than splitting in two.
type cardDoorKind uint8

const (
	// cardDoorFold is a `…N more` line: it opens the band it is the foot of
	// (homebands.go's [app.toggleBandFold]).
	cardDoorFold cardDoorKind = iota
	// cardDoorTask is a work row: it opens that task's record
	// ([app.openTaskRecord]).
	cardDoorTask
)

// cardDoor is one line of the right column, painted this frame, that a press
// acts on.
type cardDoor struct {
	kind cardDoorKind
	// text is the painted line with its colour taken off and its edges trimmed,
	// which is how a press and a hover both find it: the frame line they are
	// handed carries the left column and the gutter in front of it, so the door
	// is looked for INSIDE the row rather than compared to it.
	text string
	// fold is the band a [cardDoorFold] toggles, and task is the piece of work a
	// [cardDoorTask] opens. Exactly one of them is meant, and the kind says which.
	fold bandFoldLine
	task session.TaskIndexEntry
}

// key names a door ACROSS FRAMES, which is what a hover has to be held by: the
// column is repainted on every motion, so a pointer stored as "the third row of
// the card" would follow the redraw instead of following the door.
//
// A task is named by its own id, because that is what the record it opens is
// keyed by and it survives the card being re-laid out around it. A fold is named
// by its band and its words — a band draws one fold line, and the words say how
// many it is hiding, so a fold that changed count is a different door and stops
// being lit, which is right: the line under the pointer changed.
func (d cardDoor) key() string {
	if d.kind == cardDoorTask {
		if id := strings.TrimSpace(d.task.ID); id != "" {
			return "task\x00" + id
		}
	}
	if d.kind == cardDoorFold {
		return "fold\x00" + d.fold.band + "\x00" + d.text
	}
	return "row\x00" + d.text
}

// noteCardDoor records one painted line as a door. A line that came out empty
// is not one: it is a blank the fold left behind, and a door registered on it
// would answer for every blank row of the card.
func (a *app) noteCardDoor(door cardDoor) {
	door.text = strings.TrimSpace(ansi.Strip(door.text))
	if door.text == "" {
		return
	}
	a.home.cardDoors = append(a.home.cardDoors, door)
}

// resetCardDoors is called at the top of every card paint, and at the top of the
// phone sheet's, which is the same card in the other tier (homesheet.go).
func (a *app) resetCardDoors() { a.home.cardDoors = a.home.cardDoors[:0] }

// cardDoorAt answers the door whose row is inside a painted frame line, whatever
// kind it is. It is what the POINTER asks, because a pointer does not care what
// the door does — it cares that there is one.
func (a *app) cardDoorAt(rowText string) (cardDoor, bool) {
	plain := strings.TrimSpace(ansi.Strip(rowText))
	if plain == "" {
		return cardDoor{}, false
	}
	for _, door := range a.home.cardDoors {
		if strings.Contains(plain, door.text) {
			return door, true
		}
	}
	return cardDoor{}, false
}

// cardDoorOfKind is the same question narrowed, and it is what a PRESS asks:
// the two gestures are resolved one after the other ([app.homePress]), so a row
// that is one kind of door must not answer for the other.
func (a *app) cardDoorOfKind(rowText string, kind cardDoorKind) (cardDoor, bool) {
	plain := strings.TrimSpace(ansi.Strip(rowText))
	if plain == "" {
		return cardDoor{}, false
	}
	for _, door := range a.home.cardDoors {
		if door.kind == kind && strings.Contains(plain, door.text) {
			return door, true
		}
	}
	return cardDoor{}, false
}

// ── the pointer on them ─────────────────────────────────────────────────────

// hoverCardDoor records which door of the card the pointer is over, repainting
// only when the answer changed — a pointer crossing this column sends one
// message per cell, and a repaint per cell is what makes a surface stutter
// (hover.go's own cheapness law).
//
// IT IS THE RIGHT COLUMN'S HALF OF [app.homeHover], and it is asked with the
// same frame that hover resolved the list against, so the two halves cannot
// disagree about what was drawn. The gutter belongs to the pane on its right,
// exactly as it does for a press ([app.homePane]), so a pointer that has left
// the card — for the list, for the chrome, for a row of the card that is not a
// door — clears the hover rather than holding the last door lit under a pointer
// that has gone.
func (a *app) hoverCardDoor(x, y int, lines []string) {
	was := a.home.cardHover
	a.home.cardHover = ""
	width, _ := a.size()
	if left, right := homeColumns(width); right > 0 && x >= left+homeGutter && y >= 0 && y < len(lines) {
		if door, ok := a.cardDoorAt(lines[y]); ok {
			a.home.cardHover = door.key()
		}
	}
	if a.home.cardHover != was {
		a.touch()
	}
}

// lightCardDoor paints the hovered door's own line with the ground ladder's
// cursor step, and is THE LAST THING DONE TO THE CARD — one place, after every
// band has drawn, so no band has to remember the pointer exists (which is
// [app.hoverRow]'s bargain for every other surface).
//
// IT LIGHTS THE DOOR'S LINE AND NOT THE THING AROUND IT. A task that is not done
// draws a sentence under its name and the two are one entry of the card, but the
// press acts on the NAME row alone — so the name row is what lights, and the
// sentence under it stays prose.
//
// A DOOR THE CARD IS NOT DRAWING LIGHTS NOTHING. The subject changes under the
// pointer whenever the list's own hover moves, and the band the pointer was on
// may be folded away by the frame that follows; the key simply finds nothing,
// and the next motion resolves the hover again against what is actually there.
func (a *app) lightCardDoor(rows []string, width int, pal palette) []string {
	if a.home.cardHover == "" || len(rows) == 0 {
		return rows
	}
	text := ""
	for _, door := range a.home.cardDoors {
		if door.key() == a.home.cardHover {
			text = door.text
			break
		}
	}
	if text == "" {
		return rows
	}
	for at, row := range rows {
		if strings.Contains(ansi.Strip(row), text) {
			rows[at] = pal.cursor(row, width)
			break
		}
	}
	return rows
}

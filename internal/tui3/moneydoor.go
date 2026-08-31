package tui3

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// THE MONEY SEGMENT IS A DOOR: `$0.14` on the status row is a thing you can
// press, and it opens the Spending tab.
//
// It is the same bargain the model's name made two waves ago and the `keeping an
// eye on 2` segment made one wave ago (app.go's [app.statusPress],
// standdoor.go): the figure a person is looking at when they decide it is too
// high is already on the screen, and a figure that cannot be pressed is a label
// pretending it is not also a control. The keyboard door is unchanged and stays
// the documented one for a surface with the mouse turned off — `/budget`, and
// `ctrl+,` onto the tab — exactly as /model and /standing were left alone.
//
// AND IT WARMS AT FOUR FIFTHS OF THE RAIL. The figure rises out of the dim into
// [hueWarn] when this conversation has spent four fifths of its own ceiling: a
// bound about to be reached is not a failure and must not wear the failure hue,
// and the honest moment to say something is while there is still enough left to
// do something about it. It is the same fraction home's pulse uses for the day
// ([machineCeilingNear]) and it is deliberately the SAME predicate read against
// this segment's own denominator — the segment says what THIS conversation has
// spent, so the rail it is measured against is this conversation's.

// markMoneyDoor records where the money segment landed, so the press that may
// follow resolves against this frame rather than the one before it. It is
// [app.markKeepingDoor] for the other segment on the row and keeps that
// function's law: written AS THE ROW IS LAID OUT, never guessed at afterwards.
func (a *app) markMoneyDoor(parts []hudPart, base, row int) {
	at := base
	for i, part := range parts {
		if i > 0 {
			at += 3 // the " · " every cluster is joined on ([app.paintParts])
		}
		if part.kind == segCost {
			a.moneySpan = hudSpan{from: at, to: at + ansi.StringWidth(part.text)}
			a.moneyRow = row
			return
		}
		at += ansi.StringWidth(part.text)
	}
}

// moneyPress opens the Spending tab from the segment, and reports whether it
// took the click.
func (a *app) moneyPress(x, y int) tea.Cmd {
	if !a.moneyDoorAt(x, y) {
		return nil
	}
	return a.openSpending(spendTodayKey)
}

// moneyDoorAt is that question on its own, because the pointer asks it too: the
// set that LIGHTS has to be the set the press acts on (hover.go's own law).
func (a *app) moneyDoorAt(x, y int) bool {
	if a.copy.on || a.at(pageSettings) || a.pick.open {
		return false
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeStatus {
		return false
	}
	return mark.index == a.moneyRow && a.moneySpan.holds(x)
}

// hoveringMoney is whether the pointer is on the door right now (render.go's
// [app.paintPart] brightens it).
func (a *app) hoveringMoney() bool { return a.hot.kind == hoverMoney }

// spendTodayKey is what a door that lands on the day's own reading asks for.
//
// IT IS THE DAILY ROW'S KEY AND NOT THE WORD `today`, and the difference is the
// design's own first rule read honestly: `today` is a RECEIPT and the cursor may
// not rest on it, so "lands on today" means "opens the tab that leads with
// today, with the cursor on the first row a person can turn" — which is `per
// day`, the row the receipt is a reading of.
const spendTodayKey = config.KeyDailyBudget

// moneyNearRail reports that this conversation has spent enough of its own
// ceiling for the figure to leave the dim. No ceiling is no fraction and no
// colour: a rail nobody set is not a rail four fifths spent.
//
// THE FIGURE IS HELD AND NOT READ. This is asked once per PAINT, and the rail
// lives in a file — a status line that stat'd the profile sixty times a second
// is the shape PERF.md's allocation law exists to catch. It is read on the way
// in and again the moment the row is written ([app.readSpendRail]).
func (a *app) moneyNearRail() bool {
	if !a.railRead {
		a.readSpendRail()
	}
	return a.spendRail > 0 && a.cost >= a.spendRail*machineCeilingNear
}

// readSpendRail takes that reading. It is called once, lazily, and again from
// the registry's own Applied seam when the row is written, so the ink follows an
// edit without the paint ever touching the disk.
func (a *app) readSpendRail() {
	a.spendRail, a.railRead = config.SpendRailUSDAt(a.profileDir), true
}

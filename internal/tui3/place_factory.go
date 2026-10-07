package tui3

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// ── THE FACTORY PLACE ───────────────────────────────────────────────────────
//
// The handle the registry files (pages.go's [place]) for the page
// factory_page.go describes: the factory floor, reached by `/factory` and the
// ninth digit. It folds into `more` on a narrow bar, which is where a place a
// person visits on purpose rather than every minute belongs.
//
// placeFactory holds no state of its own; the page's state is `a.fp`.
type placeFactory struct{ placeBase }

func init() { registerPlace(placeFactory{}) }

func (placeFactory) id() page     { return pageFactory }
func (placeFactory) word() string { return "factory" }

// open starts the first read of the floor and arms the place's beat. THE BEAT
// IS ARMED WITH NOTHING CONNECTED TOO, as on every place but home: the read it
// asks for is nil then, and a seam wired later is read on the next beat.
func (placeFactory) open(a *app) tea.Cmd {
	return tea.Batch(a.armPlaceClock(), a.factoryRead())
}

// tick re-reads the floor on the three-second beat, off the loop, so a stream
// that moved is on this frame within a beat.
func (placeFactory) tick(a *app, now time.Time) (bool, tea.Cmd) {
	return true, a.factoryRead()
}

func (placeFactory) body(a *app, width, room int) []placeRow { return a.factoryBody(width, room) }

// stops is the rail line of every item, top first.
func (placeFactory) stops(a *app) []int { return a.factoryLines() }

// cursorAt is the rail line the cursor's item stands on, and 0 on a floor with
// no items, which is the first line the frame drew.
func (placeFactory) cursorAt(a *app) int {
	lines := a.factoryLines()
	if a.fp.cursor >= 0 && a.fp.cursor < len(lines) {
		return lines[a.fp.cursor]
	}
	return 0
}

// rowID names the item under the cursor by its id, which a re-read does not
// change.
func (placeFactory) rowID(a *app) string {
	if it, ok := a.factoryCursorItem(); ok {
		return "factory/" + itoa(it.ID)
	}
	return ""
}

// note is the one line the place says when the last read failed. The floor
// drawn above it is the one read before.
func (placeFactory) note(a *app, width int) []string {
	if a.fp.err == nil {
		return nil
	}
	return []string{" " + a.pal.dim(noteFit("the factory could not be read · "+a.fp.err.Error(), width-2))}
}

func (placeFactory) about() string { return "the work in flight, by where it stands" }

// hint names the keys the page has. With nothing connected there is nothing to
// walk, and only the way out is named.
func (placeFactory) hint(a *app) string {
	if len(factoryWalk(a.fp.snap)) == 0 {
		return "esc back"
	}
	return "↑↓ walk · esc back"
}

// press is a press on a row: the cursor lands on the item drawn there. A press
// is `enter` on the row (pages.go's [place.press]), and an item opens nothing
// yet, so landing the cursor is the whole of it.
func (placeFactory) press(a *app, y int) (tea.Cmd, bool) {
	a.factoryPress(y)
	return nil, true
}

// wheel walks the cursor, so the wheel over the floor never reaches the
// conversation behind it.
func (placeFactory) wheel(a *app, delta int) (tea.Cmd, bool) {
	a.factoryMove(delta)
	return nil, true
}

// key is the cursor and the way out; the router's classes are read first.
func (placeFactory) key(a *app, msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		a.leavePlace()
	case "up", "ctrl+p":
		a.factoryMove(-1)
	case "down", "ctrl+n":
		a.factoryMove(1)
	}
	return nil
}

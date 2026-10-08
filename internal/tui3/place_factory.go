package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/factory"
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
//
// A words box left open by walking away is shut on the way back in, with its
// words kept: the narrowed rail is still what the person left, and the next
// letter they press is a key again rather than a character in a box they
// cannot remember opening.
func (placeFactory) open(a *app) tea.Cmd {
	a.fp.typing = false
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

// hint names the keys the page has, for the row under the cursor. With nothing
// on the floor there is nothing to walk, and only the way out is named; with
// the words box open the keys are the box's.
//
// `space mark` is offered only on a new item, the one kind a mark means
// anything on, and `[ ] repo` only when there is more than one repo to cycle.
// `esc` says clear while anything narrows the rail, because that is what the
// first press does.
func (placeFactory) hint(a *app) string {
	if a.fp.typing {
		return "type to filter · enter keep · esc clear"
	}
	if !a.factoryFloorHas() {
		return "esc back"
	}
	parts := []string{"↑↓ walk"}
	if it, ok := a.factoryCursorItem(); ok && it.State == factory.StateNew {
		parts = append(parts, "space mark")
	}
	parts = append(parts, "/ filter")
	if len(a.fp.snap.Repos) > 1 {
		parts = append(parts, "[ ] repo")
	}
	parts = append(parts, "A backlog")
	if a.factoryNarrowed() {
		parts = append(parts, "esc clear")
	} else {
		parts = append(parts, "esc back")
	}
	return strings.Join(parts, " · ")
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

// owns is the words box, which has the whole keyboard while it is open, as
// every box inside a place does; and `space` on a new item, which marks it.
//
// SPACE IS CLAIMED HERE AND NOT IN key BECAUSE THE ROUTER READS A BARE SPACE
// AS HALF OF THE DOOR HOME before a place's own keys are asked
// ([app.placeHomeGesture]). It is claimed only on a new item, so everywhere
// else on the floor two spaces still go home.
func (placeFactory) owns(a *app, msg tea.KeyPressMsg) (tea.Cmd, bool) {
	// THE LAYOUT'S KEYS ARE READ FIRST, here rather than in key, because the
	// item page walks its stages with `↑` and the router would otherwise read
	// `↑` off the first row as the way onto the tab bar (factory_item.go).
	if cmd, took := a.factoryLayoutKey(msg); took {
		return cmd, true
	}
	// The box keeps every key but the router's walk between places and its alt
	// chords, which the hint line goes on naming while the box is open.
	if a.fp.typing {
		switch k := msg.String(); {
		case k == "tab" || k == "shift+tab" || msg.Key().Mod&tea.ModAlt != 0:
			return nil, false
		}
		return a.factoryFilterKey(msg), true
	}
	if msg.String() == "space" && a.factoryMark() {
		return nil, true
	}
	return nil, false
}

// key is the cursor, the rail's narrowings and the way out; the router's
// classes are read first. `esc` CLEARS A NARROWED RAIL BEFORE IT LEAVES, so a
// person who filtered does not lose the page to the same key that drops the
// filter.
func (placeFactory) key(a *app, msg tea.KeyPressMsg) tea.Cmd {
	switch k := msg.String(); k {
	case "esc":
		if a.factoryNarrowed() {
			a.factoryClear()
			return nil
		}
		a.leavePlace()
	case "up", "ctrl+p":
		a.factoryMove(-1)
	case "down", "ctrl+n":
		a.factoryMove(1)
	case "/":
		if a.factoryFloorHas() {
			a.factoryOpenFilter()
		}
	case "[":
		a.factoryCycleRepo(-1)
	case "]":
		a.factoryCycleRepo(1)
	case "A", "shift+a":
		a.factoryToggleBacklog()
	}
	return nil
}

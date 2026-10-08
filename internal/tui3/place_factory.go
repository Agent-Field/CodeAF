package tui3

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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
// cannot remember opening. A verb's typing row is shut the same way, and its
// words go with it, because they were about an item that may have moved on.
//
// AND THE MOCK FLOOR'S CLOCK IS ARMED HERE, only on a seam that has one
// (factory_keys.go's [app.factoryArmBeat]); it stops by itself once the page
// is not showing.
func (placeFactory) open(a *app) tea.Cmd {
	a.fp.typing = false
	a.fp.act.ask = nil
	return tea.Batch(a.armPlaceClock(), a.factoryRead(), a.factoryArmBeat())
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
// the words box or a typing row open the keys are the box's.
//
// THE VERBS COME FIRST AND NAME ONLY KEYS THAT WORK: a door the seam does not
// have is not on the line, and neither is a key the item's state refuses
// (factory_keys.go's [app.factoryVerbHint]). The rail's own keys follow, and
// WHEN THE LINE IS TOO LONG THEY ARE THE FIRST TO GO, last one first, because
// the verbs are about the item a person is standing on and the rail's keys
// are about the list they already know how to walk.
//
// `enter open` leads on every row, because `enter` opens the item page and
// nothing else. `space mark` is offered only on a new item, the one kind a
// mark means anything on, and `[ ] repo` only when there is more than one repo
// to cycle. `esc` says clear while anything narrows the rail, because that is
// what the first press does. WHILE THE ITEM PAGE IS OPEN THE LINE IS THE
// PAGE'S ([app.factoryItemHint]).
func (placeFactory) hint(a *app) string {
	if a.fp.typing {
		return "type to filter · enter keep · esc clear"
	}
	if ask := a.fp.act.ask; ask != nil {
		return factoryAskHint(ask)
	}
	var head []string
	if a.fp.act.habit != "" {
		head = append(head, "y bank it", "n not yet")
	}
	if a.fp.open {
		if it, ok := a.factoryCursorItem(); ok {
			return a.factoryItemHint(it, head)
		}
	}
	if !a.factoryFloorHas() {
		if a.factoryConnected() && a.factory.Has("new") {
			head = append(head, "n new")
		}
		return strings.Join(append(head, "esc back"), " · ")
	}
	it, ok := a.factoryCursorItem()
	var verbs []string
	if ok {
		verbs = append([]string{"enter open"}, a.factoryVerbHint(it)...)
	}
	// A FLOOR WITH NO DOORS AT ALL (the still fixture) keeps the walk at the
	// front, because walking is then the whole of what the page does.
	if len(verbs) <= 1 || (len(verbs) == 2 && verbs[1] == "space mark") {
		verbs = append([]string{"↑↓ walk"}, verbs...)
	}
	rail := []string{"/ filter"}
	if len(a.fp.snap.Repos) > 1 {
		rail = append(rail, "[ ] repo")
	}
	rail = append(rail, "A backlog", "z density")
	if a.factory.Has("sleep") {
		rail = append(rail, "S sleep 8h")
	}
	out := "esc back"
	if a.factoryNarrowed() {
		out = "esc clear"
	}
	line := func() string {
		parts := append(append(append([]string{}, head...), verbs...), rail...)
		return strings.Join(append(parts, out), " · ")
	}
	for len(rail) > 0 && a.width > 0 && ansi.StringWidth(placeTailed(line())) > a.width-2 {
		rail = rail[:len(rail)-1]
	}
	return line()
}

// factoryItemHint is the hint line while the item page is open: the habit
// offer's keys when one is drawn, the stage walk, `enter` only where it acts
// (the proof of a landed item, whose sheet it is), the item's verbs, and the
// way back to the floor. The page's own stage note is not named, because a
// key that only says it cannot open yet is not a verb. `S sleep 8h` is the
// first to go when the line is too long.
func (a *app) factoryItemHint(it factory.Item, head []string) string {
	parts := append(append([]string{}, head...), "↑↓ stages")
	if a.factoryOnProof(it) {
		switch {
		case factoryFirstFailed(it) != "" && a.factory.Has("sendback"):
			parts = append(parts, "enter send back")
		case factoryFirstFailed(it) == "" && a.factory.Has("signoff"):
			parts = append(parts, "enter ship")
		}
	}
	parts = append(parts, a.factoryVerbHint(it)...)
	tail := []string{}
	if a.factory.Has("sleep") {
		tail = append(tail, "S sleep 8h")
	}
	line := func() string {
		return strings.Join(append(append(append([]string{}, parts...), tail...), "esc floor"), " · ")
	}
	for len(tail) > 0 && a.width > 0 && ansi.StringWidth(placeTailed(line())) > a.width-2 {
		tail = tail[:len(tail)-1]
	}
	return line()
}

// press is a press on a row: the cursor lands on the item drawn there and its
// page opens, because a press is `enter` on the row (pages.go's [place.press])
// and `enter` on a row opens the item page. A press on a row that holds no
// item, or on the item page itself, does nothing.
func (placeFactory) press(a *app, y int) (tea.Cmd, bool) {
	if a.factoryPress(y) {
		a.factoryOpenItem()
	}
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
	// A VERB'S TYPING ROW HAS THE KEYBOARD ON THE SAME TERMS, and a habit
	// offer has `y` and `n` (factory_keys.go's [app.factoryOwns]).
	if cmd, took := a.factoryOwns(msg); took {
		return cmd, true
	}
	if msg.String() == "space" && a.factoryMark() {
		return nil, true
	}
	return nil, false
}

// key is the item's verbs, then the cursor, the rail's narrowings and the way
// out; the router's classes are read first. The verbs are asked first because
// none of them shares a key with the rail (factory_keys.go's [app.factoryKey]
// answers false for every key it does not take). `esc` CLEARS A NARROWED RAIL
// BEFORE IT LEAVES, so a person who filtered does not lose the page to the
// same key that drops the filter.
//
// WHILE THE ITEM PAGE IS OPEN only the verbs are asked: the rail's narrowings
// are about the floor, which is underneath.
func (placeFactory) key(a *app, msg tea.KeyPressMsg) tea.Cmd {
	if cmd, took := a.factoryKey(msg); took {
		return cmd
	}
	if a.fp.open {
		return nil
	}
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

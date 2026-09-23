package tui3

import (
	"math/rand/v2"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// ── THE WALL'S WIRING: KEYS, FRAME, POINTER AND STRIP ───────────────────────
//
// This is the only file of the wall that touches the app. The reading
// (walltail.go), the painting (wallview.go) and the spaces (spaces.go) meet
// here through the shapes in wallcontract.go.
//
// THE WALL'S DOORS ARE THE STRIP'S DOORS. Enter on a tile is [app.tabGo] and x
// is [app.tabDismiss], so a tile can never do something its tab would not: a
// held conversation is attached, a remembered one is opened, and closing a
// tile takes a view off this window and never ends work.

// wallOpenKey opens and closes the wall. alt+g is home's regroup and alt+1..7
// are the places, so the wall takes v, for view. Option+v composes to `√` on a
// Mac keyboard that is not sending alt, and the wall answers that as well.
const (
	wallOpenKey  = "alt+v"
	wallOpenDead = "√"
)

func wallOpenPressed(msg tea.KeyPressMsg) bool {
	s := msg.String()
	return s == wallOpenKey || s == wallOpenDead
}

// openWall stands the wall up and asks for every tile's tail OFF THE LOOP. The
// first frame draws whatever the cache already holds; the readings land a
// moment later and the tiles fill in.
func (a *app) openWall() tea.Cmd {
	a.spacesEnsure()
	a.wall.on = true
	a.wall.openedAt = time.Now()
	a.wall.naming, a.wall.filterOn = false, false
	if a.wall.marked == nil {
		a.wall.marked = map[string]bool{}
	}
	tiles := a.wallShown(time.Now())
	a.wall.focus = 0
	keys := make([]string, 0, len(tiles))
	for i, tile := range tiles {
		if tile.here {
			a.wall.focus = i
		}
		keys = append(keys, tile.tab.key)
	}
	a.touch()
	var tick tea.Cmd
	if !a.wall.ticking {
		tick = a.wallTick()
	}
	return tea.Batch(a.wallReadCmd(keys...), tick)
}

func (a *app) closeWall() {
	a.wall.on = false
	a.wall.naming, a.wall.filterOn = false, false
	a.wall.hover = wallHitRef{}
	a.wall.pop = wallPop{}
	a.touch()
}

// wallShown is the tiles the wall draws: every open conversation, narrowed to
// the active space's members when one is active.
func (a *app) wallShown(now time.Time) []wallTile {
	tiles := a.wallTiles(now)
	sp, ok := a.spaceActive()
	if !ok {
		return tiles
	}
	in := make(map[string]bool, len(sp.Members))
	for _, m := range sp.Members {
		in[m.Key] = true
	}
	kept := tiles[:0]
	for _, tile := range tiles {
		if in[tile.tab.key] {
			kept = append(kept, tile)
		}
	}
	return kept
}

// wallHead is the rows above the grid: the same pulse, tab strip and rule
// every other full-frame surface draws, so the wall reads as a place in this
// window and not a program of its own.
func (a *app) wallHead(width int) []string {
	return a.headRows(width, a.tabsRow(width), a.pal)
}

func (a *app) wallFrame(width, height int) []string {
	a.caret = false
	head := a.wallHead(width)
	a.wall.headRows = len(head)
	room := height - len(head)
	if room < 1 {
		return head[:height]
	}
	now := time.Now()
	tiles := a.wallShown(now)
	if a.wall.focus >= len(tiles) {
		a.wall.focus = max(len(tiles)-1, 0)
	}
	// The body is drawn with the chat's own pieces at the width the grid will
	// give it (wallmini.go), kept per reading so a quiet frame redraws nothing.
	_, tileW, _ := wallGrid(len(tiles), width, room, a.wall.cols)
	for i := range tiles {
		tiles[i].marked = a.wall.marked[tiles[i].tab.key]
		tiles[i].spaces = a.spacesOf(tiles[i].tab.key)
		tail := a.wall.tails[tiles[i].tab.key]
		tiles[i].rows = a.wallMiniRows(tail, wallInnerW(tileW))
		if tail != nil {
			tiles[i].doing = wallDoing(tail.recent, tiles[i].signal)
			tiles[i].moved = tail.freshAt
		} else {
			tiles[i].doing = wallDoing(nil, tiles[i].signal)
		}
	}
	view := wallView{
		spaces:    a.spaceNames(),
		tiles:     tiles,
		focus:     a.wall.focus,
		scroll:    a.wall.scroll,
		cols:      a.wall.cols,
		filter:    a.wall.filter,
		filtering: a.wall.filterOn,
		naming:    a.wall.naming,
		name:      a.wall.name,
		nameFresh: a.wall.nameFresh,
		made:      a.wall.made,
		madeN:     a.wall.madeN,
		madeAt:    a.wall.madeAt,
		hover:     a.wall.hover,
		choices:   a.wall.choices,
		choice:    a.wall.choice,
		pop:       a.wall.pop,
		spin:      a.paints / spinnerStep,
		now:       now,
	}
	if sp, ok := a.spaceActive(); ok {
		view.space = sp.Name
	}
	// The counts are of open conversations: a space's members this window has
	// no tab for are still members, but they are not on the wall.
	open := map[string]bool{}
	for _, tile := range a.wallTiles(now) {
		open[tile.tab.key] = true
	}
	view.total = len(open)
	for _, sp := range a.wall.spaces {
		view.hues = append(view.hues, sp.hueSpec())
		n := 0
		for _, m := range sp.Members {
			if open[m.Key] {
				n++
			}
		}
		view.counts = append(view.counts, n)
	}
	rows, hits := renderWall(a.pal, view, width, room)
	for i := range hits {
		hits[i].y0 += len(head)
		hits[i].y1 += len(head)
	}
	a.wall.hits = hits
	return append(head, rows...)
}

// wallGeometry is the room the grid has, for moving the focus by a row.
func (a *app) wallGeometry(n int) (cols, room int) {
	width, height := a.size()
	room = height - len(a.wallHead(width))
	cols, _, _ = wallGrid(n, width, room, a.wall.cols)
	return max(cols, 1), room
}

func (a *app) wallMove(to, n int) {
	if n == 0 {
		a.wall.focus = 0
		return
	}
	a.wall.focus = min(max(to, 0), n-1)
	width, _ := a.size()
	cols, room := a.wallGeometry(n)
	a.wall.scroll = wallScrollFor(a.wall.focus, a.wall.scroll, n, width, room, cols)
}

func (a *app) wallKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	tiles := a.wallShown(time.Now())
	n := len(tiles)

	if a.wall.pop.kind != wallPopNone {
		return a.wallPopKey(msg, tiles)
	}
	if a.wall.naming {
		switch key {
		case "esc":
			a.wall.naming = false
		case "enter":
			return a.wallMakeSpace(tiles)
		case "ctrl+r":
			a.wallShuffleName(tiles)
		case "left", "right":
			if c := len(a.wall.choices); c > 0 {
				step := 1
				if key == "left" {
					step = c - 1
				}
				a.wall.choice = (a.wall.choice + step) % c
			}
		case "backspace":
			a.wall.name = dropLastRune(a.wall.name)
			a.wall.nameFresh = false
		default:
			if t := msg.Key().Text; t != "" {
				// A name the wall filled in is selected: the first key typed
				// replaces it, as it would in any text field.
				if a.wall.nameFresh {
					a.wall.name = ""
				}
				a.wall.name += t
				a.wall.nameFresh = false
			}
		}
		return nil
	}
	if a.wall.filterOn {
		switch key {
		case "esc":
			a.wall.filterOn, a.wall.filter = false, ""
		case "enter":
			a.wall.filterOn = false
		case "backspace":
			a.wall.filter = dropLastRune(a.wall.filter)
		default:
			if t := msg.Key().Text; t != "" {
				a.wall.filter += t
			}
		}
		a.wall.focus = 0
		a.wall.scroll = 0
		return nil
	}
	if wallOpenPressed(msg) {
		a.closeWall()
		return nil
	}
	switch key {
	case "esc", "q":
		a.wallBack(tiles)
	case "left", "h":
		a.wallMove(a.wall.focus-1, n)
	case "right", "l":
		a.wallMove(a.wall.focus+1, n)
	case "up", "k":
		cols, _ := a.wallGeometry(n)
		a.wallMove(a.wall.focus-cols, n)
	case "down", "j":
		cols, _ := a.wallGeometry(n)
		a.wallMove(a.wall.focus+cols, n)
	case "home", "g":
		a.wallMove(0, n)
	case "end", "G":
		a.wallMove(n-1, n)
	case "enter":
		return a.wallOpen(tiles, a.wall.focus)
	case "space":
		a.wallToggle(tiles, a.wall.focus)
	case "m":
		// The focused conversation's spaces, from the same control a press on
		// its ●+ opens.
		if n > 0 {
			a.wallOpenMembers([]string{tiles[a.wall.focus].tab.key}, a.wallAnchor(wallHitSpaces, a.wall.focus))
		}
	case "s":
		a.wallStartNaming(tiles)
	case "x":
		if n == 0 {
			return nil
		}
		return a.tabDismiss(tiles[a.wall.focus].tab)
	case "/":
		a.wall.filterOn = true
	case "?":
		a.wallNext(tiles)
	case "tab", "shift+tab":
		a.wallCycleSpace(key == "tab")
	case "-":
		a.wallCols(-1, n)
	case "=", "+":
		a.wallCols(1, n)
	case "0":
		a.wall.cols = 0
		a.wallMove(a.wall.focus, n)
	case "D":
		// Delete the active space. The conversations in it are untouched: a
		// space is a view, and so is its going.
		if a.wall.active >= 0 {
			a.wallDeleteSpace(a.wall.active)
		}
	}
	return nil
}

// wallBack is esc: a filter goes first, then a selection, then the wall.
func (a *app) wallBack(tiles []wallTile) {
	switch {
	case a.wall.filter != "":
		a.wall.filter = ""
	case len(a.wallMarkedTabs(tiles)) > 0:
		a.wall.marked = map[string]bool{}
	default:
		a.closeWall()
	}
}

func (a *app) wallOpen(tiles []wallTile, i int) tea.Cmd {
	if i < 0 || i >= len(tiles) {
		return nil
	}
	tab := tiles[i].tab
	a.closeWall()
	return a.tabGo(tab)
}

// wallToggle marks tile i, or unmarks it. Any tile marked is the selection
// mode, and the last one unmarked leaves it.
func (a *app) wallToggle(tiles []wallTile, i int) {
	if i < 0 || i >= len(tiles) {
		return
	}
	k := tiles[i].tab.key
	if a.wall.marked[k] {
		delete(a.wall.marked, k)
	} else {
		a.wall.marked[k] = true
	}
}

func (a *app) wallNext(tiles []wallTile) {
	n := len(tiles)
	for step := 1; step <= n; step++ {
		i := (a.wall.focus + step) % n
		if tiles[i].signal == tabNeedsPerson {
			a.wallMove(i, n)
			return
		}
	}
}

func (a *app) wallCols(by, n int) {
	cols, _ := a.wallGeometry(n)
	a.wall.cols = min(max(cols+by, 1), 6)
	a.wallMove(a.wall.focus, n)
}

// wallStartNaming opens the new-space card with a name already in it. Nothing
// marked names the focused tile alone, which is the one a person pressing s
// is looking at.
func (a *app) wallStartNaming(tiles []wallTile) {
	marked := a.wallMarkedTabs(tiles)
	if len(marked) == 0 && len(tiles) > 0 {
		a.wall.marked[tiles[a.wall.focus].tab.key] = true
		marked = a.wallMarkedTabs(tiles)
	}
	if len(marked) == 0 {
		return
	}
	a.wall.naming = true
	a.wall.filterOn = false
	a.wall.pop = wallPop{}
	a.wall.name = spaceFreshName(marked, a.spaceNames(), "", rand.IntN)
	a.wall.nameFresh = true
	// The colours offered are the farthest from every space's, best first,
	// and the best is taken until the person takes another.
	a.wall.choices = spaceHueChoices(a.spaceHues(-1), spaceReservedHues(a.pal), wallSwatchCount)
	a.wall.choice = 0
}

// wallSwatchCount is how many colours a card offers.
const wallSwatchCount = 6

// wallShuffleName puts another pleasant word in the card, never the one that
// is there and never one a space already has, and moves the colour to the next
// best one offered.
func (a *app) wallShuffleName(tiles []wallTile) {
	a.wall.name = spaceFreshName(a.wallMarkedTabs(tiles), a.spaceNames(), a.wall.name, rand.IntN)
	a.wall.nameFresh = true
	if c := len(a.wall.choices); c > 0 {
		a.wall.choice = (a.wall.choice + 1) % c
	}
}

func (a *app) wallMarkedTabs(tiles []wallTile) []chatTab {
	var out []chatTab
	for _, tile := range tiles {
		if a.wall.marked[tile.tab.key] {
			out = append(out, tile.tab)
		}
	}
	return out
}

func (a *app) wallMakeSpace(tiles []wallTile) tea.Cmd {
	name := strings.TrimSpace(a.wall.name)
	a.wall.naming = false
	if name == "" {
		return nil
	}
	a.spacesEnsure()
	hue := nextSpaceHue(a.spaceHues(-1), spaceReservedHues(a.pal))
	if a.wall.choice >= 0 && a.wall.choice < len(a.wall.choices) {
		hue = a.wall.choices[a.wall.choice]
	}
	i, err := a.spaceMakeHued(name, a.wallMarkedTabs(tiles), hue)
	if i < 0 {
		return nil
	}
	if err != nil {
		a.note("the space is kept for this window, but " + err.Error())
	}
	// THE VIEW STAYS WHERE IT WAS. A person making a space is usually sorting
	// several at once, and a wall that jumped into the new one would hide the
	// conversations they were about to sort next. The chip row names the space
	// and its chip is one press away.
	a.wall.marked = map[string]bool{}
	a.wall.made, a.wall.madeN, a.wall.madeAt = a.wall.spaces[i].Name, len(a.wall.spaces[i].Members), time.Now()
	return nil
}

// wallSetSpace narrows the wall and the strip to space i, or widens them for
// i < 0. Like the chips' cycling it never switches the conversation in front.
func (a *app) wallSetSpace(i int) {
	if i >= len(a.wall.spaces) {
		i = -1
	}
	a.wall.active = i
	a.wall.focus, a.wall.scroll = 0, 0
}

// wallDeleteSpace forgets space i. Its conversations stay open: a space is a
// view, and so is its going.
func (a *app) wallDeleteSpace(i int) {
	if i < 0 || i >= len(a.wall.spaces) {
		return
	}
	if err := a.spaceDelete(i); err != nil {
		a.note("the space is gone from this window, but " + err.Error())
	}
	a.wall.hover = wallHitRef{}
	a.wall.pop = wallPop{}
}

// wallCycleSpace walks all → each space → all. It only narrows what the wall
// and the strip show; it never switches the conversation in front, so a person
// can look through their spaces without leaving the one they are in.
func (a *app) wallCycleSpace(forward bool) {
	n := len(a.wall.spaces)
	if n == 0 {
		return
	}
	next := a.wall.active + 1
	if !forward {
		next = a.wall.active - 1
	}
	if next >= n {
		next = -1
	}
	if next < -1 {
		next = n - 1
	}
	a.wallSetSpace(next)
}

// ── THE POINTER ─────────────────────────────────────────────────────────────
//
// Every target was written down by the painter as it drew (wallHit), and a
// press or a hover resolves against those cells and never against a second
// computation of the layout.

// wallHitAt is the target under a pointer on the last frame.
func (a *app) wallHitAt(x, y int) (wallHit, bool) {
	for _, hit := range a.wall.hits {
		if x >= hit.x0 && x < hit.x1 && y >= hit.y0 && y < hit.y1 {
			return hit, true
		}
	}
	return wallHit{}, false
}

// wallMotion answers the pointer moving while the wall is up. Over the head
// rows it is the strip's hover, as it is everywhere; over the wall it is the
// wall's, and the frame is repainted only when the target under it changed.
func (a *app) wallMotion(x, y int) {
	if y < a.wall.headRows {
		a.wallSetHover(wallHitRef{})
		a.setHover(x, y)
		return
	}
	if a.hot != (hoverAt{}) {
		a.hot = hoverAt{}
		a.touch()
	}
	hit, _ := a.wallHitAt(x, y)
	a.wallSetHover(hit.ref())
}

func (a *app) wallSetHover(ref wallHitRef) {
	if a.wall.hover == ref {
		return
	}
	a.wall.hover = ref
	a.touch()
}

// wallPress answers a left press while the wall is up and reports whether it
// took it. A press on the head rows is the strip's: its scrolling and its ×
// leave the wall up, and a tab closes the wall and lets the strip switch to it.
func (a *app) wallPress(x, y int) (tea.Cmd, bool) {
	if y < a.wall.headRows {
		if hit, ok := a.tabAt(x, y); ok {
			switch hit.kind {
			case tabSpace:
				return nil, true
			case tabScrollLeft, tabScrollRight:
				return nil, false
			case tabClose:
				if a.tabCloseAsks(hit.tab) {
					a.closeWall()
				}
				return nil, false
			}
		}
		a.closeWall()
		return nil, false
	}
	hit, ok := a.wallHitAt(x, y)
	if !ok {
		return nil, true
	}
	return a.wallDo(hit), true
}

// wallDo is what a press on one target does. Every button whose act has a key
// does what that key does, by calling the same function.
func (a *app) wallDo(hit wallHit) tea.Cmd {
	tiles := a.wallShown(time.Now())
	n := len(tiles)
	// While a space is being named the card is modal: only its own buttons
	// answer, as only its own keys do.
	if a.wall.naming {
		switch {
		case hit.kind == wallHitSwatch:
			if hit.arg < len(a.wall.choices) {
				a.wall.choice = hit.arg
			}
		case hit.kind == wallHitAction && (hit.arg == int(wallActSave) || hit.arg == int(wallActCancel) || hit.arg == int(wallActShuffle)):
			return a.wallAct(wallAct(hit.arg), tiles)
		}
		return nil
	}
	// A popover is dismissed by a press anywhere off it, and that press does
	// nothing else, as a menu's is.
	if a.wall.pop.kind != wallPopNone {
		if hit.kind != wallHitPopRow && hit.kind != wallHitSwatch {
			a.wall.pop = wallPop{}
			return nil
		}
		return a.wallPopPress(hit, tiles)
	}
	// A press anywhere but the filter's own words puts the typing down and
	// keeps what was typed, as enter does.
	if hit.kind != wallHitAction || (hit.arg != int(wallActFilter) && hit.arg != int(wallActFilterClear)) {
		a.wall.filterOn = false
	}
	switch hit.kind {
	case wallHitTile:
		switch {
		case hit.arg >= n:
		case len(a.wallMarkedTabs(tiles)) > 0:
			// The selection mode: a press anywhere on a tile picks it, the way a
			// photo grid does once one photo is picked.
			a.wallToggle(tiles, hit.arg)
		case hit.arg == a.wall.focus:
			return a.wallOpen(tiles, hit.arg)
		default:
			a.wallMove(hit.arg, n)
		}
	case wallHitSelect:
		a.wallToggle(tiles, hit.arg)
	case wallHitSpaces:
		if hit.arg < n {
			a.wallOpenMembers([]string{tiles[hit.arg].tab.key}, a.wallLocal(hit))
		}
	case wallHitOpen:
		return a.wallOpen(tiles, hit.arg)
	case wallHitClose:
		if hit.arg < n {
			return a.wallDismiss(tiles[hit.arg].tab)
		}
	case wallHitChip:
		a.wallSetSpace(hit.arg)
	case wallHitChipMenu:
		a.wallOpenSettings(hit.arg, a.wallLocal(hit))
	case wallHitAddSpace:
		a.wallStartNaming(tiles)
	case wallHitMini:
		a.wallMove(hit.arg, n)
	case wallHitAction:
		if wallAct(hit.arg) == wallActAddTo {
			var keys []string
			for _, tab := range a.wallMarkedTabs(tiles) {
				keys = append(keys, tab.key)
			}
			a.wallOpenMembers(keys, a.wallLocal(hit))
			return nil
		}
		return a.wallAct(wallAct(hit.arg), tiles)
	}
	return nil
}

func (a *app) wallAct(act wallAct, tiles []wallTile) tea.Cmd {
	n := len(tiles)
	switch act {
	case wallActBack:
		if n == 0 {
			a.closeWall()
			return nil
		}
		a.wallBack(tiles)
	case wallActOpen:
		return a.wallOpen(tiles, a.wall.focus)
	case wallActSelect:
		a.wallToggle(tiles, a.wall.focus)
	case wallActNewSpace, wallActMakeSpace:
		a.wallStartNaming(tiles)
	case wallActFilter:
		a.wall.filterOn = true
	case wallActFilterClear:
		a.wall.filterOn, a.wall.filter = false, ""
		a.wall.focus, a.wall.scroll = 0, 0
	case wallActNext:
		a.wallNext(tiles)
	case wallActColsLess:
		a.wallCols(-1, n)
	case wallActColsMore:
		a.wallCols(1, n)
	case wallActClose:
		if n > 0 {
			return a.wallDismiss(tiles[a.wall.focus].tab)
		}
	case wallActCloseViews:
		return a.wallCloseViews(tiles)
	case wallActClear:
		a.wall.marked = map[string]bool{}
	case wallActSave:
		return a.wallMakeSpace(tiles)
	case wallActCancel:
		a.wall.naming = false
	case wallActShuffle:
		a.wallShuffleName(tiles)
	}
	return nil
}

// wallDismiss is a tile's ×. A conversation with work in flight is asked about
// first (tabclose.go), and that card is drawn on the conversation's page, so
// the wall steps aside for it rather than hiding the question behind itself.
func (a *app) wallDismiss(tab chatTab) tea.Cmd {
	if a.tabCloseAsks(tab) {
		a.closeWall()
	}
	return a.tabDismiss(tab)
}

// wallCloseViews closes the view of every marked tile. The ones at rest go at
// once; the first with work in flight is asked about, on its page, and any
// others like it stay marked so nothing is closed that was not asked about.
func (a *app) wallCloseViews(tiles []wallTile) tea.Cmd {
	var cmds []tea.Cmd
	var ask *chatTab
	for _, tab := range a.wallMarkedTabs(tiles) {
		if a.tabCloseAsks(tab) {
			if ask == nil {
				held := tab
				ask = &held
			}
			continue
		}
		delete(a.wall.marked, tab.key)
		cmds = append(cmds, a.tabDismiss(tab))
	}
	if ask != nil {
		delete(a.wall.marked, ask.key)
		a.closeWall()
		cmds = append(cmds, a.tabDismiss(*ask))
	}
	a.wall.hover = wallHitRef{}
	return tea.Batch(cmds...)
}

func (a *app) wallWheel(down bool) {
	n := len(a.wallShown(time.Now()))
	cols, _ := a.wallGeometry(n)
	if down {
		a.wallMove(a.wall.focus+cols, n)
	} else {
		a.wallMove(a.wall.focus-cols, n)
	}
}

func dropLastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(r[:len(r)-1])
}

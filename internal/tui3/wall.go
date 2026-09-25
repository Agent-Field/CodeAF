package tui3

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE WALL'S WIRING: KEYS, FRAME, POINTER AND STRIP ───────────────────────
//
// This is the only file of the wall that touches the app. The reading
// (walltail.go), the painting (wallview.go) and the teams (teams.go) meet
// here through the shapes in wallcontract.go.
//
// THE WALL'S DOORS ARE THE STRIP'S DOORS. Enter on a tile is [app.tabGo] and x
// is [app.tabDismiss], so a tile can never do something its tab would not: a
// held conversation is attached, a remembered one is opened, and closing a
// tile takes a view off this window and never ends work.

// THE KEY MAP, and the press that does the same thing. Every button is a key
// and every act a key does is a press somewhere, except the few that are pure
// navigation or have no control to hang from (marked "key only").
//
//	alt+v              open or close the wall        ▦ in the dock, ▦ All on the strip
//	esc, q             back one layer: popover, card, help, selection,
//	                   filter, wall                  ‹ Back, a press off a card
//	?                  what you can do here          Help ?
//	arrows, hjkl       move the focus                (key only; hover never moves it)
//	home g, end G      first and last tile           (key only)
//	pgup, pgdown       a screen of rows              the wheel, one row a notch
//	n                  next waiting on a person      the title's needs-you count
//	enter              open the focused tile         a press on any tile, or Open
//	space              pick the focused tile         Select; any tile once one is picked
//	x                  close its view, or the picked'  Close on a tile, Close views
//	m                  its teams, or the picked'    Teams on a tile, Add to…
//	s                  new team of the picked       + New team, Make team
//	e                  the shown team's card        a segment's dot or ⋯
//	r                  resume the shown team's      the title's Open them
//	                   members not open here
//	tab, shift+tab     next or previous team        a Teams segment
//	1 to 9             that team, again for All     a Teams segment
//	D                  close the shown team         Close… on its card
//	o                  suggest teams, on All        ✦ Organize on the Teams row
//	u                  undo the last Organize       Undo, while the Teams row offers it
//	/                  filter                        Filter /
//	-, + or =          fewer or more columns         Columns − +
//	0                  columns back to automatic     (key only)
//
// Every row of the help sheet (wallhelp.go) is a press too, and does what its
// key does.
//
// A PRESS ON A TILE OPENS IT, as a window's thumbnail does in any overview:
// one press, not a press to aim and another to go. Once any tile is picked a
// press toggles instead, as a photo grid does in its selection mode. The
// pointer resting on a tile lights it and turns its bottom border into its
// action row, and changes nothing else; the keyboard's focus is its own and
// only the keys move it.
//
// THE MOTION IS SMALL AND NEVER HOLDS A KEY. The tiles come in row by row as
// the wall opens (about 140ms in all), and an opened tile's rectangle grows
// into the frame over three frames while the conversation is already live
// behind it. Both run on the paint clock, both are cut short by any key or
// press, and neither is drawn on the linear or ASCII tiers or over a remote
// link, where a few frames of movement is a stutter rather than a gesture.
// Hover is instant, as it is everywhere in a terminal, and esc is instant.

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
	a.teamsEnsure()
	now := a.now()
	a.wall.on = true
	a.wall.openedAt = now
	a.wall.naming, a.wall.filterOn = false, false
	a.wall.places = nil
	a.wall.zoomAt = time.Time{}
	a.wall.ptrIn, a.wall.rehover = false, false
	if a.wall.marked == nil {
		a.wall.marked = map[string]bool{}
	}
	tiles := a.wallShown(now)
	a.wall.focus = 0
	keys := make([]string, 0, len(tiles))
	for i, tile := range tiles {
		if tile.here {
			a.wall.focus = i
		}
		keys = append(keys, tile.tab.key)
	}
	// The conversation in front is focused AND on screen from the first frame,
	// so the eye lands where the keys are.
	a.wall.scroll = 0
	a.wallMove(a.wall.focus, len(tiles))
	a.wall.revealAt = time.Time{}
	if a.wallMotionOK() && len(tiles) > 0 {
		a.wall.revealAt = now
	}
	a.touch()
	var tick tea.Cmd
	if !a.wall.ticking {
		tick = a.wallTick()
	}
	return tea.Batch(a.wallReadCmd(keys...), tick, a.wake())
}

func (a *app) closeWall() {
	a.wall.on = false
	a.wall.naming, a.wall.filterOn = false, false
	a.wall.hover = wallHitRef{}
	a.wall.pop = wallPop{}
	a.wall.revealAt = time.Time{}
	a.wall.card = wallRect{}
	a.wall.spinning = false
	a.wall.help = false
	if a.wall.org.on {
		a.wallOrganizeClose()
	}
	a.touch()
}

// wallShown is the tiles the wall draws: every conversation open in this
// window, narrowed to the active team's members when one is active. A member
// this window does not have open is not a tile; the title counts it and
// offers to resume it ([app.wallResumeAway]).
func (a *app) wallShown(now time.Time) []wallTile {
	tiles := a.wallTiles(now)
	sp, ok := a.teamActive()
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
	// THE MANAGER'S TILE IS PINNED FIRST, as its tab is the strip's first
	// place, and wears the manager's mark (teammanager.go).
	for i, tile := range kept {
		if sp.Manager == "" || tile.tab.key != sp.Manager {
			continue
		}
		tile.manager = true
		copy(kept[1:i+1], kept[:i])
		kept[0] = tile
		break
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
	now := a.now()
	tiles := a.wallShown(now)
	if a.wall.focus >= len(tiles) {
		a.wall.focus = max(len(tiles)-1, 0)
	}
	a.wall.stirred = false
	// The body is drawn with the chat's own pieces at the width the grid will
	// give it (wallmini.go), kept per reading so a quiet frame redraws nothing.
	cols, tileW, tileH := wallGrid(len(tiles), width, room, a.wall.cols)
	// The scroll the painter will draw is the one kept, so the wheel steps
	// from what is on screen and never from a number the frame overruled.
	a.wall.scroll = wallScrollFor(a.wall.focus, a.wall.scroll, len(tiles), width, room, a.wall.cols)
	a.wall.spinning = false
	for i := range tiles {
		tiles[i].marked = a.wall.marked[tiles[i].tab.key]
		tiles[i].teams = a.teamsOf(tiles[i].tab.key)
		tail := a.wall.tails[tiles[i].tab.key]
		tiles[i].rows = a.wallMiniRows(tail, wallInnerW(tileW))
		if tail != nil {
			tiles[i].doing = wallDoing(tail.recent, tiles[i].signal)
			tiles[i].moved = tail.freshAt
		} else {
			tiles[i].doing = wallDoing(nil, tiles[i].signal)
		}
		if tiles[i].signal == tabWorking && tiles[i].live {
			a.wall.spinning = true
		}
	}
	reduced := a.wallReduced()
	if reduced {
		a.wall.spinning = false
	}
	view := wallView{
		tiles:     tiles,
		focus:     a.wall.focus,
		scroll:    a.wall.scroll,
		cols:      a.wall.cols,
		filter:    a.wall.filter,
		filtering: a.wall.filterOn,
		naming:    a.wall.naming,
		name:      a.wall.name,
		nameFresh: a.wall.nameFresh,
		asking:    a.wall.nameAsking,
		nameIn:    a.teamNameOf(a.wall.nameParent),
		made:      a.wall.made,
		madeN:     a.wall.madeN,
		madeAt:    a.wall.madeAt,
		hover:     a.wall.hover,
		choices:   a.wall.choices,
		choice:    a.wall.choice,
		pop:       a.wall.pop,
		spin:      wallSpin(now),
		now:       now,
		reduced:   reduced,
		pointerOn: a.wall.ptrIn,
		pointerY:  a.wall.ptrY - len(head),
		help:      a.wall.help,
		helpTop:   a.wall.helpTop,
		doorHot:   a.hot.kind == hoverTab && a.wall.door.pressable() && a.hot.index == a.wall.door.from,
	}
	if t, ok := a.teamActive(); ok {
		view.team = t.ID
	}
	view.popManager, view.mark = a.wallPopManagerRow(), a.teamManagerMark()
	if view.popManager != 0 {
		if t, ok := a.teamActive(); ok && len(a.wall.pop.targets) == 1 {
			view.popManagerWord = a.teamManagerMenuWord(t, a.wall.pop.targets[0])
		}
	}
	// The counts are of conversations open in this window, whatever a filter
	// is hiding: a team's members this window has no tab for are still
	// members, but they are not on the wall; the title counts them apart and
	// offers to resume them ([app.wallResumeAway]). They are read off the
	// strip's list, not a second build of every tile.
	open := map[string]bool{}
	tabs := a.tabList()
	for _, tab := range tabs {
		if !tab.start && !tab.work {
			open[tab.key] = true
		}
	}
	view.org = a.wallOrganizeFrame(tiles, tabs)
	view.total = len(open)
	for _, t := range a.wallTeams() {
		n := 0
		for _, m := range t.Members {
			if open[m.Key] {
				n++
			}
		}
		view.teams = append(view.teams, wallTeamRow{id: t.ID, name: a.wallTeamLabel(t), hue: t.HueSpec(), count: n, members: len(t.Members)})
	}
	if t, ok := a.teamActive(); ok {
		view.away = len(a.teamAway(t, tabs))
	}
	rows, hits := renderWall(a.pal, view, width, room)
	// A scroll moved the tiles under a pointer that did not move: the target
	// under it now is lit, as a page lights the link that scrolled under the
	// cursor. It costs a second paint only on the frame after a scroll.
	if a.wall.rehover {
		a.wall.rehover = false
		if a.wall.ptrIn {
			if ref := wallHitIn(hits, a.wall.ptrX, a.wall.ptrY-len(head)); ref != view.hover {
				a.wall.hover, view.hover = ref, ref
				rows, hits = renderWall(a.pal, view, width, room)
			}
		}
	}
	a.wall.card = a.wallCardRect(view, width, room, len(head))
	a.wallRevealMask(rows, hits, len(tiles), cols, tileH, room, width, now)
	for i := range hits {
		hits[i].y0 += len(head)
		hits[i].y1 += len(head)
	}
	a.wall.hits = hits
	return append(head, rows...)
}

// wallGeometry is the room the grid has, for moving the focus by a row. The
// head's height is the last frame's when there was one, so a key does not lay
// the tab strip out a second time to learn a number the frame already knew.
func (a *app) wallGeometry(n int) (cols, room int) {
	width, height := a.size()
	head := a.wall.headRows
	if head <= 0 {
		head = len(a.wallHead(width))
	}
	room = height - head
	cols, _, _ = wallGrid(n, width, room, a.wall.cols)
	return max(cols, 1), room
}

// wallRowsOnScreen is how many tile rows the grid shows at once, never under
// one.
func (a *app) wallRowsOnScreen(n int) int {
	width, _ := a.size()
	_, room := a.wallGeometry(n)
	_, _, tileH := wallGrid(n, width, room, a.wall.cols)
	return max(wallVisibleRows(room, tileH), 1)
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
	// A key finishes whatever is still moving before it does anything, so no
	// key ever lands on a picture that is not yet the whole wall.
	a.wallSettle()
	key := msg.String()
	tiles := a.wallShown(a.now())

	if a.wall.pop.kind != wallPopNone {
		return a.wallPopKey(msg, tiles)
	}
	if a.wall.help {
		if cmd, took := a.wallHelpKey(key); took {
			return cmd
		}
	}
	if a.wall.naming {
		switch key {
		case "esc":
			a.wall.naming = false
		case "enter":
			return a.wallMakeTeam(tiles)
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
			a.wall.nameFresh, a.wall.nameAsking = false, false
		default:
			if t := msg.Key().Text; t != "" {
				// A name the wall filled in is selected: the first key typed
				// replaces it, as it would in any text field. The name being
				// asked for is no longer wanted: the person is naming it.
				if a.wall.nameFresh {
					a.wall.name = ""
				}
				a.wall.name += t
				a.wall.nameFresh, a.wall.nameAsking = false, false
			}
		}
		return nil
	}
	if a.wall.org.on {
		return a.wallOrganizeKey(key)
	}
	if a.wall.filterOn {
		switch key {
		case "esc":
			a.wallClearFilter(tiles)
			return nil
		case "enter":
			// The typing is put down and the focus stays on the match it was
			// on, the first unless the arrows moved it, so a second enter opens it.
			a.wall.filterOn = false
			return nil
		case "left", "right", "up", "down":
			// The arrows walk the matches while the box stays up, as they do
			// in any type-to-find list.
			a.wall.filterOn = false
			cmd := a.wallKey(msg)
			a.wall.filterOn = true
			return cmd
		case "backspace":
			a.wall.filter = dropLastRune(a.wall.filter)
		default:
			t := msg.Key().Text
			if t == "" {
				return nil
			}
			a.wall.filter += t
		}
		// What is typed changed the matches: the focus goes to the first.
		a.wall.focus, a.wall.scroll = 0, 0
		return nil
	}
	if wallOpenPressed(msg) {
		a.closeWall()
		return nil
	}
	return a.wallCommand(key, tiles)
}

// wallCommand is what one key does on the wall at rest, with no card, filter
// or popover taking the keyboard. The help sheet's rows press it too, so a row
// does exactly what its key does.
func (a *app) wallCommand(key string, tiles []wallTile) tea.Cmd {
	n := len(tiles)
	switch key {
	case "esc", "q":
		a.wallBack(tiles)
	case "pgdown", "pgup":
		step := a.wallRowsOnScreen(n)
		if key == "pgup" {
			step = -step
		}
		cols, _ := a.wallGeometry(n)
		a.wallMove(min(max(a.wall.focus+step*cols, 0), n-1), n)
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
		// The picked conversations' teams, as the tray's Add to… opens them;
		// with none picked, the focused one's, as a press on its Teams does.
		if marked := a.wallMarkedTabs(tiles); len(marked) > 0 {
			keys := make([]string, 0, len(marked))
			for _, tab := range marked {
				keys = append(keys, tab.key)
			}
			a.wallOpenMembers(keys, a.wallAnchor(wallHitAction, int(wallActAddTo)))
		} else if n > 0 {
			a.wallOpenMembers([]string{tiles[a.wall.focus].tab.key}, a.wallAnchor(wallHitTeams, a.wall.focus))
		}
	case "s":
		return a.wallStartNaming(tiles)
	case "x":
		// The picked views, as the tray's Close views does; with none picked,
		// the focused one's, as its Close does.
		if len(a.wallMarkedTabs(tiles)) > 0 {
			return a.wallCloseViews(tiles)
		}
		if n == 0 {
			return nil
		}
		return a.wallDismissAt(tiles, a.wall.focus)
	case "r":
		// The shown team's members not open here, as the title's Open them.
		return a.wallResumeAway()
	case "e":
		// The shown team's settings, where its dot or ⋯ opens them.
		if id := a.wall.activeID; id != "" {
			return a.teamSheetOpen(id, teamSheetSettings)
		}
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		// A team by its place on the Teams row; the digit of the team that
		// is shown goes back to All, so the same key undoes itself.
		i := int(key[0] - '1')
		switch {
		case i >= len(a.wallTeams()):
		case a.wallTeams()[i].ID == a.wall.activeID:
			a.wallSetTeam("")
		default:
			a.wallSetTeam(a.wallTeams()[i].ID)
		}
	case "/":
		a.wall.filterOn = true
	case "n":
		a.wallNext(tiles)
	case "?":
		a.wallOpenHelp()
	case "tab", "shift+tab":
		a.wallCycleTeam(key == "tab")
	case "-":
		a.wallCols(-1, n)
	case "=", "+":
		a.wallCols(1, n)
	case "0":
		a.wall.cols = 0
		a.wallMove(a.wall.focus, n)
	case "D":
		// CLOSE the shown team (ruling c-9): at once with Undo when nothing
		// in it runs, and the close card when something does. A team is
		// deleted only from the teams page's Closed fold, once it is closed.
		if id := a.wall.activeID; id != "" {
			return a.teamsCloseAsk(id)
		}
	case "o":
		return a.wallOrganizeOpen()
	case "u":
		a.wallOrganizeUndo()
	}
	return nil
}

// wallBack is esc once the popover, the card and the help sheet are down: it
// takes off the innermost thing still on, the selection, then the filter, then
// the wall.
// The selection goes before the filter because it is the newer and the
// smaller of the two, and it may have been picked through the filter.
func (a *app) wallBack(tiles []wallTile) {
	switch {
	case len(a.wallMarkedTabs(tiles)) > 0:
		a.wall.marked = map[string]bool{}
	case a.wall.filter != "":
		a.wallClearFilter(tiles)
	default:
		a.closeWall()
	}
}

// wallClearFilter takes the filter off and keeps the focus on the conversation
// it was on, now among all of them, so clearing a search is not losing your
// place.
func (a *app) wallClearFilter(tiles []wallTile) {
	key := ""
	if f := a.wall.focus; f >= 0 && f < len(tiles) {
		key = tiles[f].tab.key
	}
	a.wall.filterOn, a.wall.filter = false, ""
	a.wallFocusKey(key)
}

// wallFocusKey moves the focus to the tile with key, scrolled into view, or
// to the first tile when it is not shown.
func (a *app) wallFocusKey(key string) {
	tiles := a.wallShown(a.now())
	for i, tile := range tiles {
		if tile.tab.key == key {
			a.wallMove(i, len(tiles))
			return
		}
	}
	a.wall.scroll = 0
	a.wallMove(0, len(tiles))
}

// wallOpen goes to tile i's conversation. The wall is down on the same
// message, so the conversation is live before the first frame of the zoom is
// drawn over it; the zoom only frames it (see [app.wallZoomed]).
func (a *app) wallOpen(tiles []wallTile, i int) tea.Cmd {
	if i < 0 || i >= len(tiles) {
		return nil
	}
	tab := tiles[i].tab
	from, ok := a.wallTileRect(i)
	a.closeWall()
	if ok && a.wallMotionOK() {
		a.wall.zoomFrom, a.wall.zoomAt = from, a.now()
		return tea.Batch(a.tabGo(tab), a.wake())
	}
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

// wallStartNaming opens the new-team card with a name already in it. Nothing
// marked names the focused tile alone, which is the one a person pressing s
// is looking at.
//
// THE NAME IS GIVEN ONCE, HERE, FROM WHAT THE CONVERSATIONS ARE. A shared
// project folder is the name, and nothing is asked. Otherwise a pleasant word
// is shown at once and one model call is asked for a better one off the loop
// ([app.wallAskName]); it replaces the word only if the person has not typed.
// After the card is saved nothing renames a team but the person.
func (a *app) wallStartNaming(tiles []wallTile) tea.Cmd {
	marked := a.wallMarkedTabs(tiles)
	if len(marked) == 0 && len(tiles) > 0 {
		a.wall.marked[tiles[a.wall.focus].tab.key] = true
		marked = a.wallMarkedTabs(tiles)
	}
	if len(marked) == 0 {
		return nil
	}
	a.wall.naming = true
	a.wall.filterOn = false
	a.wall.pop = wallPop{}
	a.wall.nameParent = ""
	a.wall.name = teamFreshName(marked, a.teamNames(), "", rand.IntN)
	a.wall.nameFresh = true
	// The colours offered are the farthest from every team's, best first,
	// and the best is taken until the person takes another.
	a.wall.choices = teamHueChoices(a.teamHues(""), teamReservedHues(a.pal), wallSwatchCount)
	a.wall.choice = 0
	return a.wallAskName(marked)
}

// teamNamer is the door a team's suggested name comes through: the agent's
// own naming errand (internal/session's teamname.go), on the path and the
// cheap model conversations name themselves with. It is asserted rather than
// added to [Agent], as [namedAgent] is, so an agent without it is simply not
// asked.
type teamNamer interface {
	NameTeam(ctx context.Context, titles []string) (string, error)
}

// teamNameWait is the most a suggestion is waited for. Past it the word
// already in the card is the name and the suggestion is dropped.
const teamNameWait = 5 * time.Second

// wallNameTimeMsg ends the wait for suggestion gen.
type wallNameTimeMsg struct{ gen int }

// wallAskName asks the agent once, off the loop, for a name for marked, and
// says `naming…` in the card while it waits. It asks nothing when the
// conversations share a folder, whose name is already the right one, or when
// there is no agent that names.
func (a *app) wallAskName(marked []chatTab) tea.Cmd {
	a.wall.nameGen++
	a.wall.nameAsking = false
	if teamFolder(marked) != "" || a.agent == nil {
		return nil
	}
	namer, ok := a.agent.(teamNamer)
	if !ok {
		return nil
	}
	var titles []string
	for _, tab := range marked {
		title := tab.full
		if strings.TrimSpace(title) == "" {
			title = tab.word
		}
		if title = strings.TrimSpace(title); title != "" {
			titles = append(titles, title)
		}
	}
	if len(titles) == 0 {
		return nil
	}
	gen := a.wall.nameGen
	a.wall.nameAsking = true
	ask := a.besideLine(func() func(here bool) tea.Cmd {
		ctx, cancel := context.WithTimeout(context.Background(), teamNameWait)
		defer cancel()
		name, err := namer.NameTeam(ctx, titles)
		return func(bool) tea.Cmd {
			a.wallTeamNamed(gen, name, err)
			return nil
		}
	})
	wait := tea.Tick(teamNameWait, func(time.Time) tea.Msg { return wallNameTimeMsg{gen: gen} })
	return tea.Batch(ask, wait)
}

// wallTeamNamed takes suggestion gen into the card, if it is still the one
// being waited for, the card is still up and the person has not typed. A
// failure, an empty answer or a name another team already has leaves the word
// that was there.
func (a *app) wallTeamNamed(gen int, name string, err error) {
	if gen != a.wall.nameGen || !a.wall.nameAsking {
		return
	}
	a.wall.nameAsking = false
	a.touch()
	if err != nil || !a.wall.naming || !a.wall.nameFresh {
		return
	}
	name = ansi.Truncate(strings.TrimSpace(name), teamNameCells, "")
	if name == "" {
		return
	}
	for _, taken := range a.teamNames() {
		if strings.EqualFold(taken, name) {
			return
		}
	}
	a.wall.name = name
}

// wallNameTimedOut ends the wait for suggestion gen: the word in the card
// stays, and an answer that comes later is not used.
func (a *app) wallNameTimedOut(gen int) {
	if gen == a.wall.nameGen && a.wall.nameAsking {
		a.wall.nameAsking = false
		a.touch()
	}
}

// wallSwatchCount is how many colours a card offers.
const wallSwatchCount = 6

// wallShuffleName puts another pleasant word in the card, never the one that
// is there and never one a team already has, and moves the colour to the next
// best one offered.
func (a *app) wallShuffleName(tiles []wallTile) {
	a.wall.name = teamFreshName(a.wallMarkedTabs(tiles), a.teamNames(), a.wall.name, rand.IntN)
	a.wall.nameFresh = true
	// A shuffle is the person choosing: a suggestion still on its way is
	// no longer wanted.
	a.wall.nameGen++
	a.wall.nameAsking = false
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

func (a *app) wallMakeTeam(tiles []wallTile) tea.Cmd {
	name := strings.TrimSpace(a.wall.name)
	a.wall.naming = false
	if name == "" {
		return nil
	}
	a.teamsEnsure()
	hue := nextTeamHue(a.teamHues(""), teamReservedHues(a.pal))
	if a.wall.choice >= 0 && a.wall.choice < len(a.wall.choices) {
		hue = a.wall.choices[a.wall.choice]
	}
	parent := a.wall.nameParent
	a.wall.nameParent = ""
	id, err := a.teamMakeIn(name, a.wallMarkedTabs(tiles), hue, parent)
	made, ok := a.teamByID(id)
	if !ok {
		return nil
	}
	if err != nil {
		a.note("the team is kept for this window, but " + err.Error())
	}
	// THE VIEW STAYS WHERE IT WAS. A person making a team is usually sorting
	// several at once, and a wall that jumped into the new one would hide the
	// conversations they were about to sort next. The chip row names the team
	// and its chip is one press away.
	a.wall.marked = map[string]bool{}
	a.wall.made, a.wall.madeN, a.wall.madeAt = made.Name, len(made.Members), time.Now()
	return nil
}

// wallPlace is where one team's grid was left: the focused conversation, by
// key so a tile that moved is still found, and the row at the top.
type wallPlace struct {
	key    string
	scroll int
}

// wallSetTeam narrows the wall and the strip to team id, or widens them for
// "". Like the chips' cycling it never switches the conversation in front.
//
// EACH TEAM KEEPS ITS PLACE WHILE THE WALL IS UP. Looking into harbor and
// back to All returns to the tile and the row that were on screen, as a
// browser's tabs each keep their own scroll; a team not visited yet starts
// at its first tile. The places are kept by id, so a team deleted or moved
// takes its place with it and hands it to nobody.
func (a *app) wallSetTeam(id string) {
	if teamIndex(a.wall.teams, id) < 0 {
		id = ""
	}
	if id == a.wall.activeID {
		return
	}
	if a.wall.places == nil {
		a.wall.places = map[string]wallPlace{}
	}
	tiles := a.wallShown(a.now())
	if f := a.wall.focus; f >= 0 && f < len(tiles) {
		a.wall.places[a.wall.activeID] = wallPlace{key: tiles[f].tab.key, scroll: a.wall.scroll}
	}
	a.wall.activeID = id
	a.wall.hover = wallHitRef{}
	a.wall.rehover = true
	place, ok := a.wall.places[id]
	if !ok {
		a.wall.focus, a.wall.scroll = 0, 0
		return
	}
	a.wall.scroll = place.scroll
	a.wallFocusKey(place.key)
}

// wallDeleteTeam forgets team id. Its conversations stay open: a team is a
// view, and so is its going.
func (a *app) wallDeleteTeam(id string) {
	if teamIndex(a.wall.teams, id) < 0 {
		return
	}
	if err := a.teamDelete(id); err != nil {
		a.note("the team is gone from this window, but " + err.Error())
	}
	a.wall.hover = wallHitRef{}
	a.wall.pop = wallPop{}
}

// wallCycleTeam walks all → each team → all. It only narrows what the wall
// and the strip show; it never switches the conversation in front, so a person
// can look through their teams without leaving the one they are in.
func (a *app) wallCycleTeam(forward bool) {
	shown := a.wallTeams()
	n := len(shown)
	if n == 0 {
		return
	}
	at := teamIndex(shown, a.wall.activeID) // -1 is All
	next := at + 1
	if !forward {
		next = at - 1
	}
	if next >= n {
		next = -1
	}
	if next < -1 {
		next = n - 1
	}
	if next < 0 {
		a.wallSetTeam("")
		return
	}
	a.wallSetTeam(shown[next].ID)
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
//
// A POINTER MOVING INSIDE ONE TARGET DRAWS NOTHING. The hover is a target and
// not a cell, so a hand wandering across a tile's body changes nothing the
// frame reads, and the frame Bubble Tea is about to ask for is the one it was
// given last time (coalesce.go's still). Only a motion on a message that
// changed nothing else may say so: a wheel folded into the same message has
// moved the grid (stirred).
//
// ONE TARGET CAN SPAN TWO ROWS' MEANINGS: a waiting tile's Answer and its
// row's Answer share one ref, as a picked tile's ☐ and its row's Select do,
// and the painter lights the one on the pointer's row (wallView.pointerY), so
// on those targets a change of row is a change.
func (a *app) wallMotion(x, y int) {
	rowMoved := y != a.wall.ptrY
	a.wall.ptrX, a.wall.ptrY, a.wall.ptrIn = x, y, y >= a.wall.headRows
	if y < a.wall.headRows {
		a.wallSetHover(wallHitRef{})
		a.setHover(x, y)
		return
	}
	if a.hot != (hoverAt{}) {
		a.hot = hoverAt{}
		a.touch()
		a.wall.stirred = true
	}
	hit, _ := a.wallHitAt(x, y)
	if a.wall.hover == hit.ref() && (hit.kind == wallHitOpen || hit.kind == wallHitSelect) && rowMoved {
		a.wall.stirred = true
		a.touch()
		return
	}
	if a.wall.hover == hit.ref() && !a.wall.stirred {
		a.ptr.still = true
		return
	}
	a.wallSetHover(hit.ref())
}

// wallHitIn is the target under x,y among hits, the zero ref for none.
func wallHitIn(hits []wallHit, x, y int) wallHitRef {
	for _, hit := range hits {
		if x >= hit.x0 && x < hit.x1 && y >= hit.y0 && y < hit.y1 {
			return hit.ref()
		}
	}
	return wallHitRef{}
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
			case tabTeam:
				// The chip is the team switcher here as on every page.
				a.openTeamMenu()
				return nil, true
			case tabWall:
				// The strip's own door to this view closes it, as alt+v does.
				a.closeWall()
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
	a.wallSettle()
	// A PRESS OFF A CARD PUTS THE CARD AWAY and does nothing else, as a menu
	// or a sheet does anywhere: the popover, or the new-team or Organize card,
	// which is the same as its Cancel. A press inside a card but on none of its
	// controls is a press on the card, and does nothing.
	if a.wall.card.w() > 0 && (a.wall.pop.kind != wallPopNone || a.wall.naming || a.wall.help || a.wall.org.on) {
		if !a.wall.card.holds(x, y) {
			a.wall.pop = wallPop{}
			a.wall.naming = false
			a.wall.help = false
			if a.wall.org.on {
				a.wallOrganizeClose()
			}
			a.wall.stirred = true
			return nil, true
		}
	}
	hit, ok := a.wallHitAt(x, y)
	if !ok {
		return nil, true
	}
	a.wall.stirred = true
	return a.wallDo(hit), true
}

// wallDo is what a press on one target does. Every button whose act has a key
// does what that key does, by calling the same function.
func (a *app) wallDo(hit wallHit) tea.Cmd {
	tiles := a.wallShown(a.now())
	n := len(tiles)
	// While a team is being named the card is modal: only its own buttons
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
	// The Organize card is modal as the new-team card is.
	if a.wall.org.on {
		return a.wallOrganizePress(hit)
	}
	// The help sheet answers only its own rows, as a menu does.
	if a.wall.help {
		if hit.kind == wallHitHelp {
			return a.wallHelpPress(hit.arg, tiles)
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
		default:
			// One press opens, as a thumbnail does in any overview.
			return a.wallOpen(tiles, hit.arg)
		}
	case wallHitSelect:
		a.wallToggle(tiles, hit.arg)
	case wallHitTeams:
		if hit.arg < n {
			a.wallOpenMembers([]string{tiles[hit.arg].tab.key}, a.wallLocal(hit))
		}
	case wallHitOpen:
		return a.wallOpen(tiles, hit.arg)
	case wallHitClose:
		return a.wallDismissAt(tiles, hit.arg)
	case wallHitChip:
		a.wallSetTeam(hit.id)
	case wallHitChipMenu:
		// The team's card (teamsheet.go), over the wall.
		return a.teamSheetOpen(hit.id, teamSheetSettings)
	case wallHitAddTeam:
		return a.wallStartNaming(tiles)
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
	case wallActNewTeam, wallActMakeTeam:
		return a.wallStartNaming(tiles)
	case wallActFilter:
		a.wall.filterOn = true
	case wallActFilterClear:
		a.wallClearFilter(tiles)
	case wallActNext:
		a.wallNext(tiles)
	case wallActColsLess:
		a.wallCols(-1, n)
	case wallActColsMore:
		a.wallCols(1, n)
	case wallActClose:
		return a.wallDismissAt(tiles, a.wall.focus)
	case wallActCloseViews:
		return a.wallCloseViews(tiles)
	case wallActClear:
		a.wall.marked = map[string]bool{}
	case wallActSave:
		return a.wallMakeTeam(tiles)
	case wallActCancel:
		a.wall.naming = false
	case wallActShuffle:
		a.wallShuffleName(tiles)
	case wallActHelp:
		a.wallOpenHelp()
	case wallActOrganize:
		return a.wallOrganizeOpen()
	case wallActOrgUndo:
		a.wallOrganizeUndo()
	case wallActOrgApply:
		a.wallOrganizeApply()
	case wallActOrgCancel:
		a.wallOrganizeClose()
	case wallActResume:
		return a.wallResumeAway()
	}
	return nil
}

// wallDismissAt is tile i's ×. A conversation with work in flight is asked
// about first (tabclose.go), and that card is drawn on the conversation's
// page, so the wall steps aside for it rather than hiding the question behind
// itself.
func (a *app) wallDismissAt(tiles []wallTile, i int) tea.Cmd {
	if i < 0 || i >= len(tiles) {
		return nil
	}
	tab := tiles[i].tab
	if a.tabCloseAsks(tab) {
		a.closeWall()
		return a.tabDismiss(tab)
	}
	focused := a.wallFocusedKey(tiles)
	cmd := a.tabDismiss(tab)
	a.wallRefocus(tiles, focused)
	return cmd
}

// wallResumeAway is the title's `Open them` and r: every member of the shown
// team this window does not have open is resumed BEHIND, as the manager's
// starts are ([app.trafficStarted]), so each becomes a tab and a tile and
// nothing in front moves. The focus stays on the tile it was on.
//
// A member the keeper still holds but whose tab was closed only gets its tab
// back. The rest are opened through the door on the door line, off the loop,
// one after another, because opening one is a call to the engine; what comes
// back is stowed on the loop. Over a connection that holds one conversation at
// a time there is no behind to resume into, and the note says so.
func (a *app) wallResumeAway() tea.Cmd {
	t, ok := a.teamActive()
	if !ok {
		return nil
	}
	away := a.teamAway(t, a.tabList())
	if len(away) == 0 {
		return nil
	}
	focused := a.wallFocusedKey(a.wallShown(a.now()))
	var closed []teamMember
	for _, m := range away {
		if a.behind[m.Key] != nil {
			delete(a.tabShut, m.Key)
			continue
		}
		if strings.TrimSpace(m.File) != "" {
			closed = append(closed, m)
		}
	}
	a.chatTabBar = tabBar{}
	a.wallRefocusKey(focused)
	if len(closed) == 0 {
		return nil
	}
	switch {
	case a.shared:
		a.note("could not open the rest of " + t.Name + ": " + oneConversationWord)
		return nil
	case a.open == nil:
		a.note("could not open the rest of " + t.Name + ": " + resumeUnavailableWord)
		return nil
	}
	open, name := a.open, t.Name
	return a.besideLine(func() func(bool) tea.Cmd {
		convs := make([]Conversation, 0, len(closed))
		var refused []string
		for _, m := range closed {
			conv, err := open(m.Where, m.File)
			switch {
			case errors.Is(err, session.ErrSessionLocked):
				refused = append(refused, teamMemberWord(m)+": "+sessionBusyWord)
			case err != nil:
				refused = append(refused, teamMemberWord(m)+": "+err.Error())
			case conv.Agent != nil:
				convs = append(convs, conv)
			}
		}
		return func(bool) tea.Cmd { return a.wallResumed(name, convs, refused) }
	})
}

// wallResumed holds what [app.wallResumeAway] opened, behind, and says what
// did not open.
func (a *app) wallResumed(team string, convs []Conversation, refused []string) tea.Cmd {
	focused := ""
	if a.wall.on {
		focused = a.wallFocusedKey(a.wallShown(a.now()))
	}
	var cmds []tea.Cmd
	var keys []string
	for _, conv := range convs {
		key := a.convKey(conv.SessionFile)
		cmds = append(cmds, a.stow(conv, nil))
		a.trafficBehindTop(key)
		keys = append(keys, key)
	}
	a.chatTabBar = tabBar{}
	if a.wall.on {
		a.wallRefocusKey(focused)
		cmds = append(cmds, a.wallReadCmd(keys...))
	}
	if len(refused) > 0 {
		a.note("could not open all of " + team + ": " + strings.Join(refused, "; "))
	}
	a.touch()
	return tea.Batch(cmds...)
}

// wallRefocusKey keeps the focus on the tile holding key after the tiles
// changed under it; with no such key, or the wall down, it does nothing.
func (a *app) wallRefocusKey(key string) {
	if !a.wall.on || key == "" {
		return
	}
	tiles := a.wallShown(a.now())
	for i, tile := range tiles {
		if tile.tab.key == key {
			a.wallMove(i, len(tiles))
			return
		}
	}
}

// teamMemberWord is how a note names a member: its title, else its handle.
func teamMemberWord(m teamMember) string {
	if w := strings.TrimSpace(m.Word); w != "" {
		return w
	}
	if m.Handle != "" {
		return "@" + m.Handle
	}
	return "a member"
}

// wallFocusedKey is the focused tile's conversation, "" for none.
func (a *app) wallFocusedKey(tiles []wallTile) string {
	if f := a.wall.focus; f >= 0 && f < len(tiles) {
		return tiles[f].tab.key
	}
	return ""
}

// wallRefocus puts the focus back after tiles left the wall. A focused tile
// that is still there keeps it, wherever the closing moved it to. One that
// went hands it to its nearest survivor on the right, which slides into the
// place it left, and at the end of the list to the one on its left, as
// closing a browser tab does: never back to the start.
func (a *app) wallRefocus(before []wallTile, key string) {
	if !a.wall.on {
		return
	}
	after := a.wallShown(a.now())
	at := make(map[string]int, len(after))
	for i, tile := range after {
		at[tile.tab.key] = i
	}
	if j, ok := at[key]; ok {
		a.wallMove(j, len(after))
		return
	}
	was := -1
	for i, tile := range before {
		if tile.tab.key == key {
			was = i
		}
	}
	for i := was + 1; was >= 0 && i < len(before); i++ {
		if j, ok := at[before[i].tab.key]; ok {
			a.wallMove(j, len(after))
			return
		}
	}
	for i := was - 1; i >= 0; i-- {
		if j, ok := at[before[i].tab.key]; ok {
			a.wallMove(j, len(after))
			return
		}
	}
	a.wallMove(0, len(after))
}

// wallCloseViews closes the view of every marked tile. The ones at rest go at
// once; the first with work in flight is asked about, on its page, and any
// others like it stay marked so nothing is closed that was not asked about.
func (a *app) wallCloseViews(tiles []wallTile) tea.Cmd {
	focused := a.wallFocusedKey(tiles)
	defer a.wallRefocus(tiles, focused)
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

// wallWheelSettle is how long one wheel step holds before the next in the
// same direction is taken. A tile row is a dozen lines or more, so a step per
// event would throw a trackpad's burst of thirty straight to the end; one per
// settle is a row per notch for a hand turning a wheel, and a few rows for a
// flick.
const wallWheelSettle = 90 * time.Millisecond

// wallWheel is a wheel notch over the wall at x,y: the VIEW moves one tile row,
// clamped to the list, as a page does, and the focus moves only when the
// scroll would leave it off screen, onto the same column of the nearest row
// that is on it. A notch the other way is taken at once: a hand reversing is
// not a burst.
func (a *app) wallWheel(x, y int, down bool) {
	a.wallSettle()
	a.wall.ptrX, a.wall.ptrY, a.wall.ptrIn = x, y, y >= a.wall.headRows
	dir := -1
	if down {
		dir = 1
	}
	now := a.now()
	if dir == a.wall.wheelDir && now.Sub(a.wall.wheelAt) < wallWheelSettle {
		return
	}
	a.wall.wheelDir, a.wall.wheelAt = dir, now
	// The help sheet scrolls under the wheel, and nothing behind it does; the
	// Organize card walks its rows, which scrolls it.
	if a.wall.help {
		a.wallHelpScroll(dir)
		return
	}
	if a.wall.org.on {
		if down {
			a.wallOrganizeKey("down")
		} else {
			a.wallOrganizeKey("up")
		}
		a.wall.stirred = true
		a.touch()
		return
	}
	// A popover hangs from a control that is about to move, so it goes, as a
	// menu does when the page under it scrolls.
	if a.wall.pop.kind != wallPopNone {
		a.wall.pop = wallPop{}
		a.wall.stirred = true
		a.touch()
	}
	n := len(a.wallShown(now))
	if n == 0 {
		return
	}
	cols, _ := a.wallGeometry(n)
	vis := a.wallRowsOnScreen(n)
	top := max((n+cols-1)/cols-vis, 0)
	scroll := min(max(a.wall.scroll+dir, 0), top)
	if scroll == a.wall.scroll {
		return
	}
	a.wall.scroll = scroll
	row, col := a.wall.focus/cols, a.wall.focus%cols
	row = min(max(row, scroll), scroll+vis-1)
	a.wall.focus = min(row*cols+col, n-1)
	a.wall.hover = wallHitRef{}
	a.wall.rehover = true
	a.wall.stirred = true
	a.touch()
}

// ── THE MOTION ──────────────────────────────────────────────────────────────
//
// Two small movements, both on the paint clock (app.go's [app.paint] keeps it
// turning while [app.wallAnimating] says so) and both functions of the time
// since they began, so a slow link draws the same motion in fewer frames
// rather than a slower one. Neither delays a key: every key and press settles
// the reveal first, and the zoom is drawn over a conversation that is
// already live.

// wallRevealSpan is the most the opening's reveal takes from the first tile
// row to the last, and wallRevealStep the most between two rows. Two rows
// arrive 45ms apart; four take 135ms. Past that it would be waiting.
const (
	wallRevealSpan = 135 * time.Millisecond
	wallRevealStep = 45 * time.Millisecond
)

// wallZoomFor is how long an opened tile takes to grow into the frame: three
// frames at the local cadence, on an ease-out, so most of the growth is in
// the first.
const wallZoomFor = 100 * time.Millisecond

// wallMotionOK says the wall may move at all. The linear tier is the
// screen-reader tier and draws no motion anywhere; the ASCII tier is a
// terminal that could not be trusted with the glyphs, and is not asked to
// animate either; and over a remote link a frame is a third as frequent, so a
// 140ms movement is one jump.
func (a *app) wallMotionOK() bool {
	return !a.wallReduced() && !a.remote
}

// wallReduced is the tiers that draw no motion at all, the working spinner
// included: the painter is told so ([wallView.reduced]) and draws a still
// mark. A remote link keeps its spinner, which already steps at the link's
// own cadence everywhere else on this surface.
func (a *app) wallReduced() bool {
	return a.linear || a.pal.linear || a.pal.ascii
}

// wallAnimating reports whether the wall has motion in flight that the paint
// clock must draw at its full cadence: the reveal or the zoom.
// It reads the wall's own fields and nothing else, because it is asked on
// every paint.
func (a *app) wallAnimating() bool {
	now := a.now()
	if !a.wall.zoomAt.IsZero() && now.Sub(a.wall.zoomAt) < wallZoomFor {
		return true
	}
	return a.wall.on && !a.wall.revealAt.IsZero()
}

// wallSpinning reports whether the wall is up with a live working tile, whose
// spinner wants a frame each step and nothing more.
func (a *app) wallSpinning() bool {
	return a.wall.on && a.wall.spinning
}

// wallSettle ends whatever is still moving on the wall at once, which is what
// any key or press does before it acts.
func (a *app) wallSettle() {
	if !a.wall.revealAt.IsZero() {
		a.wall.revealAt = time.Time{}
		a.touch()
	}
}

// wallZoomDone ends an opened tile's zoom at once.
func (a *app) wallZoomDone() {
	if !a.wall.zoomAt.IsZero() {
		a.wall.zoomAt = time.Time{}
		a.touch()
	}
}

// wallSpin is the working spinner's step for a frame drawn at now. It is
// counted in time at the spinner's own cadence rather than in paints, so a
// wall drawn on its half-second reading tick still shows the glyph the spinner
// has reached, not the one it had when the clock last turned.
func wallSpin(now time.Time) int {
	return int(now.UnixMilli() / int64(spinnerStep*frameInterval/time.Millisecond))
}

// wallRect is a rectangle of frame cells, inclusive-exclusive.
type wallRect struct{ x0, y0, x1, y1 int }

func (r wallRect) w() int { return r.x1 - r.x0 }

func (r wallRect) holds(x, y int) bool {
	return x >= r.x0 && x < r.x1 && y >= r.y0 && y < r.y1
}

// wallTileRect is where tile i was drawn on the last frame, from its own
// targets, in frame cells.
func (a *app) wallTileRect(i int) (wallRect, bool) {
	return wallTileBounds(a.wall.hits, i)
}

func wallTileBounds(hits []wallHit, i int) (wallRect, bool) {
	var r wallRect
	ok := false
	for _, hit := range hits {
		if !hit.ref().onTile(i) {
			continue
		}
		if !ok {
			r, ok = wallRect{hit.x0, hit.y0, hit.x1, hit.y1}, true
			continue
		}
		r.x0, r.y0 = min(r.x0, hit.x0), min(r.y0, hit.y0)
		r.x1, r.y1 = max(r.x1, hit.x1), max(r.y1, hit.y1)
	}
	return r, ok
}

// wallCardRect is where the help sheet, the popover, the Organize card or
// else the new-team card lands on this frame, so a press can be told to be on it or off it. It is laid out
// only while one is up, and a card is a handful of short rows.
func (a *app) wallCardRect(v wallView, width, room, head int) wallRect {
	var card wallCard
	g := wallGlyphsFor(a.pal.ascii)
	switch {
	case v.help:
		card = wallHelpCard(a.pal, v, width, room)
		a.wall.helpMax = card.over
	case v.org.on:
		card = wallOrgCard(a.pal, g, v, width, room)
		a.wall.org.top = card.top
	case v.pop.kind != wallPopNone && !v.naming:
		card = wallPopCard(a.pal, g, v, width, room)
	case v.naming:
		card = wallNameCard(a.pal, g, v, width, room)
	}
	if len(card.rows) == 0 {
		return wallRect{}
	}
	return wallRect{card.x, card.y + head, card.x + card.w, card.y + head + len(card.rows)}
}

// wallRevealMask blanks the tiles whose row has not come in yet, in place,
// and ends the reveal once the last row is due. The tiles were painted
// whole, from their cached rows, so the reveal costs a blanking of cells for
// a handful of frames and never a second drawing of a body. A card or the
// tray over the grid ends it at once: a reveal is for the tiles alone, and a
// half-blanked card would read as a fault.
func (a *app) wallRevealMask(rows []string, hits []wallHit, n, cols, tileH, room, width int, now time.Time) {
	if a.wall.revealAt.IsZero() {
		return
	}
	if a.wall.naming || a.wall.help || a.wall.org.on || a.wall.pop.kind != wallPopNone || len(a.wall.marked) > 0 || n == 0 || cols < 1 {
		a.wall.revealAt = time.Time{}
		return
	}
	vis := max(wallVisibleRows(room, tileH), 1)
	step := wallRevealStep
	if vis > 1 && wallRevealSpan/time.Duration(vis-1) < step {
		step = wallRevealSpan / time.Duration(vis-1)
	}
	gone := now.Sub(a.wall.revealAt)
	if gone >= step*time.Duration(vis-1) {
		a.wall.revealAt = time.Time{}
		return
	}
	first := a.wall.scroll * cols
	for i := first; i < min(first+vis*cols, n); i++ {
		row := (i - first) / cols
		if gone >= step*time.Duration(row) {
			continue
		}
		r, ok := wallTileBounds(hits, i)
		if !ok {
			continue
		}
		blank := strings.Repeat(" ", r.w())
		for y := max(r.y0, 0); y < min(r.y1, len(rows)); y++ {
			rows[y] = wallSplice(rows[y], blank, r.x0, width)
		}
	}
}

// wallZoomed is the frame of the conversation a tile just opened, drawn
// inside the tile's rectangle growing into the whole frame, with a quiet
// border on its edge. The conversation is already in front: this only frames
// its first few pictures, so a key typed on the first of them lands in its
// box. It hands the frame back untouched once the zoom is over, and forgets
// the zoom.
func (a *app) wallZoomed(frame string) string {
	if a.wall.zoomAt.IsZero() {
		return frame
	}
	gone := a.now().Sub(a.wall.zoomAt)
	if gone >= wallZoomFor || a.wall.on || !a.wallMotionOK() {
		a.wall.zoomAt = time.Time{}
		return frame
	}
	width, height := a.size()
	t := float64(gone) / float64(wallZoomFor)
	t = 1 - (1-t)*(1-t)
	from := a.wall.zoomFrom
	lerp := func(a, b int) int { return a + int(float64(b-a)*t+0.5) }
	r := wallRect{lerp(from.x0, 0), lerp(from.y0, 0), lerp(from.x1, width), lerp(from.y1, height)}
	if r.x1-r.x0 < 2 || r.y1-r.y0 < 2 {
		return frame
	}
	rows := strings.Split(frame, "\n")
	edge := a.pal.dim
	inner := r.x1 - r.x0 - 2
	for y := range rows {
		switch {
		case y < r.y0 || y >= r.y1:
			rows[y] = ""
		case y == r.y0:
			rows[y] = strings.Repeat(" ", r.x0) + edge("╭"+strings.Repeat("─", inner)+"╮")
		case y == r.y1-1:
			rows[y] = strings.Repeat(" ", r.x0) + edge("╰"+strings.Repeat("─", inner)+"╯")
		default:
			rows[y] = strings.Repeat(" ", r.x0) + edge("│") + wallFit(ansi.Cut(rows[y], r.x0+1, r.x1-1), inner) + "\x1b[0m" + edge("│")
		}
	}
	return strings.Join(rows, "\n")
}

func dropLastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(r[:len(r)-1])
}

// wallTeamLabel is a team's name on the wall's flat Teams row: `harbor › api`
// for a team inside another (its parent's name first, so two teams called api
// are told apart), and the bare name at the top level. The row stays one flat
// row (ruling c-12); the tree is the teams page's and the switcher's. Each part
// is cut on its own, so the team's own name is never the part that is lost.
func (a *app) wallTeamLabel(t team) string {
	name := t.Name
	if ansi.StringWidth(name) > wallChipCap {
		name = ansi.Truncate(name, wallChipCap, a.linearMark("…", "~"))
	}
	p, ok := a.teamByID(t.Parent)
	if !ok || p.Root {
		return name
	}
	parent := p.Name
	if ansi.StringWidth(parent) > wallChipCap/2 {
		parent = ansi.Truncate(parent, wallChipCap/2, a.linearMark("…", "~"))
	}
	return parent + " " + a.linearMark("›", ">") + " " + name
}

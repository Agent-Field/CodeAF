package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// ── THE MANAGER'S REAL CONVERSATION, HOSTED ON THE TEAMS PAGE ───────────────
//
// The ruling (c-2) is that the right pane of the teams page IS the selected
// team's manager conversation, the full chat: typing steers it, its prompts are
// answered in it, its Traffic rail is there. Not a render of it: the same
// conversation, with every key and every press it would have had.
//
// SO THE PAGE DOES NOT DRAW A CONVERSATION; IT LENDS ONE A RECTANGLE. While the
// pane hosts the manager ([app.teamsHosting]), the conversation's own geometry
// is the terminal less the rail on the left: [app.size] answers the narrower
// width, so every layout, scroll and hit test the conversation makes resolves
// against the cells it is really drawn in. The frame is the conversation's own
// ([app.chatFrameLines]) with two changes: the head's rows are the places'
// head at the full width (so the bar still says `teams`), and the page's header,
// members and inbox are pinned rows between the head and the transcript,
// charged in [app.topHeight] exactly as the task strip is, so the scrolling
// knows they are there. The rail is joined on the left of every row under the
// head.
//
// EVERY KEY AND POINTER EVENT THE PAGE DOES NOT TAKE IS HANDED TO THE
// CONVERSATION as if no place were standing ([app.teamsRoute]): the page is
// set aside for the length of that one message, the pointer's column is moved
// into the conversation's own cells, and the conversation answers exactly as
// it does on its own screen. The page keeps `tab`, the digits and the map (the
// place grammar), `alt+↑↓` (the page's buttons), and presses on its own rows.
//
// THE MANAGER IS BROUGHT IN FRONT when its team is selected
// ([app.teamsBringManager]), and that is the one move of the front this page
// makes: choosing a team is choosing whom to talk to. The conversation that was
// in front stays open behind.

// teamsHosting reports whether the pane hosts the manager now: the page is
// standing (or a message is being handed to the conversation), the selected
// team's manager is the conversation in front, and no surface that takes the
// whole frame is up over it. It is asked by [app.size] on every layout, so it
// reads a handful of fields and allocates nothing.
func (a *app) teamsHosting() bool {
	return a.tp.host != "" && (a.at(pageTeams) || a.tp.forwarding) && !a.wall.on && !a.setup.open &&
		!a.railTaskPlanOn && !a.workTabOn && !a.pasteEdit.open && a.tp.host == a.frontTabKey()
}

// teamsHostRail is the rail's columns while the pane hosts the manager, its
// separator included, and 0 when it does not.
func (a *app) teamsHostRail() int {
	if !a.teamsHosting() {
		return 0
	}
	return a.tp.railW
}

// teamsSync settles the page after every message: the selection, the manager
// the pane hosts, and the rail's width. It reads memory only, and costs one
// comparison on every other place.
func (a *app) teamsSync() {
	if !a.at(pageTeams) {
		a.tp.host = ""
		return
	}
	a.teamsSettle()
	host := ""
	if t, ok := a.teamsSelected(); ok && !t.Closed() && t.Manager != "" && !a.teamsOff() && t.Manager == a.frontTabKey() {
		host = t.Manager
		a.tp.opening = ""
	}
	if host != a.tp.host {
		a.tp.host = host
		a.tp.top = teamsTopCache{}
		a.touch()
	}
	if host == "" {
		a.tp.focus = true
	}
	a.tp.railW = teamsRailCols(a.width)
}

// ── THE FRAME ───────────────────────────────────────────────────────────────

// teamsHostTopHeight is the page's pinned rows over the hosted transcript,
// charged in [app.topHeight]. Zero when the pane hosts nothing.
func (a *app) teamsHostTopHeight() int {
	if !a.teamsHosting() {
		return 0
	}
	width, _ := a.size()
	return len(a.teamsHostTop(width))
}

// teamsHostTop is the page's header, members and inbox as rows over the hosted
// transcript, kept between frames while nothing they are drawn from moved.
// Their targets are recorded in frame cells.
func (a *app) teamsHostTop(width int) []string {
	t, _ := a.teamsSelected()
	key := teamsTopKey{
		width: width, edits: a.traffic.edits, stamp: a.traffic.stamp, sel: a.tp.sel,
		cur: a.tp.cur, hot: a.tp.hot, focus: a.tp.focus, sig: a.teamsTopSig(t),
		minute: a.now().Unix() / 60, answering: a.tp.answering, answer: string(a.tp.answer.value),
		expand: a.tp.expand, ascii: a.pal.ascii, linear: a.linear, undoing: a.teamsUndoing(),
	}
	if c := &a.tp.top; c.ok && c.key == key {
		return c.rows
	}
	d := &teamsDraw{a: a}
	rows := a.teamsTop(d, width)
	rows = append(rows, a.pal.dim(rule(width)))
	for i := range rows {
		rows[i] = teamsPad(rows[i], width)
	}
	a.tp.top = teamsTopCache{key: key, rows: rows, targets: d.targets, ok: true}
	return rows
}

// teamsUndoRow is the one row that offers Undo for a close, while it is offered.
func (a *app) teamsUndoRow(d *teamsDraw, width, y int) []string {
	if !a.teamsUndoing() {
		return nil
	}
	pal := a.pal
	word := " " + pal.dim(a.tp.undo.name+" is closed") + "  "
	s, _ := d.button("Undo", teamsTarget{act: teamsActUndo, x0: ansi.StringWidth(word), y: y,
		hint: "Reopen " + a.tp.undo.name + " and its tabs" + hintSegment + "u"}, pal.ink)
	return []string{word + s}
}

// teamsHostFrame is the hosted page's whole frame, and false when the pane
// hosts nothing (the shared place frame draws the page then).
func (a *app) teamsHostFrame() ([]string, []placeHit, int, int, bool) {
	if !a.teamsHosting() {
		return nil, nil, 0, 0, false
	}
	width, height := max(a.width, 8), max(a.height, 1)
	railW := a.tp.railW
	paneW, _ := a.size()
	// THE CONVERSATION'S OWN FRAME, laid out in its own cells: the page is set
	// aside for the draw so every surface inside it answers as it does in the
	// conversation.
	a.tp.forwarding, a.page = true, pageNone
	chat, caretX, caretY := a.chatFrameLines(paneW, height)
	headN := a.tabsHeight(paneW) + a.headSealHeight(paneW)
	topAt := a.headHeight() + a.stripHeight()
	a.tp.forwarding, a.page = false, pageTeams
	// THE HEAD IS THE PLACES' HEAD, AT THE FULL WIDTH, drawn exactly as every
	// place draws it, so the bar says where the person is standing.
	was := a.pal
	a.pal = was.onPlaces()
	a.pal.placeRows = true
	a.tabRow = -1
	head := []string(nil)
	if headN > 0 {
		a.tabRow = placeTabRow
		head = a.headRows(width, a.placeTabBar(width, a.mapShowing, a.pal), a.pal)
		for len(head) < headN {
			head = append(head, "")
		}
		head = head[:headN]
	}
	a.pal = was
	d := &teamsDraw{a: a}
	var rail []string
	if railW > 0 {
		rail = a.teamsRail(d, railW-1, height-headN)
		d.shift(0, 0, headN)
	}
	// The pane's own rows were recorded as they were drawn; they are moved into
	// frame cells here.
	for _, t := range a.tp.top.targets {
		t.x0 += railW
		t.x1 += railW
		t.y += topAt
		d.targets = append(d.targets, t)
	}
	a.tp.targets = d.targets
	sep := a.pal.dim(a.linearMark("│", "|"))
	lines := make([]string, 0, height)
	lines = append(lines, head...)
	for i := headN; i < height; i++ {
		row := ""
		if i < len(chat) {
			row = chat[i]
		}
		left := ""
		if railW > 0 {
			left = strings.Repeat(" ", railW-1)
			if r := i - headN; r < len(rail) {
				left = rail[r]
			}
			left += sep
		}
		lines = append(lines, left+row)
	}
	// A reply is read once its transcript is on screen, as in the conversation.
	delete(a.unreadChats, a.frontTabKey())
	return lines, nil, caretX + railW, caretY, true
}

// ── THE KEYS AND THE POINTER ────────────────────────────────────────────────

// teamsRoute takes what the page keeps of a message and hands the rest to the
// conversation it hosts. It is read at the top of [app.route], and answers
// false at once on every other place and with a card or a menu up.
func (a *app) teamsRoute(msg tea.Msg) (tea.Cmd, bool) {
	if !a.at(pageTeams) || a.tp.forwarding || a.tsheet.on || a.teamMenu.on || a.wall.on {
		return nil, false
	}
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		return a.teamsRouteKey(m)
	case tea.MouseClickMsg:
		return a.teamsRouteMouse(m, m.Mouse())
	case tea.MouseReleaseMsg:
		return a.teamsRouteMouse(m, m.Mouse())
	case tea.MouseMotionMsg:
		return a.teamsRouteMouse(m, m.Mouse())
	case tea.MouseWheelMsg:
		return a.teamsRouteMouse(m, m.Mouse())
	}
	return nil, false
}

// teamsRouteKey is a key on the page. The page's own keys are read when it has
// the keyboard; a hosted page hands everything else to the composer.
func (a *app) teamsRouteKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	if key == "ctrl+c" {
		return nil, false
	}
	if key == "u" && a.teamsUndoing() && a.teamsHasKeys() {
		return a.teamsUndoClose(), true
	}
	if !a.teamsHosting() {
		// The shared place grammar and the page's own keys ([placeTeams.key]).
		return nil, false
	}
	switch key {
	case "alt+up", "alt+down":
		a.tp.focus = true
		if a.teamsCursorIndex() < 0 {
			a.teamsCursorHome()
		} else if key == "alt+up" {
			a.teamsWalk(0, -1)
		} else {
			a.teamsWalk(0, 1)
		}
		a.tp.top = teamsTopCache{}
		a.touch()
		return nil, true
	case "tab", "shift+tab":
		if a.menu.open || a.comp.open {
			return a.teamsForward(msg), true
		}
		return a.walkPage(key == "shift+tab"), true
	case placeMapKey:
		a.mapShowing = !a.mapShowing
		a.touch()
		return nil, true
	}
	if cmd, took := a.placeJumpKey(msg); took {
		return cmd, true
	}
	if a.tp.answering != "" {
		return a.teamsAnswerKey(msg), true
	}
	if a.tp.focus {
		if cmd, took := a.teamsKey(msg); took {
			a.tp.top = teamsTopCache{}
			return cmd, true
		}
		// ANY OTHER KEY GOES BACK TO THE COMPOSER, and types there: a person
		// who starts a sentence has stopped choosing a button.
		a.tp.focus = false
		a.tp.top = teamsTopCache{}
	}
	return a.teamsForward(msg), true
}

// teamsRouteMouse is a pointer event on the page: the rail and the page's own
// rows answer, the head is the router's, and everything over the hosted
// conversation is handed to it with its column moved into its own cells.
func (a *app) teamsRouteMouse(msg tea.Msg, m tea.Mouse) (tea.Cmd, bool) {
	hosted := a.teamsHosting()
	if m.Y < placeHeadRows && a.tabRow >= 0 {
		if !hosted {
			return nil, false
		}
		// The bar and the rest of the head are the router's, as on every place.
		return nil, false
	}
	if t, ok := a.teamsTargetAt(m.X, m.Y); ok {
		switch msg.(type) {
		case tea.MouseClickMsg:
			if m.Button == tea.MouseLeft {
				a.tp.hot = t.ref()
				return a.teamsDo(t), true
			}
		case tea.MouseMotionMsg:
			a.teamsHover(m.X, m.Y)
			return nil, true
		}
	}
	if _, isMotion := msg.(tea.MouseMotionMsg); isMotion && a.tp.hot != (teamsRef{}) {
		a.tp.hot = teamsRef{}
		a.tp.top = teamsTopCache{}
		a.touch()
	}
	if !hosted {
		if _, click := msg.(tea.MouseClickMsg); click {
			return nil, true
		}
		return nil, false
	}
	railW := a.tp.railW
	if m.X < railW {
		return nil, true
	}
	topAt := a.headHeight() + a.stripHeight()
	if m.Y >= topAt && m.Y < topAt+a.teamsHostTopHeight() {
		return nil, true
	}
	// THE CONVERSATION'S OWN CELLS: the column moved by the rail, and the
	// conversation answers as it does on its own screen.
	m.X -= railW
	switch msg.(type) {
	case tea.MouseClickMsg:
		return a.teamsForward(tea.MouseClickMsg(m)), true
	case tea.MouseReleaseMsg:
		return a.teamsForward(tea.MouseReleaseMsg(m)), true
	case tea.MouseMotionMsg:
		return a.teamsForward(tea.MouseMotionMsg(m)), true
	case tea.MouseWheelMsg:
		return a.teamsForward(tea.MouseWheelMsg(m)), true
	}
	return nil, false
}

// teamsForward hands one message to the hosted conversation as if no place were
// standing, and puts the page back after it unless the message itself went
// somewhere else (a place, a page, another conversation's own screen).
func (a *app) teamsForward(msg tea.Msg) tea.Cmd {
	a.tp.forwarding, a.page = true, pageNone
	_, cmd := a.route(msg)
	a.tp.forwarding = false
	if !a.pageShowing() {
		a.page = pageTeams
	} else if !a.at(pageTeams) {
		// The message walked to another place; this one closes as it would
		// have under the router.
		placeTeams{}.close(a)
	}
	return cmd
}

// teamsPageHint is what the hint line says over the page's own rows while the
// pane hosts the conversation, and the page's two keys otherwise.
func (a *app) teamsPageHint() string {
	if !a.teamsHosting() {
		return ""
	}
	words := a.teamsTargetHint()
	if words != "" && a.tp.focus {
		words += hintSegment + "esc back to the box"
	}
	return words
}

// teamsComposerWord is the composer's `to` while the pane hosts the manager:
// `to ◆ harbor manager`, which says which team the words go to as well as who.
func (a *app) teamsComposerWord() string {
	if !a.teamsHosting() {
		return ""
	}
	t, ok := a.teamsSelected()
	if !ok {
		return ""
	}
	name := t.Name
	if t.Root {
		name = "all teams"
	}
	return "to " + a.teamManagerMark() + " " + name + " manager"
}

// trafficHidden reports whether the Traffic rail is put away: the window's own
// answer, and on the teams page the page's, which starts folded because the
// page has a rail of its own on the left. `alt+t` or the grip unfolds it.
func (a *app) trafficHidden() bool {
	if a.teamsHosting() {
		return !a.tp.traffic
	}
	return a.traffic.hidden
}

package tui3

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── THE CENTER OF THE ITEM PAGE IS THE REAL CHAT ────────────────────────────
//
// The owner's layout (2026-10-09): selecting a step shows that step's chat
// in the center, and selecting the manager shows the manager's, THE SAME
// VIEW AS A NORMAL CODEAF CHAT, its tasks panel on the right included. Not a
// render of the transcript: the conversation itself, live, scrollable, with
// its own box to type into.
//
// SO THE PAGE DOES NOT DRAW A CHAT; IT LENDS ONE A RECTANGLE, the way the
// teams page once hosted its manager. While the center hosts a chat
// ([app.factoryHosting]) the conversation in front IS that chat, and its own
// geometry is the terminal less the left column: [app.size] answers the
// narrower width, and [app.topHeight] charges the page's head and top bar,
// so every layout, scroll and hit test the chat makes resolves against the
// cells it is really drawn in. The frame is the chat's own
// ([app.chatFrameLines]) with the places' head and the top bar across the
// whole width over it, and the left column joined on the left of every row
// under them.
//
// THE CHAT IS BROUGHT IN FRONT BY SELECTING ITS ROW ([app.factoryHostSync],
// after every message): the manager's chat is the item's own conversation
// ([factory.Item.Talk]), a step's is its phase's ([factory.Phase.Chat]).
// It is opened the way a stage room has always been opened, through the
// window's own door ([Options.Open]), off the loop for the folder and on it
// for the open; the conversation that was in front goes on running behind,
// one tab away. One attempt per selection: a refusal is said in the center,
// and choosing the row again asks again.
//
// THE KEYS WALK THE PAGE UNTIL THE BOX HAS THEM. `→`, `tab`, `enter` on the
// row, or a press on the chat puts them in the box ([factoryPage.box]);
// then every key is the chat's, the arrows editing the words, until `esc` or
// `tab` gives them back to the page. A press on the left column or the bar
// gives them back too. While the keys walk the page the chat's caret is not
// drawn, because a caret is a promise that a letter lands there.
//
// A TERMINAL TOO NARROW FOR THE COLUMN HOSTS NOTHING: under
// [factoryStageFloor] the center is the page's own, and `enter` on a step
// with a chat walks into it the way a room is walked into.

// factoryHost is the chat the center hosts and how it got there.
type factoryHost struct {
	// key is the hosted chat's identity while it is the conversation in
	// front, "" while the center hosts none.
	key string
	// want is the transcript the selected row wants in the center, and
	// wantKey its identity, read once per want (it is a disk question).
	want    string
	wantKey string
	// asked is the transcript an open was last asked for, so one selection
	// asks once; why is what that open said when it refused.
	asked string
	why   string
	// forwarding says a message is being handed to the hosted chat, or the
	// chat's frame is being drawn, with the page set aside for it.
	forwarding bool
	// focus says the box takes the keys as soon as the chat being brought
	// lands: `enter` asked for the manager's chat and is waiting on it.
	focus bool
	// talks is the manager's chat the Talk door answered, by item, for a
	// floor read that does not carry it yet.
	talks map[int]string
}

// factoryCanHost says whether the page can host a chat at all on this
// window: a door to open conversations, and the width for the column.
func (a *app) factoryCanHost() bool {
	return a.canOpen() && a.width >= factoryStageFloor
}

// factoryChatOf is the transcript the row wants in the center: the item's
// own conversation on the manager row, a step's chat on a step row that ran
// as one, and "" on every other row.
func (a *app) factoryChatOf(it factory.Item, r factoryPageRow) string {
	switch r.kind {
	case factoryPageManager:
		if talk := strings.TrimSpace(it.Talk); talk != "" {
			return talk
		}
		return a.fp.host.talks[it.ID]
	case factoryPageStage:
		return factoryRoomOf(r)
	}
	return ""
}

// factoryTalkHere is `enter` on the issue or the manager row, and `T` on the
// item page: the manager's chat, IN THE CENTER. The cursor goes to the
// manager row and the box takes the keys once the chat is in front; an item
// with no conversation yet asks the Talk door for one first, off the loop,
// and the floor is read in the same ask so the row learns it has one.
func (a *app) factoryTalkHere(it factory.Item) tea.Cmd {
	for i, row := range a.factoryItemRows(it) {
		if row.kind == factoryPageManager {
			a.factoryStageSelect(i)
			break
		}
	}
	h := &a.fp.host
	h.focus = true
	if a.factoryChatOf(it, factoryPageRow{kind: factoryPageManager}) != "" {
		return nil
	}
	seam, id := a.factory, it.ID
	a.fp.act.doing = "opening " + it.Ref() + "'s conversation…"
	return a.offLoop(func() func(bool) tea.Cmd {
		chat, err := seam.Talk(context.Background(), id)
		var snap factory.Snapshot
		var lerr error
		if seam.Load != nil {
			snap, lerr = seam.Load()
		}
		return func(bool) tea.Cmd {
			a.fp.act.doing = ""
			switch {
			case lerr != nil:
				a.fp.err = lerr
			case seam.Load != nil:
				a.factoryFold(snap)
			}
			if err != nil {
				a.fp.host.focus = false
				a.pageMsg = strings.TrimSpace(err.Error())
				a.touch()
				return nil
			}
			chat = strings.TrimSpace(chat)
			if chat == "" {
				// NO CHAT CAME BACK TO HOST: the manager's own box takes the
				// keys instead, and the Say door makes the chat on the first
				// words.
				a.fp.host.focus = false
				if it, ok := a.factoryItemByID(id); ok {
					a.factoryTLOpenBox(it)
				}
				a.touch()
				return nil
			}
			if a.fp.host.talks == nil {
				a.fp.host.talks = map[int]string{}
			}
			a.fp.host.talks[id] = chat
			a.touch()
			return nil
		}
	})
}

// factoryHosting reports whether the center hosts its chat now: the item
// page is standing (or a message is being handed to the chat), the selected
// row's chat is the conversation in front, and nothing that takes the whole
// frame is over it. It is asked by [app.size] on every layout, so it reads a
// handful of fields and allocates nothing.
func (a *app) factoryHosting() bool {
	h := &a.fp.host
	if h.key == "" || !a.fp.open || !(a.at(pageFactory) || h.forwarding) {
		return false
	}
	if a.wall.on || a.setup.open || a.pasteEdit.open || a.fp.keys || a.fp.pick != nil || a.fp.recipe != nil {
		return false
	}
	return a.width >= factoryStageFloor && h.key == a.frontTabKey()
}

// factoryHostLeft is the columns the left column and its rule take from the
// chat while the center hosts it, and 0 when it does not.
func (a *app) factoryHostLeft() int {
	if !a.factoryHosting() {
		return 0
	}
	return factoryItemColW + factoryRuleW
}

// factoryHostTopHeight is the top bar's rows over the hosted chat, charged
// in [app.topHeight]. Zero when the center hosts nothing.
func (a *app) factoryHostTopHeight() int {
	if !a.factoryHosting() {
		return 0
	}
	it, ok := a.factoryCursorItem()
	if !ok {
		return 0
	}
	return a.factoryBarCount(it)
}

// factoryHostSync settles which chat the center hosts, after every message:
// the selected row's chat when it is in front, and an open asked for it when
// it is not and none was asked for this selection. It reads memory only on
// every other place.
func (a *app) factoryHostSync() tea.Cmd {
	h := &a.fp.host
	if !a.at(pageFactory) || !a.fp.open || h.forwarding {
		if !a.at(pageFactory) || !a.fp.open {
			if h.key != "" || h.want != "" {
				*h = factoryHost{}
			}
			a.fp.box = false
		}
		return nil
	}
	it, ok := a.factoryCursorItem()
	if !ok {
		return nil
	}
	want := ""
	if r, ok := a.factoryPageRowAt(it); ok && a.factoryCanHost() {
		want = a.factoryChatOf(it, r)
	}
	if want != h.want {
		if h.want != "" {
			h.focus = false
		}
		h.want, h.wantKey, h.why = want, "", ""
		if want != "" {
			h.wantKey = a.convKey(want)
		}
	}
	if want == "" {
		if h.key != "" {
			h.key = ""
			a.touch()
		}
		a.fp.box = false
		return nil
	}
	if h.wantKey != "" && h.wantKey == a.frontTabKey() {
		if h.key != h.wantKey {
			h.key, h.why = h.wantKey, ""
			a.touch()
		}
		if h.focus {
			h.focus, a.fp.box = false, true
		}
		return nil
	}
	if h.key != "" {
		h.key = ""
		a.touch()
	}
	a.fp.box = false
	if h.asked == want {
		return nil
	}
	h.asked = want
	return a.factoryHostBring(want)
}

// factoryHostBring brings transcript chat in front without leaving the page:
// at once when this window holds it behind, and otherwise its folder read
// off the loop and the open asked on it, only if the page still wants it
// when the folder comes back.
func (a *app) factoryHostBring(chat string) tea.Cmd {
	if a.holding(chat) {
		cmd, _ := a.bringForward(chat)
		a.touch()
		return cmd
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		where := factoryChatFolder(chat)
		return func(bool) tea.Cmd {
			h := &a.fp.host
			if h.want != chat || !a.at(pageFactory) || !a.fp.open {
				if h.asked == chat {
					h.asked = ""
				}
				return nil
			}
			cmd, refusal := a.openBeside(where, chat)
			if refusal != "" {
				h.why = refusal
			}
			a.touch()
			return cmd
		}
	})
}

// factoryHostWaitWords is what the center says while the selected row's chat
// is not in front yet: that it is opening, or why it did not open. "" when
// the row wants no chat, or it is in front.
func (a *app) factoryHostWaitWords() string {
	h := &a.fp.host
	if h.want == "" || h.key != "" {
		return ""
	}
	if h.why != "" {
		return "the chat did not open" + rowSep + h.why
	}
	return "opening the chat…"
}

// ── THE FRAME ───────────────────────────────────────────────────────────────

// factoryHostFrame is the item page's whole frame while the center hosts a
// chat, and false when it hosts none (the shared place frame draws the page
// then).
func (a *app) factoryHostFrame() ([]string, int, int, bool) {
	if !a.factoryHosting() {
		return nil, 0, 0, false
	}
	it, ok := a.factoryCursorItem()
	if !ok {
		return nil, 0, 0, false
	}
	width, height := max(a.width, 8), max(a.height, 1)
	left := factoryItemColW + factoryRuleW
	paneW, _ := a.size()
	// THE CHAT'S OWN FRAME, laid out in its own cells: the page is set aside
	// for the draw, so every surface inside it answers as it does in the
	// conversation.
	h := &a.fp.host
	h.forwarding, a.page = true, pageNone
	chat, caretX, caretY := a.chatFrameLines(paneW, height)
	headN := a.tabsHeight(paneW) + a.headSealHeight(paneW)
	h.forwarding, a.page = false, pageFactory
	// THE HEAD IS THE PLACES' HEAD AND THE BAR IS THE PAGE'S, AT THE FULL
	// WIDTH, drawn in the places' palette as the page draws them.
	was := a.pal
	a.pal = was.onPlaces()
	a.pal.placeRows = true
	defer func() { a.pal = was }()
	a.tabRow = -1
	var head []string
	if headN > 0 {
		head = a.headRows(width, "", a.pal)
		for len(head) < headN {
			head = append(head, "")
		}
		head = head[:headN]
	}
	rows := a.factoryItemRows(it)
	a.fp.stage = moveCursor(a.fp.stage, 0, len(rows))
	g := &a.fp.geo
	*g = factoryItemGeo{barY: headN, ctlX0: -1, ctlX1: -1, boxY: -1, hosted: true}
	bar := a.factoryBarRows(it, width)
	g.leftY, g.leftW = headN+len(bar), factoryItemColW
	g.centerX, g.centerY = left, g.leftY
	col := a.factoryItemColumn(it, rows, max(height-g.leftY, 0))
	sep := a.pal.dim(a.linearMark("│", "|"))
	lines := make([]string, 0, height)
	lines = append(lines, head...)
	lines = append(lines, bar...)
	for i := len(lines); i < height; i++ {
		row := ""
		if i < len(chat) {
			row = chat[i]
		}
		c := factorySpaces(factoryItemColW)
		if r := i - g.leftY; r >= 0 && r < len(col) {
			c = col[r]
		}
		lines = append(lines, c+sep+row)
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	// A reply is read once its transcript is on screen, as in the chat.
	delete(a.unreadChats, a.frontTabKey())
	// THE CARET IS THE BOX'S ONLY WHILE THE BOX HAS THE KEYS.
	if !a.fp.box {
		a.caret = false
	}
	return lines, caretX + left, caretY, true
}

// ── THE KEYS AND THE POINTER ────────────────────────────────────────────────

// factoryRoute takes what the page keeps of a message while the center
// hosts a chat and hands the rest to the chat. It is read at the top of
// [app.route], and answers false at once on every other place, with an
// overlay of the page up, and while a message is already being handed on.
func (a *app) factoryRoute(msg tea.Msg) (tea.Cmd, bool) {
	if a.fp.host.forwarding || !a.factoryHosting() || a.mapShowing || a.bar.on {
		return nil, false
	}
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		return a.factoryRouteKey(m)
	case tea.PasteMsg:
		if a.fp.box {
			return a.factoryForward(m), true
		}
	case tea.MouseClickMsg:
		return a.factoryRouteMouse(m, m.Mouse())
	case tea.MouseReleaseMsg:
		return a.factoryRouteMouse(m, m.Mouse())
	case tea.MouseMotionMsg:
		return a.factoryRouteMouse(m, m.Mouse())
	case tea.MouseWheelMsg:
		return a.factoryRouteMouse(m, m.Mouse())
	}
	return nil, false
}

// factoryRouteKey is a key while the center hosts a chat: with the box
// focused every key is the chat's but `esc` and `tab`, which give the keys
// back to the page (a running turn is paused from the bar, not from the
// box); with the keys walking the page, the page has them.
func (a *app) factoryRouteKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.fp.box {
		return nil, false
	}
	switch msg.String() {
	case "ctrl+c":
		return nil, false
	case "esc", "tab", "shift+tab":
		a.fp.box = false
		a.touch()
		return nil, true
	}
	return a.factoryForward(msg), true
}

// factoryRouteMouse is a pointer event while the center hosts a chat: over
// the chat it is the chat's, its column moved into the chat's own cells, and
// a press there puts the keys in the box; everywhere else it is the page's,
// and a press there gives the keys back to the page.
func (a *app) factoryRouteMouse(msg tea.Msg, m tea.Mouse) (tea.Cmd, bool) {
	g := &a.fp.geo
	if m.Y < g.centerY || m.X < g.centerX {
		if _, click := msg.(tea.MouseClickMsg); click && a.fp.box {
			a.fp.box = false
			a.touch()
		}
		return nil, false
	}
	if a.fp.hot != (factoryItemHot{}) {
		a.fp.hot = factoryItemHot{}
		a.fp.crumbHover = factoryCrumbNone
		a.touch()
	}
	m.X -= g.centerX
	switch msg.(type) {
	case tea.MouseClickMsg:
		a.fp.box = true
		a.touch()
		return a.factoryForward(tea.MouseClickMsg(m)), true
	case tea.MouseReleaseMsg:
		return a.factoryForward(tea.MouseReleaseMsg(m)), true
	case tea.MouseMotionMsg:
		return a.factoryForward(tea.MouseMotionMsg(m)), true
	case tea.MouseWheelMsg:
		return a.factoryForward(tea.MouseWheelMsg(m)), true
	}
	return nil, false
}

// factoryForward hands one message to the hosted chat as if no place were
// standing, and puts the page back after it unless the message itself went
// somewhere else (another place, or a page of its own).
func (a *app) factoryForward(msg tea.Msg) tea.Cmd {
	h := &a.fp.host
	h.forwarding, a.page = true, pageNone
	_, cmd := a.route(msg)
	h.forwarding = false
	if !a.pageShowing() {
		a.page = pageFactory
	}
	return cmd
}

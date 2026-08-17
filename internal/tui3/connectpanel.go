package tui3

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/connect"
)

// THE CONNECTIONS PANEL: /connect.
//
// The offer beside it (connect.go) is what a person meets when the agent needs
// an account it does not have. This is the same subject asked the other way
// round — "what have I connected, and what else could I" — and it is a LIST,
// which is a thing this surface already knows how to draw:
//
//	  ✓ Google              jane@example.com
//
//	  · Slack               your channels and messages
//
// Five decisions, the last two of which are what a CATALOG did to this panel:
//
//   - IT IS THE OVERLAY GRAMMAR, unchanged (palette.go). A short list under the
//     draft, ↑↓ to move, enter to act, esc to leave — the same rows the model
//     picker and the session picker draw, because a second list that behaved
//     differently would be a second thing to learn for a question that is the
//     same shape.
//   - TWO GLYPHS AND A NAME. A tick where the account is held, a dim dot where
//     it is not, and then the word a person owns the account by. What is dim on
//     the right is whichever fact the row actually has: the account when there
//     is one, and otherwise the one line that says what connecting it would be
//     for. Never both, and never a placeholder for either.
//   - ENTER MEANS THE OBVIOUS THING IN EVERY DIRECTION. On a browser row that is
//     not connected it starts the sign-in — the same browser, the same waiting
//     block, the same tick as the offer's own path, because it IS that path. On
//     a key row it opens the same box the offer opens, in the same place the
//     filter was. On a connected row it asks once and disconnects on the second
//     press: an account is a thing somebody else's session may be using, and one
//     keystroke is not enough of a decision to drop it.
//   - CONNECTED FIRST, THEN A GAP, THEN THE REST. The list answers two questions
//     and they are not the same question: "what have I got" is a handful of rows
//     a person recognizes, and "what else is there" is a catalog. Putting the
//     first above the second, separated by one blank row, means the answer to
//     the first is never further than the top of the list — and the blank is a
//     BLANK and not a rule, because a rule labelled "available" would be
//     furniture explaining what two blank rows already said.
//   - PAST TEN AVAILABLE IT IS A LIST YOU SEARCH, not one you read. A catalog of
//     a few hundred services cannot be walked with ↓, so the box under it
//     becomes a filter and the list narrows as it is typed into — the palette
//     idiom, in the palette's own position (palette.go, input.go's
//     [app.inputBlock]). Under ten, there is no box: a filter over six rows is a
//     widget explaining itself.
//
// The panel is modal while it is up, which every list on this surface that opens
// on a COMMAND is (the model picker, the session picker). The lists that open by
// TYPING are the ones that are not.

// connectRowsMax is how many LINES the panel takes at most. It is the same
// ceiling every bottom-anchored list on this surface has.
const connectRowsMax = 10

// connectFilterFloor is where the list stops being a thing a person reads and
// starts being a thing they search. It is counted in AVAILABLE services and not
// in rows: the connected ones are a handful by definition and they sit at the
// top where they are always reachable, so the number that decides whether ↓ is
// still a way through this list is the number underneath them.
const connectFilterFloor = 10

// The sentences this surface says about connections when it cannot show a list.
const (
	connectUnavailableWord = "connections are unavailable here"
	noServicesWord         = "there is nothing to connect yet"
	noConnectMatchWord     = "nothing matches"
)

// The two dim tags an available row carries once the list is big enough to be
// searched: what pressing enter on it is going to ask of the person. They are
// not shown on a short list, where the row's blurb is the more useful fact and
// there is room to read it.
const (
	signInTag = "sign in"
	keyTag    = "key"
)

// connectFilterHint is the placeholder in the empty filter box — the panel's one
// legend, in the box a person is already looking at.
const connectFilterHint = "filter · ↑↓ · enter connect · esc close"

// keyEntry is one key being typed into the panel: which service it is for, the
// word a person knows it by, and the box itself.
type keyEntry struct {
	id   string
	name string
	box  editor
}

// connectPanel is the overlay's whole state. The zero value is closed.
type connectPanel struct {
	open bool

	// all is the catalog in the order this panel draws it — connected first,
	// then the rest, each half in the order the engine handed it over. lower is
	// the same rows folded once at open, which is what the filter matches
	// against: narrowing runs on every keystroke over every row, and lowercasing
	// three hundred names on each of them is the one cost this path cannot pay
	// per frame. score is per-row scratch, reused across keystrokes.
	all   []connect.Status
	lower []string
	score []int

	// hits are indexes into all, in the order they are drawn — the rows actually
	// on offer. THE SLICE IS BUILT WHEN THE QUERY CHANGES AND NEVER WHEN THE
	// FRAME IS PAINTED: a paint runs many times a second and a keystroke does
	// not, so the filtering belongs to the keystroke.
	hits []int
	// cursor indexes hits, and top is the first hit drawn.
	cursor int
	top    int

	// filtering says the box under the list is a filter rather than the draft.
	// It is decided when the list is resolved and not per keystroke, so the
	// panel cannot grow and lose its box while somebody is typing into it.
	filtering bool
	filter    editor

	// entry is the key box, open over the list, or nil. It takes the filter's
	// place rather than a row of its own — one box under the list, answering one
	// question at a time.
	entry *keyEntry

	// armed is the row a second enter would disconnect, or -1. It is an index
	// into ALL rather than into hits, because the query moves: an arm that
	// survived a keystroke would be a confirmation a person gave about a
	// different account.
	armed int

	// owner maps each screen line of the block back to the HIT that drew it —
	// the geometry recorded at layout, which is the same bargain the approval
	// question's answers make (app.go's [app.askTaps]). At [tierPhone] a row is
	// two lines and the section gap is a line belonging to nothing, so the
	// pointer cannot resolve this arithmetic on its own.
	owner []int
}

func (p *connectPanel) close() { *p = connectPanel{} }

// start opens the panel over one reading of the services.
func (p *connectPanel) start(rows []connect.Status) {
	*p = connectPanel{open: true, armed: -1}
	p.adopt(rows)
}

// adopt takes a fresh reading and rebuilds everything derived from it.
func (p *connectPanel) adopt(rows []connect.Status) {
	p.all = orderConnections(rows)
	p.lower = make([]string, len(p.all))
	for i, row := range p.all {
		// The id is folded in beside the name because it is a word people know
		// services by — "gh" finds GitHub through its id long before it finds it
		// through its name.
		p.lower[i] = strings.ToLower(row.Name + " " + row.ID)
	}
	p.score = make([]int, len(p.all))
	p.filtering = availableCount(p.all) > connectFilterFloor
	p.armed = -1
	p.rank()
}

// orderConnections is the panel's one law about order: what this profile HAS,
// then what it could have, each half in the order it arrived.
//
// It is a stable partition rather than a sort, so the engine's own order — which
// is the catalog's — survives inside both halves.
func orderConnections(rows []connect.Status) []connect.Status {
	out := make([]connect.Status, 0, len(rows))
	for _, row := range rows {
		if row.Connected {
			out = append(out, row)
		}
	}
	for _, row := range rows {
		if !row.Connected {
			out = append(out, row)
		}
	}
	return out
}

// availableCount is how many services are on offer but not held.
func availableCount(rows []connect.Status) int {
	n := 0
	for _, row := range rows {
		if !row.Connected {
			n++
		}
	}
	return n
}

// rank re-filters against the filter box, with the picker's own scoring
// (palette.go's [tokenScore]): every token must match, and each matches as a
// prefix, a substring or a subsequence, in that order of preference.
//
// CONNECTED STILL COMES FIRST, above the score. The section order is a law about
// what the list IS and not a tie-break — a person who typed three letters is
// narrowing the list, not asking it to forget which accounts they already hold —
// so an account that matches at all stays above every service that is only on
// offer.
func (p *connectPanel) rank() {
	tokens := strings.Fields(strings.ToLower(p.filter.String()))
	p.hits = p.hits[:0]
	for i, text := range p.lower {
		if len(tokens) == 0 {
			p.score[i] = 0
			p.hits = append(p.hits, i)
			continue
		}
		total, matched := 0, true
		for _, token := range tokens {
			score, hit := tokenScore(text, token)
			if !hit {
				matched = false
				break
			}
			total += score
		}
		if !matched {
			continue
		}
		p.score[i] = total
		p.hits = append(p.hits, i)
	}
	if len(tokens) > 0 {
		sort.SliceStable(p.hits, func(x, y int) bool {
			a, b := p.hits[x], p.hits[y]
			if p.all[a].Connected != p.all[b].Connected {
				return p.all[a].Connected
			}
			return p.score[a] < p.score[b]
		})
	}
	// A changed query is a changed list, and a cursor left at row nine of the
	// old one points at nothing anybody chose.
	p.cursor, p.top = 0, 0
}

func (p *connectPanel) move(delta int) {
	p.cursor = moveCursor(p.cursor, delta, len(p.hits))
	p.follow(connectRowsMax)
	// Moving off a row un-asks the question that was asked about it.
	p.armed = -1
}

func (p *connectPanel) follow(height int) {
	p.top = listTop(p.cursor, p.top, len(p.hits), height)
}

// at resolves one hit: the row, its index in all, and whether there is one.
func (p *connectPanel) at(hit int) (connect.Status, int, bool) {
	if hit < 0 || hit >= len(p.hits) {
		return connect.Status{}, -1, false
	}
	return p.all[p.hits[hit]], p.hits[hit], true
}

// choice is the service under the cursor, and false when there is none — which
// is what a filter matching nothing leaves behind.
func (p *connectPanel) choice() (connect.Status, bool) {
	if !p.open {
		return connect.Status{}, false
	}
	row, _, ok := p.at(p.cursor)
	return row, ok
}

// gapBefore reports whether the blank row that separates the two sections falls
// in front of this hit. It is derived rather than stored, so it survives every
// narrowing without a second thing having to be kept in step.
func (p *connectPanel) gapBefore(hit int) bool {
	if hit <= 0 || hit >= len(p.hits) {
		return false
	}
	return p.all[p.hits[hit-1]].Connected && !p.all[p.hits[hit]].Connected
}

// note is the dim tail of one row: the account where the service is held, the
// question where one has been asked, the tag on a catalog, and the blurb on a
// list short enough to read.
//
// It is the emptiness law twice over. A connected service nobody named an
// account for draws NOTHING on the right — not "connected", which the tick
// already said, and not an empty parenthetical. A service with no blurb draws
// nothing either.
//
// ONE FACT PER ROW, STILL. The tag and the blurb are not stacked: on a catalog
// the blurb is three hundred sentences nobody is reading and the tag is what
// tells a person what enter is about to ask them for, and on a short list it is
// the other way round.
func (p *connectPanel) note(hit int) string {
	row, i, ok := p.at(hit)
	if !ok {
		return ""
	}
	if i == p.armed {
		return "enter again to disconnect"
	}
	if row.Connected {
		return row.Account
	}
	if p.filtering {
		return connectTag(row.Service)
	}
	return row.Blurb
}

// connectTag is what an available row says about how it is connected.
func connectTag(service connect.Service) string {
	if keyService(service) {
		return keyTag
	}
	return signInTag
}

// label is the row's own half: the state, then the name.
func (p *connectPanel) label(hit int, pal palette) string {
	row, _, ok := p.at(hit)
	if !ok {
		return ""
	}
	mark := glyphIdle
	if row.Connected {
		mark = glyphConnected
	}
	if pal.linear {
		mark = glyphIdleASCII
		if row.Connected {
			mark = glyphConnectedASCII
		}
	}
	name := row.Name
	if name == "" {
		name = row.ID
	}
	return mark + " " + name
}

// height is how many rows the overlay wants — a ceiling in LINES, so a phone
// shows fewer services with what they are readable rather than ten rows of
// clipped sentence (palette.go).
func (p *connectPanel) height(width int) int {
	switch {
	case !p.open:
		return 0
	case len(p.hits) == 0:
		// A filter that matched nothing has to say so where the list was.
		return 1
	}
	return p.window(width, connectRowsMax)
}

// window is how many lines the rows from top take, stopping at the ceiling — the
// list's own [overlayWindow], with the section gap counted in. A row that would
// straddle the bottom edge is not counted, because it is not drawn.
func (p *connectPanel) window(width, ceiling int) int {
	lines := 0
	for at := p.top; at < len(p.hits) && lines < ceiling; at++ {
		take := overlayItemLines(width, p.note(at))
		if at > p.top && p.gapBefore(at) {
			take++
		}
		if lines+take > ceiling {
			break
		}
		lines += take
	}
	return lines
}

func (p *connectPanel) draw(width, n int, pal palette, hover int) []string {
	if n <= 0 || len(p.all) == 0 {
		return nil
	}
	if len(p.hits) == 0 {
		p.owner = []int{-1}
		return []string{pal.dim(fit("  "+noConnectMatchWord, width))}
	}
	p.follow(overlayItems(n, width))
	fill := newOverlayFill(width, n, pal, hover)
	for at := p.top; at < len(p.hits) && fill.room(); at++ {
		if at > p.top && p.gapBefore(at) && !fill.plain("") {
			break
		}
		if !fill.add(at, p.label(at, pal), p.note(at), at == p.cursor, false) {
			break
		}
	}
	lines, owner := fill.done()
	// THE BLOCK IS EXACTLY THE HEIGHT IT WAS PROMISED (palette.go's
	// [overlayFill.done] says why). The gap is what makes the promise breakable
	// here and nowhere else: [connectPanel.height] counts it against the top the
	// last frame left behind, and the scroll above may have moved that top since.
	// A line short would leave the frame a line short of the terminal.
	for len(lines) < n {
		lines = append(lines, "")
		owner = append(owner, -1)
	}
	p.owner = owner
	return lines
}

// ── the app's side ──────────────────────────────────────────────────────────

// openConnect is /connect.
//
// The list is resolved HERE rather than held from boot, on the terms the session
// picker resolves its own: an account connected in another window an hour ago is
// an account this list has to know about, and asking costs a read.
func (a *app) openConnect() {
	if a.conns == nil {
		a.note(connectUnavailableWord)
		return
	}
	rows := a.conns.Services()
	if len(rows) == 0 {
		a.note(noServicesWord)
		return
	}
	a.closeLists()
	a.dismissWelcome()
	a.connPanel.start(rows)
	a.touch()
}

// refreshConnect re-reads the list under the cursor's own row, after something
// changed it.
//
// THE CURSOR FOLLOWS THE SERVICE AND NOT THE INDEX. Disconnecting a row moves it
// out of the connected section and down into the catalog, so the index it was at
// belongs to somebody else the instant the list is re-read — and a panel that
// kept the number would leave the cursor on a stranger.
func (a *app) refreshConnect() {
	p := &a.connPanel
	if !p.open || a.conns == nil {
		return
	}
	was, had := p.choice()
	p.adopt(a.conns.Services())
	if had {
		for at := range p.hits {
			if row, _, ok := p.at(at); ok && row.ID == was.ID {
				p.cursor = at
				p.follow(connectRowsMax)
				break
			}
		}
	}
	a.touch()
}

// connectPanelKey routes one keypress while the panel owns the keyboard.
func (a *app) connectPanelKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &a.connPanel
	if p.entry != nil {
		return a.connectEntryKey(msg)
	}
	var cmd tea.Cmd
	switch msg.String() {
	case "esc":
		// ESC UNDOES ONE THING AT A TIME, nearest first: the question standing on
		// a row, then the query narrowing the list, and only then the panel. A
		// dismiss key that closed the whole overlay while a person could still
		// see something smaller to dismiss would be throwing away work they can
		// see.
		switch {
		case p.armed >= 0:
			p.armed = -1
		case len(p.filter.value) > 0:
			p.filter.reset()
			p.rank()
		default:
			p.close()
		}

	case "up", "ctrl+p":
		p.move(-1)

	case "down", "ctrl+n":
		p.move(1)

	case "pgup":
		p.move(-connectRowsMax)

	case "pgdown":
		p.move(connectRowsMax)

	case "enter":
		cmd = a.connectAct(p.cursor)

	default:
		// TYPING NARROWS THE LIST, and only on a list big enough to need it. On a
		// short one every other key does nothing, which is what modal has always
		// meant here — and a filter box over six rows is a widget explaining
		// itself.
		if p.filtering {
			listNavigate(msg, &p.filter, p.move, p.rank, connectRowsMax)
		}
	}
	a.touch()
	return cmd
}

// connectEntryKey drives the key box open over the list. enter connects, an
// empty box is not an answer at all, and esc puts the person back on the row
// they pressed it from — which is the difference between this box and the
// offer's: nothing is waiting on it, so backing out of it declines nothing.
func (a *app) connectEntryKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &a.connPanel
	entry := p.entry
	switch msg.String() {
	case "esc":
		p.entry = nil

	case "enter":
		key := strings.TrimSpace(entry.box.String())
		p.entry = nil
		if key == "" {
			break
		}
		p.close()
		a.touch()
		return a.beginConnectKey(entry.id, entry.name, key)

	default:
		listNavigate(msg, &entry.box, func(int) {}, func() {}, 1)
	}
	a.touch()
	return nil
}

// connectPanelPress resolves a click on one of the panel's rows.
func (a *app) connectPanelPress(y int) tea.Cmd {
	p := &a.connPanel
	if p.entry != nil {
		// A box being typed into is not a list. Every press is swallowed and
		// none of them acts — esc is the way out, which is the way out of every
		// box on this surface.
		return nil
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeOverlay {
		// A press anywhere else closes it, which is what pressing outside a list
		// means everywhere a list is modal.
		p.close()
		a.touch()
		return nil
	}
	at := -1
	if mark.index >= 0 && mark.index < len(p.owner) {
		at = p.owner[mark.index]
	}
	if at < 0 {
		// The section gap, or a blank under the last row: a line belonging to no
		// service. It is swallowed rather than acted on.
		return nil
	}
	// The pointer moves the cursor before it acts, so the row a person pressed is
	// the row the panel is talking about afterwards — and so a mis-aimed press on
	// a connected row arms the question on the row they can see armed.
	if at != p.cursor {
		p.cursor, p.armed = at, -1
	}
	cmd := a.connectAct(at)
	a.touch()
	return cmd
}

// connectAct is enter, and the click that means the same thing: the sign-in on a
// browser row, the key box on a key row, and ask-then-disconnect on a row that
// is already held.
func (a *app) connectAct(at int) tea.Cmd {
	p := &a.connPanel
	row, i, ok := p.at(at)
	if !ok {
		return nil
	}
	if !row.Connected {
		name := a.serviceName(row.ID, row.Name)
		if keyService(row.Service) {
			// THE PANEL STAYS UP UNDER THE BOX, unlike the browser path, and the
			// difference is where the next thing happens: a sign-in continues in
			// another window and there is nothing left to look at here, while a
			// key is given HERE, on the row a person is pointing at.
			p.entry = &keyEntry{id: row.ID, name: name}
			return nil
		}
		p.close()
		return a.beginConnect(row.ID, name)
	}
	if p.armed != i {
		p.armed = i
		return nil
	}
	p.armed = -1
	if a.conns == nil {
		return nil
	}
	if err := a.conns.Disconnect(row.ID); err != nil {
		a.note(err.Error())
		return nil
	}
	a.refreshConnect()
	return nil
}

// beginConnect starts one sign-in from the panel. It is a COMMAND because
// BeginAuth reaches the network, and the model loop is not a place to wait —
// the same reason the repository probe is one (app.go's [gitMsg]).
func (a *app) beginConnect(service, name string) tea.Cmd {
	if a.conns == nil {
		return nil
	}
	conns, ctx := a.conns, a.ctx
	return func() tea.Msg {
		flow, err := conns.BeginAuth(ctx, service)
		return connectFlowMsg{service: service, name: name, flow: flow, err: err}
	}
}

// beginConnectKey is the key path's half of it: the key goes to the engine, and
// what comes back is the same [connectResultMsg] a finished sign-in produces.
//
// The waiting block goes up BEFORE the command runs, for the reason the sign-in
// draws its link before it waits: verifying a key is a network trip, and a
// surface that showed nothing during it would be a surface that ate a keystroke.
func (a *app) beginConnectKey(service, name, key string) tea.Cmd {
	if a.conns == nil {
		return nil
	}
	a.openConnectCheck(service, name)
	conns, ctx := a.conns, a.ctx
	return func() tea.Msg {
		status, err := conns.ConnectKey(ctx, service, key)
		return connectResultMsg{service: service, name: name, status: status, err: err}
	}
}

// adoptConnectFlow takes the sign-in a command started.
//
// THE ORDER IS THE WHOLE OF IT: the link is valid the moment BeginAuth returns,
// so it is shown and opened FIRST and waited on afterwards — a surface that
// waited before it drew would be a surface holding the link a person needs while
// it waits for them to use it.
func (a *app) adoptConnectFlow(msg connectFlowMsg) tea.Cmd {
	if msg.err != nil {
		a.note(msg.err.Error())
		// AND THE SHEET IS TOLD, because the note above lands in a transcript
		// that is behind a fullscreen panel while one is up: a sign-in started
		// from the Connections tab that never reached a browser has to say so on
		// the tab it was started from (connectcaps.go).
		a.connTabStopped(msg.service, msg.err.Error())
		return nil
	}
	if msg.flow == nil {
		a.connTabStopped(msg.service, "")
		return nil
	}
	// A second attempt at the same service abandons the first: two listeners on
	// one account is one of them waiting for something that will never come.
	a.abandonConnect(msg.service)
	if a.connFlows == nil {
		a.connFlows = map[string]*connect.Flow{}
	}
	a.connFlows[msg.service] = msg.flow
	a.openConnectFlow(msg.service, msg.name, msg.flow.URL())
	flow, ctx := msg.flow, a.ctx
	return func() tea.Msg {
		status, err := flow.Wait(ctx)
		return connectResultMsg{service: msg.service, name: msg.name, status: status, err: err}
	}
}

// abandonConnect drops the sign-in this surface is holding for a service, and
// tells it so. A flow that has already answered is simply forgotten — Cancel on
// a finished sign-in changes nothing, and the check would be a second place to
// keep that fact.
func (a *app) abandonConnect(service string) {
	flow := a.connFlows[service]
	if flow == nil {
		return
	}
	delete(a.connFlows, service)
	flow.Cancel()
}

// abandonConnects is every one of them, which is what replacing the conversation
// does: a sign-in belongs to the conversation that asked for it (app.go's
// [app.renew]).
func (a *app) abandonConnects() {
	for service := range a.connFlows {
		a.abandonConnect(service)
	}
	a.connFlows = nil
}

// adoptConnectResult settles the block the browser — or the key — left open, and
// tells the session what it now has.
//
// TELLING THE SESSION IS THE POINT OF THIS PATH. A person can open /connect in the
// middle of an idle conversation and connect an account the agent asked for ten
// minutes ago; without this the session would go on believing it has nothing,
// and would ask again the next time it reached.
func (a *app) adoptConnectResult(msg connectResultMsg) {
	delete(a.connFlows, msg.service)
	failed := msg.err != nil || !msg.status.Connected
	account := msg.status.Account
	a.settleConnect(msg.service, msg.name, account, failed)
	if !failed && a.agent != nil {
		a.agent.NoteConnected(msg.service, account)
	}
	a.refreshConnect()
	// And the settings sheet's Connections tab, which is the OTHER list this
	// outcome is news for: a row that has just gained an account opens on what
	// that account may do (connectcaps.go).
	a.connTabSettled(msg.service, msg.name, !failed)
}

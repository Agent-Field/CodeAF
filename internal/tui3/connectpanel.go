package tui3

import (
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
//   - CONNECTED FIRST AND FLAT, THEN THE CATALOG UNDER ITS OWN WORDS. The list
//     answers two questions and they are not the same question: "what have I
//     got" is a handful of rows a person recognizes, and "what else is there" is
//     a catalog of a few hundred. So the accounts this profile HAS sit at the
//     top with no heading over them — a rule labelled "connected" would be
//     furniture explaining what the ticks already said, and six rows a person
//     recognizes need no word to be found by — and everything underneath is
//     grouped by category, one dim lowercase word per group, alphabetical with
//     "other" last. THE WORDS ARE NOT THIS FILE'S. The grouping and the filter
//     over it are [groupConnections] and [filterConnections] (connectcaps.go),
//     called from here, because the settings sheet's Connections tab lists the
//     same catalog and two surfaces that sorted it separately would eventually
//     disagree about which category Stripe is in. A HEADING IS A LABEL AND NOT A
//     ROW: ↑↓ step over it, it belongs to no service, and a press on it does
//     nothing rather than acting on whichever row it was nearest. And where no
//     service declares a category at all the grouping comes back as ONE UNHEADED
//     GROUP, which is this panel's older shape exactly — connected, one blank
//     row, the rest — so a catalog gaining its categories changes what the list
//     SAYS and never what it is.
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

// connectPanel is the overlay's whole state. The zero value is closed.
type connectPanel struct {
	open bool

	// groups is the catalog as a person browses it — the held accounts, then one
	// category per group — and all is those same rows flattened, which is the
	// STABLE SLICE everything else in here indexes into. Both are built once per
	// reading of the engine and never per keystroke and never per frame: the
	// catalog is heading for a couple of hundred services, and a grouping sort
	// per repaint is the difference between a filter that types and one that
	// lags (connectcaps.go says it first, about the same two functions).
	groups []connGroup
	all    []connect.Status
	// where is a service's id to its place in all, which is how a row that came
	// back through the filter finds its way home: [filterConnections] answers in
	// [connect.Status] values rather than in indexes, because it is shared with a
	// sheet that has no flat slice at all.
	where map[string]int

	// hits are indexes into all, in the order they are drawn — the rows actually
	// on offer — and heads is the category label drawn IN FRONT of each of them,
	// or "" for the ones that open no group. THE TWO SLICES ARE BUILT WHEN THE
	// QUERY CHANGES AND NEVER WHEN THE FRAME IS PAINTED: a paint runs many times
	// a second and a keystroke does not, so the filtering belongs to the
	// keystroke.
	//
	// A HEADING IS NOT A HIT. It is a line the draw emits in front of one, so
	// there is no index a cursor could hold that points at a word — which is what
	// makes ↑↓, pgup/pgdn and the pointer all step over the headings without any
	// of them having to know the headings exist.
	hits  []int
	heads []string
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

	// armed is the SERVICE a second enter would disconnect, by id, or "". It is
	// the id rather than any row number because every number in this panel moves:
	// the query re-ranks hits on each keystroke, and a re-read of the engine
	// re-groups all — and an arm that survived either of those as an index would
	// be a confirmation a person gave about one account standing on another.
	armed string

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
	*p = connectPanel{open: true}
	p.adopt(rows)
}

// adopt takes a fresh reading and rebuilds everything derived from it.
//
// THE ORDER IS THE GROUPING'S AND NOT THIS FILE'S. all is the groups flattened,
// so the flat slice a hit indexes into is already in the order the list draws
// it: the accounts this profile has, and then the catalog by category.
func (p *connectPanel) adopt(rows []connect.Status) {
	p.groups = groupConnections(rows)
	p.all = make([]connect.Status, 0, len(rows))
	p.where = make(map[string]int, len(rows))
	for _, group := range p.groups {
		for _, row := range group.rows {
			p.where[row.ID] = len(p.all)
			p.all = append(p.all, row)
		}
	}
	p.filtering = availableCount(p.all) > connectFilterFloor
	p.armed = ""
	p.rank()
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

// rank re-filters against the filter box and lays the surviving groups out as
// the flat run of rows this panel draws.
//
// THE NARROWING IS [filterConnections] AND NOT A SCORING OF THIS PANEL'S OWN,
// which is the whole of what this wave changed here. The catalog's own words are
// part of the match — typing "billing" has to reach Stripe and Chargebee and
// Recurly, none of which contain it — and a name hit outranks a category hit,
// because somebody typing "stripe" wants Stripe and not the eleven other things
// filed beside it. The panel used to rank by [tokenScore] over a folded
// "name id" string, which could answer neither question and answered the second
// one differently from the settings sheet listing the same catalog.
//
// CONNECTED STILL COMES FIRST, above the ranking, and [filterConnections] is
// where that is decided now: the held group is pinned whatever the scores say. A
// person who typed three letters is narrowing the list, not asking it to forget
// which accounts they already hold.
func (p *connectPanel) rank() {
	query := strings.ToLower(strings.TrimSpace(p.filter.String()))
	groups := p.groups
	if query != "" {
		groups = filterConnections(groups, query)
	}
	p.hits, p.heads = p.hits[:0], p.heads[:0]
	for _, group := range groups {
		// The label carries a count while a filter is on, which is the tab's own
		// rule for it and the one a person reads on the other surface.
		head := group.label(query)
		for _, row := range group.rows {
			i, ok := p.where[row.ID]
			if !ok {
				// A row the grouping produced that the flattening did not: it
				// cannot happen, and drawing a hit that indexes nothing would be
				// the one way this panel could point at a stranger.
				continue
			}
			p.hits = append(p.hits, i)
			// Only the group's FIRST surviving row carries its word. The heading
			// is the line where the list changes subject, not a tag on every row
			// under it.
			p.heads = append(p.heads, head)
			head = ""
		}
	}
	// A changed query is a changed list, and a cursor left at row nine of the
	// old one points at nothing anybody chose.
	p.cursor, p.top = 0, 0
}

// move walks the list by ROWS, which is what keeps the cursor off the headings:
// a heading has no place in hits, so there is no delta that can land on one and
// no clamping rule needed to say so.
func (p *connectPanel) move(delta int) {
	p.cursor = moveCursor(p.cursor, delta, len(p.hits))
	p.follow(connectRowsMax)
	// Moving off a row un-asks the question that was asked about it.
	p.armed = ""
}

func (p *connectPanel) follow(height int) {
	p.top = listTop(p.cursor, p.top, len(p.hits), height)
}

// at resolves one hit: the row, and whether there is one.
func (p *connectPanel) at(hit int) (connect.Status, bool) {
	if hit < 0 || hit >= len(p.hits) || p.hits[hit] < 0 || p.hits[hit] >= len(p.all) {
		return connect.Status{}, false
	}
	return p.all[p.hits[hit]], true
}

// choice is the service under the cursor, and false when there is none — which
// is what a filter matching nothing leaves behind.
func (p *connectPanel) choice() (connect.Status, bool) {
	if !p.open {
		return connect.Status{}, false
	}
	return p.at(p.cursor)
}

// headBefore is the category word drawn in front of this hit, or "" where the
// hit opens no group.
func (p *connectPanel) headBefore(hit int) string {
	if hit < 0 || hit >= len(p.heads) {
		return ""
	}
	return p.heads[hit]
}

// gapBefore reports whether the blank row that separates the held accounts from
// the rest falls in front of this hit.
//
// IT IS THE FALLBACK AND NOTHING ELSE. Where the catalog declares categories the
// two sections are told apart by the word standing over the second one, and a
// blank as well would be this surface saying the same thing twice in the ten
// lines it has; where it declares none, [groupConnections] hands back one
// unheaded group and this is what keeps the panel the shape it has always been.
// It is derived rather than stored, so it survives every narrowing without a
// second thing having to be kept in step.
func (p *connectPanel) gapBefore(hit int) bool {
	if hit <= 0 || hit >= len(p.hits) || p.headBefore(hit) != "" {
		return false
	}
	before, ok := p.at(hit - 1)
	row, here := p.at(hit)
	return ok && here && before.Connected && !row.Connected
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
	row, ok := p.at(hit)
	if !ok {
		return ""
	}
	if p.armed != "" && row.ID == p.armed {
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
	row, ok := p.at(hit)
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
// list's own [overlayWindow], with the headings and the section gap counted in.
// A row that would straddle the bottom edge is not counted, because it is not
// drawn.
//
// A HEADING COSTS A LINE AND IS COUNTED HERE FOR THE ONE REASON EVERYTHING ELSE
// ON THIS SURFACE IS: [connectPanel.draw] must hand back exactly the lines
// [connectPanel.height] reserved, and a count that forgot the words would leave
// the frame short of the terminal by however many groups were on screen.
func (p *connectPanel) window(width, ceiling int) int {
	lines := 0
	for at := p.top; at < len(p.hits) && lines < ceiling; at++ {
		take := overlayItemLines(width, p.note(at))
		// The word stands at the top of a scrolled window too, unlike the gap: a
		// blank first line is a line spent on nothing, and a category's name is
		// the one thing a person scrolled into the middle of a catalog cannot
		// work out from the rows themselves.
		if p.headBefore(at) != "" {
			take++
		} else if at > p.top && p.gapBefore(at) {
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
		// THE HEADING IS A [overlayFill.plain] LINE, which is what makes it
		// unpressable without anything downstream having to know it is a heading:
		// plain records the line as belonging to row -1, the same value the gap
		// carries, and [app.connectPanelPress] already swallows -1.
		if head := p.headBefore(at); head != "" {
			if !fill.plain(pal.dim(fit("  "+head, width))) {
				break
			}
		} else if at > p.top && p.gapBefore(at) && !fill.plain("") {
			break
		}
		if !fill.add(at, p.label(at, pal), p.note(at), at == p.cursor, false) {
			break
		}
	}
	lines, owner := fill.done()
	// THE BLOCK IS EXACTLY THE HEIGHT IT WAS PROMISED (palette.go's
	// [overlayFill.done] says why). The lines that are not rows — the headings and
	// the gap — are what make the promise breakable here and nowhere else:
	// [connectPanel.height] counts them against the top the last frame left
	// behind, and the scroll above may have moved that top since. A line short
	// would leave the frame a line short of the terminal.
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
	// IT STATES THE FACT RATHER THAN GOING MISSING (host.go). The command still
	// exists, still answers, and answers with the reason: a sign-in opens a
	// browser and waits on a loopback port, and over --host the browser is here
	// while the account store is on the other machine. A command that quietly did
	// nothing would send a person looking for a bug in their terminal.
	if a.hosted() {
		a.note(connectRemoteWord)
		return
	}
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
			if row, ok := p.at(at); ok && row.ID == was.ID {
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
		case p.armed != "":
			p.armed = ""
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

// connectEntryKey drives the typed-answer box open over the list. enter connects, an
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
		answer := strings.TrimSpace(entry.box.String())
		p.entry = nil
		if answer == "" {
			break
		}
		p.close()
		a.touch()
		if entry.secret {
			return a.beginConnectKey(entry.id, entry.name, answer)
		}
		return a.beginConnect(entry.id, entry.name, answer)

	default:
		entry.typeInto(msg)
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
		// A category heading, the section gap, or a blank under the last row: a
		// line belonging to no service. It is swallowed rather than acted on —
		// pressing a word must not act on whichever row it happened to be nearest.
		return nil
	}
	// The pointer moves the cursor before it acts, so the row a person pressed is
	// the row the panel is talking about afterwards — and so a mis-aimed press on
	// a connected row arms the question on the row they can see armed.
	if at != p.cursor {
		p.cursor, p.armed = at, ""
	}
	cmd := a.connectAct(at)
	a.touch()
	return cmd
}

// connectAct is enter, and the click that means the same thing: the sign-in on a
// browser row, the typed-answer box where a row needs one, and
// ask-then-disconnect on a row that is already held.
func (a *app) connectAct(at int) tea.Cmd {
	p := &a.connPanel
	row, ok := p.at(at)
	if !ok {
		return nil
	}
	if !row.Connected {
		name := a.serviceName(row.ID, row.Name)
		if keyService(row.Service) || row.Service.Blank != "" {
			// THE PANEL STAYS UP UNDER THE BOX, unlike the browser path, and the
			// difference is where the next thing happens: a sign-in continues in
			// another window and there is nothing left to look at here, while a
			// answer is given HERE, on the row a person is pointing at.
			p.entry = newKeyEntry(row.Service, name)
			return nil
		}
		p.close()
		return a.beginConnect(row.ID, name, "")
	}
	if p.armed != row.ID {
		p.armed = row.ID
		return nil
	}
	p.armed = ""
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
func (a *app) beginConnect(service, name, answer string) tea.Cmd {
	if a.conns == nil {
		return nil
	}
	conns, ctx := a.conns, a.ctx
	return func() tea.Msg {
		flow, err := conns.BeginAuth(ctx, service, answer)
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
	why := ""
	if msg.err != nil {
		why = msg.err.Error()
	}
	a.connTabSettled(msg.service, msg.name, !failed, why)
}

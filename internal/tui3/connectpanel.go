package tui3

import (
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
//	  · Slack               your channels and messages
//
// Three decisions:
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
//   - ENTER MEANS THE OBVIOUS THING IN BOTH DIRECTIONS. On a row that is not
//     connected it starts the sign-in — the same browser, the same waiting
//     block, the same tick as the offer's own path, because it IS that path. On
//     a connected row it asks once and disconnects on the second press: an
//     account is a thing somebody else's session may be using, and one keystroke
//     is not enough of a decision to drop it.
//
// The panel is modal while it is up, which every list on this surface that opens
// on a COMMAND is (the model picker, the session picker). The lists that open by
// TYPING are the ones that are not.

// connectRowsMax is how many services are on offer at once. It is a ceiling
// nobody currently reaches — there is one service — and it exists so the overlay
// has one.
const connectRowsMax = 10

// The two sentences this surface says about connections when it cannot show a
// list.
const (
	connectUnavailableWord = "connections are unavailable here"
	noServicesWord         = "there is nothing to connect yet"
)

// connectPanel is the overlay's whole state. The zero value is closed.
type connectPanel struct {
	open bool

	rows   []connect.Status
	cursor int
	top    int

	// armed is the row a second enter would disconnect, or -1. It is an INDEX
	// and not a flag because the cursor moves: an arm that survived a walk down
	// the list would be a confirmation a person gave about a different account.
	armed int

	// owner maps each screen line of the block back to the row that drew it —
	// the geometry recorded at layout, which is the same bargain the approval
	// question's answers make (app.go's [app.askTaps]). At [tierPhone] a row is
	// two lines, so the pointer cannot resolve this arithmetic on its own.
	owner []int
}

func (p *connectPanel) close() { *p = connectPanel{} }

func (p *connectPanel) move(delta int) {
	p.cursor = moveCursor(p.cursor, delta, len(p.rows))
	p.follow(connectRowsMax)
	// Moving off a row un-asks the question that was asked about it.
	p.armed = -1
}

func (p *connectPanel) follow(height int) {
	p.top = listTop(p.cursor, p.top, len(p.rows), height)
}

// choice is the service under the cursor, and false when there is none.
func (p *connectPanel) choice() (connect.Status, bool) {
	if !p.open || p.cursor < 0 || p.cursor >= len(p.rows) {
		return connect.Status{}, false
	}
	return p.rows[p.cursor], true
}

// note is the dim tail of one row: the account where the service is held, the
// blurb where it is not, and the question where one has been asked.
//
// It is the emptiness law twice over. A connected service nobody named an
// account for draws NOTHING on the right — not "connected", which the tick
// already said, and not an empty parenthetical. A service with no blurb draws
// nothing either.
func (p *connectPanel) note(at int) string {
	if at < 0 || at >= len(p.rows) {
		return ""
	}
	if at == p.armed {
		return "enter again to disconnect"
	}
	row := p.rows[at]
	if row.Connected {
		return row.Account
	}
	return row.Blurb
}

// label is the row's own half: the state, then the name.
func (p *connectPanel) label(at int, pal palette) string {
	row := p.rows[at]
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
	if !p.open {
		return 0
	}
	return overlayWindow(width, p.top, len(p.rows), connectRowsMax, p.note)
}

func (p *connectPanel) draw(width, n int, pal palette, hover int) []string {
	if n <= 0 || len(p.rows) == 0 {
		return nil
	}
	p.follow(overlayItems(n, width))
	fill := newOverlayFill(width, n, pal, hover)
	for at := p.top; at < len(p.rows) && fill.room(); at++ {
		if !fill.add(at, p.label(at, pal), p.note(at), at == p.cursor, false) {
			break
		}
	}
	lines, owner := fill.done()
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
	a.connPanel = connectPanel{open: true, rows: rows, armed: -1}
	a.touch()
}

// refreshConnect re-reads the list under the cursor's own row, after something
// changed it.
func (a *app) refreshConnect() {
	if !a.connPanel.open || a.conns == nil {
		return
	}
	a.connPanel.rows = a.conns.Services()
	a.connPanel.cursor = moveCursor(a.connPanel.cursor, 0, len(a.connPanel.rows))
	a.connPanel.armed = -1
	a.touch()
}

// connectPanelKey routes one keypress while the panel owns the keyboard.
func (a *app) connectPanelKey(msg tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd
	switch msg.String() {
	case "esc":
		// A question asked is un-asked before the panel is left: esc is the
		// dismiss key at every rung, and the nearest thing to dismiss is the
		// confirmation standing on a row.
		if a.connPanel.armed >= 0 {
			a.connPanel.armed = -1
			break
		}
		a.connPanel.close()

	case "up", "ctrl+p":
		a.connPanel.move(-1)

	case "down", "ctrl+n":
		a.connPanel.move(1)

	case "enter":
		cmd = a.connectAct(a.connPanel.cursor)
	}
	a.touch()
	return cmd
}

// connectPress resolves a click on one of the panel's rows.
func (a *app) connectPanelPress(y int) tea.Cmd {
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeOverlay {
		// A press anywhere else closes it, which is what pressing outside a list
		// means everywhere a list is modal.
		a.connPanel.close()
		a.touch()
		return nil
	}
	at := -1
	if mark.index >= 0 && mark.index < len(a.connPanel.owner) {
		at = a.connPanel.owner[mark.index]
	}
	if at < 0 {
		return nil
	}
	// The pointer moves the cursor before it acts, so the row a person pressed is
	// the row the panel is talking about afterwards — and so a mis-aimed press on
	// a connected row arms the question on the row they can see armed.
	if at != a.connPanel.cursor {
		a.connPanel.cursor, a.connPanel.armed = at, -1
	}
	cmd := a.connectAct(at)
	a.touch()
	return cmd
}

// connectAct is enter, and the click that means the same thing: sign in where
// the service is not connected, and ask-then-disconnect where it is.
func (a *app) connectAct(at int) tea.Cmd {
	if at < 0 || at >= len(a.connPanel.rows) {
		return nil
	}
	row := a.connPanel.rows[at]
	if !row.Connected {
		a.connPanel.close()
		return a.beginConnect(row.ID, a.serviceName(row.ID, row.Name))
	}
	if a.connPanel.armed != at {
		a.connPanel.armed = at
		return nil
	}
	a.connPanel.armed = -1
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

// adoptConnectFlow takes the sign-in a command started.
//
// THE ORDER IS THE WHOLE OF IT: the link is valid the moment BeginAuth returns,
// so it is shown and opened FIRST and waited on afterwards — a surface that
// waited before it drew would be a surface holding the link a person needs while
// it waits for them to use it.
func (a *app) adoptConnectFlow(msg connectFlowMsg) tea.Cmd {
	if msg.err != nil {
		a.note(msg.err.Error())
		return nil
	}
	if msg.flow == nil {
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

// adoptConnectResult settles the block the browser left open, and tells the
// session what it now has.
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
}

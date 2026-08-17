package tui3

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/connect"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// CONNECTING AN ACCOUNT, from the four sides a person meets it: the offer and
// its two keys, the browser handoff and the link it writes down, the outcome
// that settles in place, and the panel /connect opens.

// ── the scripted session's connect methods ──────────────────────────────────
//
// They live here rather than beside [fakeAgent] because they arrived with this
// wave: the plain fake answers them the way a session with nothing to say does,
// and [connectAgent] below is the one that records.

func (f *fakeAgent) ResolveConnect(string, bool)      {}
func (f *fakeAgent) ResolveConnectKey(string, string) {}
func (f *fakeAgent) NoteConnected(string, string)     {}

// connectAgent records the two answers this wave sends back into the session.
type connectAgent struct {
	*fakeAgent
	resolved []connectAnswer
	noted    []connectAnswer
}

type connectAnswer struct {
	id      string
	approve bool
	account string
	// key is what the key path sent back, and keyed says it took that path at
	// all — the difference between "no key" and "a decline", which are the same
	// empty string and not the same answer.
	key   string
	keyed bool
}

func (c *connectAgent) ResolveConnect(id string, approve bool) {
	c.resolved = append(c.resolved, connectAnswer{id: id, approve: approve})
}

func (c *connectAgent) ResolveConnectKey(id string, key string) {
	c.resolved = append(c.resolved, connectAnswer{
		id: id, approve: key != "", key: key, keyed: true,
	})
}

func (c *connectAgent) NoteConnected(service, account string) {
	c.noted = append(c.noted, connectAnswer{id: service, account: account})
}

// fakeConnections is the door onto the accounts, scripted.
type fakeConnections struct {
	rows []connect.Status
	err  error
	// began and dropped are what the panel asked for, in order.
	began   []string
	dropped []string
	// keyed is every (service, key) pair the panel handed over, and keyErr is
	// what the far end says about them.
	keyed  []connectAnswer
	keyErr error

	// The capability half of the door (connectcaps.go). caps is what each
	// service may be asked to do, states where each of those stands, set what
	// the surface asked for — in order, with the exact arguments — and setErr a
	// refusal the engine hands back.
	// reads counts how many times the catalog was asked for, which is what the
	// once-per-read discipline is asserted against (connectcaps.go).
	reads  int
	caps   map[string][]connect.Capability
	states map[string]connect.CapabilityState
	set    []capChange
	setErr error
}

// capChange is one SetCapabilityState call, recorded whole.
type capChange struct {
	service    string
	capability string
	state      connect.CapabilityState
}

func (f *fakeConnections) Services() []connect.Status {
	f.reads++
	return f.rows
}

func (f *fakeConnections) BeginAuth(ctx context.Context, id string) (*connect.Flow, error) {
	f.began = append(f.began, id)
	// A nil flow is what a stubbed engine hands back, and the surface treats it
	// as nothing to wait on — which is exactly the shape these tests want: the
	// ASK is what the panel owns, and the answer arrives as its own message.
	return nil, f.err
}

func (f *fakeConnections) ConnectKey(ctx context.Context, id string, key string) (connect.Status, error) {
	f.keyed = append(f.keyed, connectAnswer{id: id, key: key})
	if f.keyErr != nil {
		return connect.Status{}, f.keyErr
	}
	for i := range f.rows {
		if f.rows[i].ID != id {
			continue
		}
		f.rows[i].Connected, f.rows[i].Account = true, "jane@example.com"
		return f.rows[i], nil
	}
	return connect.Status{}, nil
}

func (f *fakeConnections) Disconnect(id string) error {
	f.dropped = append(f.dropped, id)
	for i := range f.rows {
		if f.rows[i].ID == id {
			f.rows[i].Connected, f.rows[i].Account = false, ""
		}
	}
	return nil
}

func (f *fakeConnections) Capabilities(service string) []connect.Capability {
	return f.caps[service]
}

// CapabilityState answers what was last set, and otherwise the default the
// contract states: looking is yes, acting asks first.
func (f *fakeConnections) CapabilityState(service, capability string) connect.CapabilityState {
	if state, ok := f.states[capKey(service, capability)]; ok {
		return state
	}
	for _, may := range f.caps[service] {
		if may.ID == capability && may.Acts {
			return connect.StateAsk
		}
	}
	return connect.StateYes
}

func (f *fakeConnections) SetCapabilityState(service, capability string, state connect.CapabilityState) error {
	f.set = append(f.set, capChange{service: service, capability: capability, state: state})
	if f.setErr != nil {
		return f.setErr
	}
	if f.states == nil {
		f.states = map[string]connect.CapabilityState{}
	}
	f.states[capKey(service, capability)] = state
	return nil
}

func capKey(service, capability string) string { return service + "/" + capability }

// connectApp is a surface with a recording session behind it, and no browser in
// front of it: every handoff lands in opened rather than on the machine running
// the test.
func connectApp(t *testing.T) (*connectAgent, *app, *[]string) {
	t.Helper()
	agent := &connectAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(agent)
	opened := new([]string)
	was := processOpener
	processOpener = func(target string) error {
		*opened = append(*opened, target)
		return nil
	}
	t.Cleanup(func() { processOpener = was })
	return agent, a, opened
}

// askConnectEvent is the session's own offer.
func askConnectEvent(id, service, name string) session.Event {
	return session.Event{
		Kind: session.EventConnectAsk, ConnectID: id, Service: service, ServiceName: name,
	}
}

// connectBlock is the offer block as a reader sees it, laid out at the frame's
// width.
func connectBlock(a *app) []string {
	out := make([]string, 0, 4)
	for _, line := range a.connectAskRows(a.width) {
		out = append(out, plain(line))
	}
	return out
}

// ── 1. the offer ────────────────────────────────────────────────────────────

// THE BLOCK SAYS THREE THINGS AND STOPS: which account, who wants it, and the
// two keys. No scopes, no provider machinery, no third answer.
func TestTheConnectOfferNamesTheAccountAndTwoAnswers(t *testing.T) {
	_, a, _ := connectApp(t)
	drive(t, a, streamOf(a, askConnectEvent("c1", "google", "Google")))

	rows := connectBlock(a)
	if len(rows) != 3 {
		t.Fatalf("the offer is %d rows, want three:\n%s", len(rows), strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[0], "Google") {
		t.Fatalf("the heading does not name the account: %q", rows[0])
	}
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "wants to connect your Google account") {
		t.Fatalf("the block never says what it is asking for:\n%s", joined)
	}
	for _, want := range []string{"[enter]", "connect", "[esc]", "not now"} {
		if !strings.Contains(rows[connectOfferRow], want) {
			t.Fatalf("the offer is missing %q: %q", want, rows[connectOfferRow])
		}
	}
	// NO MACHINERY, ANYWHERE ON THE BLOCK. These are the words a person should
	// never have to read to connect their own account.
	for _, banned := range []string{"OAuth", "oauth", "token", "scope", "URL", "redirect"} {
		if strings.Contains(joined, banned) {
			t.Fatalf("the offer says %q:\n%s", banned, joined)
		}
	}
	// And the block is counted the way it is drawn: a frame whose geometry
	// disagreed with its layout puts the caret a row off the box.
	if got := a.connectAskHeight(); got != len(rows) {
		t.Fatalf("the block is %d rows and counts itself as %d", len(rows), got)
	}
}

// ENTER APPROVES, ESC DECLINES, and the answer goes back to the session with the
// token it came with. y and n are read silently beside them.
func TestTheConnectOfferIsAnsweredByOneKey(t *testing.T) {
	for _, test := range []struct {
		key     string
		approve bool
	}{
		{"enter", true}, {"y", true}, {"esc", false}, {"n", false},
	} {
		agent, a, _ := connectApp(t)
		drive(t, a, streamOf(a, askConnectEvent("c1", "google", "Google")))
		drive(t, a, key(test.key))

		if a.asksConnect() {
			t.Fatalf("%q left the offer on screen", test.key)
		}
		if len(agent.resolved) != 1 {
			t.Fatalf("%q sent %d answers to the session", test.key, len(agent.resolved))
		}
		if got := agent.resolved[0]; got.id != "c1" || got.approve != test.approve {
			t.Fatalf("%q answered %+v, want c1/%v", test.key, got, test.approve)
		}
	}
}

// A DECLINE RECORDS NOTHING. Nothing happened, and a line saying "you said not
// now" would be the surface keeping a note about a thing it did not do.
func TestDecliningTheConnectOfferWritesNothingDown(t *testing.T) {
	_, a, _ := connectApp(t)
	before := len(a.entries)
	drive(t, a, streamOf(a, askConnectEvent("c1", "google", "Google")))
	drive(t, a, key("esc"))
	if len(a.entries) != before {
		t.Fatalf("declining wrote %d rows into the transcript", len(a.entries)-before)
	}
}

// THE OFFER OWNS THE KEYBOARD while it is up, exactly as the approval question
// does: a key that is not an answer types nothing.
func TestTheConnectOfferSuspendsTheDraft(t *testing.T) {
	_, a, _ := connectApp(t)
	drive(t, a, streamOf(a, askConnectEvent("c1", "google", "Google")))
	drive(t, a, key("h"), key("i"))
	if got := a.input.String(); got != "" {
		t.Fatalf("the draft took %q while an offer was up", got)
	}
}

// THEY QUEUE, oldest first, and what is behind the one on screen is on screen.
func TestConnectOffersQueue(t *testing.T) {
	agent, a, _ := connectApp(t)
	drive(t, a,
		streamOf(a, askConnectEvent("c1", "google", "Google")),
		streamOf(a, askConnectEvent("c2", "slack", "Slack")),
	)
	if !strings.Contains(strings.Join(connectBlock(a), "\n"), "1 more") {
		t.Fatalf("the queue behind the offer is not on screen:\n%s",
			strings.Join(connectBlock(a), "\n"))
	}
	drive(t, a, key("enter"))
	if got := connectBlock(a); !strings.Contains(got[0], "Slack") {
		t.Fatalf("the second offer did not come forward: %q", got[0])
	}
	if strings.Contains(strings.Join(connectBlock(a), "\n"), "more") {
		t.Fatal("the last offer still claims something is behind it")
	}
	drive(t, a, key("esc"))
	if len(agent.resolved) != 2 || agent.resolved[0].id != "c1" || agent.resolved[1].id != "c2" {
		t.Fatalf("the queue was answered as %+v, want c1 then c2", agent.resolved)
	}
}

// THE POINTER ANSWERS IT TOO, and the word is part of the target: a press on
// "connect" is the press on "[enter]" beside it.
func TestTheConnectOfferIsAnsweredByThePointer(t *testing.T) {
	agent, a, _ := connectApp(t)
	drive(t, a, streamOf(a, askConnectEvent("c1", "google", "Google")))

	y := connectOfferY(t, a)
	// The offer's own spans, resolved from the layout that drew them.
	if len(a.connTaps) != 2 {
		t.Fatalf("the offer recorded %d targets, want two", len(a.connTaps))
	}
	yes := a.connTaps[0]
	if !yes.approve {
		t.Fatal("the first target on the offer is not the yes")
	}
	// A column inside the WORD, not on the chip: the whole answer is the target.
	if !a.connectPress(yes.span.to-1, y) {
		t.Fatal("a press on the offer was not taken")
	}
	if len(agent.resolved) != 1 || !agent.resolved[0].approve {
		t.Fatalf("the press answered %+v, want an approval", agent.resolved)
	}
}

// A PRESS THAT MISSES BOTH ANSWERS IS STILL SWALLOWED: the block is a thing the
// session is waiting on, and a press falling through it would act on the
// transcript underneath.
func TestTheConnectOfferSwallowsEveryPressOnIt(t *testing.T) {
	agent, a, _ := connectApp(t)
	drive(t, a, streamOf(a, askConnectEvent("c1", "google", "Google")))
	if !a.connectPress(a.width-1, connectOfferY(t, a)) {
		t.Fatal("a press in the block's empty columns fell through")
	}
	if len(agent.resolved) != 0 {
		t.Fatalf("a press on nothing answered the offer: %+v", agent.resolved)
	}
}

// connectOfferY is the screen row the offer is drawn on, derived the way
// [app.chromeAt] derives it backwards.
func connectOfferY(t *testing.T, a *app) int {
	t.Helper()
	_, marks, _, _ := a.chrome(a.width)
	for at, mark := range marks {
		if mark.kind == chromeConnectAsk {
			return at + a.height - len(marks)
		}
	}
	t.Fatal("no row of the frame is marked as the connect offer")
	return -1
}

// ── 2. the browser handoff ──────────────────────────────────────────────────

const testAuthLink = "https://accounts.example.com/sign-in?state=abcdef"

// THE LINK GOES TO THE PLATFORM AND ONTO THE SCREEN. Both, always: a browser
// opened on the far end of an ssh connection is a browser nobody is sitting at.
func TestTheHandoffOpensTheBrowserAndWritesTheLinkDown(t *testing.T) {
	_, a, opened := connectApp(t)
	a.width = 100
	drive(t, a, streamOf(a, session.Event{
		Kind: session.EventConnectAuth, Service: "google", AuthURL: testAuthLink,
	}))

	if len(*opened) != 1 || (*opened)[0] != testAuthLink {
		t.Fatalf("the browser was handed %v, want the link once", *opened)
	}
	screen := strings.Join(plainRows(a), "\n")
	if !strings.Contains(screen, "waiting in your browser") {
		t.Fatalf("the surface does not say what it is waiting for:\n%s", screen)
	}
	if !strings.Contains(screen, testAuthLink) {
		t.Fatalf("the link is not on screen as text:\n%s", screen)
	}
	// AND IT IS A HYPERLINK where the sequence is safe — the plain text above is
	// what a person selects, this is what a modern terminal makes clickable.
	painted := strings.Join(rowTexts(a), "\n")
	if !strings.Contains(painted, "\x1b]8;;"+testAuthLink) {
		t.Fatal("the link on screen carries no hyperlink")
	}
	// The waiting block is the one thing on this surface that keeps the paint
	// clock turning with no turn running.
	if !a.connectAnimating() {
		t.Fatal("a sign-in in flight does not ask for frames")
	}
}

// A PLATFORM THAT CANNOT OPEN A BROWSER STILL SHOWS THE LINK. The handoff is not
// the feature; reaching the sign-in is.
func TestAFailedHandoffStillLeavesTheLink(t *testing.T) {
	_, a, _ := connectApp(t)
	a.width = 100
	processOpener = func(string) error { return errNoBrowser }
	drive(t, a, streamOf(a, session.Event{
		Kind: session.EventConnectAuth, Service: "google", AuthURL: testAuthLink,
	}))
	screen := strings.Join(plainRows(a), "\n")
	if !strings.Contains(screen, testAuthLink) {
		t.Fatalf("a failed handoff took the link with it:\n%s", screen)
	}
}

// ── 3. the outcome ──────────────────────────────────────────────────────────

// IT SETTLES IN PLACE: the waiting block becomes the line, rather than a second
// block being written under it.
func TestAFinishedSignInSettlesTheWaitingBlock(t *testing.T) {
	_, a, _ := connectApp(t)
	a.width = 100
	drive(t, a,
		streamOf(a, askConnectEvent("c1", "google", "Google")),
		key("enter"),
		streamOf(a, session.Event{
			Kind: session.EventConnectAuth, Service: "google", AuthURL: testAuthLink,
		}),
	)
	blocks := connectEntries(a)
	if len(blocks) != 1 {
		t.Fatalf("the handoff wrote %d blocks", len(blocks))
	}
	drive(t, a, streamOf(a, session.Event{
		Kind: session.EventConnectDone, Service: "google", Account: "jane@example.com",
	}))
	if blocks = connectEntries(a); len(blocks) != 1 {
		t.Fatalf("the outcome wrote a second block: %d in all", len(blocks))
	}
	screen := strings.Join(plainRows(a), "\n")
	if !strings.Contains(screen, glyphConnected+" Google connected as jane@example.com") {
		t.Fatalf("the tick line is not what settled:\n%s", screen)
	}
	if strings.Contains(screen, "waiting in your browser") {
		t.Fatalf("the waiting line survived the outcome:\n%s", screen)
	}
	if strings.Contains(screen, testAuthLink) {
		t.Fatalf("the link survived the sign-in it was for:\n%s", screen)
	}
	if a.connectAnimating() {
		t.Fatal("a settled sign-in still asks for frames")
	}
}

// THE EMPTINESS LAW. No account reported is no parenthetical, no "unknown", and
// no gap where one would have been.
func TestAConnectionWithNoAccountSaysNothingExtra(t *testing.T) {
	_, a, _ := connectApp(t)
	a.width = 100
	drive(t, a, streamOf(a, session.Event{
		Kind: session.EventConnectDone, Service: "google", ServiceName: "Google",
	}))
	screen := strings.Join(plainRows(a), "\n")
	if !strings.Contains(screen, glyphConnected+" Google connected") {
		t.Fatalf("the connection was not reported at all:\n%s", screen)
	}
	if strings.Contains(screen, " as ") {
		t.Fatalf("a connection with no account still says who:\n%s", screen)
	}
}

// A FAILURE IS QUIET AND HONEST, and it does not spend the failure glyph — which
// on this surface means a call that broke.
func TestAnUnfinishedSignInSaysSoQuietly(t *testing.T) {
	_, a, _ := connectApp(t)
	a.width = 100
	drive(t, a,
		streamOf(a, session.Event{
			Kind: session.EventConnectAuth, Service: "google", AuthURL: testAuthLink,
		}),
		streamOf(a, session.Event{Kind: session.EventConnectDone, Service: "google", Failed: true}),
	)
	screen := strings.Join(plainRows(a), "\n")
	if !strings.Contains(screen, "connection didn't complete") {
		t.Fatalf("a failed sign-in said nothing:\n%s", screen)
	}
	if strings.Contains(screen, glyphBad) {
		t.Fatalf("a connection that did not finish is drawn as a broken call:\n%s", screen)
	}
}

// THE NAME SURVIVES THE EVENTS THAT DO NOT CARRY ONE. Only the offer names a
// service; the two events after it carry an id, and the block still reads
// "Google".
func TestTheServiceKeepsItsNameAcrossTheLane(t *testing.T) {
	_, a, _ := connectApp(t)
	a.width = 100
	drive(t, a,
		streamOf(a, askConnectEvent("c1", "google", "Google")),
		key("enter"),
		streamOf(a, session.Event{
			Kind: session.EventConnectAuth, Service: "google", AuthURL: testAuthLink,
		}),
		streamOf(a, session.Event{
			Kind: session.EventConnectDone, Service: "google", Account: "jane@example.com",
		}),
	)
	if screen := strings.Join(plainRows(a), "\n"); !strings.Contains(screen, "Google connected") {
		t.Fatalf("the service lost its name on the way:\n%s", screen)
	}
}

// connectEntries is every sign-in block in the conversation.
func connectEntries(a *app) []*connectCard {
	out := make([]*connectCard, 0, 2)
	for i := range a.entries {
		if a.entries[i].kind == entryConnect {
			out = append(out, a.entries[i].conn)
		}
	}
	return out
}

// rowTexts is the conversation with its styling on, which is where the
// hyperlink lives.
func rowTexts(a *app) []string {
	out := make([]string, 0, 8)
	for _, r := range rows(a) {
		out = append(out, r.text)
	}
	return out
}

var errNoBrowser = errConnect("this machine has no way to open a browser")

type errConnect string

func (e errConnect) Error() string { return string(e) }

// ── 4. the panel ────────────────────────────────────────────────────────────

// panelApp is a surface with a scripted set of connections behind /connect.
//
// NOTHING HERE CONSTRUCTS A [connect.Flow], deliberately: the flow is the
// engine's object, and a test that reached into it would be a test pinned to a
// stub rather than to the contract. The two halves of a sign-in are exercised
// where the surface actually owns them — what it ASKED the engine for, and what
// it does with the answer ([connectResultMsg]).
func panelApp(t *testing.T, rows []connect.Status) (*connectAgent, *app, *fakeConnections) {
	t.Helper()
	agent, a, _ := connectApp(t)
	conns := &fakeConnections{rows: rows}
	a.conns = conns
	return agent, a, conns
}

var twoServices = []connect.Status{
	{Service: connect.Service{ID: "google", Name: "Google", Blurb: "your calendar and mail"}},
	{
		Service:   connect.Service{ID: "slack", Name: "Slack", Blurb: "your channels"},
		Connected: true, Account: "jane@example.com",
	},
}

// EACH ROW IS TWO GLYPHS AND A NAME, with whichever fact it actually has beside
// it: the account where there is one, the blurb where there is not.
func TestTheConnectPanelDrawsWhatEachServiceHas(t *testing.T) {
	_, a, _ := panelApp(t, twoServices)
	a.width = 100
	typeLine(t, a, "/connect")
	if !a.connPanel.open {
		t.Fatal("/connect opened nothing")
	}
	screen := strings.Join(plainOverlay(a), "\n")
	if !strings.Contains(screen, glyphIdle+" Google") ||
		!strings.Contains(screen, "your calendar and mail") {
		t.Fatalf("an unconnected row is not a dot, a name and its blurb:\n%s", screen)
	}
	if !strings.Contains(screen, glyphConnected+" Slack") ||
		!strings.Contains(screen, "jane@example.com") {
		t.Fatalf("a connected row is not a tick, a name and the account:\n%s", screen)
	}
	// The connected row says the account and NOT the blurb: one fact per row, and
	// the one that is true of this profile.
	if strings.Contains(screen, "your channels") {
		t.Fatalf("a connected row is still advertising itself:\n%s", screen)
	}
	drive(t, a, key("esc"))
	if a.connPanel.open {
		t.Fatal("esc did not close the panel")
	}
}

// ENTER ON AN UNCONNECTED ROW STARTS THE SIGN-IN and gets out of the way: the
// panel closes, because what happens next is a browser and a waiting block.
func TestTheConnectPanelStartsTheSignIn(t *testing.T) {
	_, a, conns := panelApp(t, twoServices)
	a.width = 100
	typeLine(t, a, "/connect")
	// The connected account sits at the top of the list now, so the row on offer
	// is the one under it (connectpanel.go's [orderConnections]).
	drive(t, a, key("down"), key("enter"))

	if len(conns.began) != 1 || conns.began[0] != "google" {
		t.Fatalf("the panel began %v, want one google sign-in", conns.began)
	}
	if a.connPanel.open {
		t.Fatal("the panel stayed up over the sign-in it started")
	}
}

// AND WHAT COMES BACK SETTLES THE BLOCK AND TELLS THE SESSION — which is the
// whole point of this door: a conversation that gave up on an account learns it
// has one, so the next thing that reaches for it does not ask again.
func TestAPanelSignInSettlesAndTellsTheSession(t *testing.T) {
	agent, a, _ := panelApp(t, twoServices)
	a.width = 100
	a.openConnectFlow("google", "Google", testAuthLink)
	drive(t, a, connectResultMsg{
		service: "google", name: "Google",
		status: connect.Status{
			Service:   connect.Service{ID: "google", Name: "Google"},
			Connected: true, Account: "jane@example.com",
		},
	})

	screen := strings.Join(plainRows(a), "\n")
	if !strings.Contains(screen, glyphConnected+" Google connected as jane@example.com") {
		t.Fatalf("the panel's sign-in did not settle into the tick line:\n%s", screen)
	}
	if len(agent.noted) != 1 || agent.noted[0].id != "google" ||
		agent.noted[0].account != "jane@example.com" {
		t.Fatalf("the session was told %+v", agent.noted)
	}
}

// A SIGN-IN THAT CAME BACK WITH NOTHING IS A SIGN-IN THAT DID NOT HAPPEN, and
// the session is told nothing at all.
func TestAnAbandonedPanelSignInTellsTheSessionNothing(t *testing.T) {
	agent, a, _ := panelApp(t, twoServices)
	a.width = 100
	a.openConnectFlow("google", "Google", testAuthLink)
	drive(t, a, connectResultMsg{service: "google", name: "Google"})

	if len(agent.noted) != 0 {
		t.Fatalf("an unfinished sign-in was reported as connected: %+v", agent.noted)
	}
	if !strings.Contains(strings.Join(plainRows(a), "\n"), "connection didn't complete") {
		t.Fatalf("it settled as something other than unfinished:\n%s",
			strings.Join(plainRows(a), "\n"))
	}
}

// DISCONNECTING TAKES TWO PRESSES. An account another window may be using is not
// a one-keystroke decision.
func TestTheConnectPanelAsksBeforeItDisconnects(t *testing.T) {
	_, a, conns := panelApp(t, twoServices)
	a.width = 100
	typeLine(t, a, "/connect")
	// The connected account is the first row, so the cursor opens on it.
	drive(t, a, key("enter"))

	if len(conns.dropped) != 0 {
		t.Fatalf("one press disconnected %v", conns.dropped)
	}
	if !strings.Contains(strings.Join(plainOverlay(a), "\n"), "enter again to disconnect") {
		t.Fatalf("the panel disconnected silently:\n%s", strings.Join(plainOverlay(a), "\n"))
	}
	// esc un-asks the question rather than closing the panel: the nearest thing
	// to dismiss is the confirmation standing on the row.
	drive(t, a, key("esc"))
	if !a.connPanel.open {
		t.Fatal("esc closed the panel instead of the question on it")
	}
	drive(t, a, key("enter"), key("enter"))
	if len(conns.dropped) != 1 || conns.dropped[0] != "slack" {
		t.Fatalf("the second press disconnected %v", conns.dropped)
	}
	if strings.Contains(strings.Join(plainOverlay(a), "\n"), "jane@example.com") {
		t.Fatalf("the panel still shows the account it dropped:\n%s",
			strings.Join(plainOverlay(a), "\n"))
	}
}

// A SURFACE WITH NO DOOR ONTO CONNECTIONS SAYS SO, rather than opening an empty
// list somebody has to dismiss before it can be told it was useless.
func TestConnectSaysSoWhenThereIsNoDoor(t *testing.T) {
	_, a, _ := connectApp(t)
	typeLine(t, a, "/connect")
	if a.connPanel.open {
		t.Fatal("a surface with no connections opened a panel anyway")
	}
	if !strings.Contains(strings.Join(plainRows(a), "\n"), connectUnavailableWord) {
		t.Fatalf("nothing was said:\n%s", strings.Join(plainRows(a), "\n"))
	}
}

// plainOverlay is whatever list is open, as a reader sees it.
func plainOverlay(a *app) []string {
	out := make([]string, 0, 8)
	for _, line := range a.overlayRows(a.width, a.overlayHeight()) {
		out = append(out, plain(line))
	}
	return out
}

// ── 5. the command, and the settings rows ───────────────────────────────────

// /connect IS ON THE LIST AND IN /help, which is one table read twice
// (commands.go).
func TestConnectIsOnTheCommandList(t *testing.T) {
	found := false
	for _, c := range commands {
		found = found || c.name == "connect"
	}
	if !found {
		t.Fatal("/connect is not on the command list")
	}
	if !strings.Contains(helpText(""), "/connect") {
		t.Fatal("/connect is not in /help")
	}
}

// THE TWO SIGN-IN ROWS HAVE A HOME, a label and a line — the same three things
// the completeness gate asks of every registry row (chrome_test.go).
func TestTheGoogleSignInRowsAreInThePanel(t *testing.T) {
	for _, key := range []string{config.KeyGoogleOAuthClient, config.KeyGoogleOAuthSecret} {
		meta, ok := settingUI[key]
		if !ok {
			t.Fatalf("row %q has no place in the settings panel", key)
		}
		if meta.tab == "" || meta.label == "" || meta.about == "" {
			t.Fatalf("row %q reaches the panel as %+v", key, meta)
		}
		placed := false
		for _, tab := range settingTabs {
			placed = placed || tab == meta.tab
		}
		if !placed {
			t.Fatalf("row %q sits on unknown tab %q", key, meta.tab)
		}
		// No machinery in the words a person reads.
		words := meta.label + " " + meta.about
		for _, banned := range []string{"OAuth", "oauth", "token", "client id"} {
			if strings.Contains(words, banned) {
				t.Fatalf("row %q says %q: %s", key, banned, words)
			}
		}
	}
}

// ── the lane, in the shape the surface reads it ─────────────────────────────

// streamOf is one session event arriving on this surface's turn stream, which is
// where the connect lane's three kinds land.
func streamOf(a *app, ev session.Event) tea.Msg {
	return streamEventMsg{gen: a.gen, ev: ev}
}

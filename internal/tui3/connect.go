package tui3

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/connect"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// CONNECTING AN ACCOUNT.
//
// The agent reaches for something the person has not connected yet — their
// calendar, their mail — and the session stops and asks. That question, the
// browser it opens, and the line that says how it went are this file; the list
// a person opens on purpose is connectpanel.go beside it.
//
// It is drawn in the family the approval question belongs to (consent.go): a
// bottom-anchored block above the draft, answered with one key, queued when
// there is more than one. It is deliberately QUIETER than that question, and the
// difference is what the two are about. An approval question is a call about to
// run against somebody's machine and it takes the loudest colour on this
// surface. This is an offer:
//
//	? Google
//	  openaf wants to connect your Google account
//	  [enter] connect · [esc] not now
//
// Four decisions, each the reason a row is shaped the way it is:
//
//   - THE SERVICE IS THE HEADING. Not "authorize", not the scopes, not the
//     provider's product name for its own sign-in — the thing a person owns, in
//     the word they own it by. The glyph beside it is the one this surface
//     already spends on a question, so the block reads as a question before it
//     is read at all.
//   - ONE LINE OF PURPOSE, DIM. It says who is asking and what for, and it says
//     it in the sentence a person would use out loud. Every other sentence this
//     block could have carried — the scopes, the redirect, the word "OAuth" — is
//     machinery, and machinery on this row is how a person ends up approving
//     something they did not read.
//   - TWO ANSWERS AND NO THIRD. There is no "always" here on purpose: an account
//     is connected once and stays connected, so a widening answer would widen
//     nothing. Declining is "not now" rather than "no" because it IS not now —
//     nothing is remembered, and the next time the agent needs the account it
//     asks again.
//   - IT QUEUES, oldest first, with what is behind it on screen — for the reason
//     the approval question queues (consent.go): a person who answers one
//     question and gets another must have been told it was coming.
//
// AFTER THE ANSWER THE BLOCK GOES AND THE TRANSCRIPT SPEAKS. What the session
// does next is a browser and a wait, and both of those are things that HAPPENED,
// so they land in the conversation where everything else that happened is
// ([app.connectAuth] below).

// Connections is the door onto the accounts this profile has connected. It is
// an interface for the reason [Agent] is one: the surface is driven in a test
// by a scripted one, and the real engine (internal/connect) is wired at the
// door.
//
// Nil is a surface that cannot manage connections — a headless frame, a test,
// a build whose door has not wired one — and /connect says so rather than
// opening an empty list.
type Connections interface {
	// Services is every service this build knows about, each with whether this
	// profile has it and the account it is held as.
	Services() []connect.Status
	// BeginAuth starts one sign-in, with the one address answer the service may
	// have asked for. It may reach the network, so it is called from a command
	// and never from the model loop.
	BeginAuth(ctx context.Context, id, answer string) (*connect.Flow, error)
	// ConnectKey connects one service from a key the person pasted, and hands
	// back where that service stands afterwards. It is the whole of the flow for
	// a [connect.Service] whose Auth is "key": there is no browser, no waiting
	// listener and nothing to abandon, so the panel calls this where it would
	// have called BeginAuth and settles on what comes back.
	//
	// It reaches the network for the same reason BeginAuth does — the key is
	// verified and the account asked for — so it is called from a command too.
	ConnectKey(ctx context.Context, id string, key string) (connect.Status, error)
	// Disconnect forgets one.
	Disconnect(id string) error

	// The three below are what a connected account may DO, which is the
	// question the settings sheet's Connections tab asks (connectcaps.go). They
	// are on this interface rather than on a second one for the reason the first
	// three are on it at all: a surface holds ONE door onto its accounts, and
	// two doors is two answers to "is this connected".

	// Capabilities is what a service may be asked to do, in the order a screen
	// lists them. A service with nothing to say answers with nothing.
	Capabilities(service string) []connect.Capability
	// CapabilityState is where one of them stands right now.
	CapabilityState(service, capability string) connect.CapabilityState
	// SetCapabilityState writes one answer, and says plainly why it could not.
	SetCapabilityState(service, capability string, state connect.CapabilityState) error
}

// keyService reports whether a service is connected by pasting a key.
//
// The word is the ENGINE'S — [connect.AuthKey], where [connect.Service.Auth] is
// defined — and this surface does not keep a second copy of it: two spellings of
// one vocabulary is two things that can drift apart, and the one that drifts is
// the one that decides whether a person gets a browser or a box.
//
// ANYTHING THAT IS NOT [connect.AuthKey] READS AS A BROWSER TRIP: an empty
// field, a word this build has not heard of, [connect.AuthBrowser] itself. That
// is what every plug shipped before this wave was, and it is the safe way round
// — a browser that opens on a service wanting a key is a wasted trip, while a
// key box on a service that has none is a question nobody can answer.
func keyService(service connect.Service) bool { return service.Auth == connect.AuthKey }

// connAsk is one unanswered offer.
type connAsk struct {
	// id is the token [Agent.ResolveConnect] takes back.
	id string
	// service is the id the engine knows it by, and name the word a person does.
	// The name is what every row here draws; the service is what the answer and
	// the transcript are keyed by.
	service string
	name    string
	// needsKey says this account needs a typed answer: a key, or the one thing
	// its address is missing (session.Event's NeedsKey).
	needsKey bool
	// blank distinguishes the non-secret address answer from a pasted key, and
	// secret decides whether the answer may be drawn. ask is the service's own
	// sentence over that box.
	blank  string
	secret bool
	ask    string
	// key is the typed-answer box, and nil until the offer has been accepted. It
	// hangs off the ask rather than off the surface so that everything which drops
	// an offer — the turn settling, /new, a resumed session — drops the half-typed
	// answer with it, in the one assignment it already makes.
	key *editor
}

// entering reports whether the offer at the head of the queue is collecting a
// key right now.
func (a *app) entering() bool {
	return len(a.connAsks) > 0 && a.connAsks[0].key != nil
}

// keyBox is whichever box on this surface is collecting a key, or nil. There are
// two of them and they are never up together — the offer closes the panel on its
// way in (see [app.askConnect]) — so the clipboard has one question to ask
// (app.go's [app.paste]).
func (a *app) keyBox() *editor {
	switch {
	case a.entering():
		return a.connAsks[0].key
	case a.connPanel.open && a.connPanel.entry != nil:
		return &a.connPanel.entry.box
	case a.sheet.open && a.sheet.conn.entry != nil:
		// AND THE THIRD ONE, which is the settings sheet's own row
		// (connectcaps.go). It is the same box asked in the same words, so it
		// takes the clipboard on the same terms — newlines dropped rather than
		// flattened, which is the whole reason this door exists.
		return &a.sheet.conn.entry.box
	}
	return nil
}

// askConnect takes one session.EventConnectAsk.
func (a *app) askConnect(ev session.Event) {
	name := strings.TrimSpace(ev.ServiceName)
	if name == "" {
		// A service the session named only by its id. The id is a word a person
		// half-recognizes ("google"), which is a better heading than nothing at
		// all — and there is no third rung under it worth drawing.
		name = strings.TrimSpace(ev.Service)
	}
	if name == "" {
		return
	}
	// The typed lists follow the draft, and the draft is suspended while an
	// offer is up — a list left open under a modal is a list answering keys
	// nobody is pressing (consent.go says it first).
	a.closeLists()
	if a.pick.open {
		a.pick.close()
	}
	// AND THE PANEL GOES. /connect is the same subject asked the other way
	// round, and a list of services over a question about one of them is two
	// answers to one question.
	a.connPanel.close()
	a.rememberService(ev.Service, name)
	var blank, ask string
	secret := ev.NeedsKey
	if ev.NeedsKey {
		if service, found := a.connectServiceForAsk(ev.Service); found {
			blank = strings.TrimSpace(service.Blank)
			secret = keyService(service)
			ask = strings.TrimSpace(service.KeyAsk)
		}
	}
	a.connAsks = append(a.connAsks, connAsk{
		id: ev.ConnectID, service: ev.Service, name: name, needsKey: ev.NeedsKey,
		blank: blank, secret: secret, ask: ask,
	})
	a.follow()
	a.touch()
}

// connectServiceForAsk finds the catalog words that belong over one typed
// answer. The event road stays unchanged: both ends of --host already compile
// against the same catalog, and the local manager is preferred when a test or
// another door supplies its own rows.
func (a *app) connectServiceForAsk(id string) (connect.Service, bool) {
	if a.conns != nil {
		for _, status := range a.conns.Services() {
			if strings.EqualFold(status.ID, id) {
				return status.Service, true
			}
		}
	}
	for _, plug := range connect.Registered() {
		if strings.EqualFold(plug.Service().ID, id) {
			return plug.Service(), true
		}
	}
	return connect.Service{}, false
}

// asksConnect reports whether an offer owns the keyboard.
func (a *app) asksConnect() bool { return len(a.connAsks) > 0 }

// answerConnect resolves the offer at the head of the queue.
//
// A YES WHEN THE SERVICE NEEDS A TYPED ANSWER IS NOT AN ANSWER YET, it is the
// start of one: the block opens a box in place and waits for the key, or the one
// thing the browser address is missing ([app.connectKeyRow]). The session hears
// nothing until that box is submitted or backed out of — exactly one answer per
// offer, sent when the person has actually given one.
func (a *app) answerConnect(approve bool) {
	if len(a.connAsks) == 0 {
		return
	}
	if approve && a.connAsks[0].needsKey && a.connAsks[0].key == nil {
		a.connAsks[0].key = &editor{}
		a.touch()
		return
	}
	head := a.connAsks[0]
	a.connAsks = a.connAsks[1:]
	switch {
	case a.agent == nil:
	case head.needsKey:
		// The key path has ONE road back into the session, and a decline takes it
		// with an empty key rather than reaching for the other method: two ways
		// to say no about one offer is two things the engine has to keep in step.
		a.agent.ResolveConnectKey(head.id, "")
	default:
		a.agent.ResolveConnect(head.id, approve)
	}
	// Nothing is written to the transcript here, in either direction. An
	// approval's next line is the browser opening, which the session announces
	// and [app.connectAuth] draws; a decline changed nothing, and a surface that
	// recorded "you said not now" would be keeping a note about a thing that did
	// not happen.
	a.touch()
}

// submitConnectKey ends the offer at the head of the queue with whatever is in
// its box: the key, or nothing at all.
//
// AN EMPTY BOX IS A DECLINE and not an error. A person who pressed enter on a
// box they never typed into has said "not now" as plainly as esc would have, and
// a surface that answered them with a complaint would be holding a session open
// to argue about a form.
func (a *app) submitConnectKey() {
	if !a.entering() {
		return
	}
	head := a.connAsks[0]
	a.connAsks = a.connAsks[1:]
	key := strings.TrimSpace(head.key.String())
	if a.agent != nil {
		a.agent.ResolveConnectKey(head.id, key)
	}
	if key != "" && head.blank == "" {
		// The key is on its way to the far end, which takes a network trip and
		// can take a while. That is a thing that HAPPENED, so it lands in the
		// conversation the way the browser handoff does, and the outcome settles
		// it in place ([app.settleConnect]).
		a.openConnectCheck(head.service, head.name)
	}
	a.touch()
}

// dropConnectAsks forgets every unanswered offer. It runs where the approval
// questions are dropped and for the same reason (app.go's [app.settle]): the
// turn that raised them is over, so the answers are late.
func (a *app) dropConnectAsks() {
	if len(a.connAsks) == 0 {
		return
	}
	a.connAsks = nil
	a.touch()
}

// ── the keys ────────────────────────────────────────────────────────────────

// The two answers, as the keys that give them. enter and esc are what the block
// NAMES, because they are the two keys every overlay on this surface already
// answers to — an offer is a yes-or-nothing, and this surface's yes is enter.
//
// y and n are read silently beside them. They are the letters the approval
// question uses one row up (consent.go), a hand that has learned them there will
// reach for them here, and neither can collide with anything while the draft is
// suspended. They are not on the offer: a line naming four keys for two answers
// would be teaching the keyboard instead of the choice.
func (a *app) connectAskKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if !a.asksConnect() {
		return nil, false
	}
	if msg.String() == "ctrl+c" {
		// Leaving is never modal, and mid-turn ctrl+c is the interrupt — which
		// releases the blocked call the honest way.
		return nil, false
	}
	// THE BOX TAKES EVERY OTHER KEY WHILE IT IS OPEN, y and n included: they are
	// two letters of a key, and a surface that read them as answers would be a
	// box that declined halfway through a paste. Only the two keys the offer
	// named survive, meaning what they meant one row up.
	if a.entering() {
		switch msg.String() {
		case "enter":
			a.submitConnectKey()
		case "esc":
			// Back out to not now. The block is gone, the session is told, and
			// what was typed is dropped rather than kept somewhere for later —
			// a half-entered secret is not a draft.
			a.answerConnect(false)
		default:
			// The filter box's key map, which is this surface's ONE way of
			// typing into a one-line box (palette.go's [listNavigate]). There is
			// no list under this one, so the walk and the page are no-ops.
			listNavigate(msg, a.connAsks[0].key, func(int) {}, func() {}, 1)
		}
		a.touch()
		return nil, true
	}
	switch msg.String() {
	case "enter", "y":
		// The one key this block refuses over --host, and it refuses by DOING
		// NOTHING rather than by answering something else: approving is what opens
		// the browser, the browser is on the wrong machine, and a yes turned
		// quietly into a no would be the surface answering a question in somebody
		// else's name. The row above has already dropped the offer (host.go), so
		// this catches the muscle memory and the y.
		if a.hostedBrowserSignIn() {
			return nil, true
		}
		a.answerConnect(true)
	case "esc", "n":
		a.answerConnect(false)
	}
	// Everything else does nothing rather than typing into a conversation that
	// cannot move — the modal rule the approval question states in full.
	return nil, true
}

// ── the block ───────────────────────────────────────────────────────────────

// connectOfferRow is where the offer sits inside the block: under the heading
// and the sentence. The frame needs it to know which row the pointer can be
// over (view.go).
const connectOfferRow = 2

// connectAskHeight is how many rows the block takes: the service, the sentence,
// the offer, and the count of the offers behind it when there are any.
func (a *app) connectAskHeight() int {
	if !a.asksConnect() {
		return 0
	}
	if len(a.connAsks) > 1 {
		return 4
	}
	return 3
}

// connectAskRows draws the block, directly under the approval question's slot
// and above the draft — which is where this surface puts everything it wants
// answered.
func (a *app) connectAskRows(width int) []string {
	// The targets are rewritten by every layout and by nothing else: a stale
	// span is a tap that answers about the previous offer.
	a.connTaps = nil
	if !a.asksConnect() {
		return nil
	}
	head := a.connAsks[0]
	out := make([]string, 0, 4)
	out = append(out, a.pal.askBold(glyphAsk)+" "+a.pal.bold(a.pal.ink(fit(head.name, width-2))))
	// THE SENTENCE IS THE REASON THE BLOCK IS THERE, and over --host the reason
	// has changed: the session reached for an account and this surface cannot get
	// one connected, so the row says that instead of asking for something it
	// cannot deliver (host.go). The offer below it drops to "not now" for the
	// same reason.
	sentence := connectPurpose(head.name)
	if a.hostedBrowserSignIn() {
		sentence = connectAskRemoteWord
	} else if head.key != nil && head.blank != "" {
		sentence = head.ask
	}
	out = append(out, a.pal.dim(fit("  "+sentence, width)))
	// THE BOX TAKES THE OFFER'S OWN ROW, so the block does not grow, shift or
	// re-flow under a hand that has just pressed a key on it. The sentence above
	// stays because it is still the reason the box is there.
	if head.key != nil {
		out = append(out, a.connectKeyRow(head, width))
	} else {
		out = append(out, a.connectOffer(width))
	}
	if more := len(a.connAsks) - 1; more > 0 {
		out = append(out, a.pal.dim(fit("  "+itoa(more)+" more", width)))
	}
	return out
}

// connectPurpose is the one quiet sentence: who is asking, and what for. The
// product names itself from the one constant that holds its name (styles.go), so
// a rename is a rename and not a search.
func connectPurpose(name string) string {
	return product + " wants to connect your " + name + " account"
}

// connectMark is what the pointer is over on row i of the block, which is the
// frame's half of the same geometry ([app.chrome]).
//
// The offer is the one row of it that is pressable. The heading and the sentence
// are statements, and a statement that lit up under the pointer would be
// claiming to be a thing you could press.
func (a *app) connectMark(i int) chromeRow {
	if i == connectOfferRow {
		return chromeRow{kind: chromeConnectAsk, index: i}
	}
	return chromeRow{}
}

// connectOffer is the answers. The whole line takes the question hue and the
// keys are bold within it, exactly as the approval question's offer is: a person
// looking for which key to press finds the key, and the sentence around it is
// there to be recognized rather than read twice.
func (a *app) connectOffer(width int) string {
	// Pairs: the words at even indices, the keys — the only bold cells on the
	// line — at odd ones, which is what [app.recordConnectTaps] reads.
	parts := []string{"  ", "[enter]", " connect · ", "[esc]", " not now"}
	// OVER --HOST THERE IS ONE ANSWER, and the row offers only that one. An
	// [enter] that could not connect anything would be a key drawn as an
	// affordance and answering as a failure, which is the exact thing the
	// sentence above it has just said will not work (host.go).
	if a.hostedBrowserSignIn() {
		parts = []string{"  ", "[esc]", " not now"}
	}
	line := strings.Join(parts, "")
	if ansi.StringWidth(line) > width {
		// Too narrow for both answers spelled out. The line is cut rather than
		// re-spelled — there is no shorter honest wording for two words — and it
		// records no targets, because a target under an ellipsis is a press that
		// answers something a person cannot read.
		return a.pal.ask(fit(line, width))
	}
	a.recordConnectTaps(parts)
	var out string
	for i, part := range parts {
		if i%2 == 1 {
			out += a.pal.askBold(part)
			continue
		}
		out += a.pal.ask(part)
	}
	if a.hoveringConnectAsk() {
		return a.pal.cursor(out, width)
	}
	return out
}

// ── the key, typed in place ─────────────────────────────────────────────────
//
// Some services have no sign-in page: what they hand a person is a key, from a
// settings screen somewhere, and connecting one means pasting it. The question
// is the same question — may aforge connect this account — so the block is the
// same block, and only the row that WAS the offer changes:
//
//	? Notion
//	  openaf wants to connect your Notion account
//	  › paste your Notion key
//
// Three decisions:
//
//   - THE KEY IS NEVER DRAWN. Not once, not while it is being typed, not
//     behind a "show" toggle. What is on the row is a bullet per character and
//     how many of them there are — the same mask the settings panel puts over a
//     credential (settings.go), and for the stronger reason: this row is on
//     screen while somebody is at a desk with a key in their clipboard.
//   - THE COUNT IS THE ONLY TELEMETRY, and it is there for the paste. A key is
//     forty or two hundred characters, the bullets run off the end of the row
//     long before that, and the count is what tells a person the whole thing
//     arrived. It is dim and it is a number, which is what this surface spends
//     on a fact nobody is reading twice.
//   - AN EMPTY BOX DRAWS THE SENTENCE AND NOT A ROW OF NOTHING. The emptiness
//     law with a hint in its place: the box says what to put in it while there
//     is nothing in it, which is the picker's own bargain (palette.go) and costs
//     the block no extra row.

// connectKeyHint is what an empty box says: the one instruction, in the word the
// person owns the account by.
func connectKeyHint(name, blank string) string {
	if blank = strings.TrimSpace(blank); blank != "" {
		return "your " + strings.ToLower(blank)
	}
	return "paste your " + name + " key"
}

// ── the box itself, wherever it is opened ───────────────────────────────────
//
// There are two places on this surface where a person gives a key on purpose —
// the /connect panel (connectpanel.go) and the settings sheet's Connections tab
// (connectcaps.go) — and they are ONE BOX with one shape, because they are one
// question. What follows is that box: the value, the two lines around it, and
// the one method that types into it.
//
// The offer's row (above) is deliberately NOT built on this. It is a single row
// inside a block whose height the session's question owns, and the two lines
// this box can grow are two lines that block cannot spare.

// keyEntry is one typed answer being given: which service it is for, the word a
// person knows it by, what the service says about answering, and the box.
//
// ask and link are copied off the [connect.Service] at the moment the box opens
// rather than looked up while it is drawn: a paint runs many times a second, and
// what a service says about its own key does not change between two of them.
type keyEntry struct {
	id   string
	name string
	// blank is the plain name of a visible answer. Empty means this is a key.
	blank string
	// answers is the service's closed list for blank. It is copied when the box
	// opens so the question and the accepted values cannot drift apart.
	answers []string
	// secret says the typed answer is a key and must never be drawn. A browser
	// address's one missing fact is false so a typo stays visible.
	secret bool
	// ask is the instruction for a service that wants more than a key — the
	// workspace, a space, then the key — or one visible address answer
	// ([connect.Service.KeyAsk]). Empty for nearly all of them.
	ask string
	// link is where the key is to be found ([connect.Service.KeyHint]). Empty
	// where nobody could say, and then nothing is drawn.
	link string
	box  editor
}

// newKeyEntry opens the box for one service.
func newKeyEntry(service connect.Service, name string) *keyEntry {
	return &keyEntry{
		id:      service.ID,
		name:    name,
		blank:   strings.TrimSpace(service.Blank),
		answers: append([]string(nil), service.Answers...),
		secret:  keyService(service),
		ask:     strings.TrimSpace(service.KeyAsk),
		link:    strings.TrimSpace(service.KeyHint),
	}
}

// typeInto is every key that is not one of the two the box answers to.
//
// It is the filter box's key map, which is this surface's ONE way of typing into
// a one-line box (palette.go's [listNavigate]). There is no list under this box,
// so the walk and the page are no-ops.
func (e *keyEntry) typeInto(msg tea.KeyPressMsg) {
	listNavigate(msg, &e.box, func(int) {}, func() {}, 1)
}

// value is what has been typed, trimmed — an answer or nothing at all.
func (e *keyEntry) value() string { return strings.TrimSpace(e.box.String()) }

// keyHintLine is the one dim line under the box: where this key is to be found.
//
// ── IT IS DRAWN WHILE THE BOX IS OPEN AND AT NO OTHER TIME ──
//
// Not on the row before somebody presses enter on it, and not after the account
// is connected. A person browsing a catalog of two hundred services is not
// looking for anybody's settings page, and a person who has connected an account
// has already found it — so on both of those screens this is a line of furniture
// under every row. The one moment it is the most useful thing on the screen is
// the moment the box is open and empty, which is the moment somebody realises
// they do not have the key in their clipboard after all.
//
// THE ADDRESS IS THE WHOLE OF IT. The scheme is cut because nobody reads it and
// it costs eight cells of a line that has to fit; the hyperlink is applied over
// the shortened text, so a terminal that can follow it opens the real address
// and one that cannot shows something a person can type (opener.go).
func keyHintLine(link string, pal palette, width int) string {
	link = strings.TrimSpace(link)
	if link == "" {
		return ""
	}
	shown := strings.TrimPrefix(strings.TrimPrefix(link, "https://"), "http://")
	lead := "  find it at "
	return pal.dim(lead + linkify(fit(shown, width-ansi.StringWidth(lead)), link))
}

// keyBoxLines is the box as the lines it takes, and where the caret sits inside
// them: the instruction where the service has one, the answer box, and either
// the accepted values or the address where a key lives.
//
// It answers a caret ROW as well as a column because the instruction can stand
// above the box, and a caller that assumed the box was the first line would put
// the caret on a sentence.
//
// indent is how far in the whole block sits, which is the one thing the two
// surfaces disagree about: the panel's box takes the draft's own position at the
// left edge, and the sheet's is drawn INSIDE the row it was opened from and has
// to hang under it (connectcaps.go). Everything else about the block — what it
// says, what it shows, what it links — is the same in both places.
func keyBoxLines(entry *keyEntry, pal palette, width, indent, maxRows int) ([]string, int, int) {
	if indent < 0 {
		indent = 0
	}
	if maxRows < 1 {
		maxRows = 1
	}
	lead := strings.Repeat(" ", indent)
	width -= indent
	askRows := make([]string, 0, 3)
	if entry.ask != "" {
		for _, line := range wrap(entry.ask, width-2) {
			askRows = append(askRows, lead+pal.dim(fit("  "+line, width)))
		}
	}
	line, caretX := keyLine(&entry.box, connectKeyHint(entry.name, entry.blank), entry.secret, pal, width)
	tailRows := make([]string, 0, 3)
	if !entry.secret && len(entry.answers) != 0 {
		for _, line := range wrap(strings.Join(entry.answers, ", "), width-2) {
			tailRows = append(tailRows, lead+pal.dim(fit("  "+line, width)))
		}
	} else if hint := keyHintLine(entry.link, pal, width); hint != "" {
		tailRows = append(tailRows, lead+hint)
	}
	// THE ANSWER LIST GIVES WAY FIRST. The question and its box are what a
	// person is answering; the closed list is help beside them, and a refusal
	// names it again if the answer is not accepted. If those two still outgrow
	// the box, the question keeps its beginning and drops its tail.
	over := len(askRows) + 1 + len(tailRows) - maxRows
	if over > 0 {
		drop := min(over, len(tailRows))
		tailRows = tailRows[:len(tailRows)-drop]
		over -= drop
	}
	if over > 0 {
		drop := min(over, len(askRows))
		askRows = askRows[:len(askRows)-drop]
	}
	caretRow := len(askRows)
	out := append(askRows, lead+line)
	out = append(out, tailRows...)
	return out, caretX + indent, caretRow
}

// envExampleFor is the environment variable this surface names when it teaches
// somebody that a variable's NAME is an answer too.
//
// It is built out of the service in front of them rather than picked once and
// spelled into a sentence: "$STRIPE_KEY" on a screen about Chargebee is an
// example a person has to translate before they can use it, and the whole reason
// the line exists is that the thing it teaches is not guessable. Nothing is
// promised by it — a variable may be called anything at all, and the engine
// reads whichever one is named (internal/connect's keyref.go).
func envExampleFor(name string) string {
	word := strings.ToUpper(strings.TrimSpace(name))
	if at := strings.IndexAny(word, " \t"); at > 0 {
		// The first word only. A service whose name is four words would
		// otherwise produce an example longer than the line it sits on.
		word = word[:at]
	}
	clean := make([]rune, 0, len(word))
	for _, r := range word {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			clean = append(clean, r)
		case len(clean) > 0 && clean[len(clean)-1] != '_':
			clean = append(clean, '_')
		}
	}
	word = strings.Trim(string(clean), "_")
	if word == "" || word[0] >= '0' && word[0] <= '9' {
		return "$API_KEY"
	}
	return "$" + word + "_KEY"
}

// connectKeyRow is the offer's row while a key is being typed into it.
func (a *app) connectKeyRow(head connAsk, width int) string {
	line, _ := keyLine(head.key, connectKeyHint(head.name, head.blank), head.secret, a.pal, width-2)
	return "  " + line
}

// keyLine draws one key being typed — the mark, the mask, and the count — and
// says which column the caret sits in. It is shared by the offer's row and by
// the panel's own box (connectpanel.go), because there is ONE way of entering a
// key on this surface and a second one that looked almost like it would be a
// second thing to trust.
func keyLine(box *editor, hint string, secret bool, pal palette, width int) (string, int) {
	lead := ansi.StringWidth(prompt)
	mark := pal.dim(prompt)
	if len(box.value) == 0 {
		return mark + pal.dim(fit(hint, width-lead)), lead
	}
	if !secret {
		answer := box.String()
		shown := fit(answer, max(0, width-lead))
		before := fit(string(box.value[:box.cursor]), max(0, width-lead))
		return mark + pal.ink(shown), lead + ansi.StringWidth(before)
	}
	bullet := "•"
	if pal.ascii {
		bullet = "*"
	}
	count := itoa(len(box.value))
	// The mask gives way and the count does not: a run of bullets cut short says
	// nothing a shorter run does not already say, and the number is the one cell
	// on the row carrying a fact.
	room := width - lead - ansi.StringWidth(count) - 2
	if room < 0 {
		room = 0
	}
	shown := min(len(box.value), room)
	return mark + pal.ink(strings.Repeat(bullet, shown)) + pal.dim("  "+count), lead + shown
}

// ── the pointer ─────────────────────────────────────────────────────────────

// connTap is one answer's columns on the offer row. A press inside
// [span.from, span.to) is that answer, and nothing outside any span answers
// anything.
type connTap struct {
	span    hudSpan
	approve bool
}

// recordConnectTaps writes the offer line's columns: every key chip on it, and
// the word beside it, are one target.
//
// THE WORD IS PART OF THE TARGET, for the reason the approval question's is:
// `[esc]` is five cells, `[esc] not now` is thirteen, and that is the difference
// between a target a person hits and one they aim at. The separator between the
// two answers belongs to neither — a press in the gap must not resolve as
// either.
func (a *app) recordConnectTaps(parts []string) {
	taps := make([]connTap, 0, 2)
	at := 0
	for i, part := range parts {
		width := ansi.StringWidth(part)
		approve, ok := false, false
		switch part {
		case "[enter]":
			approve, ok = true, true
		case "[esc]":
			approve, ok = false, true
		}
		if !ok {
			at += width
			continue
		}
		to := at + width
		if i+1 < len(parts) {
			to += ansi.StringWidth(strings.TrimSuffix(parts[i+1], " · "))
		}
		taps = append(taps, connTap{span: hudSpan{from: at, to: to}, approve: approve})
		at += width
	}
	a.connTaps = taps
}

// connectPress resolves a click on the block, and reports whether it took it.
//
// THE BLOCK SWALLOWS EVERY PRESS IN IT, answer or no answer, for the reason the
// approval question's does: it is a thing the session is waiting on, and a press
// that missed the offer and fell through would expand a tool call while somebody
// was trying to answer a question about their account.
func (a *app) connectPress(x, y int) bool {
	if !a.asksConnect() || a.copy.on || a.sheet.open {
		return false
	}
	// THE ROW IS RESOLVED BEFORE THE COLUMN: laying the chrome out is what writes
	// the spans, and reading them first would be reading where the answers were
	// drawn on the frame before this one.
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeConnectAsk {
		return false
	}
	for _, tap := range a.connTaps {
		if !tap.span.holds(x) {
			continue
		}
		a.answerConnect(tap.approve)
		return true
	}
	return true
}

// hoveringConnectAsk reports whether the pointer is on the offer row.
func (a *app) hoveringConnectAsk() bool { return a.hot.kind == hoverConnectAsk }

// ── the browser, and what came of it ────────────────────────────────────────
//
// Everything below here is a REPORT rather than a question, so it is drawn where
// the reports are: in the conversation, as one block that opens waiting and
// settles in place. It is the shape a compaction pass has (render.go's
// [app.compactRow]) and it is the shape for the same reason — a thing that takes
// seconds and would otherwise be a silence.

// connectState is where one handshake is.
type connectState uint8

const (
	// connectWaiting is the browser being somewhere else. It is the only state
	// that animates, and the only one that is not cached.
	connectWaiting connectState = iota
	connectConnected
	connectFailed
)

// connectCard is one sign-in, from the browser opening to the line it leaves
// behind. It hangs off [entry.conn] for kind entryConnect and nothing else — a
// POINTER, for the reason a proposal's card is one (app.go): the row and the
// state a later event writes into it must never be able to disagree.
type connectCard struct {
	service string
	name    string
	// link is where the sign-in happens, drawn as text under the waiting line so
	// a person on the far end of an ssh connection can still get there.
	link    string
	account string
	state   connectState
	// byKey says this attempt was a key somebody pasted rather than a browser
	// trip. It changes two sentences and nothing else: what the card is waiting
	// FOR while it waits, and what it says when it did not work — "the key
	// didn't work" is the honest line for a key, and "the connection didn't
	// complete" is the honest line for a browser nobody came back from. Neither
	// sentence is true of the other flow.
	byKey bool
	// copied says the link has been taken to the clipboard, which the card says
	// out loud for one reason: a press that changes nothing on the screen is a
	// press a person repeats, and then doubts.
	copied bool
}

// connectLinkPress copies a waiting card's sign-in link, and reports whether
// the press was one it wanted.
//
// THE WHOLE CARD IS THE TARGET, not the two rows the link happens to wrap over.
// It is the argument a thinking block makes for taking a click anywhere on
// itself (app.go): the card has no other gesture, and asking somebody to land
// on a particular row of a wrapped address is asking them to aim.
//
// It exists because a sign-in link is the one thing on this surface that a
// person needs somewhere ELSE — in the browser on their laptop, when the
// session is on a machine three hops away that has no browser at all. Copy
// mode can reach it and it reaches it as the frame drew it: two rows, indented,
// with the address split across them. Here it is one link, whole.
func (a *app) connectLinkPress(i int) (tea.Cmd, bool) {
	if !a.connectLinkable(i) {
		return nil, false
	}
	card := a.bodyDeck().entries[i].conn
	card.copied = true
	a.touch()
	return tea.Raw(osc52(card.link, a.tmux)), true
}

// connectLinkable reports whether this block is a sign-in still waiting, with an
// address worth taking. It is the whole of [app.connectLinkPress]'s condition,
// asked on its own so the pointer can light exactly what a press would act on —
// and so a card that has SETTLED, which has nothing left to copy, stays as dark
// as any other report in the transcript (hover.go's law).
func (a *app) connectLinkable(i int) bool {
	es := a.bodyDeck().entries
	if i < 0 || i >= len(es) || es[i].kind != entryConnect {
		return false
	}
	card := es[i].conn
	return card != nil && card.state == connectWaiting && card.link != ""
}

// connectAuth takes one session.EventConnectAuth: the sign-in has started, and
// it finishes in a browser.
//
// THE HANDOFF AND THE LINK ARE NOT AN EITHER-OR. The browser is opened, and the
// link is written down whether or not that worked — see opener.go for why the
// second one is not a fallback.
func (a *app) connectAuth(ev session.Event) {
	a.openConnectFlow(ev.Service, a.serviceName(ev.Service, ev.ServiceName), ev.AuthURL)
}

// openConnectFlow is the half both doors share: the session's own
// EventConnectAuth, and a row pressed in the /connect panel. It opens the
// browser and puts the waiting block on screen.
func (a *app) openConnectFlow(service, name, link string) {
	a.rememberService(service, name)
	if a.hosted() {
		// A FLOW MINTED ON ANOTHER MACHINE IS NOT OPENED ON THIS ONE. Both doors
		// into here are closed over --host already (host.go), so this is the belt
		// and not the braces — but the cost of being wrong is a browser sent to
		// http://127.0.0.1:<port> on the WRONG localhost, which is either nothing
		// at all or somebody else's server. The card still goes up with the link
		// written on it, which is what the card's own link field is for.
		a.note(connectRemoteWord)
	} else if err := processOpener(link); err != nil {
		// The platform could not do it. That is not a failed sign-in — the link
		// under the block is still a way through — so it is said once, dim, and
		// the block goes up as it would have anyway.
		a.note(err.Error())
	}
	a.closeLive()
	a.entries = append(a.entries, entry{
		kind: entryConnect, turn: a.turn,
		conn: &connectCard{service: service, name: name, link: link, state: connectWaiting},
	})
	a.follow()
	a.touch()
}

// openConnectCheck is the key path's half of [app.openConnectFlow]: the key has
// been handed over and the far end is being asked about it.
//
// There is no browser, no link and nothing to abandon — which is the whole
// difference between the two flows — so this is the waiting block and nothing
// else. It settles on the same EventConnectDone.
func (a *app) openConnectCheck(service, name string) {
	a.rememberService(service, name)
	a.closeLive()
	a.entries = append(a.entries, entry{
		kind: entryConnect, turn: a.turn,
		conn: &connectCard{
			service: service, name: a.serviceName(service, name),
			state: connectWaiting, byKey: true,
		},
	})
	a.follow()
	a.touch()
}

// connectDone takes one session.EventConnectDone and settles the block the
// browser left open.
func (a *app) connectDone(ev session.Event) {
	a.settleConnect(ev.Service, a.serviceName(ev.Service, ev.ServiceName), ev.Account, ev.Failed)
}

// settleConnect closes the newest unsettled block for a service, and opens a
// settled one when there is none.
//
// The second case is ordinary rather than defensive: a person can finish a
// sign-in that another window started, and an outcome with no block to land on
// is still an outcome worth one line.
func (a *app) settleConnect(service, name, account string, failed bool) {
	state := connectConnected
	if failed {
		state = connectFailed
	}
	for i := len(a.entries) - 1; i >= 0; i-- {
		e := &a.entries[i]
		if e.kind != entryConnect || e.conn == nil || e.conn.state != connectWaiting {
			continue
		}
		if e.conn.service != service {
			continue
		}
		e.conn.state, e.conn.account = state, strings.TrimSpace(account)
		if e.conn.name == "" {
			// The block already knows what this service is CALLED — it was named
			// when the browser opened — and the outcome event carries only an id.
			// A name written here would be the id overwriting the word.
			e.conn.name = name
		}
		e.stale = true
		a.follow()
		a.touch()
		return
	}
	a.closeLive()
	a.entries = append(a.entries, entry{
		kind: entryConnect, turn: a.turn,
		conn: &connectCard{
			service: service, name: a.serviceName(service, name),
			account: strings.TrimSpace(account), state: state,
		},
	})
	a.follow()
	a.touch()
}

// rememberService and serviceName are the surface's own memory of what a
// service is CALLED.
//
// The session names one once — on the offer, which is the only event carrying a
// service's name — and the two that follow it name only the id, because by then
// the naming has been done. So the word is kept here, keyed by the id every
// event does carry, and the rungs below it are the id itself and then nothing.
// A surface that re-asked would have nobody to ask.
func (a *app) rememberService(service, name string) {
	service, name = strings.TrimSpace(service), strings.TrimSpace(name)
	if service == "" || name == "" || name == service {
		return
	}
	if a.connNames == nil {
		a.connNames = map[string]string{}
	}
	a.connNames[service] = name
}

func (a *app) serviceName(service, given string) string {
	if given = strings.TrimSpace(given); given != "" {
		return given
	}
	if name := a.connNames[strings.TrimSpace(service)]; name != "" {
		return name
	}
	return strings.TrimSpace(service)
}

// connectAnimating reports whether a browser is still out there, which is what
// keeps the paint clock turning while one is (app.go's [app.paint]). It is the
// one thing this file animates.
func (a *app) connectAnimating() bool {
	for i := range a.entries {
		if e := &a.entries[i]; e.kind == entryConnect && e.conn != nil &&
			e.conn.state == connectWaiting {
			return true
		}
	}
	return false
}

// ── the block in the transcript ─────────────────────────────────────────────

// glyphConnected is the one tick this surface draws, and it is worth saying why
// it exists at all: there is no success glyph here on purpose (styles.go's D11),
// because a column of ticks beside tool calls is a column that has to be read to
// learn nothing.
//
// A CONNECTION IS NOT AN OUTCOME, IT IS A STATE. "Connected" and "not connected"
// are two things a person is scanning a list to tell apart, and telling them
// apart is the whole job of the row — which is exactly the case the tool column
// does not have, where quiet already means fine. So the tick earns its place
// here, and nowhere else.
const (
	glyphConnected      = "✓"
	glyphConnectedASCII = "+"
)

// connectRows draws one handshake: waiting, connected, or not.
//
//	⠋ waiting in your browser…
//	  https://accounts.google.com/…
//
//	✓ Google connected as jane@example.com
//
// THE EMPTINESS LAW IS THE ACCOUNT. A connection whose account nobody reported
// says "Google connected" and stops — there is no parenthetical, no "as
// (unknown)", nothing standing in for a fact this surface does not have.
func (a *app) connectRows(e *entry, width int) []string {
	card := e.conn
	if card == nil {
		return nil
	}
	switch card.state {
	case connectWaiting:
		mark := tokens.Spinner(a.paints / spinnerStep)
		if a.linear {
			// A spinner is a claim made thirty times a second, and a surface being
			// read aloud hears it thirty times a second (toolview.go's objection).
			mark = glyphRunASCII
		}
		waiting := "waiting in your browser…"
		if card.byKey {
			// No browser was opened, so the surface does not claim one. What it
			// is doing is asking the far end whether the key is any good.
			waiting = "checking your " + card.name + " key…"
		}
		out := []string{a.pal.dim(fit(mark+" "+waiting, width))}
		if card.link == "" {
			return out
		}
		// THE LINK IS WRAPPED AND NEVER CUT. It has no spaces in it, so it breaks
		// at the frame's width rather than at a word — and a link with its tail
		// truncated away is a link nobody can use, which is the one thing this
		// row exists to prevent. The hyperlink is applied to each line after the
		// layout is done with it: an OSC 8 occupies no cells (opener.go).
		for _, line := range wrap(card.link, width-2) {
			out = append(out, a.pal.dim("  "+linkify(line, card.link)))
		}
		if card.copied {
			out = append(out, a.pal.dim(fit("  copied — paste it wherever you can sign in", width)))
		}
		return out

	case connectFailed:
		// Honest and quiet. It did not work, nothing was connected, and the
		// person can ask again — none of which is worth the failure glyph, which
		// on this surface means a call that broke.
		mark := a.linearMark(glyphIdle, glyphIdleASCII)
		said := card.name + " connection didn't complete"
		if card.byKey {
			// The key path's honest sentence. Nothing about the far end is
			// claimed — it may have refused the key, it may not have answered at
			// all — and either way the person's next move is the same one.
			said = "the " + card.name + " key didn't work"
		}
		return []string{a.pal.dim(fit(mark+" "+said, width))}
	}

	mark := glyphConnected
	if a.linear {
		mark = glyphConnectedASCII
	}
	line := card.name + " connected"
	if card.account != "" {
		line += " as " + card.account
	}
	return []string{a.pal.add(mark) + a.pal.dim(fit(" "+line, width-1))}
}

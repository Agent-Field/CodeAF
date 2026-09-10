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
}

// connAskOf is the lane's own facts about one open offer, found by the token the
// question carries.
//
// THE QUESTION IS THE QUESTION AND THIS IS WHAT IS LEFT. The block draws and
// answers the offer (question.go); what it does not carry is the three things
// only this lane knows — the service id the transcript is keyed by, whether the
// typed answer is a secret or the missing half of an address, and the word the
// catalog puts over the box — so they stay here, keyed by the same token.
func (a *app) connAskOf(id string) (connAsk, bool) {
	for _, ask := range a.connAsks {
		if ask.id == id {
			return ask, true
		}
	}
	return connAsk{}, false
}

// entering reports whether the open offer is collecting a typed answer right
// now, which on the block is the whole of what a key offer is: the box under the
// question IS the answer lane ([session.InputText]).
func (a *app) entering() bool {
	head, ok := a.connectAsking()
	return ok && head.question.Input.Kind == session.InputText
}

// connectAsking is the open connect question, and false where there is none.
func (a *app) connectAsking() (questionShown, bool) {
	for _, open := range a.questions {
		if open.question.Kind == session.QuestionConnect {
			return open, true
		}
	}
	return questionShown{}, false
}

// keyBox is whichever box on this surface is collecting a key, or nil. There are
// two of them and they are never up together — the offer closes the panel on its
// way in (see [app.askConnect]) — so the clipboard has one question to ask
// (app.go's [app.paste]).
func (a *app) keyBox() *editor {
	switch {
	case a.entering():
		// THE BLOCK'S BOX IS THE MAIN DRAFT, which is the whole of what moving
		// this question onto the block bought: the answer is typed where every
		// answer is typed ([questionOwnsBox]), and the clipboard asks the same
		// box it always asks.
		return &a.input
	case a.connPanel.open && a.connPanel.entry != nil:
		return &a.connPanel.entry.box
	case a.at(pageSettings) && a.sheet.conn.entry != nil:
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
	held := connAsk{
		id: ev.ConnectID, service: ev.Service, name: name, needsKey: ev.NeedsKey,
		blank: blank, secret: secret, ask: ask,
	}
	a.connAsks = append(a.connAsks, held)
	a.raiseQuestion(a.connectShown(held))
	a.follow()
	a.touch()
}

// connectShown is one offer as the block holds it: the engine's own question
// object, plus what this program does about an answer.
//
// THE OBJECT IS THE ENGINE'S OWN BUILDER ([session.ConnectQuestion]). The lane
// hands the surface the event a moment before the questions lane reaches it, and
// the block keys a question by its lane and its token — so one builder, called
// from both roads, with the three words only this surface has passed into it.
func (a *app) connectShown(ask connAsk) questionShown {
	q := session.ConnectQuestion(ask.id, ask.name, ask.needsKey,
		firstNonEmpty(ask.ask, connectKeyHint(ask.name, ask.blank)), ask.secret)
	if a.hostedBrowserSignInFor(ask) {
		// A BROWSER SIGN-IN CANNOT BE FINISHED FROM HERE, so the offer does not
		// pretend it can: the reason says what is actually true and the `connect`
		// answer is taken off, leaving `not now` and `esc` (host.go says why the
		// line is the browser and not the account).
		q.Reason = connectAskRemoteWord
		q.Options = connectRemoteOptions(q.Options)
	}
	return questionShown{
		question: q,
		answered: func(answer session.Answer) session.Answer {
			a.settleConnectAsk(ask, answer)
			return answer
		},
	}
}

// connectRemoteOptions is the offer's answers with the one that cannot work
// taken off. A key drawn as an affordance and answering as a failure is worse
// than an answer that is not there.
func connectRemoteOptions(options []session.AnswerOption) []session.AnswerOption {
	out := make([]session.AnswerOption, 0, len(options))
	for _, option := range options {
		if option.Safe {
			out = append(out, option)
		}
	}
	return out
}

// settleConnectAsk is what this PROGRAM does about an answer: the lane's own
// facts are forgotten, and a key that was actually given opens the block that
// says the far end is being asked about it.
//
// IT DECIDES NOTHING. The answer is already on its way to
// [session.Agent.ResolveQuestion], which reads the lane off it and sends words
// through `ResolveConnectKey` and a bare pick through `ResolveConnect`.
//
// Nothing is written to the transcript on a no, in either shape. An approval's
// next line is the browser opening, which the session announces and
// [app.connectAuth] draws; a decline changed nothing, and a surface that
// recorded "you said not now" would be keeping a note about a thing that did not
// happen.
func (a *app) settleConnectAsk(ask connAsk, answer session.Answer) {
	a.forgetConnectAsk(ask.id)
	words := strings.TrimSpace(answer.Words())
	if words == "" || ask.blank != "" {
		return
	}
	// The key is on its way to the far end, which takes a network trip and can
	// take a while. That is a thing that HAPPENED, so it lands in the
	// conversation the way the browser handoff does, and the outcome settles it
	// in place ([app.settleConnect]).
	a.openConnectCheck(ask.service, ask.name)
}

// forgetConnectAsk drops the lane's own facts about one offer.
func (a *app) forgetConnectAsk(id string) {
	for i, ask := range a.connAsks {
		if ask.id == id {
			a.connAsks = append(a.connAsks[:i], a.connAsks[i+1:]...)
			return
		}
	}
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

// asksConnect reports whether an offer is open on the block.
//
// IT IS NO LONGER "OWNS THE KEYBOARD", and that is the whole of what changed
// here: the block is not modal (question.go), so every key it has not drawn
// falls straight through to the message box. What is still true — and what the
// callers of this actually want — is that the session is waiting on somebody
// about an account.
func (a *app) asksConnect() bool {
	_, ok := a.connectAsking()
	return ok
}

// dropConnectAsks takes back every unanswered offer. It runs where the approval
// questions are dropped and for the same reason (app.go's [app.settle]): the
// turn that raised them is over, so the answers are late.
//
// IT SAYS SO RATHER THAN VANISHING ([app.withdrawQuestion] writes the one dim
// line), because an offer that was on screen a moment ago and is simply gone
// leaves somebody hunting for what they were about to answer.
func (a *app) dropConnectAsks() {
	if len(a.connAsks) == 0 {
		return
	}
	for _, ask := range a.connAsks {
		a.withdrawQuestion(session.ConnectQuestion(ask.id, ask.name, ask.needsKey, ask.ask, ask.secret),
			connectOfferGoneWord)
	}
	a.connAsks = nil
	a.touch()
}

// connectOfferGoneWord is why an offer was taken back: the turn that wanted the
// account has finished, so there is nothing left for a yes to unblock.
const connectOfferGoneWord = "the turn that asked for it has finished"

// ── what the lane keeps of its own ──────────────────────────────────────────
//
// THE ROW, THE KEYS AND THE POINTER ARE GONE. The offer had a three-row block of
// its own directly under the approval question's, with `[enter] connect · [esc]
// not now` on the third row, its own click targets, and a key router that took
// every keystroke on the frame while it was up — including `y` and `n`, read
// silently beside the two it named. All of it is deleted. The offer is a card on
// the question block now: `1` connects, `2` is not now, `esc` is *later*, and
// every other key falls through to the message box, which is where the typed
// answer goes.
//
// What is left in this file is the lane's own three things — the service id the
// transcript is keyed by, the catalog's sentence over the box and whether the
// answer is a secret ([connAsk]) — the masked box those decide the shape of
// (below), and the browser flow the answer raises.

// connectPurpose is the one quiet sentence: who is asking, and what for. The
// product names itself from the one constant that holds its name (styles.go), so
// a rename is a rename and not a search.
//
// It is the /connect panel's heading now. The offer above the box says
// [session.ConnectAskReason] instead, which is the same fact in the engine's own
// voice — one question, one sentence, whichever road it arrived by.
func connectPurpose(name string) string {
	return product + " wants to connect your " + name + " account"
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

// keyLine draws one key being typed — the mark, the mask, and the count — and
// says which column the caret sits in. It is shared by the MESSAGE BOX while a
// question asked for a secret (input.go's [app.secretDraftBlock]) and by the
// panel's own box (connectpanel.go), because there is ONE way of entering a key
// on this surface and a second one that looked almost like it would be a second
// thing to trust.
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
// A CONNECTION THAT CAME OFF IS THE VOCABULARY'S SETTLED MARK and not a second
// tick: one shape for "this is done", on a row of accounts as on a row of work.
const (
	glyphConnected      = tokens.GlyphSettled
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
			mark = a.icon(tokens.GWorking)
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

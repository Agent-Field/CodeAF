package tui3

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// errandAgent is the scripted session an errand runs against. It is
// [fakeAgent] with two differences, both of which are about what an errand
// actually does: its Submit CLOSES the stream when the scripted turn runs out,
// so a test does not spend a second and a half waiting on a channel nobody will
// close, and it answers the standing card ([session.Agent.ResolveStanding]),
// which is the one thing the surface asks of an errand's agent that it does not
// ask of an ordinary one.
type errandAgent struct {
	fakeAgent
	answered []session.StandingAnswer
	answerID []uint64
}

func (e *errandAgent) Submit(ctx context.Context, text string) (<-chan session.Event, error) {
	e.sent = append(e.sent, text)
	if e.failing != nil {
		return nil, e.failing
	}
	out := make(chan session.Event, 64)
	if e.turn < len(e.turns) {
		for _, event := range e.turns[e.turn] {
			out <- event
		}
		e.turn++
	}
	close(out)
	return out, nil
}

func (e *errandAgent) ResolveStanding(id uint64, answer session.StandingAnswer) {
	e.answerID = append(e.answerID, id)
	e.answered = append(e.answered, answer)
}

// errandLab is a home lab with a standing root beside it and an errand seam
// wired, which is the whole of what `ask here` needs that home does not.
type errandLab struct {
	*homeLab
	standing string
	agent    *errandAgent
	dirs     []string
}

func newErrandLab(t *testing.T) *errandLab {
	t.Helper()
	return &errandLab{homeLab: newHomeLab(t), standing: t.TempDir()}
}

// app builds the surface with the seam on it, and remembers every folder the
// seam was handed — which is how a test asserts WHERE the exchange was made
// without knowing the id it was given.
func (l *errandLab) app(here string, turns ...[]session.Event) *app {
	l.t.Helper()
	l.agent = &errandAgent{fakeAgent: fakeAgent{model: "m", turns: turns}}
	a := l.homeLab.app(here)
	a.standingRoot = l.standing
	a.errand = func(dir, workspace string) (Agent, error) {
		l.dirs = append(l.dirs, dir)
		if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"),
			[]byte(`{"type":"session","version":1}`+"\n"), 0o600); err != nil {
			return nil, err
		}
		return l.agent, nil
	}
	return a
}

// errandRows is the frame as a reader sees it, trailing space dropped.
// [homeText] answers the same frame as one string; this is the row-by-row form
// an assertion about the LAST row (the hint under the box) needs.
func errandRows(a *app) []string {
	width, height := a.size()
	lines, _, _, _ := a.homeFrame(width, height)
	rows := make([]string, len(lines))
	for i, line := range lines {
		rows[i] = strings.TrimRight(ansi.Strip(line), " ")
	}
	return rows
}

// typeHome types into home's box the way a person does, through the key router.
func typeHome(a *app, text string) {
	for _, r := range text {
		a.homeKey(key(string(r)))
	}
}

// TestTypingAtHomeOffersAskHereDirectlyAboveStartingAConversation is the shape
// of the offer: two rows, in that order, one cluster, with the cursor still
// resting on the one it always rested on.
//
// THE ORDER IS THE WHOLE ASSERTION. `ask here` is one ↑ from the rest row —
// which is the cheapest keystroke on the screen after enter itself — and
// `start a new conversation` keeps the rest, so nothing a person already knew
// how to do changed meaning.
func TestTypingAtHomeOffersAskHereDirectlyAboveStartingAConversation(t *testing.T) {
	lab := newErrandLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", now)

	a := lab.app(mine)
	a.openHome()
	typeHome(a, "remind me at 6")

	ask, start := -1, -1
	for at, line := range a.home.lines {
		switch line.kind {
		case homeAskHere:
			ask = at
		case homeAction:
			start = at
		}
	}
	if ask < 0 || start < 0 {
		t.Fatalf("both rows should be on the list, got ask=%d start=%d", ask, start)
	}
	if start != ask+1 {
		t.Fatalf("`ask here` should sit directly above the action row, got ask=%d start=%d", ask, start)
	}
	if a.home.cursor != start {
		t.Fatalf("the cursor should still rest on the action row, it is on line %d", a.home.cursor)
	}
	frame := homeText(a)
	if !strings.Contains(frame, homeAskHereWord+`: "remind me at 6"`) {
		t.Fatalf("the ask row does not quote the sentence:\n%s", frame)
	}
	if !strings.Contains(frame, homeStartWord+`: "remind me at 6"`) {
		t.Fatalf("the action row is gone:\n%s", frame)
	}
	// The hint under the box names both readings of the same characters.
	if hint := errandRows(a)[len(errandRows(a))-1]; !strings.Contains(hint, "ctrl+enter ask here") ||
		!strings.Contains(hint, "enter starts a new conversation") {
		t.Fatalf("the hint does not say both things enter can do:\n%s", hint)
	}
}

// TestOneUpFromTheRestRowLandsOnAskHere pins the keystroke the row was placed
// for. A row that has to be walked to past a heading and a blank is a row
// nobody reaches.
func TestOneUpFromTheRestRowLandsOnAskHere(t *testing.T) {
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())

	a := lab.app(mine)
	a.openHome()
	typeHome(a, "remind me at 6")
	a.homeKey(key("up"))

	line, ok := a.home.focusedLine()
	if !ok || line.kind != homeAskHere {
		t.Fatalf("↑ from the rest row should land on `ask here`, it landed on kind %d", line.kind)
	}
}

// TestAskHereMakesItsFolderOutsideTheProjectsAndSendsTheSentence is the
// mechanism: a real agent on a real transcript, in a folder home cannot list,
// with the words already sent and the reply arriving in the right pane.
func TestAskHereMakesItsFolderOutsideTheProjectsAndSendsTheSentence(t *testing.T) {
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())

	a := lab.app(mine, []session.Event{
		text(session.EventTextDelta, "I will remind you at 6."),
		{Kind: session.EventTurnDone},
	})
	a.openHome()
	typeHome(a, "remind me at 6")
	drive(t, a, key("up"), key("enter"))

	if len(lab.agent.sent) != 1 || lab.agent.sent[0] != "remind me at 6" {
		t.Fatalf("the sentence should have gone to the errand agent, it sent %v", lab.agent.sent)
	}
	if len(lab.dirs) != 1 {
		t.Fatalf("one folder should have been made, the seam saw %v", lab.dirs)
	}
	made := lab.dirs[0]
	if parent := filepath.Dir(made); parent != filepath.Join(lab.standing, "exchanges") {
		t.Fatalf("the exchange should live under the standing root's exchanges/, it is at %s", made)
	}
	if strings.HasPrefix(made, lab.root) {
		t.Fatalf("the exchange must not be under the projects root, it is at %s", made)
	}
	if _, err := os.Stat(filepath.Join(made, "transcript.jsonl")); err != nil {
		t.Fatalf("the exchange has no transcript: %v", err)
	}
	// AND HOME STILL CANNOT SEE IT. The list is read off the projects root, and
	// nothing under the standing root is in it.
	a.refreshHome()
	for _, project := range a.home.world.Projects {
		for _, row := range project.Sessions {
			if strings.HasPrefix(row.Dir, lab.standing) {
				t.Fatalf("home listed the exchange as a conversation: %s", row.Dir)
			}
		}
	}
	if frame := homeText(a); !strings.Contains(frame, "I will remind you at 6.") {
		t.Fatalf("the reply did not stream into the right pane:\n%s", frame)
	}
}

// TestTheChordAsksHereWithoutWalkingToTheRow pins the second way in. Both
// spellings are bound because terminals disagree about which one they can send
// (home.go's ctrl+enter case says so); alt+enter is the one every terminal here
// delivers, so it is the one the test presses.
func TestTheChordAsksHereWithoutWalkingToTheRow(t *testing.T) {
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())

	a := lab.app(mine, []session.Event{{Kind: session.EventTurnDone}})
	a.openHome()
	typeHome(a, "remind me at 6")
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})

	if len(lab.agent.sent) != 1 {
		t.Fatalf("alt+enter should have asked here, the agent saw %v", lab.agent.sent)
	}
	if a.home.exchange == nil {
		t.Fatal("alt+enter left no exchange in the pane")
	}
	// AND ctrl+enter IS THE SAME DOOR. It reaches the router only on a terminal
	// that can tell it from a plain enter, and where it does it must mean this.
	second := lab.app(mine, []session.Event{{Kind: session.EventTurnDone}})
	second.openHome()
	typeHome(second, "tell me when CI goes red")
	drive(t, second, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModCtrl})
	if len(lab.agent.sent) != 1 {
		t.Fatalf("ctrl+enter should have asked here, the agent saw %v", lab.agent.sent)
	}
}

// standingProposal is one EventStandingProposal as the engine sends it.
func standingProposal(id uint64, words string) session.Event {
	return session.Event{
		Kind: session.EventStandingProposal,
		Standing: &session.StandingNotice{
			ID: id,
			Item: standing.Item{
				Words:     words,
				Workspace: "/tmp/alpha",
				When:      standing.When{Kind: standing.WhenAt, Words: "at 6 today"},
				Does:      standing.Action{Kind: standing.ActionSay, Say: "leave"},
				Rails:     standing.Rails{PerRunUSD: 0.02, MaxPerDay: 1},
			},
			WhenWords: "at 6 today",
			CostWords: "about $0.02, once",
		},
	}
}

// TestTheCardInThePaneIsAnsweredWithOne is the decision moment: the card is
// drawn with the four facts the contract guarantees, and `1` is a yes that
// reaches the session's own answer lane.
func TestTheCardInThePaneIsAnsweredWithOne(t *testing.T) {
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())

	a := lab.app(mine, []session.Event{
		text(session.EventTextDelta, "Here is what I will do."),
		standingProposal(7, "remind me at 6 to leave"),
		{Kind: session.EventTurnDone},
	})
	a.openHome()
	typeHome(a, "remind me at 6 to leave")
	drive(t, a, key("up"), key("enter"))

	frame := homeText(a)
	for _, want := range []string{"remind me at 6 to leave", "at 6 today", "about $0.02, once", "1 yes · 2 change · 3 once"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the card does not say %q:\n%s", want, frame)
		}
	}
	drive(t, a, key("1"))
	if len(lab.agent.answered) != 1 {
		t.Fatalf("`1` should have answered the card, the agent saw %v", lab.agent.answered)
	}
	if !lab.agent.answered[0].Approved {
		t.Fatalf("`1` is a yes, the answer was %+v", lab.agent.answered[0])
	}
	if lab.agent.answerID[0] != 7 {
		t.Fatalf("the answer went back on token %d, want 7", lab.agent.answerID[0])
	}
	// AND THE CARD IS STILL THERE, SETTLED. It used to be taken off the pane the
	// instant somebody answered it, which left the one thing on screen that
	// records what was decided blank at the moment it had something to record.
	if a.home.exchange.card == nil || a.home.exchange.view == nil {
		t.Fatal("answering took the card off the pane")
	}
	if !a.home.exchange.view.settled() {
		t.Fatal("the answered card is still asking")
	}
	settled := homeText(a)
	if !strings.Contains(settled, standYesWord+" · "+standSetWord) {
		t.Fatalf("the settled card does not carry the answer and what it came to:\n%s", settled)
	}
	if strings.Contains(settled, "[ 1 "+standYesWord+" ]") {
		t.Fatalf("the settled card is still drawing its chips:\n%s", settled)
	}
}

// TestContinueAsAConversationMovesTheFolderIntoTheBucket is the promotion: the
// errand turned out to be a conversation, so the folder becomes one — moved,
// named, and opened through the same door a session row opens through.
func TestContinueAsAConversationMovesTheFolderIntoTheBucket(t *testing.T) {
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())

	a := lab.app(mine, []session.Event{
		text(session.EventTextDelta, "That depends on the pricing sheet."),
		{Kind: session.EventTurnDone},
	})
	a.openHome()
	typeHome(a, "what did we decide about pricing")
	drive(t, a, key("up"), key("enter"))

	made := lab.dirs[0]
	id := filepath.Base(made)
	if !strings.Contains(homeText(a), homeContinueWord) {
		t.Fatalf("the offer is not on the pane once a reply has landed:\n%s", homeText(a))
	}
	drive(t, a, key("down"), key("enter"))

	moved := filepath.Join(lab.project("-tmp-alpha"), id)
	if _, err := os.Stat(filepath.Join(moved, "transcript.jsonl")); err != nil {
		t.Fatalf("the folder did not move into the bucket: %v", err)
	}
	if _, err := os.Stat(made); err == nil {
		t.Fatalf("the folder is in two places at once — %s is still there", made)
	}
	meta, err := session.LoadMeta(moved)
	if err != nil {
		t.Fatal(err)
	}
	if meta.ID != id {
		t.Fatalf("meta.json names %q, want %q", meta.ID, id)
	}
	if meta.Title != "what did we decide about pricing" {
		t.Fatalf("the promoted conversation is called %q", meta.Title)
	}
	if meta.Workspace != "/tmp/alpha" {
		t.Fatalf("the promoted conversation records workspace %q", meta.Workspace)
	}
	if meta.LastUserAt.IsZero() || meta.Created.IsZero() {
		t.Fatalf("the promoted conversation has no clock: %+v", meta)
	}
	// AND HOME CLOSED INTO IT, which is what promoting means: the conversation
	// is on screen and the screen it was asked from is gone.
	if a.home.open {
		t.Fatal("home is still open after the exchange became a conversation")
	}
	if a.file != filepath.Join(moved, "transcript.jsonl") {
		t.Fatalf("the window is in %q, not the promoted conversation", a.file)
	}
}

// TestStandingUpMovesTheExchangeUnderTheItemItMade is provenance: the thing
// stands, and the short exchange that produced it is filed under it rather than
// left in the errands drawer — which is what makes "why did I get this?" a door
// ([standing.Store.ExchangeDir], [standing.Origin]).
//
// THE MOVE HAPPENS WHEN THE EXCHANGE ENDS AND NOT WHEN THE NEWS ARRIVES. The
// news arrives mid-turn, so closing the agent to free the transcript's lock
// there cancelled the running turn and parked the update loop on the close's
// grace period — which is what a person felt as the screen going dead just
// after they said yes (homeexchange.go's header).
func TestStandingUpMovesTheExchangeUnderTheItemItMade(t *testing.T) {
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())

	item := standing.Item{
		ID: "cccc000000000009", Words: "remind me at 6 to leave", Workspace: "/tmp/alpha",
		When: standing.When{Kind: standing.WhenAt, Words: "at 6 today"},
		Does: standing.Action{Kind: standing.ActionSay, Say: "leave"},
	}
	a := lab.app(mine, []session.Event{
		{Kind: session.EventStandingUpdate, Standing: &session.StandingNotice{
			Update: "stood", Item: item, Text: "I will tell you at 6.",
		}},
		{Kind: session.EventTurnDone},
	})
	a.openHome()
	typeHome(a, "remind me at 6 to leave")
	drive(t, a, key("up"), key("enter"))

	made := lab.dirs[0]
	store, err := standing.Open(lab.standing)
	if err != nil {
		t.Fatal(err)
	}
	want := store.ExchangeDir(item.ID)
	// NOTHING HAS MOVED AND NOTHING HAS BEEN CLOSED YET.
	if lab.agent.closes != 0 {
		t.Fatalf("the agent was closed mid-turn, %d times", lab.agent.closes)
	}
	if _, err := os.Stat(filepath.Join(made, "transcript.jsonl")); err != nil {
		t.Fatalf("the exchange left its folder before it ended: %v", err)
	}
	if !a.home.exchange.stood || a.home.exchange.itemID != item.ID {
		t.Fatalf("the exchange did not remember what stood: %+v", a.home.exchange.stood)
	}
	// AND A FOLLOW-UP STILL WORKS, because the session is still there.
	before := len(lab.agent.sent)
	drive(t, a, key("tab"))
	typeHome(a, "make it 7")
	drive(t, a, key("enter"))
	if len(lab.agent.sent) != before+1 {
		t.Fatalf("a follow-up after something stood went nowhere, the agent saw %v", lab.agent.sent)
	}

	// THE EXCHANGE ENDS, AND ONLY THEN DOES THE FOLDER GO UNDER THE ITEM.
	a.closeHome()
	if lab.agent.closes == 0 {
		t.Fatal("closing home left the errand's agent open")
	}
	if _, err := os.Stat(filepath.Join(want, "transcript.jsonl")); err != nil {
		t.Fatalf("the exchange was not filed under the item: %v", err)
	}
	if _, err := os.Stat(made); err == nil {
		t.Fatalf("the exchange is in two places at once — %s is still there", made)
	}
}

// TestSomethingStandingKeepsItsCardAndHandsBackTheKeyboard is the other half of
// the news arriving: the card that proposed the thing settles in place rather
// than vanishing, and the hand goes back to the column — the thing they asked
// for exists now, and the list is where a person goes next.
func TestSomethingStandingKeepsItsCardAndHandsBackTheKeyboard(t *testing.T) {
	lab := newErrandLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "porting the picker", "/tmp/alpha", now.Add(-time.Hour))

	item := standing.Item{
		ID: "cccc000000000009", Words: "remind me at 6 to leave", Workspace: "/tmp/alpha",
		When: standing.When{Kind: standing.WhenAt, Words: "at 6 today"},
		Does: standing.Action{Kind: standing.ActionSay, Say: "leave"},
	}
	a := lab.app(mine, []session.Event{
		standingProposal(7, "remind me at 6 to leave"),
		{Kind: session.EventStandingUpdate, Standing: &session.StandingNotice{Update: "stood", Item: item}},
		{Kind: session.EventTurnDone},
	})
	a.openHome()
	typeHome(a, "remind me at 6 to leave")
	drive(t, a, key("up"), key("enter"))

	ex := a.home.exchange
	if ex == nil || ex.view == nil {
		t.Fatal("the card is gone from the pane")
	}
	if !ex.view.settled() {
		t.Fatal("a card whose proposal now stands is still asking")
	}
	frame := homeText(a)
	if !strings.Contains(frame, standSetWord) {
		t.Fatalf("the settled card does not say what it came to:\n%s", frame)
	}
	if !strings.Contains(frame, homeAskStoodWord) {
		t.Fatalf("the pane does not say where the record went:\n%s", frame)
	}
	if ex.focused {
		t.Fatal("the keyboard stayed in the pane after something stood")
	}
	before := a.home.cursor
	drive(t, a, key("down"))
	if a.home.cursor == before {
		t.Fatal("the list does not move after something stood")
	}
}

// TestEscLeavesTheExchangeAliveAndTheListMoving is the other half of hosting an
// exchange at home: the keyboard goes back to the column and the pane stays
// exactly where it was. A door that had to be closed to look at anything else
// would be a modal conversation, which is the thing this screen is not.
func TestEscLeavesTheExchangeAliveAndTheListMoving(t *testing.T) {
	lab := newErrandLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", now)
	lab.session("-tmp-beta", "bbbb000000000001", "beta notes", "/tmp/beta", now.Add(-time.Hour))

	a := lab.app(mine, []session.Event{
		text(session.EventTextDelta, "I will remind you at 6."),
		{Kind: session.EventTurnDone},
	})
	a.openHome()
	typeHome(a, "remind me at 6")
	drive(t, a, key("up"), key("enter"))

	drive(t, a, key("esc"))
	if a.home.exchange == nil {
		t.Fatal("esc closed the exchange; it should only hand back the keyboard")
	}
	if a.home.exchange.focused {
		t.Fatal("esc left the keyboard in the pane")
	}
	if !strings.Contains(homeText(a), "I will remind you at 6.") {
		t.Fatalf("the exchange left the pane on esc:\n%s", homeText(a))
	}
	// AND THE COLUMN WORKS UNDERNEATH IT.
	before := a.home.cursor
	drive(t, a, key("down"))
	if a.home.cursor == before {
		t.Fatal("the list did not move after esc")
	}
	// AND HOME CLOSING TAKES THE AGENT WITH IT, folder left where it is.
	a.closeHome()
	if lab.agent.closes == 0 {
		t.Fatal("closing home left the errand's agent open")
	}
	if _, err := os.Stat(filepath.Join(lab.dirs[0], "transcript.jsonl")); err != nil {
		t.Fatalf("closing home took the record with it: %v", err)
	}
}

// ── the two zones ───────────────────────────────────────────────────────────

// exchangeLab is a home with two projects, three conversations and one exchange
// standing in the pane — the shape every assertion about the two zones needs.
func exchangeLab(t *testing.T) (*errandLab, *app) {
	t.Helper()
	lab := newErrandLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "porting the picker", "/tmp/alpha", now.Add(-time.Hour))
	lab.session("-tmp-beta", "bbbb000000000001", "beta notes", "/tmp/beta", now.Add(-2*time.Hour))

	a := lab.app(mine, []session.Event{
		text(session.EventTextDelta, "I will remind you at 6."),
		{Kind: session.EventTurnDone},
	})
	a.openHome()
	typeHome(a, "remind me at 6")
	drive(t, a, key("up"), key("enter"))
	return lab, a
}

// TestTabTogglesTheListAndThePaneAtHome is the zone toggle, both ways, from
// every state either zone is in. A pane with one way out and a state that did
// not offer it is the trap somebody reports as "stuck".
func TestTabTogglesTheListAndThePaneAtHome(t *testing.T) {
	_, a := exchangeLab(t)
	ex := a.home.exchange
	if !ex.focused {
		t.Fatal("asking here should put the keyboard in the pane")
	}
	drive(t, a, key("tab"))
	if ex.focused {
		t.Fatal("tab did not hand the keyboard to the list")
	}
	drive(t, a, key("tab"))
	if !ex.focused {
		t.Fatal("tab did not bring the keyboard back to the pane")
	}
	// AND IT LEAVES FROM THE OFFER ROW TOO, which is the state that held the
	// keyboard hostage: `continue as a conversation` answered ↑ and enter and
	// nothing else.
	drive(t, a, key("down"))
	if !ex.onOffer {
		t.Fatal("↓ in the pane did not reach the offer row")
	}
	drive(t, a, key("tab"))
	if ex.focused {
		t.Fatalf("tab did not leave %q", homeContinueWord)
	}
	// AND SO DOES esc, from the same row.
	drive(t, a, key("tab"), key("down"), key("esc"))
	if ex.focused || ex.onOffer {
		t.Fatalf("esc did not leave %q", homeContinueWord)
	}
	if a.home.exchange == nil {
		t.Fatal("leaving the pane closed the exchange")
	}
}

// TestTheListWalksWhileAnExchangeIsAliveAndThePaneKeepsIt is the complaint in
// one test: with the keyboard on the column every walking key moves the column,
// and the exchange stays drawn beside it the whole time.
func TestTheListWalksWhileAnExchangeIsAliveAndThePaneKeepsIt(t *testing.T) {
	_, a := exchangeLab(t)
	drive(t, a, key("tab"))

	seen := map[int]bool{a.home.cursor: true}
	for _, pressed := range []string{"down", "ctrl+n", "up", "ctrl+p", "pgdown", "pgup"} {
		before := a.home.cursor
		drive(t, a, key(pressed))
		if a.home.cursor == before && len(seen) < 2 {
			t.Fatalf("%q did not move the list off line %d", pressed, before)
		}
		seen[a.home.cursor] = true
	}
	if len(seen) < 2 {
		t.Fatalf("the list never moved while the exchange was alive, it sat on %v", seen)
	}
	// AND THE PANE IS STILL THE EXCHANGE, not a preview of whatever row the
	// cursor walked onto.
	if frame := homeText(a); !strings.Contains(frame, "I will remind you at 6.") {
		t.Fatalf("walking the list took the exchange off the pane:\n%s", frame)
	}
}

// TestSayingYesHandsTheKeyboardBackToTheList pins the gesture the report is
// about: after the one answer that finishes the errand, the arrows move the
// column again without anybody having to find the key that says so.
func TestSayingYesHandsTheKeyboardBackToTheList(t *testing.T) {
	lab := newErrandLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "porting the picker", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.app(mine, []session.Event{
		standingProposal(7, "remind me at 6 to leave"),
		{Kind: session.EventTurnDone},
	})
	a.openHome()
	typeHome(a, "remind me at 6 to leave")
	// THE TWO DRIVES ARE THE POINT. [drive] queues what a command produced
	// behind the keys already in hand, so a `1` sent in the same call would be
	// pressed before the card it answers had arrived.
	drive(t, a, key("up"), key("enter"))
	drive(t, a, key("1"))

	ex := a.home.exchange
	if ex == nil {
		t.Fatal("a yes closed the exchange")
	}
	if ex.focused {
		t.Fatal("the keyboard stayed in the pane after a yes")
	}
	before := a.home.cursor
	drive(t, a, key("down"))
	if a.home.cursor == before {
		t.Fatal("the list does not move after a yes")
	}
	// AND THE EXCHANGE IS STILL REACHABLE for a follow-up.
	drive(t, a, key("tab"))
	if !ex.focused {
		t.Fatal("tab did not bring the answered exchange back")
	}
}

// ── the pointer ─────────────────────────────────────────────────────────────

// homeClickAt clicks the screen row that draws a given line of the left column.
func homeClickAt(t *testing.T, a *app, at int) {
	t.Helper()
	width, height := a.size()
	_, hits, _, _ := a.homeFrame(width, height)
	for y, hit := range hits {
		if hit == at {
			drive(t, a, tea.MouseClickMsg{Button: tea.MouseLeft, X: 2, Y: y})
			return
		}
	}
	t.Fatalf("line %d is not on the screen", at)
}

// TestAClickOnARowSelectsItWhileAnExchangeIsUp is the other half of the
// complaint: a row used to light up under the pointer and then go on ignoring
// every key, because the click moved the cursor and left the keyboard in the
// pane.
func TestAClickOnARowSelectsItWhileAnExchangeIsUp(t *testing.T) {
	_, a := exchangeLab(t)
	ex := a.home.exchange

	want := -1
	for at, line := range a.home.lines {
		if line.kind == homeSession && at != a.home.cursor {
			want = at
			break
		}
	}
	if want < 0 {
		t.Fatal("the lab drew no second conversation to click on")
	}
	homeClickAt(t, a, want)
	if a.home.cursor != want {
		t.Fatalf("the click put the cursor on line %d, not %d", a.home.cursor, want)
	}
	if ex.focused {
		t.Fatal("the click selected a row and left the keyboard in the pane")
	}
	// AND THE KEYBOARD IS REALLY ON THE COLUMN: the next arrow moves it.
	before := a.home.cursor
	drive(t, a, key("down"))
	if a.home.cursor == before {
		t.Fatal("the row was selected but the list still does not answer the arrows")
	}
}

// paneRowAt is the screen position of one of the pane's own rows.
func paneRowAt(a *app, row int) (x, y int, ok bool) {
	width, height := a.size()
	a.homeFrame(width, height)
	left, _ := homeColumns(width)
	for y, at := range a.home.pane {
		if at == row {
			return left + homeGutter + 1, y, true
		}
	}
	return 0, 0, false
}

// TestTheContinueRowLightsUpUnderThePointerAndPromotesOnAClick is the row that
// had no hover at all: a thing you can click that never says so is a thing
// nobody clicks.
func TestTheContinueRowLightsUpUnderThePointerAndPromotesOnAClick(t *testing.T) {
	lab, a := exchangeLab(t)
	ex := a.home.exchange
	if !ex.offering() {
		t.Fatal("the exchange is not offering to become a conversation")
	}
	// Draw once so the pane records where it put the row.
	homeText(a)
	x, y, ok := paneRowAt(a, ex.offerAt)
	if !ok {
		t.Fatalf("%q was not drawn on any screen row", homeContinueWord)
	}
	drive(t, a, tea.MouseMotionMsg{X: x, Y: y})
	if !ex.hover {
		t.Fatalf("the pointer over %q lit nothing up", homeContinueWord)
	}
	if frame := homeText(a); !strings.Contains(frame, homeContinueWord) {
		t.Fatalf("the offer row left the pane:\n%s", frame)
	}
	made := lab.dirs[0]
	id := filepath.Base(made)
	drive(t, a, tea.MouseClickMsg{Button: tea.MouseLeft, X: x, Y: y})
	moved := filepath.Join(lab.project("-tmp-alpha"), id)
	if _, err := os.Stat(filepath.Join(moved, "transcript.jsonl")); err != nil {
		t.Fatalf("a click on the offer row did not promote the exchange: %v", err)
	}
	if a.home.open {
		t.Fatal("home is still open after the exchange became a conversation")
	}
}

// TestAClickInThePaneTakesTheKeyboardAndAnswersTheCard is the pointer's half of
// the zone model, and the card's chips answered the way they are answered in
// the conversation.
func TestAClickInThePaneTakesTheKeyboardAndAnswersTheCard(t *testing.T) {
	lab := newErrandLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "porting the picker", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.app(mine, []session.Event{
		standingProposal(7, "remind me at 6 to leave"),
		{Kind: session.EventTurnDone},
	})
	a.openHome()
	typeHome(a, "remind me at 6 to leave")
	drive(t, a, key("up"), key("enter"))
	drive(t, a, key("tab"))

	ex := a.home.exchange
	if ex.focused {
		t.Fatal("tab left the keyboard in the pane")
	}
	homeText(a)
	if ex.cardAt < 0 {
		t.Fatal("the card's chips were not drawn on the pane")
	}
	// A PRESS ON THE PANE'S BODY IS THE ZONE CHANGE AND NOTHING ELSE.
	if bx, by, ok := paneRowAt(a, 0); ok {
		drive(t, a, tea.MouseClickMsg{Button: tea.MouseLeft, X: bx, Y: by})
	}
	if !ex.focused {
		t.Fatal("a click in the pane did not take the keyboard")
	}
	drive(t, a, key("tab"))
	x, y, ok := paneRowAt(a, ex.cardAt)
	if !ok {
		t.Fatal("the chips row is not on the screen")
	}
	// The yes chip's own columns, taken from where the renderer put them — a
	// chip that did not fit was dropped rather than truncated, so the spans are
	// the authority on where the answers actually are ([app.standChips]).
	span := ex.view.spans[0]
	drive(t, a, tea.MouseClickMsg{Button: tea.MouseLeft, X: x - 1 + span.from, Y: y})
	// The yes hands the keyboard straight back to the list, which is the whole
	// of [app.answerCard]'s last line — so what a click on a chip proves about
	// the zones is proved above, on a press that landed on the body.
	if ex.focused {
		t.Fatal("a yes clicked in the pane kept the keyboard")
	}
	if len(lab.agent.answered) != 1 || !lab.agent.answered[0].Approved {
		t.Fatalf("clicking the yes chip did not answer, the agent saw %v", lab.agent.answered)
	}
	if !ex.view.settled() || !strings.Contains(homeText(a), standSetWord) {
		t.Fatalf("the clicked card did not settle:\n%s", homeText(a))
	}
}

// TestAChangedCardIsReplacedByTheOneThatFollowsIt is the one case where a card
// leaves the pane: `2 change when`, a correction typed, and the model proposing
// again. Two cards about one proposal would be one question asked twice.
func TestAChangedCardIsReplacedByTheOneThatFollowsIt(t *testing.T) {
	lab := newErrandLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "porting the picker", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.app(mine, []session.Event{standingProposal(7, "remind me at 6 to leave"), {Kind: session.EventTurnDone}})
	a.openHome()
	typeHome(a, "remind me at 6 to leave")
	drive(t, a, key("up"), key("enter"))
	drive(t, a, key("2"))

	ex := a.home.exchange
	if !ex.changing {
		t.Fatal("`2` did not arm the correction")
	}
	if ex.view.settled() {
		t.Fatal("`2` settled the card; it is still a question until the words arrive")
	}
	if !strings.Contains(homeText(a), homeAskChangeWord) {
		t.Fatalf("the pane does not ask for the change:\n%s", homeText(a))
	}
	first := ex.view
	typeHome(a, "make it 8")
	drive(t, a, key("enter"))
	if first.verdict != standChangedWord {
		t.Fatalf("the corrected card settled as %q, want %q", first.verdict, standChangedWord)
	}
	// THE RE-PROPOSAL COMES BACK ON THE SAME TURN in a real session — the tool
	// call that raised the first card is still blocked on the answer — so it
	// arrives here as the event it is rather than as a second Submit.
	spent := make(chan session.Event)
	close(spent)
	drive(t, a, errandEventMsg{ex: ex, ch: spent, ev: standingProposal(8, "remind me at 8 to leave")})

	// AND THE SECOND CARD REPLACES IT.
	cards := 0
	for _, row := range ex.rows {
		if row.kind == exchangeCard {
			cards++
		}
	}
	if cards != 1 {
		t.Fatalf("the pane is drawing %d cards, want 1", cards)
	}
	if ex.view == first || ex.view.id != 8 {
		t.Fatalf("the re-proposal did not take the card slot: %+v", ex.view)
	}
	if !ex.asking() {
		t.Fatal("the new card is not asking")
	}
}

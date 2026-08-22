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

// theExchange is the one errand a test opened, and nil when it opened none.
// Several can be open at once now ([app.exchanges]), so a test that means "the
// one I just asked" says so through here rather than through a field that used
// to be able to hold only one.
func theExchange(a *app) *homeExchange {
	if len(a.exchanges) == 0 {
		return nil
	}
	return a.exchanges[len(a.exchanges)-1]
}

// exchangeRowAt is the line of the left column that draws one exchange, and -1
// when the column is not drawing it.
func exchangeRowAt(a *app, ex *homeExchange) int {
	for at, line := range a.home.lines {
		if line.kind == homeExchangeRow && line.ex == ex {
			return at
		}
	}
	return -1
}

// cursorWord names the line the cursor is on, so a test can say "the cursor
// moved" without counting rows: a settled exchange filed on the way past takes
// its row with it, and the LINE NUMBER after that walk can be the one it was
// before while the cursor is on something else entirely.
func cursorWord(a *app) string {
	line, ok := a.home.focusedLine()
	if !ok {
		return "nothing"
	}
	switch line.kind {
	case homeSession:
		return "session " + line.row.Transcript
	case homeExchangeRow:
		return "exchange " + line.ex.id
	case homeItem:
		return "item " + line.item.ID
	}
	return "line " + itoa(int(line.kind))
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
	if theExchange(a) == nil {
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
	if theExchange(a).card == nil || theExchange(a).view == nil {
		t.Fatal("answering took the card off the pane")
	}
	if !theExchange(a).view.settled() {
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

// AND `0` IS HOW THE PANE SAYS NO, which until now it could not say at all.
//
// In a conversation `esc` declines the card. Here `esc` is the one-layer undo
// that hands the keyboard back to the list, and a card left standing on the
// column is not an answer — so a person who asked for a reminder from home and
// then thought better of it had nothing to press. The decline is the engine's
// own `0 not set up` ([session.StandingNoKey]), which is the same key on this
// pane, on home's answer band and in the conversation.
func TestTheCardInThePaneIsDeclinedWithZero(t *testing.T) {
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())

	a := lab.app(mine, []session.Event{
		standingProposal(7, "remind me at 6 to leave"),
		{Kind: session.EventTurnDone},
	})
	a.openHome()
	typeHome(a, "remind me at 6 to leave")
	drive(t, a, key("up"), key("enter"))

	// THE HINT NAMES IT, because this is the only way out of the question that
	// answers it.
	if frame := homeText(a); !strings.Contains(frame, "0 no") {
		t.Fatalf("the pane does not name the decline:\n%s", frame)
	}
	drive(t, a, key(session.StandingNoKey))
	if len(lab.agent.answered) != 1 {
		t.Fatalf("`%s` should have answered the card, the agent saw %v", session.StandingNoKey, lab.agent.answered)
	}
	if lab.agent.answered[0] != (session.StandingAnswer{}) {
		t.Fatalf("`%s` sent %+v, want the decline", session.StandingNoKey, lab.agent.answered[0])
	}
	if lab.agent.answerID[0] != 7 {
		t.Fatalf("the answer went back on token %d, want 7", lab.agent.answerID[0])
	}
	ex := theExchange(a)
	if ex.view == nil || !ex.view.settled() {
		t.Fatal("the declined card is still asking")
	}
	if settled := homeText(a); !strings.Contains(settled, standNoWord) {
		t.Fatalf("the declined card does not say what it came to:\n%s", settled)
	}
	// AND THE KEYBOARD GOES BACK TO THE LIST, as it does on a yes: the question
	// is over either way.
	if ex.focused {
		t.Fatal("the declined card kept the keyboard in the pane")
	}
	// WITH NO CARD UP IT IS A CHARACTER AGAIN. Everything the digits do here
	// they do only while a card is asking; a `0` typed afterwards is somebody
	// writing.
	drive(t, a, key("tab"), key(session.StandingNoKey))
	if len(lab.agent.answered) != 1 {
		t.Fatalf("a second `%s` answered a settled card: %v", session.StandingNoKey, lab.agent.answered)
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
	if !theExchange(a).stood || theExchange(a).itemID != item.ID {
		t.Fatalf("the exchange did not remember what stood: %+v", theExchange(a).stood)
	}
	// AND A FOLLOW-UP STILL WORKS, because the session is still there.
	before := len(lab.agent.sent)
	drive(t, a, key("tab"))
	typeHome(a, "make it 7")
	drive(t, a, key("enter"))
	if len(lab.agent.sent) != before+1 {
		t.Fatalf("a follow-up after something stood went nowhere, the agent saw %v", lab.agent.sent)
	}

	// AND CLOSING HOME DOES NOT END IT. The exchange belongs to the window, not
	// to the screen (homeexchange.go's header).
	a.closeHome()
	if lab.agent.closes != 0 {
		t.Fatalf("closing home closed the errand's agent, %d times", lab.agent.closes)
	}
	if _, err := os.Stat(filepath.Join(made, "transcript.jsonl")); err != nil {
		t.Fatalf("closing home moved the exchange's folder: %v", err)
	}

	// THE EXCHANGE ENDS WITH THE WINDOW, AND ONLY THEN DOES THE FOLDER GO UNDER
	// THE ITEM.
	a.quit()
	if lab.agent.closes == 0 {
		t.Fatal("quitting left the errand's agent open")
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

	ex := theExchange(a)
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
	before := cursorWord(a)
	drive(t, a, key("down"))
	if cursorWord(a) == before {
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
	if theExchange(a) == nil {
		t.Fatal("esc closed the exchange; it should only hand back the keyboard")
	}
	if theExchange(a).focused {
		t.Fatal("esc left the keyboard in the pane")
	}
	if !strings.Contains(homeText(a), "I will remind you at 6.") {
		t.Fatalf("the exchange left the pane on esc:\n%s", homeText(a))
	}
	// AND HOME CLOSING LEAVES BOTH THE AGENT AND THE RECORD ALONE. The window
	// takes them on the way out and nothing else does.
	a.closeHome()
	if lab.agent.closes != 0 {
		t.Fatalf("closing home closed the errand's agent, %d times", lab.agent.closes)
	}
	if _, err := os.Stat(filepath.Join(lab.dirs[0], "transcript.jsonl")); err != nil {
		t.Fatalf("closing home took the record with it: %v", err)
	}
	// AND THE COLUMN WORKS UNDERNEATH IT, on the screen opened again.
	a.openHome()
	before := cursorWord(a)
	drive(t, a, key("down"))
	if cursorWord(a) == before {
		t.Fatal("the list did not move with an exchange standing beside it")
	}
	a.quit()
	if lab.agent.closes == 0 {
		t.Fatal("quitting left the errand's agent open")
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
	ex := theExchange(a)
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
	if theExchange(a) == nil {
		t.Fatal("leaving the pane closed the exchange")
	}
}

// TestTheListWalksWhileAnExchangeIsAliveAndTheRowKeepsIt is the complaint in
// one test: with the keyboard on the column every walking key moves the column,
// and the exchange keeps its own row on it the whole time.
func TestTheListWalksWhileAnExchangeIsAliveAndTheRowKeepsIt(t *testing.T) {
	_, a := exchangeLab(t)
	ex := theExchange(a)
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
	// AND THE EXCHANGE IS STILL A ROW ON THE COLUMN, wherever the cursor went.
	at := exchangeRowAt(a, ex)
	if at < 0 {
		t.Fatalf("walking the list took the exchange row off the column:\n%s", homeText(a))
	}
	// AND WALKING BACK ONTO IT BRINGS THE PANE BACK. The pane is about the row
	// under the cursor, always.
	a.home.cursor = at
	if frame := homeText(a); !strings.Contains(frame, "I will remind you at 6.") {
		t.Fatalf("the row under the cursor did not draw its exchange:\n%s", frame)
	}
}

// TestWalkingOffTheExchangeRowShowsTheOtherRowsCard is the third complaint:
// "once the reminder is set I am unable to see other previews on the right".
// The pane belonged to the exchange for as long as one existed; it belongs to
// the row under the cursor now.
func TestWalkingOffTheExchangeRowShowsTheOtherRowsCard(t *testing.T) {
	_, a := exchangeLab(t)
	ex := theExchange(a)
	drive(t, a, key("tab"))

	want := -1
	for at, line := range a.home.lines {
		if line.kind == homeSession {
			want = at
			break
		}
	}
	if want < 0 {
		t.Fatal("the lab drew no conversation to walk onto")
	}
	a.home.cursor = want
	name := homeName(a.home.lines[want].row)
	frame := homeText(a)
	if !strings.Contains(frame, name) {
		t.Fatalf("the row under the cursor has no card of its own:\n%s", frame)
	}
	if strings.Contains(frame, "I will remind you at 6.") {
		t.Fatalf("the exchange kept the pane on a row that is not it:\n%s", frame)
	}
	// AND THE EXCHANGE IS STILL THERE, with its agent untouched.
	if exchangeRowAt(a, ex) < 0 {
		t.Fatal("looking at another row took the exchange away")
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

	ex := theExchange(a)
	if ex == nil {
		t.Fatal("a yes closed the exchange")
	}
	if ex.focused {
		t.Fatal("the keyboard stayed in the pane after a yes")
	}
	before := cursorWord(a)
	drive(t, a, key("down"))
	if cursorWord(a) == before {
		t.Fatal("the list does not move after a yes")
	}
	// AND THE EXCHANGE IS STILL REACHABLE for a follow-up: its row is where it
	// was, and tab on it takes the keyboard back into the pane. tab is about the
	// row under the cursor now, which is what lets every OTHER row keep its own
	// card while an errand is open.
	drive(t, a, key("up"), key("tab"))
	if !ex.focused {
		t.Fatal("tab on the exchange row did not bring the answered exchange back")
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
			drive(t, a, tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 2, Y: y})
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
	ex := theExchange(a)

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
	wanted := "session " + a.home.lines[want].row.Transcript
	homeClickAt(t, a, want)
	if cursorWord(a) != wanted {
		t.Fatalf("the click put the cursor on %s, not on %s", cursorWord(a), wanted)
	}
	if ex.focused {
		t.Fatal("the click selected a row and left the keyboard in the pane")
	}
	// AND THE KEYBOARD IS REALLY ON THE COLUMN: the next arrow moves it.
	before := cursorWord(a)
	drive(t, a, key("down"))
	if cursorWord(a) == before {
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
	ex := theExchange(a)
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
	drive(t, a, tea.MouseReleaseMsg{Button: tea.MouseLeft, X: x, Y: y})
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

	ex := theExchange(a)
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
		drive(t, a, tea.MouseReleaseMsg{Button: tea.MouseLeft, X: bx, Y: by})
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
	drive(t, a, tea.MouseReleaseMsg{Button: tea.MouseLeft, X: x - 1 + span.from, Y: y})
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

	ex := theExchange(a)
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

// ── an exchange is a row, and it outlives the screen it was asked on ─────────

// askedHere opens home, asks one thing, and hands back the exchange without
// running the turn: the errand is WORKING and nothing has come back yet, which
// is the state every assertion about liveness needs and the state a scripted
// turn is already past by the time [drive] returns.
func askedHere(t *testing.T, lab *errandLab, a *app, said string) *homeExchange {
	t.Helper()
	// The keyboard goes back to the column first. A second `ask here` is typed
	// into HOME's box, and after the first one the pane has the hand — which is
	// exactly what a person does with tab or esc before typing again.
	a.homeTakeList()
	typeHome(a, said)
	a.homeKey(key("up"))
	a.homeKey(key("enter"))
	ex := theExchange(a)
	if ex == nil {
		t.Fatal("`ask here` opened no exchange")
	}
	return ex
}

// TestAnExchangeIsARowInTheColumnWearingWhatItIsDoing is the shape of the
// repair: the errand is a line on the left, in its project's block, above the
// conversations, with its state in the tail.
func TestAnExchangeIsARowInTheColumnWearingWhatItIsDoing(t *testing.T) {
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())

	a := lab.app(mine)
	a.openHome()
	ex := askedHere(t, lab, a, "remind me at 6 to leave")

	at := exchangeRowAt(a, ex)
	if at < 0 {
		t.Fatalf("the exchange has no row on the column:\n%s", homeText(a))
	}
	if a.home.cursor != at {
		t.Fatalf("`ask here` left the cursor on line %d, not on its own row %d", a.home.cursor, at)
	}
	if !ex.focused {
		t.Fatal("`ask here` did not put the keyboard in the pane")
	}
	// THE ROW SITS IN THE PROJECT'S BLOCK, ABOVE ITS CONVERSATIONS.
	heading, firstSession := -1, -1
	for i, line := range a.home.lines {
		if line.kind == homeHeading && heading < 0 {
			heading = i
		}
		if line.kind == homeSession && firstSession < 0 {
			firstSession = i
		}
	}
	if !(heading < at && at < firstSession) {
		t.Fatalf("the row is not inside the project block: heading=%d row=%d session=%d", heading, at, firstSession)
	}
	// WORKING, with the sentence as its name.
	frame := homeText(a)
	if !strings.Contains(frame, homeAskHereGlyph+" remind me at 6 to leave") {
		t.Fatalf("the row does not say what was asked:\n%s", frame)
	}
	if !strings.Contains(frame, homeAskWorkingWord) {
		t.Fatalf("a working exchange does not say so on its row:\n%s", frame)
	}
	// WAITING ON YOU, the moment a card arrives.
	a.errandEvent(ex, standingProposal(7, "remind me at 6 to leave"))
	a.home.build()
	if frame := homeText(a); !strings.Contains(frame, homeAskWaitingWord) {
		t.Fatalf("an exchange holding a card does not say it wants you:\n%s", frame)
	}
	// AND `stood` ONCE SOMETHING DOES. The card settles into the answer the
	// world gave it, and the row settles with it.
	a.errandEvent(ex, session.Event{Kind: session.EventStandingUpdate, Standing: &session.StandingNotice{
		Update: "stood", Item: standing.Item{ID: "cccc000000000009"},
	}})
	a.errandEvent(ex, session.Event{Kind: session.EventTurnDone})
	a.home.build()
	if frame := homeText(a); !strings.Contains(frame, standOffGlyph+" "+homeAskStoodTail) {
		t.Fatalf("a stood exchange does not say so on its row:\n%s", frame)
	}
}

// TestAWaitingExchangeSortsAboveAWorkingOne pins the triage order the column
// keeps for everything else: what wants you first.
func TestAWaitingExchangeSortsAboveAWorkingOne(t *testing.T) {
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())

	a := lab.app(mine)
	a.openHome()
	first := askedHere(t, lab, a, "remind me at 6 to leave")
	second := askedHere(t, lab, a, "tell me when CI goes red")
	if len(a.exchanges) != 2 {
		t.Fatalf("a second `ask here` should have ADDED an exchange, there are %d", len(a.exchanges))
	}
	a.errandEvent(second, standingProposal(7, "tell me when CI goes red"))
	a.home.build()

	firstAt, secondAt := exchangeRowAt(a, first), exchangeRowAt(a, second)
	if firstAt < 0 || secondAt < 0 {
		t.Fatalf("both exchanges should have rows, got %d and %d", firstAt, secondAt)
	}
	if secondAt > firstAt {
		t.Fatalf("the exchange holding a card should sort above the working one, got %d and %d", secondAt, firstAt)
	}
}

// TestASecondAskHereDoesNotCloseTheFirst is the second half of "several at
// once": the first exchange keeps its agent, its transcript and its card.
func TestASecondAskHereDoesNotCloseTheFirst(t *testing.T) {
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())

	a := lab.app(mine)
	a.openHome()
	first := askedHere(t, lab, a, "remind me at 6 to leave")
	a.errandEvent(first, standingProposal(7, "remind me at 6 to leave"))
	askedHere(t, lab, a, "tell me when CI goes red")

	if lab.agent.closes != 0 {
		t.Fatalf("a second `ask here` closed the first exchange's agent, %d times", lab.agent.closes)
	}
	if !first.asking() {
		t.Fatal("a second `ask here` took the first one's question away")
	}
	if len(lab.dirs) != 2 || lab.dirs[0] == lab.dirs[1] {
		t.Fatalf("each exchange should have its own folder, the seam saw %v", lab.dirs)
	}
}

// TestOpeningAnotherConversationLeavesTheExchangeRunning is the report,
// verbatim: "if I don't reply and check another chat, it seems to go away and
// not stay waiting". It went away because opening another conversation closes
// home and closing home closed the agent — so the engine answered the person's
// own card with "the card was left unanswered — nothing was set up".
func TestOpeningAnotherConversationLeavesTheExchangeRunning(t *testing.T) {
	lab := newErrandLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "porting the picker", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.app(mine)
	a.openHome()
	ex := askedHere(t, lab, a, "remind me at 6 to leave")
	a.errandEvent(ex, standingProposal(7, "remind me at 6 to leave"))

	// Open the other conversation, which is what home closing IS.
	a.home.cursor = 0
	for at, line := range a.home.lines {
		if line.kind == homeSession && line.row.Transcript != mine {
			a.home.cursor = at
		}
	}
	drive(t, a, key("esc"), key("enter"))
	if a.home.open {
		a.closeHome()
	}

	if lab.agent.closes != 0 {
		t.Fatalf("looking at another conversation closed the errand's agent, %d times", lab.agent.closes)
	}
	if lab.agent.stops != 0 {
		t.Fatalf("looking at another conversation interrupted the errand, %d times", lab.agent.stops)
	}
	if len(lab.agent.answered) != 0 {
		t.Fatalf("the card was answered by nobody pressing anything: %v", lab.agent.answered)
	}
	if !ex.asking() {
		t.Fatal("the card stopped being a question when home closed")
	}
	// AND THE PUMP IS STILL RUNNING WITH NO SCREEN ON. A card that arrives while
	// home is closed is simply HELD — there is no clock on it, and it is on the
	// pane the moment somebody looks.
	spent := make(chan session.Event)
	close(spent)
	drive(t, a, errandEventMsg{ex: ex, ch: spent, ev: standingProposal(9, "remind me at 6 to leave, again")})
	if ex.view == nil || ex.view.id != 9 {
		t.Fatalf("an event arriving with home closed did not reach the exchange: %+v", ex.view)
	}
	// AND THE ROW IS BACK ON THE SCREEN, still waiting, when home opens again.
	a.openHome()
	if exchangeRowAt(a, ex) < 0 {
		t.Fatalf("the exchange lost its row when home reopened:\n%s", homeText(a))
	}
	if frame := homeText(a); !strings.Contains(frame, homeAskWaitingWord) {
		t.Fatalf("the reopened screen does not say the errand wants you:\n%s", frame)
	}
	// AND IT IS STILL ANSWERABLE.
	a.home.cursor = exchangeRowAt(a, ex)
	drive(t, a, key("tab"), key("1"))
	if len(lab.agent.answered) != 1 || !lab.agent.answered[0].Approved {
		t.Fatalf("the card outlived home but could not be answered, the agent saw %v", lab.agent.answered)
	}
}

// TestThePaneSaysWhatItIsDoingWhileItWorks is the first complaint: "I am
// unable to see what's happening — no waiting or thinking or any UI response to
// know something is happening". The pane drew one dim `…` for the whole of a
// turn.
func TestThePaneSaysWhatItIsDoingWhileItWorks(t *testing.T) {
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())

	a := lab.app(mine)
	at := time.Now()
	a.clock = func() time.Time { return at }
	a.openHome()
	ex := askedHere(t, lab, a, "remind me at 6 to leave")

	// NOTHING BACK YET: it is thinking, and it says how long it has been.
	at = at.Add(4 * time.Second)
	frame := homeText(a)
	if !strings.Contains(frame, homeAskThinkWord+" · 4s") {
		t.Fatalf("the pane does not say it is thinking, or for how long:\n%s", frame)
	}
	if !strings.Contains(frame, a.exchangeSpin()) {
		t.Fatalf("the pane has no spinner on it:\n%s", frame)
	}
	// AND THE FRAME CLOCK IS TURNING FOR IT. A spinner nothing redraws is a
	// still photograph of the second the last event arrived.
	if !a.exchangeAnimating() {
		t.Fatal("a working exchange does not keep the frame clock running")
	}
	// A CALL RUNNING: the word changes and the strip says which call.
	a.errandEvent(ex, session.Event{
		Kind: session.EventToolBegin, Tool: "bash", Args: `{"command":"date"}`, Hint: "bash date",
	})
	at = at.Add(2 * time.Second)
	frame = homeText(a)
	if !strings.Contains(frame, homeAskRunWord+" · 6s") {
		t.Fatalf("a call running does not say so:\n%s", frame)
	}
	if !strings.Contains(frame, "bash · date") {
		t.Fatalf("the strip does not say which call is running:\n%s", frame)
	}
	// THE REPLY STREAMING: the word changes again.
	a.errandEvent(ex, session.Event{Kind: session.EventToolEnd, Tool: "bash"})
	a.errandEvent(ex, text(session.EventTextDelta, "I will remind you at 6."))
	frame = homeText(a)
	if !strings.Contains(frame, homeAskWriteWord+" · 6s") {
		t.Fatalf("a reply streaming does not say so:\n%s", frame)
	}
	// AND IT ALL GOES WHEN THE TURN DOES.
	a.errandEvent(ex, session.Event{Kind: session.EventTurnDone})
	frame = homeText(a)
	for _, gone := range []string{homeAskThinkWord, homeAskWriteWord, homeAskRunWord} {
		if strings.Contains(frame, gone+" · ") {
			t.Fatalf("the live block outlived the turn (%q):\n%s", gone, frame)
		}
	}
	if a.exchangeAnimating() {
		t.Fatal("the frame clock is still turning for a finished exchange")
	}
}

// TestAToolRowIsReadableAndNeverSaysUnknown is the fourth complaint: the pane
// drew `bash · unknown` and `stand · unknown`, because the fallback gloss for a
// call with no hint was [errText] of a nil error.
func TestAToolRowIsReadableAndNeverSaysUnknown(t *testing.T) {
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())

	a := lab.app(mine)
	a.openHome()
	ex := askedHere(t, lab, a, "remind me at 6 to leave")

	a.errandEvent(ex, session.Event{
		Kind: session.EventToolBegin, Tool: "bash", Args: `{"command":"date +%H:%M"}`,
	})
	a.errandEvent(ex, session.Event{Kind: session.EventToolEnd, Tool: "bash"})
	a.errandEvent(ex, session.Event{
		Kind: session.EventToolBegin, Tool: "stand",
		Args: `{"op":"propose","words":"remind me at 6 to leave"}`, Hint: "stand",
	})
	a.errandEvent(ex, session.Event{Kind: session.EventToolEnd, Tool: "stand", Hint: "stand"})

	frame := homeText(a)
	if strings.Contains(frame, "unknown") {
		t.Fatalf("a tool row says `unknown`:\n%s", frame)
	}
	if !strings.Contains(frame, "bash · date +%H:%M") {
		t.Fatalf("the bash row does not say what it ran:\n%s", frame)
	}
	if !strings.Contains(frame, "stand · proposing remind me at 6 to leave") {
		t.Fatalf("the stand row does not say what it is doing:\n%s", frame)
	}
}

// TestTheLiveStripKeepsTheNewestTwoThingsAndScrolls is the strip: two lines,
// the newest two, and a third pushes the first out.
func TestTheLiveStripKeepsTheNewestTwoThingsAndScrolls(t *testing.T) {
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())

	a := lab.app(mine)
	a.openHome()
	ex := askedHere(t, lab, a, "remind me at 6 to leave")

	for _, path := range []string{"one.go", "two.go", "three.go"} {
		a.errandEvent(ex, session.Event{
			Kind: session.EventToolBegin, Tool: "read", Args: `{"path":"` + path + `"}`,
		})
		a.errandEvent(ex, session.Event{Kind: session.EventToolEnd, Tool: "read"})
	}
	strip := a.exchangeStrip(ex, 40, a.pal)
	if len(strip) != exchangeStripRows {
		t.Fatalf("the strip is %d rows, want %d", len(strip), exchangeStripRows)
	}
	joined := ansi.Strip(strings.Join(strip, "\n"))
	if strings.Contains(joined, "one.go") {
		t.Fatalf("the third call did not push the first out of the strip:\n%s", joined)
	}
	if !strings.Contains(joined, "two.go") || !strings.Contains(joined, "three.go") {
		t.Fatalf("the strip is not the newest two calls:\n%s", joined)
	}
	// AND THE REPLY'S OWN TAIL IS IN IT, so a long answer visibly grows.
	a.errandEvent(ex, text(session.EventTextDelta, "the last line of this is what shows"))
	joined = ansi.Strip(strings.Join(a.exchangeStrip(ex, 60, a.pal), "\n"))
	if !strings.Contains(joined, "shows") {
		t.Fatalf("the strip does not carry the reply's tail:\n%s", joined)
	}
}

// TestAFollowUpGetsTheSameSignalAsTheFirstTurn is the report's second half: "a
// repeated second text in ask here has no signal as well". The turn state was
// set when the stream answered, so a follow-up drew the sentence and then a
// still screen until the first token arrived.
func TestAFollowUpGetsTheSameSignalAsTheFirstTurn(t *testing.T) {
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())

	a := lab.app(mine, []session.Event{
		text(session.EventTextDelta, "I will remind you at 6."),
		{Kind: session.EventTurnDone},
	})
	at := time.Now()
	a.clock = func() time.Time { return at }
	a.openHome()
	typeHome(a, "remind me at 6")
	drive(t, a, key("up"), key("enter"))

	ex := theExchange(a)
	if !ex.over() {
		t.Fatal("the scripted turn did not finish")
	}
	if strings.Contains(homeText(a), homeAskThinkWord+" · ") {
		t.Fatalf("a settled exchange is still claiming to be working:\n%s", homeText(a))
	}
	// The follow-up, typed into the pane and sent — the pane already has the
	// keyboard, which is where `ask here` left it. The command it hands back is
	// not run: what is being pinned is that the SCREEN says something before any
	// answer could have arrived.
	if !ex.focused {
		t.Fatal("the pane lost the keyboard after its first turn")
	}
	typeHome(a, "make it 7")
	a.homeKey(key("enter"))
	at = at.Add(3 * time.Second)
	if !ex.working {
		t.Fatal("a follow-up left the exchange looking idle")
	}
	frame := homeText(a)
	if !strings.Contains(frame, homeAskThinkWord+" · 3s") {
		t.Fatalf("a follow-up gets no signal that anything is happening:\n%s", frame)
	}
	if !strings.Contains(frame, homeAskWorkingWord) {
		t.Fatalf("the row does not say the exchange went back to work:\n%s", frame)
	}
}

// TestASettledExchangeIsFiledOnlyOnceItWasSeenAndLeft is the lifecycle in one
// test: it does not vanish while it is working, it does not vanish while
// nobody has read what it came to, and it goes the moment both are false.
func TestASettledExchangeIsFiledOnlyOnceItWasSeenAndLeft(t *testing.T) {
	lab := newErrandLab(t)
	now := time.Now()
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", now)
	lab.session("-tmp-alpha", "aaaa000000000002", "porting the picker", "/tmp/alpha", now.Add(-time.Hour))

	a := lab.app(mine)
	a.openHome()
	ex := askedHere(t, lab, a, "remind me at 6 to leave")

	// WORKING: moving off it files nothing.
	drive(t, a, key("esc"), key("down"))
	if len(a.exchanges) != 1 {
		t.Fatal("a working exchange was filed the moment the cursor left it")
	}
	// OVER BUT UNREAD: still nothing.
	a.errandEvent(ex, text(session.EventTextDelta, "I will remind you at 6."))
	a.errandEvent(ex, session.Event{Kind: session.EventTurnDone})
	drive(t, a, key("down"))
	if len(a.exchanges) != 1 {
		t.Fatalf("an exchange nobody has seen settled was filed: %d left", len(a.exchanges))
	}
	// SEEN: the cursor goes back onto its row and the pane is drawn.
	a.home.cursor = exchangeRowAt(a, ex)
	homeText(a)
	if !ex.seen {
		t.Fatal("drawing the pane of a settled exchange did not count as seeing it")
	}
	if len(a.exchanges) != 1 {
		t.Fatal("an exchange was filed while the cursor was still on it")
	}
	// AND LEFT: now it goes, agent closed, record where it was made.
	drive(t, a, key("down"))
	if len(a.exchanges) != 0 {
		t.Fatalf("a seen, settled exchange was not filed when the cursor left it: %d left", len(a.exchanges))
	}
	if lab.agent.closes == 0 {
		t.Fatal("filing the exchange left its agent open")
	}
	if _, err := os.Stat(filepath.Join(lab.dirs[0], "transcript.jsonl")); err != nil {
		t.Fatalf("filing an exchange that came to nothing took its record: %v", err)
	}
	if exchangeRowAt(a, ex) >= 0 {
		t.Fatal("the filed exchange kept its row")
	}
}

// TestQuittingFilesEveryOpenExchange is the way out: the window takes them all,
// and a stood one's folder reaches the item it made.
func TestQuittingFilesEveryOpenExchange(t *testing.T) {
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())

	a := lab.app(mine)
	a.openHome()
	first := askedHere(t, lab, a, "remind me at 6 to leave")
	askedHere(t, lab, a, "tell me when CI goes red")

	item := standing.Item{ID: "cccc000000000009", Words: "remind me at 6 to leave"}
	a.errandEvent(first, session.Event{Kind: session.EventStandingUpdate,
		Standing: &session.StandingNotice{Update: "stood", Item: item}})

	a.quit()
	if len(a.exchanges) != 0 {
		t.Fatalf("quitting left %d exchanges open", len(a.exchanges))
	}
	if lab.agent.closes < 2 {
		t.Fatalf("quitting closed %d of two errand agents", lab.agent.closes)
	}
	store, err := standing.Open(lab.standing)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.ExchangeDir(item.ID), "transcript.jsonl")); err != nil {
		t.Fatalf("the stood exchange was not filed under its item on the way out: %v", err)
	}
	if _, err := os.Stat(filepath.Join(lab.dirs[1], "transcript.jsonl")); err != nil {
		t.Fatalf("the other exchange lost its record on the way out: %v", err)
	}
}

// ── the narrow frame: stacked instead of refused ────────────────────────────

// TestANarrowWindowStacksTheExchangeOverTheList is the phone shape. `ask here`
// used to refuse outright on a frame with no second column, which is a person
// on a narrow terminal being told the door is for other people.
func TestANarrowWindowStacksTheExchangeOverTheList(t *testing.T) {
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())

	a := lab.app(mine)
	a.width, a.height = 60, 24
	a.openHome()
	ex := askedHere(t, lab, a, "remind me at 6 to leave")
	if _, ok := a.homeStacked(); !ok {
		t.Fatal("a narrow frame did not stack the exchange over the list")
	}
	frame := homeText(a)
	if !strings.Contains(frame, homeAskHereWord) || !strings.Contains(frame, "› remind me at 6 to leave") {
		t.Fatalf("the stacked pane is not on the screen:\n%s", frame)
	}
	if strings.Contains(frame, "Pricing Research") {
		t.Fatalf("the list is still drawn under the stacked pane:\n%s", frame)
	}
	if a.home.msg != "" {
		t.Fatalf("a narrow window refused with %q", a.home.msg)
	}
	// esc PUTS THE LIST BACK, with the row on it wearing its tail.
	drive(t, a, key("esc"))
	if _, ok := a.homeStacked(); ok {
		t.Fatal("esc did not bring the list back")
	}
	frame = homeText(a)
	if !strings.Contains(frame, homeAskHereGlyph+" remind me at 6 to leave") {
		t.Fatalf("the list came back without the exchange's row:\n%s", frame)
	}
	if !strings.Contains(frame, homeAskWorkingWord) {
		t.Fatalf("the row lost its tail on a narrow frame:\n%s", frame)
	}
	// AND enter ON THE ROW OPENS IT AGAIN.
	a.home.cursor = exchangeRowAt(a, ex)
	drive(t, a, key("enter"))
	if _, ok := a.homeStacked(); !ok {
		t.Fatal("enter on the row did not open the stacked pane again")
	}
	// THE CARD IS ANSWERABLE THERE.
	a.errandEvent(ex, standingProposal(7, "remind me at 6 to leave"))
	drive(t, a, key("1"))
	if len(lab.agent.answered) != 1 || !lab.agent.answered[0].Approved {
		t.Fatalf("the card could not be answered on a narrow frame, the agent saw %v", lab.agent.answered)
	}
}

// TestAResizeBetweenTheTwoShapesKeepsTheExchange is the other half: the same
// flag decides both shapes, so dragging a window narrow loses nothing.
func TestAResizeBetweenTheTwoShapesKeepsTheExchange(t *testing.T) {
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())

	a := lab.app(mine)
	a.width, a.height = 120, 24
	a.openHome()
	ex := askedHere(t, lab, a, "remind me at 6 to leave")
	a.errandEvent(ex, text(session.EventTextDelta, "I will remind you at 6."))
	if _, ok := a.homeStacked(); ok {
		t.Fatal("a wide frame stacked the exchange")
	}
	if !strings.Contains(homeText(a), "I will remind you at 6.") {
		t.Fatalf("the wide frame is not drawing the exchange beside the list:\n%s", homeText(a))
	}

	a.width = 60
	if _, ok := a.homeStacked(); !ok {
		t.Fatal("the narrowed frame did not stack the exchange it was already drawing")
	}
	if theExchange(a) != ex {
		t.Fatal("the resize dropped the exchange")
	}
	if !strings.Contains(homeText(a), "I will remind you at 6.") {
		t.Fatalf("the narrowed frame lost what had been said:\n%s", homeText(a))
	}
	// AND BACK AGAIN.
	a.width = 120
	if _, ok := a.homeStacked(); ok {
		t.Fatal("the widened frame is still stacked")
	}
	if !strings.Contains(homeText(a), "I will remind you at 6.") {
		t.Fatalf("the widened frame lost the exchange:\n%s", homeText(a))
	}
}

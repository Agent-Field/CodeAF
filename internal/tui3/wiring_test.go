package tui3

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The wave-4 surface: the approval question, the session's name, ctrl+q, and
// the model's own thinking.

// ── the scripted agent's wave-4 methods ─────────────────────────────────────
//
// They live here rather than beside [fakeAgent] because they arrived with this
// wave: the plain fake answers them the way a session with nothing to say does,
// and [wiredAgent] below is the one that records.

func (f *fakeAgent) FollowUp(string) (<-chan session.Event, error) {
	return make(chan session.Event), nil
}
func (f *fakeAgent) ResolveConsent(uint64, bool)                               {}
func (f *fakeAgent) ResolveConsentRemember(uint64, bool, session.ConsentScope) {}

func (f *fakeAgent) ResolveHarness(uint64, bool, string) {}
func (f *fakeAgent) Title() string                       { return "" }

// wiredAgent records the three answers this wave sends back into the session:
// a consent resolution, a queued message, and nothing else.
type wiredAgent struct {
	*fakeAgent
	name          string
	answers       []answered
	asked         []string
	follow        chan session.Event
	followStreams []chan session.Event
	followErr     error
}

type answered struct {
	id    uint64
	allow bool
	scope session.ConsentScope
}

func (w *wiredAgent) Title() string { return w.name }

func (w *wiredAgent) ResolveConsent(id uint64, allow bool) {
	w.ResolveConsentRemember(id, allow, session.ConsentOnce)
}

func (w *wiredAgent) ResolveConsentRemember(id uint64, allow bool, scope session.ConsentScope) {
	w.answers = append(w.answers, answered{id: id, allow: allow, scope: scope})
}

// ResolveQuestion is THE ONE DOOR, on the stand-in.
//
// IT ROUTES RATHER THAN DECIDING, exactly as the engine's does (session's
// [Agent.ResolveQuestion]): it reads which lane the answer names, asks the ONE
// mapping what the key means ([session.AnswerFromKey]) and how far it reaches
// ([session.ConsentScopeOf]), and hands it to the resolver these tests already
// record against. A stand-in that mapped keys itself would be a second table,
// and the first hour one of them moved the tests would be green about the wrong
// answer.
func (w *wiredAgent) ResolveQuestion(answer session.Answer) error {
	action, ok := session.AnswerFromKey(answer.Kind, answer.FirstKey())
	if !ok || action.Kind != session.QuestionConsent {
		return nil
	}
	w.ResolveConsentRemember(answer.ID, action.Allow, session.ConsentScopeOf(action, answer))
	return nil
}

func (w *wiredAgent) FollowUp(text string) (<-chan session.Event, error) {
	w.asked = append(w.asked, text)
	if w.followErr != nil {
		return nil, w.followErr
	}
	w.follow = make(chan session.Event, 8)
	w.followStreams = append(w.followStreams, w.follow)
	return w.follow, nil
}

func (w *wiredAgent) Interrupt() { w.InterruptFor(session.StopByPerson) }

func (w *wiredAgent) InterruptFor(door session.StopDoor) {
	w.fakeAgent.InterruptFor(door)
	for _, stream := range w.followStreams {
		close(stream)
	}
	w.followStreams = nil
}

func wired(turns ...[]session.Event) (*wiredAgent, *app) {
	agent := &wiredAgent{fakeAgent: &fakeAgent{model: "m", turns: turns}}
	return agent, newTestApp(agent)
}

// consentEvent is the gate's own question: about a TOOL, so a session-scoped
// answer to it would stand for something and the always key is on the offer
// (session.Event's Memo, and consent.go's [ask.memo]).
func consentEvent(id uint64, tool, hint, rule string) session.Event {
	return session.Event{
		Kind: session.EventConsentRequest, ID: id, Tool: tool, Hint: hint, Rule: rule,
		Memo: true,
	}
}

// ── the approval question, as a test reaches it ─────────────────────────────
//
// The block holds it now (question.go), so these are the three things a test
// used to read straight off `app.asks`: which question is up, its reading clock,
// and how many are queued behind it.

// askHead is the approval question the block is drawing.
func askHead(t *testing.T, a *app) *questionShown {
	t.Helper()
	open := a.consentOpen()
	if open == nil {
		t.Fatal("no approval question is on the block")
	}
	return open
}

// askCount is how many approval questions are open, queued ones included.
func askCount(a *app) int {
	n := 0
	for _, open := range a.questions {
		if open.question.Kind == session.QuestionConsent {
			n++
		}
	}
	return n
}

// raiseAsk puts one approval question on the block without a turn behind it,
// which is what a test asserting about the QUESTION rather than about the gate
// wants.
func raiseAsk(a *app, id uint64, tool string) {
	a.raiseQuestion(a.consentShown(consentEvent(id, tool, tool+" something", "")))
}

// askClockLeft is what is left of the head question's reading clock.
func askClockLeft(a *app) time.Duration {
	left, _, ok := a.questionReadingLeft()
	if !ok {
		return 0
	}
	return left
}

// askHeld reports whether that clock has stopped.
func askHeld(a *app) bool {
	_, held, ok := a.questionReadingLeft()
	return ok && held
}

// settleAsk puts every open question far enough back that the block will take a
// key from it ([app.questionSettled]).
//
// A test that pressed a key on the frame the question arrived on would be
// testing the settle guard rather than the answer — 250ms is one keystroke at a
// fast typing speed, and the whole point of the guard is that nothing inside it
// counts. The guard's OWN test drives its own clock (question_test.go's lab);
// everything else here says "the question has been on screen a moment" and gets
// on with what it is about.
func settleAsk(a *app) {
	for i := range a.questions {
		a.questions[i].shown = a.now().Add(-questionSettle - time.Millisecond)
	}
}

// askOffer is the block's answers row as a reader sees it, and which row of the
// block it is drawn on. Both come out of the draw itself ([app.questionSpanRow]
// is written there), so a test and the surface cannot disagree about where the
// answers are.
func askOffer(t *testing.T, a *app) (string, int) {
	t.Helper()
	rows := a.questionRows(a.width)
	at := a.questionSpanRow
	if at < 0 || at >= len(rows) {
		t.Fatalf("the block drew %d rows and put its answers on row %d", len(rows), at)
	}
	return rows[at], at
}

// startAskClock stamps the head question's reading clock at a moment a test
// names, which is how a test reaches an expiry without waiting for one.
func startAskClock(a *app, at time.Time, held bool) {
	open := a.consentOpen()
	if open == nil {
		return
	}
	open.clockAt, open.clockFor, open.clockHeld = at, a.askWait, held
}

// ── 1. the approval question ────────────────────────────────────────────────

func TestAConsentQuestionShowsTheCallTheOfferAndTheRule(t *testing.T) {
	agent, a := wired([]session.Event{
		toolBegin("bash", "bash rm -rf build"),
		consentEvent(7, "bash", "bash rm -rf build", `bash pattern "rm -rf *"`),
	})
	// An ordinary terminal, which is the width the line form is measured at: a
	// narrower one promotes to the card rather than cutting an answer off the
	// end (see [TestANarrowFrameGivesEveryAnswerARowRatherThanCuttingOne]).
	a.width = 80
	typeLine(t, a, "clean the tree")
	settleAsk(a)

	got := plain(frame(a))
	for _, want := range []string{
		"rm -rf build",          // the row the transcript already drew
		"allow? [1] allow once", // the answers, by the digits every question takes
		"[3] deny",
		"] always",                // the widening yes
		"[esc] later",             // and the way out, which cancels nothing
		`bash pattern "rm -rf *"`, // the policy's own words for why
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the question is missing %q:\n%s", want, got)
		}
	}
	// A FRAME WITH ROOM SAYS HOW FAR THE WIDENING YES GOES. The short spelling
	// on a narrower frame is the same answer with its aside dropped — never one
	// with an answer truncated off the end.
	a.width = 120
	if wide := plain(frame(a)); !strings.Contains(wide, "[2] always, this tool (session)") {
		t.Fatalf("the wide answers row does not say how far always reaches:\n%s", wide)
	}

	// THE BLOCK IS NEVER MODAL. A key it does not draw belongs to the composer,
	// which is what the ladder's last rung needs: somewhere to type the answer
	// that was not on offer.
	drive(t, a, key("x"))
	if a.input.String() != "x" {
		t.Fatalf("a key the block does not draw did not reach the box: %q", a.input.String())
	}
	if !a.asking() {
		t.Fatal("typing took the question off the screen")
	}
	a.input.reset()

	drive(t, a, key("1"))
	if len(agent.answers) != 1 || agent.answers[0] != (answered{id: 7, allow: true, scope: session.ConsentOnce}) {
		t.Fatalf("[1] resolved %+v", agent.answers)
	}
	if a.asking() {
		t.Fatal("the question stayed up after it was answered")
	}

	// The row stays, annotated with what was decided.
	got = plain(frame(a))
	if !strings.Contains(got, "rm -rf build") || !strings.Contains(got, "allowed") {
		t.Fatalf("the answered call lost its row or its annotation:\n%s", got)
	}
	if strings.Contains(got, "allow? [1]") {
		t.Fatalf("the answers row survived the answer:\n%s", got)
	}
	// AND THE ANSWER LEFT ITS RECEIPT where the question was.
	if !strings.Contains(got, "decided") {
		t.Fatalf("the answer left no receipt:\n%s", got)
	}
}

// THE WIDENING ANSWER IS NOT ON A QUESTION IT WOULD DO NOTHING TO. The stuck
// question (session's recovery.go) borrows the consent lane to ask about a TURN,
// and the engine drops a tool-session scope on it — so the row leaves the answer
// off, and pressing its key anyway does not answer the question by accident.
func TestTheAlwaysKeyIsHiddenAndInertOnAQuestionThatCannotRememberIt(t *testing.T) {
	ask := consentEvent(5, "bash", "bash make test", "the turn is repeating itself")
	ask.Memo = false
	agent, a := wired([]session.Event{toolBegin("bash", "bash make test"), ask})
	typeLine(t, a, "go on")
	settleAsk(a)

	got := plain(frame(a))
	if strings.Contains(got, "always") {
		t.Fatalf("an inert answer is on the row:\n%s", got)
	}
	if !strings.Contains(got, "[1] allow once") || !strings.Contains(got, "[3] deny") {
		t.Fatalf("the two real answers went with it:\n%s", got)
	}

	drive(t, a, key("2"))
	if len(agent.answers) != 0 {
		t.Fatalf("the missing answer's key answered anyway: %+v", agent.answers)
	}
	if !a.asking() {
		t.Fatal("the question was resolved by a key that is not on it")
	}
	a.input.reset()
	drive(t, a, key("3"))
	if len(agent.answers) != 1 || agent.answers[0].allow {
		t.Fatalf("[3] resolved %+v, want a deny", agent.answers)
	}
}

// THE READING CLOCK HOLDS AT EXPIRY AND NEVER ANSWERS.
//
// The clock exists so a tool call cannot be parked forever on a question nobody
// is reading. It can never answer — F41 was a hidden ten-second timer that
// recorded "denied" and killed work nobody refused — and any keypress the block
// reads ends it, since a key is evidence of a person mid-decision.
func TestTheApprovalCountdownPausesAtExpiryAndNeverDenies(t *testing.T) {
	at := time.Now()
	agent, a := wired([]session.Event{
		toolBegin("bash", "bash rm -rf build"),
		consentEvent(9, "bash", "bash rm -rf build", `bash pattern "rm -rf *"`),
	})
	// The countdown is pinned rather than read off the machine's own profile:
	// the setting's default is 10 (config), and a test that resolved it from
	// disk would be a test of whoever is running it.
	a.clock, a.askWait = func() time.Time { return at }, 10*time.Second
	a.width = 120
	typeLine(t, a, "clean it")
	settleAsk(a)

	if !strings.Contains(plain(frame(a)), "· 10s") {
		t.Fatalf("the answers row is not counting down:\n%s", plain(frame(a)))
	}
	// Short of the deadline nothing happens.
	at = at.Add(9 * time.Second)
	drive(t, a, frameMsg{})
	if len(agent.answers) != 0 {
		t.Fatalf("the clock answered early: %+v", agent.answers)
	}
	// Past it the call is NOT refused: silence is not a no (F41). The engine
	// stays blocked on the question and the row says it paused.
	at = at.Add(2 * time.Second)
	drive(t, a, frameMsg{})
	if len(agent.answers) != 0 {
		t.Fatalf("the clock answered on the person's behalf: %+v", agent.answers)
	}
	if !strings.Contains(plain(frame(a)), "paused") {
		t.Fatalf("the expired row does not say it paused:\n%s", plain(frame(a)))
	}
	if !a.asking() {
		t.Fatal("the question went away at expiry instead of waiting")
	}

	// A key that answers nothing still stops the clock, for good.
	at = time.Now()
	agent, a = wired([]session.Event{
		toolBegin("edit", "edit main.go"),
		consentEvent(4, "edit", "edit main.go", `tool "edit"`),
	})
	a.clock, a.askWait = func() time.Time { return at }, 10*time.Second
	a.width = 120
	typeLine(t, a, "edit it")
	settleAsk(a)
	drive(t, a, key("esc"))
	drive(t, a, tea.KeyPressMsg{Code: 'a', Mod: tea.ModAlt})
	if !strings.Contains(plain(frame(a)), "· paused") {
		t.Fatalf("the clock did not say it is paused:\n%s", plain(frame(a)))
	}
	at = at.Add(time.Hour)
	drive(t, a, frameMsg{})
	if len(agent.answers) != 0 {
		t.Fatalf("a paused clock answered anyway: %+v", agent.answers)
	}
	if !a.asking() {
		t.Fatal("the question went away on a paused clock")
	}
}

// esc IS LATER AND CANCELS NOTHING, which is the one word on this block that
// changed meaning. It was `cancel` for a year and cancel meant deny — the safe
// reading of "get this off my screen" when the only alternative was a modal
// nobody could leave. The block is not modal any more, so esc folds the question
// to the chip, leaves the turn paused on it, and lets the person type.
func TestEscapeIsLaterOnAnApprovalQuestionAndAnswersNothing(t *testing.T) {
	agent, a := wired([]session.Event{
		toolBegin("edit", "edit main.go"),
		consentEvent(3, "edit", "edit main.go", `tool "edit"`),
	})
	a.width = 80
	typeLine(t, a, "fix it")
	settleAsk(a)
	drive(t, a, key("esc"))
	if len(agent.answers) != 0 {
		t.Fatalf("esc answered %+v, and it answers nothing", agent.answers)
	}
	if !a.asking() {
		t.Fatal("esc took the question off the block instead of folding it")
	}
	if got := plain(frame(a)); !strings.Contains(got, "1 question · "+questionChipKey) {
		t.Fatalf("the folded question is not counted on the chip:\n%s", got)
	}
	// And the chip brings it back.
	drive(t, a, tea.KeyPressMsg{Code: 'a', Mod: tea.ModAlt})
	if got := plain(frame(a)); !strings.Contains(got, "allow?") {
		t.Fatalf("the chip did not raise the folded question:\n%s", got)
	}
}

// The widening yes is the only answer that widens anything, and it says which
// scope it carried.
func TestTheWideningYesTakesItsOwnRoad(t *testing.T) {
	agent, a := wired([]session.Event{
		toolBegin("read", "read go.mod"),
		consentEvent(11, "read", "read go.mod", `tool "read"`),
	})
	typeLine(t, a, "look at it")
	settleAsk(a)
	drive(t, a, key("2"))
	want := answered{id: 11, allow: true, scope: session.ConsentToolSession}
	if len(agent.answers) != 1 || agent.answers[0] != want {
		t.Fatalf("[2] resolved %+v, want %+v", agent.answers, want)
	}
}

// A NARROW FRAME GIVES EVERY ANSWER A ROW RATHER THAN CUTTING ONE OFF.
//
// The line puts the question and its answers on one row, and on a frame with no
// room for that the honest shape is the card — an offer with its tail truncated
// is an offer that hides an answer, and the answer it hides is the last one,
// which on this lane is `deny`. Forms promote and never demote
// (docs/design/questions/DESIGN.md).
func TestANarrowFrameGivesEveryAnswerARowRatherThanCuttingOne(t *testing.T) {
	_, a := wired([]session.Event{
		toolBegin("bash", "bash rm -rf build"),
		consentEvent(7, "bash", "bash rm -rf build", `bash pattern "rm -rf *"`),
	})
	a.width = 60
	typeLine(t, a, "clean the tree")
	settleAsk(a)

	got := plain(frame(a))
	for _, want := range []string{"1  allow once", "2  always", "3  deny", "[esc] later"} {
		if !strings.Contains(got, want) {
			t.Fatalf("at sixty columns the card is missing %q:\n%s", want, got)
		}
	}
	for _, row := range a.questionRows(a.width) {
		if w := ansi.StringWidth(row); w > a.width {
			t.Fatalf("a row is %d cells wide on a %d-column frame: %q", w, a.width, row)
		}
	}
}

func TestQuestionsQueueOldestFirstAndSayHowManyAreBehind(t *testing.T) {
	agent, a := wired([]session.Event{
		toolBegin("bash", "bash make"),
		toolBegin("bash", "bash git push"),
		toolBegin("bash", "bash rm -rf ."),
		consentEvent(1, "bash", "bash make", "default"),
		consentEvent(2, "bash", "bash git push", "default"),
		consentEvent(3, "bash", "bash rm -rf .", "default"),
	})
	typeLine(t, a, "do the three things")
	settleAsk(a)

	if askCount(a) != 3 {
		t.Fatalf("%d questions are queued, want 3", askCount(a))
	}
	if !strings.Contains(plain(frame(a)), "2 more") {
		t.Fatalf("the block has to say what is behind it:\n%s", plain(frame(a)))
	}

	// Oldest first, and the count follows.
	drive(t, a, key("2"))
	if len(agent.answers) != 1 || agent.answers[0].id != 1 {
		t.Fatalf("the queue answered %+v first", agent.answers)
	}
	if !strings.Contains(plain(frame(a)), "1 more") {
		t.Fatalf("the count did not follow the answer:\n%s", plain(frame(a)))
	}
	drive(t, a, key("3"))
	drive(t, a, key("2"))
	if len(agent.answers) != 3 || agent.answers[1].id != 2 || agent.answers[2].id != 3 {
		t.Fatalf("the queue resolved out of order: %+v", agent.answers)
	}
	if a.asking() || strings.Contains(plain(frame(a)), "allow? [1]") {
		t.Fatalf("the block survived an empty queue:\n%s", plain(frame(a)))
	}
	// Each call kept the decision that was made about it.
	got := plain(frame(a))
	if strings.Count(got, "allowed") != 2 || strings.Count(got, "denied") != 1 {
		t.Fatalf("the annotations do not match the answers:\n%s", got)
	}
}

// ── 2. the session's name ───────────────────────────────────────────────────

func TestTheTitleReachesTheStatusLineLiveAndOnResume(t *testing.T) {
	_, a := wired([]session.Event{{Kind: session.EventTitleChanged, Text: "porting the parser"}})
	typeLine(t, a, "port it")
	settleAsk(a)

	status := plain(a.status(a.width))
	if !strings.Contains(status, "porting the parser") {
		t.Fatalf("the status line is missing the title:\n%s", status)
	}
	if strings.Index(status, "porting the parser") > strings.Index(status, a.model) {
		t.Fatalf("the title has to sit left of the model:\n%s", status)
	}

	// A resumed session is already named, and opens saying so.
	named := &wiredAgent{fakeAgent: &fakeAgent{model: "m"}, name: "the tasker wave"}
	resumed := newTestApp(named)
	if !strings.Contains(plain(resumed.status(resumed.width)), "the tasker wave") {
		t.Fatalf("a resumed session opened without its name:\n%s", plain(resumed.status(resumed.width)))
	}
}

// ── 3. ctrl+q ───────────────────────────────────────────────────────────────

func ctrlQ() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl} }

func TestCtrlQQueuesAMessageForAfterTheTurnAndShowsTheCount(t *testing.T) {
	agent, a := wired([]session.Event{text(session.EventTextDelta, "working on it")})
	typeLine(t, a, "the first thing")
	settleAsk(a)
	if a.state != stateWorking {
		t.Fatalf("state is %v, want working", a.state)
	}

	// Nothing typed is nothing queued.
	drive(t, a, ctrlQ())
	if len(agent.asked) != 0 {
		t.Fatalf("an empty draft queued %q", agent.asked)
	}

	typeInto(t, a, "and then the tests")
	drive(t, a, ctrlQ())
	if len(agent.asked) != 1 || agent.asked[0] != "and then the tests" {
		t.Fatalf("ctrl+q sent %q to FollowUp", agent.asked)
	}
	if a.input.String() != "" {
		t.Fatalf("the box kept %q", a.input.String())
	}
	if !strings.Contains(plain(frame(a)), "after yield · 1") {
		t.Fatalf("the count is not above the box:\n%s", plain(frame(a)))
	}
	// It is NOT in the transcript yet: it lands where it actually runs.
	if strings.Contains(strings.Join(plainRows(a), "\n"), "and then the tests") {
		t.Fatal("a queued message was drawn before its turn")
	}

	// The turn ends, and the queued message's own turn begins on the channel
	// session handed back when it was queued — which is already carrying the
	// turn's first word, exactly as a real one would be by the time it is read.
	agent.follow <- text(session.EventTextDelta, "the tests pass")
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
	if len(a.follows) != 0 {
		t.Fatalf("%d messages are still queued", len(a.follows))
	}
	if a.state != stateWorking {
		t.Fatalf("the follow-up's turn did not start (state %v)", a.state)
	}
	body := strings.Join(plainRows(a), "\n")
	if !strings.Contains(body, "› and then the tests") {
		t.Fatalf("the follow-up's own message is not in the transcript:\n%s", body)
	}
	if strings.Contains(plain(frame(a)), "after yield") {
		t.Fatalf("the count outlived the queue:\n%s", plain(frame(a)))
	}

	// And that channel is the live stream now. Deltas become rows on the frame
	// clock, so the frame is asked for one.
	drive(t, a, frameMsg{})
	if !strings.Contains(strings.Join(plainRows(a), "\n"), "the tests pass") {
		t.Fatalf("the follow-up's turn is not streaming:\n%s", strings.Join(plainRows(a), "\n"))
	}
}

func TestAnInterruptDropsWhatWasQueued(t *testing.T) {
	agent, a := wired([]session.Event{text(session.EventTextDelta, "working")})
	typeLine(t, a, "go")
	settleAsk(a)
	typeInto(t, a, "and after that")
	drive(t, a, ctrlQ())
	if len(a.follows) != 1 {
		t.Fatalf("%d queued, want 1", len(a.follows))
	}
	_ = agent

	drive(t, a, key("esc"))
	if len(a.follows) != 0 {
		t.Fatal("the interrupt kept the queue the session just dropped")
	}
	got := plain(frame(a))
	if !strings.Contains(got, "1 queued message dropped") {
		t.Fatalf("the surface dropped a message silently:\n%s", got)
	}
}

// M9: two ctrl+q follow-ups become fresh turns in FIFO order, and the parked
// message waits until both session-owned streams have closed.
func TestFollowUpsDrainInOrderBeforeTheParkedMessage(t *testing.T) {
	agent, a := wired([]session.Event{text(session.EventTextDelta, "working")})
	typeLine(t, a, "the first turn")
	settleAsk(a)
	typeInto(t, a, "first follow-up")
	drive(t, a, ctrlQ())
	typeInto(t, a, "second follow-up")
	drive(t, a, ctrlQ())
	parkLine(t, a, "the parked message")
	if got := agent.asked; len(got) != 2 || got[0] != "first follow-up" || got[1] != "second follow-up" {
		t.Fatalf("the queued follow-ups are %q", got)
	}
	if len(a.parks) != 1 {
		t.Fatalf("the parked message is %+v", a.parks)
	}
	firstTurn := a.turn

	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
	if a.turn != firstTurn+1 || len(a.follows) != 1 || len(a.parks) != 1 {
		t.Fatalf("after the first close: turn=%d follows=%d parks=%d", a.turn, len(a.follows), len(a.parks))
	}
	close(agent.followStreams[0])
	drive(t, a, streamClosedMsg{gen: a.gen})
	if a.turn != firstTurn+2 || len(a.follows) != 0 || len(a.parks) != 1 {
		t.Fatalf("after the second close: turn=%d follows=%d parks=%d", a.turn, len(a.follows), len(a.parks))
	}
	close(agent.followStreams[1])
	drive(t, a, streamClosedMsg{gen: a.gen})
	if a.turn != firstTurn+3 || len(a.parks) != 0 {
		t.Fatalf("the parked turn did not start last: turn=%d parks=%d", a.turn, len(a.parks))
	}

	var said []string
	for _, entry := range a.entries {
		if entry.kind == entryUser {
			said = append(said, entry.text)
		}
	}
	want := []string{"the first turn", "first follow-up", "second follow-up", "the parked message"}
	if len(said) != len(want) {
		t.Fatalf("the fresh-turn order is %q, want %q", said, want)
	}
	for i := range want {
		if said[i] != want[i] {
			t.Fatalf("the fresh-turn order is %q, want %q", said, want)
		}
	}
	if len(agent.sent) != 2 || agent.sent[1] != "the parked message" {
		t.Fatalf("the parked message was submitted %q", agent.sent)
	}
}

// M10: Esc drops both session-owned queues, closes every follow-up stream, and
// drops the surface-owned parked queue before the stopped stream ends.
func TestEscClosesQueuedStreamsAndDropsTheParkedTurn(t *testing.T) {
	agent, a := wired([]session.Event{text(session.EventTextDelta, "working")})
	typeLine(t, a, "the first turn")
	settleAsk(a)
	for _, line := range []string{"first follow-up", "second follow-up"} {
		typeInto(t, a, line)
		drive(t, a, ctrlQ())
	}
	parkLine(t, a, "the parked message")
	firstTurn := a.turn

	drive(t, a, key("esc"))
	if len(a.follows) != 0 {
		t.Fatalf("Esc left %d follow-ups on the surface", len(a.follows))
	}
	for index, stream := range agent.followStreams {
		if _, open := <-stream; open {
			t.Fatalf("follow-up stream %d remained open after Esc", index)
		}
	}
	if len(agent.sent) != 1 || len(a.parks) != 0 {
		t.Fatalf("Esc did not drop the parked message: sent=%q parks=%+v", agent.sent, a.parks)
	}

	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})
	if len(agent.sent) != 1 {
		t.Fatalf("the stream close resurrected a dropped message: %q", agent.sent)
	}
	if a.turn != firstTurn || len(a.parks) != 0 {
		t.Fatalf("the close opened orphaned turn %d with %d parked", a.turn, len(a.parks))
	}
}

// ── 4. the thinking block ───────────────────────────────────────────────────

// thoughtAt is the index of the newest thinking block.
func thoughtAt(t *testing.T, a *app) int {
	t.Helper()
	for i := len(a.entries) - 1; i >= 0; i-- {
		if a.entries[i].kind == entryThinking {
			return i
		}
	}
	t.Fatal("there is no thinking block")
	return -1
}

// clickEntry drives a left click on the first visible row of one entry.
func clickEntry(t *testing.T, a *app, entry int) {
	t.Helper()
	body, _ := a.window(a.width, a.viewHeight())
	for i, r := range body {
		if r.entry == entry {
			drive(t, a, tea.MouseClickMsg{Y: a.bodyTop() + i, Button: tea.MouseLeft})
			drive(t, a, tea.MouseReleaseMsg{Y: a.bodyTop() + i, Button: tea.MouseLeft})
			return
		}
	}
	t.Fatalf("entry %d is not on screen:\n%s", entry, strings.Join(plainRows(a), "\n"))
}

func TestTheThinkingBlockStreamsCollapsesAndExpands(t *testing.T) {
	_, a := wired([]session.Event{
		text(session.EventReasoning, "the parser is probably under internal/, "),
		text(session.EventReasoning, "so read that first"),
	})
	typeLine(t, a, "where is the parser?")
	settleAsk(a)
	showLiveWork(t, a)

	got := plain(frame(a))
	if !strings.Contains(got, glyphThought) || !strings.Contains(got, "probably under internal/") {
		t.Fatalf("the streaming block is not on screen:\n%s", got)
	}

	// The turn's first non-reasoning word collapses it.
	drive(t, a, streamEventMsg{gen: a.gen, ev: text(session.EventTextDelta, "internal/parse/parse.go")})
	// A full answer appears once its response is confirmed, while the
	// thought block retains its independent disclosure underneath work.
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{Kind: session.EventAssistantDone}})
	at := -1
	for i := range a.entries {
		if a.entries[i].kind == entryThinking {
			at = i
		}
	}
	if at < 0 {
		t.Fatal("the block is gone entirely")
	}
	// A label with a number in it needs a span to measure; the deltas above
	// arrived in one millisecond.
	a.entries[at].began = a.entries[at].ended.Add(-6 * time.Second)
	a.entries[at].stale = true
	a.touch()

	got = plain(frame(a))
	// The size rides the collapsed row beside the span (thinking.go).
	want := "⠿ thought for 6s · " + thoughtCount(&a.entries[at]) + " · ctrl+e"
	if !strings.Contains(got, want) {
		t.Fatalf("the collapsed row is wrong, want %q:\n%s", want, got)
	}
	if strings.Contains(got, "probably under internal/") {
		t.Fatalf("the collapsed block is still showing its words:\n%s", got)
	}
	if !strings.Contains(got, "internal/parse/parse.go") {
		t.Fatalf("the answer is missing:\n%s", got)
	}

	// The block's own door opens it, and closes it again. It is called rather
	// than pressed as `ctrl+e`, because over a running turn that key belongs to
	// the whole work the block sits inside (workfold.go's
	// [app.toggleLatestWorkfold]); a click on the block reaches this one
	// (thinking.go).
	a.toggleLatestThought()
	if !strings.Contains(plain(frame(a)), "probably under internal/") {
		t.Fatalf("the block's own door did not expand it:\n%s", plain(frame(a)))
	}
	a.toggleLatestThought()
	if strings.Contains(plain(frame(a)), "probably under internal/") {
		t.Fatalf("the block's own door did not close it again:\n%s", plain(frame(a)))
	}

	// With a sentence in the box ctrl+e is end-of-line, where the caret is.
	typeInto(t, a, "next")
	a.input.home()
	drive(t, a, tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	if a.input.cursor != len("next") {
		t.Fatalf("ctrl+e with a draft moved the caret to %d", a.input.cursor)
	}
	if strings.Contains(plain(frame(a)), "probably under internal/") {
		t.Fatal("ctrl+e with a draft opened the block as well")
	}
}

func TestALongThoughtIsCappedAndAClickOpensIt(t *testing.T) {
	lines := make([]string, 0, thoughtWindow+40)
	for i := 0; i < thoughtWindow+40; i++ {
		lines = append(lines, "step "+itoa(i))
	}
	_, a := wired([]session.Event{text(session.EventReasoning, strings.Join(lines, "\n"))})
	typeLine(t, a, "think it through")
	settleAsk(a)
	showLiveWork(t, a)
	drive(t, a, streamEventMsg{gen: a.gen, ev: text(session.EventTextDelta, "done")})

	// Collapsed to one row.
	drawn := strings.Join(plainRows(a), "\n")
	if strings.Contains(drawn, "step 3") {
		t.Fatalf("the collapsed block is drawing its body:\n%s", drawn)
	}

	// A click anywhere on that row opens it, capped, saying by how much.
	clickEntry(t, a, thoughtAt(t, a))
	drawn = strings.Join(plainRows(a), "\n")
	if !strings.Contains(drawn, "step 0") || strings.Contains(drawn, "step "+itoa(thoughtWindow+10)) {
		t.Fatalf("the expansion is not capped at %d rows:\n%s", thoughtWindow, drawn)
	}
	if !strings.Contains(drawn, "… 40 more") {
		t.Fatalf("the cap is not declared:\n%s", drawn)
	}
}

func TestReasoningPersistsOnScreenAndIsNeverReplayed(t *testing.T) {
	agent, a := wired([]session.Event{
		text(session.EventReasoning, "weighing it up"),
		text(session.EventTextDelta, "yes"),
	})
	typeLine(t, a, "well?")
	settleAsk(a)
	agent.finish()
	drive(t, a, streamClosedMsg{gen: a.gen})

	thoughts := 0
	for i := range a.entries {
		if a.entries[i].kind == entryThinking {
			thoughts++
			if !a.entries[i].settled || a.entries[i].open {
				t.Fatal("the block did not settle closed at the end of the turn")
			}
		}
	}
	if thoughts != 1 {
		t.Fatalf("the turn left %d thinking blocks", thoughts)
	}

	// The session journals no reasoning, so a resumed surface over the same
	// conversation has none to draw — and must not invent one.
	agent.past = []session.DisplayEntry{
		{Role: "user", Text: "well?"},
		{Role: "assistant", Text: "yes"},
	}
	next := newTestApp(agent)
	next.entries = nil
	next.replay()
	for i := range next.entries {
		if next.entries[i].kind == entryThinking {
			t.Fatal("replay drew a thinking block the session never recorded")
		}
	}
}

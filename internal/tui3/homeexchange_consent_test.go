package tui3

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
)

// ── an approval asked inside an exchange ─────────────────────────────────────
//
// The errand's session is built with the gate allowed to ASK (cmd/codeaf's
// chatv3_local.go sets AskConsent on it, because its own standing card needs
// the same door), so a call the policy grades PROMPT emits
// session.EventConsentRequest on the errand's own stream and then BLOCKS until
// that agent is answered (internal/session's consent.go). Nothing else in this
// program holds that agent, so the pane is the only place the question can be
// answered — and these tests are what it has to do about it.

// ciArgs is one gated call's arguments, as the engine renders them.
const ciArgs = `{"command":"gh run list --branch main"}`

// ciRule is the policy's own sentence for asking, which the engine carries in
// Event.Rule and the question draws as its reason.
const ciRule = `bash pattern "gh *" asks first`

// ciCall is the call the turn announces before the gate stops it.
func ciCall(callID string) session.Event {
	return session.Event{
		Kind: session.EventToolBegin, Tool: "bash", CallID: callID,
		Args: ciArgs, Hint: "gh run list --branch main",
	}
}

// ciConsent is the gate's request for that call, exactly as askAnswer sends
// it: the id an answer names, the call it is about, and the rule.
func ciConsent(id uint64, callID string) session.Event {
	return session.Event{
		Kind: session.EventConsentRequest, ID: id, Tool: "bash", CallID: callID,
		Hint: "gh run list --branch main", Args: ciArgs, Rule: ciRule,
		Memo: true, Wait: session.ConsentWaiting,
	}
}

// consentGiven is one answer the errand's own agent was handed.
type consentGiven struct {
	id    uint64
	allow bool
	scope session.ConsentScope
}

// gatedErrand is the scripted errand whose turn STOPS ON A PERSON. It is
// [errandAgent] with the two differences that make a gate a gate: its stream
// is LEFT OPEN after the scripted events, because the engine holds the call
// until somebody answers, and it records the answers it is given — which is the
// only way a test can tell that a key in the pane reached the errand's session
// rather than the window's.
type gatedErrand struct {
	*errandAgent
	given   []consentGiven
	streams []chan session.Event
}

func (g *gatedErrand) Submit(ctx context.Context, text string) (<-chan session.Event, error) {
	g.sent = append(g.sent, text)
	out := make(chan session.Event, 64)
	if g.turn < len(g.turns) {
		for _, event := range g.turns[g.turn] {
			out <- event
		}
		g.turn++
	}
	// THE STREAM STAYS OPEN: the turn is blocked on the call, not finished.
	g.streams = append(g.streams, out)
	return out, nil
}

func (g *gatedErrand) ResolveConsent(id uint64, allow bool) {
	g.ResolveConsentRemember(id, allow, session.ConsentOnce)
}

func (g *gatedErrand) ResolveConsentRemember(id uint64, allow bool, scope session.ConsentScope) {
	g.given = append(g.given, consentGiven{id: id, allow: allow, scope: scope})
}

// gatedApp is [errandLab.app] with the seam handing out the gated errand, its
// streams closed when the test ends so no reader is left parked on them.
func (l *errandLab) gatedApp(here string, turns ...[]session.Event) (*app, *gatedErrand) {
	l.t.Helper()
	a := l.app(here)
	gated := &gatedErrand{errandAgent: &errandAgent{fakeAgent: fakeAgent{model: "m", turns: turns}}}
	a.errand = func(orders ErrandOrders) (Agent, error) {
		l.dirs = append(l.dirs, orders.Dir)
		if err := os.WriteFile(filepath.Join(orders.Dir, "transcript.jsonl"),
			[]byte(`{"type":"session","version":1}`+"\n"), 0o600); err != nil {
			return nil, err
		}
		return gated, nil
	}
	l.t.Cleanup(func() {
		for _, stream := range gated.streams {
			close(stream)
		}
	})
	return a, gated
}

// askGated opens home on a frame wide enough to hold the pane beside the list,
// pins the clock, asks one thing whose first turn stops on the gate, and draws
// the frame once — the approval has then been on a screen, which is what the
// settle guard counts from. The clock is handed back so a test can let the
// question settle before a key.
func askGated(t *testing.T, turn []session.Event) (*app, *gatedErrand, *homeExchange, *time.Time) {
	t.Helper()
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())
	a, gated := lab.gatedApp(mine, turn)
	besideTheList(a)
	now := time.Now()
	a.clock = func() time.Time { return now }
	a.openHome()
	typeHome(a, "what is CI doing on main")
	drive(t, a, key("up"), key("enter"))
	ex := theExchange(a)
	if ex == nil {
		t.Fatal("`ask here` opened no exchange")
	}
	homeText(a)
	return a, gated, ex, &now
}

// paneAnswers is what the pane drew at each target its last draw recorded for
// the question its digits answer ([homeExchange.askAt]) — the rows a press
// resolves to, read rather than searched for, so an answer named only in the
// hint under the box does not count as drawn.
func paneAnswers(a *app, ex *homeExchange) []string {
	rows := a.exchangePane(ex, 60, 200, a.pal)
	out := make([]string, 0, len(ex.askAt))
	for _, at := range ex.askAt {
		if at < 0 || at >= len(rows) {
			out = append(out, "")
			continue
		}
		out = append(out, strings.TrimSpace(ansi.Strip(rows[at])))
	}
	return out
}

// paneRowsSaying counts the pane's own rows that say something.
func paneRowsSaying(a *app, ex *homeExchange, words string) int {
	count := 0
	for _, row := range a.exchangePane(ex, 60, 200, a.pal) {
		if strings.Contains(ansi.Strip(row), words) {
			count++
		}
	}
	return count
}

// spentStream is a stream that has already ended, for feeding an exchange the
// rest of a turn by hand: the reader the real stream left parked cannot be
// handed an event without taking it, so the remainder arrives as the messages
// the pump would have made of it (homeexchange_test.go does the same).
func spentStream() <-chan session.Event {
	spent := make(chan session.Event)
	close(spent)
	return spent
}

// TestAnApprovalInsideAnExchangeIsAskedAndAnsweredInThePane is the defect and
// its repair in one walk: a call made inside an `ask here` errand needs the
// person's ok, and the only place it can be given is the pane the errand is
// drawn in.
//
// IT USED TO HAVE NOWHERE TO BE ANSWERED. errandEvent folded text, tools, the
// standing card and the turn's end, and let EventConsentRequest fall through —
// so the session sat blocked on a question no screen drew, the row said
// `working` with its clock counting up for as long as the window was open, and
// a `1` pressed at it was typed into the follow-up box.
func TestAnApprovalInsideAnExchangeIsAskedAndAnsweredInThePane(t *testing.T) {
	a, gated, ex, now := askGated(t, []session.Event{
		text(session.EventTextDelta, "Checking the latest run."),
		ciCall("call_7"),
		ciConsent(7, "call_7"),
	})

	// THE ROW SAYS IT WANTS YOU, and sorts as the thing that does.
	if tail := ansi.Strip(a.exchangeTail(ex, false)); !strings.Contains(tail, homeAskWaitingWord) {
		t.Fatalf("an exchange stopped on an approval says %q on its row, want %q", tail, homeAskWaitingWord)
	}
	if !ex.waiting() || exchangeRank(ex) != 0 {
		t.Fatalf("an exchange stopped on an approval is not waiting (waiting=%v rank=%d)", ex.waiting(), exchangeRank(ex))
	}
	// THE PANE ASKS IT: the gate's own sentence, the call it is about, the
	// policy's reason, and the two answers — and never `always`, which would be
	// a standing permission written from a forty-cell errand pane.
	frame := homeText(a)
	for _, want := range []string{session.ConsentHead("bash", ciArgs), "gh run list --branch main", ciRule} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the pane does not say %q:\n%s", want, frame)
		}
	}
	answers := paneAnswers(a, ex)
	if len(answers) != 2 || !strings.Contains(answers[0], "1") || !strings.Contains(answers[0], "allow once") ||
		!strings.Contains(answers[1], "3") || !strings.Contains(answers[1], "deny") {
		t.Fatalf("the pane's answers are %q, want `1 allow once` and `3 deny`:\n%s", answers, frame)
	}
	if strings.Contains(frame, "always") {
		t.Fatalf("the pane offers a widening answer:\n%s", frame)
	}
	// A CALL WAITING ON A PERSON IS NOT A CALL DOING WORK, so the live block
	// says who it is waiting on rather than `running`.
	if strings.Contains(frame, homeAskRunWord) {
		t.Fatalf("the pane claims the call is running while it waits for an answer:\n%s", frame)
	}
	// AND THE HINT NAMES EXACTLY THOSE ANSWERS.
	hint := exchangeHint(ex)
	if !strings.Contains(hint, "1 allow once") || !strings.Contains(hint, "3 deny") {
		t.Fatalf("the hint does not name the approval's answers: %q", hint)
	}
	if strings.Contains(hint, "always") || strings.HasPrefix(hint, questionCommentKey+" ") ||
		strings.Contains(hint, " · "+questionCommentKey+" ") {
		t.Fatalf("the hint names an answer the approval does not offer: %q", hint)
	}

	// `1` IS ALLOW ONCE, and it reaches the ERRAND's own session.
	*now = now.Add(time.Second)
	drive(t, a, key("1"))
	want := []consentGiven{{id: 7, allow: true, scope: session.ConsentOnce}}
	if len(gated.given) != 1 || gated.given[0] != want[0] {
		t.Fatalf("the errand's session was answered %+v, want %+v", gated.given, want)
	}
	if got := ex.box.String(); got != "" {
		t.Fatalf("the answer was typed into the follow-up box as %q", got)
	}
	if ex.waiting() {
		t.Fatal("the exchange is still waiting after its approval was answered")
	}
	if !ex.working {
		t.Fatal("an allowed call's turn is still running, and the exchange stopped saying so")
	}
	if paneRowsSaying(a, ex, "allow once") != 0 || len(ex.askAt) != 0 {
		t.Fatalf("the answered approval is still on the pane:\n%s", homeText(a))
	}
	// THE WINDOW KEEPS NO RECEIPT OF IT. A receipt above the box belongs to
	// the conversation the window is holding, and `c change` on one is answered
	// by THAT engine — so an errand's decision filed there would be a key that
	// re-asks it of a session that never asked it.
	if len(a.questionRecords) != 0 {
		t.Fatalf("the window filed a receipt for the errand's approval: %+v", a.questionRecords)
	}

	// AND THE ROW KEEPS WHAT WAS DECIDED once the call comes back.
	spent := spentStream()
	drive(t, a,
		errandEventMsg{ex: ex, ch: spent, ev: session.Event{Kind: session.EventToolEnd, Tool: "bash", CallID: "call_7", Args: ciArgs}},
		errandEventMsg{ex: ex, ch: spent, ev: text(session.EventTextDelta, "main is green.")},
		errandEventMsg{ex: ex, ch: spent, ev: session.Event{Kind: session.EventTurnDone}},
	)
	if ex.working || !ex.over() {
		t.Fatalf("the turn ended and the exchange is not over (working=%v)", ex.working)
	}
	settled := homeText(a)
	if !strings.Contains(settled, "gh run list --branch main · "+decisionWord(true)) {
		t.Fatalf("the call's row does not keep the answer it was given:\n%s", settled)
	}
	if !strings.Contains(settled, "main is green.") {
		t.Fatalf("the reply after the approval did not reach the pane:\n%s", settled)
	}
}

// TestThreeDeniesAnApprovalInThePane is the other answer: `3` refuses the call
// on the errand's session, and the row says so at once rather than spinning
// until the batch around it finishes.
func TestThreeDeniesAnApprovalInThePane(t *testing.T) {
	a, gated, ex, now := askGated(t, []session.Event{ciCall("call_7"), ciConsent(7, "call_7")})

	*now = now.Add(time.Second)
	drive(t, a, key("3"))
	if len(gated.given) != 1 || gated.given[0] != (consentGiven{id: 7, allow: false, scope: session.ConsentOnce}) {
		t.Fatalf("the errand's session was answered %+v, want a refusal of 7", gated.given)
	}
	if ex.waiting() {
		t.Fatal("the exchange is still waiting after its approval was refused")
	}
	if frame := homeText(a); !strings.Contains(frame, "gh run list --branch main · "+decisionWord(false)) {
		t.Fatalf("the refused call's row does not say it was refused:\n%s", frame)
	}
	// AND THE ENGINE'S OWN ACCOUNT OF THE REFUSAL, which arrives when the rest
	// of the step does, is the MODEL's sentence: it lands on the same row and
	// leaves it saying what the person said, rather than opening a second row or
	// saying `denied` twice.
	drive(t, a, errandEventMsg{ex: ex, ch: spentStream(), ev: session.Event{
		Kind: session.EventToolFailed, Tool: "bash", CallID: "call_7", Args: ciArgs,
		Hint: "denied by the person: " + ciRule,
	}})
	tools := 0
	for _, row := range ex.rows {
		if row.kind == exchangeTool {
			tools++
		}
	}
	if tools != 1 {
		t.Fatalf("the refusal opened a second row for one call: %d tool rows", tools)
	}
	frame := homeText(a)
	if !strings.Contains(frame, "gh run list --branch main · "+decisionWord(false)) {
		t.Fatalf("the refused call's row lost the person's answer:\n%s", frame)
	}
	if strings.Contains(frame, "denied by the person") {
		t.Fatalf("the row repeats the refusal in the engine's words:\n%s", frame)
	}
}

// TestADigitThatLandsWithTheApprovalIsDropped is the block's settle guard, kept
// in the pane: a key that arrives before the question has been up for
// [questionSettle] was aimed at whatever was there before it. Here that is the
// follow-up box, so a `1` typed into a sentence must not allow a call the
// person never read.
func TestADigitThatLandsWithTheApprovalIsDropped(t *testing.T) {
	a, gated, ex, now := askGated(t, []session.Event{ciCall("call_7"), ciConsent(7, "call_7")})

	drive(t, a, key("1"))
	if len(gated.given) != 0 {
		t.Fatalf("a key that landed with the question answered it: %+v", gated.given)
	}
	if !ex.waiting() {
		t.Fatal("the dropped key took the question down")
	}
	if got := ex.box.String(); got != "" {
		t.Fatalf("the dropped key was typed into the box as %q", got)
	}
	*now = now.Add(questionSettle)
	drive(t, a, key("1"))
	if len(gated.given) != 1 || !gated.given[0].allow {
		t.Fatalf("a settled `1` did not allow the call: %+v", gated.given)
	}
}

// TestTwoApprovalsInOneStepAreAskedOneAtATime is a batch that stops on two
// calls: the gate raises one question per call, both are on the pane, and the
// digits answer the oldest until it is answered.
func TestTwoApprovalsInOneStepAreAskedOneAtATime(t *testing.T) {
	second := `{"command":"gh pr list"}`
	a, gated, ex, now := askGated(t, []session.Event{
		ciCall("call_7"),
		{Kind: session.EventToolBegin, Tool: "bash", CallID: "call_8", Args: second, Hint: "gh pr list"},
		ciConsent(7, "call_7"),
		{
			Kind: session.EventConsentRequest, ID: 8, Tool: "bash", CallID: "call_8",
			Hint: "gh pr list", Args: second, Rule: ciRule, Memo: true, Wait: session.ConsentWaiting,
		},
	})

	if len(ex.permits) != 2 {
		t.Fatalf("the pane holds %d approvals, want 2", len(ex.permits))
	}
	// ONE SET OF ANSWERS ON THE SCREEN, under the call the digits answer, and
	// the gate's sentence under both calls.
	if drawn := paneRowsSaying(a, ex, "allow once"); drawn != 1 {
		t.Fatalf("the pane draws the answers %d times, want once:\n%s", drawn, homeText(a))
	}
	if heads := paneRowsSaying(a, ex, session.ConsentHead("bash", second)); heads != 2 {
		t.Fatalf("the pane says the gate's sentence %d times, want once under each call:\n%s", heads, homeText(a))
	}
	*now = now.Add(time.Second)
	drive(t, a, key("1"))
	// A SECOND KEY ON THE HEELS OF THE FIRST IS AIMED AT THE QUESTION IT JUST
	// ANSWERED, not at the one that moved under it — so it is dropped until the
	// next approval has been on the screen for the settle guard's quarter second.
	drive(t, a, key("3"))
	if len(gated.given) != 1 {
		t.Fatalf("a key that landed with the second approval answered it: %+v", gated.given)
	}
	homeText(a)
	*now = now.Add(questionSettle)
	drive(t, a, key("3"))
	want := []consentGiven{
		{id: 7, allow: true, scope: session.ConsentOnce},
		{id: 8, allow: false, scope: session.ConsentOnce},
	}
	if len(gated.given) != 2 || gated.given[0] != want[0] || gated.given[1] != want[1] {
		t.Fatalf("the two approvals were answered %+v, want %+v", gated.given, want)
	}
	if ex.waiting() {
		t.Fatal("the exchange is still waiting after both approvals were answered")
	}
}

// TestAnApprovalLeavesThePaneWhenItsTurnEnds is the question outliving its
// call. A turn that ends while the gate is asking — interrupted, or the window
// closing — has already released the call with "not approved", so the question
// is about work that is over: it comes off the pane, the digits are characters
// again, and the exchange can settle and be filed.
func TestAnApprovalLeavesThePaneWhenItsTurnEnds(t *testing.T) {
	a, gated, ex, now := askGated(t, []session.Event{ciCall("call_7"), ciConsent(7, "call_7")})

	drive(t, a, errandClosedMsg{ex: ex})
	if ex.waiting() || len(ex.permits) != 0 {
		t.Fatalf("the approval outlived its turn (waiting=%v, %d held)", ex.waiting(), len(ex.permits))
	}
	if !ex.over() {
		t.Fatal("an exchange whose turn ended is not over")
	}
	if paneRowsSaying(a, ex, "allow once") != 0 {
		t.Fatalf("the approval is still drawn after its turn ended:\n%s", homeText(a))
	}
	*now = now.Add(time.Second)
	drive(t, a, key("1"))
	if len(gated.given) != 0 {
		t.Fatalf("a question nobody is waiting for was answered: %+v", gated.given)
	}
	if got := ex.box.String(); got != "1" {
		t.Fatalf("with no question up `1` is a character, the box holds %q", got)
	}
}

// TestAClickOnAnApprovalsAnswerTakesIt is the pointer's half: each answer owns
// its whole row in the pane, and a press on it answers exactly as the digit
// does.
func TestAClickOnAnApprovalsAnswerTakesIt(t *testing.T) {
	a, gated, ex, now := askGated(t, []session.Event{ciCall("call_7"), ciConsent(7, "call_7")})

	// The draw is what writes the answers' rows, so the pane is drawn first.
	homeText(a)
	if len(ex.askAt) != 2 || ex.askAt[1] < 0 {
		t.Fatalf("the draw did not record where the approval's answers are: %v", ex.askAt)
	}
	*now = now.Add(time.Second)
	spend(t, a, a.exchangePress(0, ex.askAt[1]))
	if len(gated.given) != 1 || gated.given[0] != (consentGiven{id: 7, allow: false, scope: session.ConsentOnce}) {
		t.Fatalf("a press on `deny` answered %+v", gated.given)
	}
}

// TestAClickOnTheCardsAnswerInThePaneTakesIt holds the card to the drawing it
// now shares with the approval ([app.exchangeAnswers]): the targets a press
// resolves to are the card's own answers, and the pane leaves none of its rows
// in the block's press map, where a click in the conversation would find them.
func TestAClickOnTheCardsAnswerInThePaneTakesIt(t *testing.T) {
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())
	a := lab.app(mine, []session.Event{
		standingProposal(7, "remind me at 6 to leave"),
		{Kind: session.EventTurnDone},
	})
	besideTheList(a)
	a.openHome()
	typeHome(a, "remind me at 6 to leave")
	drive(t, a, key("up"), key("enter"))

	ex := theExchange(a)
	if ex == nil || !ex.asking() {
		t.Fatal("the card is not asking")
	}
	answers := paneAnswers(a, ex)
	if len(a.questionBands) != 0 {
		t.Fatalf("the pane left %d rows in the block's press map", len(a.questionBands))
	}
	no := -1
	for i, option := range ex.ask.question.Options {
		if strings.TrimSpace(option.Key) == session.StandingNoKey {
			no = i
		}
	}
	if no < 0 || no >= len(answers) || !strings.Contains(answers[no], session.StandingNoKey) {
		t.Fatalf("the card's no is not drawn at its target: answers %q", answers)
	}
	readCard(t, a)
	spend(t, a, a.exchangePress(0, ex.askAt[no]))
	if len(lab.agent.answered) != 1 || lab.agent.answered[0].Approved || lab.agent.answered[0].Once {
		t.Fatalf("a press on the card's no answered %+v", lab.agent.answered)
	}
}

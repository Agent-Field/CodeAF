package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// ── the pane's card, held to the block's own laws ────────────────────────────
//
// Home's `ask here` pane routes its card's keys straight into the block's router
// ([app.questionOptionKey]), past the two guards the block keeps in front of it
// (question.go's FOUR LAWS): a key that lands before the question has been on the
// screen is dropped, and a box somebody is writing in is never answered out from
// under them. These tests are those two laws in the pane, and the third thing the
// block's door did there that it should not: file the card's answer as a receipt
// above the window's own conversation.

// askCard opens home on a frame wide enough to draw the pane beside the list,
// pins the clock, and asks one thing whose turn proposes a reminder. The card is
// up and has NOT been drawn yet. The clock is handed back so a test can move it.
func askCard(t *testing.T) (*app, *errandLab, *homeExchange, *time.Time) {
	t.Helper()
	lab := newErrandLab(t)
	mine := lab.session("-tmp-alpha", "aaaa000000000001", "pricing research", "/tmp/alpha", time.Now())
	a := lab.app(mine, []session.Event{
		automationProposal(7, "remind me at 6 to leave"),
		{Kind: session.EventTurnDone},
	})
	besideTheList(a)
	now := time.Now()
	a.clock = func() time.Time { return now }
	a.openHome()
	typeHome(a, "remind me at 6 to leave")
	drive(t, a, key("up"), key("enter"))
	ex := theExchange(a)
	if ex == nil || !ex.asking() {
		t.Fatal("the turn did not leave a card asking in the pane")
	}
	return a, lab, ex, &now
}

// TestTypingACorrectionNeverAnswersTheCard is the defect worth the most of the
// three. After `o` the pane says `type the change and press enter`, and the
// sentence a person types is a time — so it is full of digits. The card went on
// owning `1`, `3` and `0` while it waited for that sentence: `make it 10
// tomorrow` answered the card yes at its `1`, set up the reminder as first
// proposed, and dropped the rest of the words.
func TestTypingACorrectionNeverAnswersTheCard(t *testing.T) {
	a, lab, ex, now := askCard(t)
	homeText(a)
	*now = now.Add(time.Second)

	drive(t, a, key(questionCommentKey))
	if !ex.changing {
		t.Fatalf("`%s` did not arm the correction", questionCommentKey)
	}
	const words = "make it 10 tomorrow, or 3 if that is too late"
	typeHome(a, words)
	if len(lab.agent.answered) != 0 {
		t.Fatalf("typing the correction answered the card: %+v", lab.agent.answered)
	}
	if got := ex.box.String(); got != words {
		t.Fatalf("the correction lost characters to the card, the box holds %q", got)
	}
	if !ex.asking() || !ex.changing {
		t.Fatal("the card stopped waiting for its correction while it was being typed")
	}
	drive(t, a, key("enter"))
	if len(lab.agent.answered) != 1 || lab.agent.answered[0].Words() != words || lab.agent.answered[0].FirstKey() == session.AutomationSaveKey {
		t.Fatalf("enter did not send the whole correction, the agent saw %+v", lab.agent.answered)
	}
}

// TestADigitThatLandsWithTheCardIsDropped is the settle guard: a card that has
// not been drawn, or was drawn less than [questionSettle] ago, takes no key and
// no press — they were aimed at whatever was on the screen before it — and once
// it has been read, the same key answers it.
func TestADigitThatLandsWithTheCardIsDropped(t *testing.T) {
	a, lab, ex, now := askCard(t)

	// NOT YET DRAWN: the card has been on nobody's screen.
	drive(t, a, key("1"))
	if len(lab.agent.answered) != 0 {
		t.Fatalf("a key answered a card nobody had seen: %+v", lab.agent.answered)
	}
	// DRAWN THIS INSTANT: a key and a press both land with it.
	homeText(a)
	drive(t, a, key("1"))
	if at := ex.askAt; len(at) > 0 && at[0] >= 0 {
		spend(t, a, a.exchangePress(0, at[0]))
	}
	if len(lab.agent.answered) != 0 {
		t.Fatalf("a key or press on the heels of the card answered it: %+v", lab.agent.answered)
	}
	if got := ex.box.String(); got != "" {
		t.Fatalf("a dropped key was typed into the box as %q", got)
	}
	if !ex.asking() {
		t.Fatal("a dropped key took the card down")
	}
	// READ: the same key is now an answer.
	*now = now.Add(questionSettle)
	drive(t, a, key("1"))
	if len(lab.agent.answered) != 1 || lab.agent.answered[0].FirstKey() != session.AutomationSaveKey {
		t.Fatalf("a settled `1` did not answer the card, the agent saw %+v", lab.agent.answered)
	}
}

// TestTheCardsAnswerLeavesNoReceiptInTheWindow is the card's answer kept where
// it belongs. The block's door files a receipt above the WINDOW's message box,
// and the window is holding a different conversation from the errand — so the
// answer to a reminder asked from home turned up as a decision line in whatever
// conversation was opened next. The card settles in place and is its own record.
func TestTheCardsAnswerLeavesNoReceiptInTheWindow(t *testing.T) {
	a, lab, ex, _ := askCard(t)
	readCard(t, a)

	drive(t, a, key("1"))
	if len(lab.agent.answered) != 1 {
		t.Fatalf("`1` did not answer the card, the agent saw %+v", lab.agent.answered)
	}
	if len(a.questionRecords) != 0 {
		t.Fatalf("the window filed a receipt for the errand's card: %+v", a.questionRecords)
	}
	if !ex.view.settled() || !strings.Contains(homeText(a), "remind me at 6 to leave") {
		t.Fatalf("the card did not keep its answer in the pane:\n%s", homeText(a))
	}
}

// TestACorrectionNeverAnswersAnApprovalBesideTheCard is the same law with both
// questions up. One step can propose a reminder and stop on a gated call at once,
// and the box still owns every key while a correction is written: `make it 10`
// used to allow the call at its `1` and leave `make it 0` in the box.
func TestACorrectionNeverAnswersAnApprovalBesideTheCard(t *testing.T) {
	a, gated, ex, now := askGated(t, nil)
	a.errandEvent(ex, automationProposal(5, "remind me at 6 to leave"))
	homeText(a)
	*now = now.Add(time.Second)
	drive(t, a, key(questionCommentKey))
	if !ex.changing {
		t.Fatalf("`%s` did not arm the correction", questionCommentKey)
	}
	a.errandEvent(ex, ciCall("call_7"))
	a.errandEvent(ex, ciConsent(7, "call_7"))
	homeText(a)
	*now = now.Add(time.Second)

	// TYPED THROUGH THE LOOP, so an answer a key gave would reach the agent.
	for _, r := range "make it 10" {
		drive(t, a, key(string(r)))
	}
	if len(gated.given) != 0 || len(gated.answered) != 0 {
		t.Fatalf("typing the correction answered a question: approvals %+v, card %+v", gated.given, gated.answered)
	}
	if got := ex.box.String(); got != "make it 10" {
		t.Fatalf("the correction lost characters, the box holds %q", got)
	}
	// AND ONCE IT IS SENT, THE APPROVAL'S DIGITS ARE ITS OWN AGAIN.
	drive(t, a, key("enter"))
	if len(gated.answered) != 1 || gated.answered[0].Change != "make it 10" {
		t.Fatalf("enter did not send the correction, the card saw %+v", gated.answered)
	}
	drive(t, a, key("1"))
	if len(gated.given) != 1 || !gated.given[0].allow {
		t.Fatalf("after the correction `1` did not allow the call: %+v", gated.given)
	}
}

// TestTheCardSettlesAgainAfterAnApprovalHidIt is the settle guard re-armed. An
// approval hides the card's answers; when it is answered they come back under
// the hand that answered it, so a `1 1` allows the call and must not also set up
// the reminder in the same instant.
func TestTheCardSettlesAgainAfterAnApprovalHidIt(t *testing.T) {
	a, gated, ex, now := askGated(t, nil)
	a.errandEvent(ex, automationProposal(5, "remind me at 6 to leave"))
	homeText(a)
	*now = now.Add(time.Second)
	a.errandEvent(ex, ciCall("call_7"))
	a.errandEvent(ex, ciConsent(7, "call_7"))
	homeText(a)
	*now = now.Add(time.Second)

	drive(t, a, key("1"))
	if len(gated.given) != 1 || !gated.given[0].allow {
		t.Fatalf("`1` did not allow the call: %+v", gated.given)
	}
	drive(t, a, key("1"))
	if len(gated.answered) != 0 {
		t.Fatalf("the second `1` answered the card the instant its answers came back: %+v", gated.answered)
	}
	homeText(a)
	*now = now.Add(questionSettle)
	drive(t, a, key("1"))
	if len(gated.answered) != 1 || gated.answered[0].FirstKey() != session.AutomationSaveKey {
		t.Fatalf("a `1` on the card once it was read did not answer it: %+v", gated.answered)
	}
}

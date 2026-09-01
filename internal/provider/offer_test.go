package provider

import (
	"context"
	"strings"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// ── A PIN THAT GOES QUIET ───────────────────────────────────────────────────
//
// A person who names a machine has said something, and the two ways of ignoring
// them are both wrong: going somewhere else without asking answers a question
// nobody put, and waiting forever on a machine that has gone quiet is the
// reported defect. So the wait becomes one sentence, and the sentence is
// answerable — unless there is nobody there to answer it, in which case the
// answer is to get them their answer and write down what was done.
//
// The controller's arithmetic is identical for a pinned lane and an unpinned
// one; the whole difference is the ACT, which is what these tests are about.

// pinnedChoice is what a strict pin looks like once it reaches the transport:
// the machine the person named, and the frontier the offer would point at.
//
// A PIN THAT CARRIES NO FRONTIER CAN NEVER RAISE AN OFFER, because there is
// nowhere for a `y` to go — the request would report the wait and nothing else.
// That is a fact about `internal/lane`'s chooser rather than about this file,
// and it is why the pin's own row is written with both.
func pinnedChoice(model, pin string, alternatives ...string) lanes.Choice {
	choice := lanes.Choice{
		Only:     []string{pin},
		Frontier: []lanes.Scored{{ID: lanes.ID{Model: model, Lane: pin}, TTFT: 2, Rate: 2000, Price: 0.01}},
	}
	for _, lane := range alternatives {
		choice.Frontier = append(choice.Frontier, lanes.Scored{
			ID: lanes.ID{Model: model, Lane: lane}, TTFT: 5, Rate: 2000, Price: 0.01,
		})
	}
	return choice
}

// waitForOffer is the token of the question this request raised, or a failure.
func waitForOffer(t *testing.T, told *heard) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, news := range told.all() {
			if news.Phase == PhaseAsking && news.Ask != "" {
				return news.Ask
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("a pinned lane went quiet and nothing was ever asked: %v", told.story())
	return ""
}

// TestAPinnedLaneRaisesAnOfferAndYesFiresTheRescueAtOnce is the whole seam: the
// transport raises it, the phase channel carries it, and the answer comes back
// through the one door a keystroke has.
func TestAPinnedLaneRaisesAnOfferAndYesFiresTheRescueAtOnce(t *testing.T) {
	told := listen(t)
	rig := newLaneRig(t, "pin/asked",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: time.Second, Rate: 2000, Tokens: 24, Heartbeats: true,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)
	rig.patience(t, 150*time.Millisecond)

	report := &HedgeReport{}
	ctx := WithHedgeReport(context.Background(), report)
	ctx = WithLaneChoice(ctx, pinnedChoice(rig.model, "A", "B"))

	answers := make(chan *heldAnswer, 1)
	go func() {
		response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
		if err != nil {
			answers <- nil
			return
		}
		answers <- &heldAnswer{tokens: answerTokens(response)}
	}()

	ask := waitForOffer(t, told)
	// WHAT THE PERSON READS IS ONE SENTENCE ABOUT ONE MACHINE.
	asking, _ := told.find(PhaseAsking)
	if !strings.Contains(asking.Detail, "A") || !strings.EqualFold(asking.Then, "B") {
		t.Fatalf("the offer read %q · switch to %q; want the pinned machine and the lane a yes would go to",
			asking.Detail, asking.Then)
	}
	// AND NOTHING WENT OUT WHILE IT WAS OPEN. A pin is asked, never overridden.
	if got := rig.server.Requests("B"); got != 0 {
		t.Fatalf("Requests(B) = %d before anybody answered; a pin was overridden", got)
	}
	if !AnswerOffer(ask, true) {
		t.Fatal("the offer was gone before it was answered")
	}
	answer := <-answers
	if answer == nil {
		t.Fatal("the rescue never produced an answer")
	}
	if answer.tokens != 24 {
		t.Fatalf("the answer is %d tokens, want the rescuer's 24", answer.tokens)
	}
	if got := rig.server.Requests("B"); got != 1 {
		t.Fatalf("Requests(B) = %d, want the one rescue the person asked for", got)
	}
	if report.Action() != "ask" {
		t.Fatalf("the row says the wait was answered with %q, want the question", report.Action())
	}
	// AND THE SAME TOKEN IS ANSWERED ONCE. A second `y` lands on nothing.
	if AnswerOffer(ask, true) {
		t.Fatal("one offer was answered twice")
	}
}

// heldAnswer is the one figure the goroutine above carries back. It is a
// struct rather than a bare int so that a nil is unmistakably "no answer".
type heldAnswer struct{ tokens int }

// TestAVisibleTokenWithdrawsTheOffer is the pin coming good.
//
// The question was about a machine that had said nothing; a word on the screen
// is that machine answering, so the question is moot and a surface that left it
// up would be asking a person to decide something that had already decided
// itself. A THINKING DELTA DOES NOT WITHDRAW IT, because nothing has arrived
// that anybody can read.
func TestAVisibleTokenWithdrawsTheOffer(t *testing.T) {
	told := listen(t)
	rig := newLaneRig(t, "pin/withdrawn",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 250 * time.Millisecond, Rate: 2000, Tokens: 24, Heartbeats: true,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)
	rig.patience(t, 100*time.Millisecond)

	ctx := WithLaneChoice(context.Background(), pinnedChoice(rig.model, "A", "B"))
	done := make(chan int, 1)
	go func() {
		response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
		if err != nil {
			done <- 0
			return
		}
		done <- answerTokens(response)
	}()

	ask := waitForOffer(t, told)
	if tokens := <-done; tokens != 24 {
		t.Fatalf("the answer is %d tokens, want the pinned lane's own 24", tokens)
	}
	// THE QUESTION IS GONE, and a keystroke that arrives late lands on nothing
	// rather than on a rescue nobody wants any more.
	if AnswerOffer(ask, true) {
		t.Fatal("a `y` pressed after the pin came good still fired a rescue")
	}
	if got := rig.server.Requests("B"); got != 0 {
		t.Fatalf("Requests(B) = %d, want none: the machine the person named answered", got)
	}
	// AND THE WITHDRAWAL WAS SAID OUT LOUD. A surface that was never told stops
	// drawing the question only when the phase window runs out.
	withdrew := false
	for _, news := range told.all() {
		if news.Phase == PhaseAsking && news.Ask != "" {
			withdrew = false
			continue
		}
		if news.Ask == "" {
			withdrew = true
		}
	}
	if !withdrew {
		t.Fatalf("the offer was never taken back down: %v", told.story())
	}
}

// TestWithNobodyToAskThePinBorrowsAndSaysSo is the headless half, and it is the
// case the invariant matters most in: nobody is watching to notice a run that
// has stopped.
//
// "No reader" is [OnPhase] having no registered reader — the same seam the
// surface uses, so the two ideas of headless cannot drift apart.
func TestWithNobodyToAskThePinBorrowsAndSaysSo(t *testing.T) {
	read := loggingTo(t)
	// NOBODY IS LISTENING. Whatever ran before this test may have left a reader
	// behind, so the absence is stated rather than assumed.
	previous := OnPhase(nil)
	t.Cleanup(func() { OnPhase(previous) })

	rig := newLaneRig(t, "pin/headless",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: time.Second, Rate: 2000, Tokens: 24, Heartbeats: true,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)
	rig.patience(t, 150*time.Millisecond)

	report := &HedgeReport{}
	ctx := WithHedgeReport(context.Background(), report)
	ctx = WithLaneChoice(ctx, pinnedChoice(rig.model, "A", "B"))

	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if tokens := answerTokens(response); tokens != 24 {
		t.Fatalf("the answer is %d tokens, want the borrowed lane's 24", tokens)
	}
	if got := rig.server.Requests("B"); got != 1 {
		t.Fatalf("Requests(B) = %d, want the one borrowed rescue", got)
	}
	if report.Action() != "borrow" {
		t.Fatalf("the row says %q, want the borrow a headless run makes when there is nobody to ask", report.Action())
	}
	// AND IT SAID SO. An instruction whose author cannot be reached is honoured
	// by getting them their answer and telling them what was done.
	waitFor(t, func() bool {
		for _, row := range ended(read()) {
			if strings.Contains(row.Note, "borrowing") && strings.Contains(row.Note, "A") {
				return true
			}
		}
		return false
	})
}

// TestAnOfferThatIsRefusedLeavesTheAnswerWhereItIs is the other answer. `n` is
// a person saying the pin means what it says, and the wait goes on being the
// pinned machine's.
func TestAnOfferThatIsRefusedLeavesTheAnswerWhereItIs(t *testing.T) {
	told := listen(t)
	rig := newLaneRig(t, "pin/refused",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 300 * time.Millisecond, Rate: 2000, Tokens: 24, Heartbeats: true,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)
	rig.patience(t, 100*time.Millisecond)

	ctx := WithLaneChoice(context.Background(), pinnedChoice(rig.model, "A", "B"))
	done := make(chan int, 1)
	go func() {
		response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
		if err != nil {
			done <- 0
			return
		}
		done <- answerTokens(response)
	}()

	if !AnswerOffer(waitForOffer(t, told), false) {
		t.Fatal("the offer was gone before it was refused")
	}
	if tokens := <-done; tokens != 24 {
		t.Fatalf("the answer is %d tokens, want the pinned lane's own 24", tokens)
	}
	if got := rig.server.Requests("B"); got != 0 {
		t.Fatalf("Requests(B) = %d, want none: the person said no", got)
	}
}

// TestAnOfferIsRaisedOnceAndNotAgain: a second question about one answer is
// nagging, and the controller may keep on saying the wait is real.
func TestAnOfferIsRaisedOnceAndNotAgain(t *testing.T) {
	told := listen(t)
	rig := newLaneRig(t, "pin/once",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 700 * time.Millisecond, Rate: 2000, Tokens: 24, Heartbeats: true,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)
	rig.patience(t, 100*time.Millisecond)

	ctx := WithLaneChoice(context.Background(), pinnedChoice(rig.model, "A", "B"))
	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	asked := map[string]bool{}
	for _, news := range told.all() {
		if news.Phase == PhaseAsking && news.Ask != "" {
			asked[news.Ask] = true
		}
	}
	if len(asked) != 1 {
		t.Fatalf("%d questions were raised about one answer, want exactly one", len(asked))
	}
}

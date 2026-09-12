package provider

import (
	"context"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// TestAStalledLaneArmsARescueWithoutATerminalError is F33, staged.
//
// A first token that has not arrived is not an error. Walk only runs after
// one, so a stall that kept the stream open sat at "all lanes slow" for
// 129s with arms:None. The purse here allows one rescue; the test is about
// acting on the stall rather than bypassing the budget. B answers inside
// the ceiling, so TTFT is the rescue's and not A's stall.
func TestAStalledLaneArmsARescueWithoutATerminalError(t *testing.T) {
	rig := newLaneRig(t, "stall/no-error",
		// Thirty virtual seconds to a first word, with the router's own
		// comment lines on the way — so the path is alive and there is no
		// terminal error for the walk to see.
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 300 * time.Millisecond, Rate: 2000, Tokens: 24, Heartbeats: true,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	// THE PURSE ALLOWS ONE RESCUE, so keepalives cannot turn a live but
	// stalled stream into an unbounded wait.
	rig.patience(t, 150*time.Millisecond)

	report := &HedgeReport{}
	ctx := WithHedgeReport(talking(), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 0))

	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Hedged() {
		t.Fatal("a stall with no terminal error was not rescued")
	}
	// B answering is the bound. A wall-clock figure against A's 300 ms first
	// token only ever held on a quiet machine; the winner names the same
	// fact at any speed.
	if winner, _ := report.Lanes(); winner != "B" {
		t.Fatalf("winner = %q, want the answer from the lane the stall armed", winner)
	}
	if tokens := answerTokens(response); tokens != 24 {
		t.Fatalf("the answer is %d tokens, want B's 24", tokens)
	}
	if got := rig.server.Requests("B"); got != 1 {
		t.Fatalf("Requests(B) = %d, want one rescue arm", got)
	}
	waitFor(t, func() bool { return rig.server.Cancels("A") == 1 })
}

// ── A WIRE THAT IS WRITING AND STILL LOSING THE PERSON'S AFTERNOON ──────────

// slowWireRig stages the 2026-09-11 call on the wire: a machine the choice
// expected sixty tokens a second from, delivering seven, beside one that is
// believed fast and is.
func slowWireRig(t *testing.T, name string) *laneRig {
	t.Helper()
	rig := newLaneRig(t, name,
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 40 * time.Millisecond, Rate: 7, Tokens: 40, Heartbeats: true,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	// What the choice expected of the machine it named, which is the bar the
	// stream is held to.
	rig.believes("A", 40, 60)
	rig.believes("B", 5, 2000)
	rig.patience(t, 150*time.Millisecond)
	return rig
}

// unattended is the role of a task node nobody is sitting in front of: it still
// has a λ ([lane.UnattendedValue]), because a task still ends when its slowest
// call ends.
func unattended() context.Context {
	return WithRole(context.Background(), lanes.RoleLeafUnattended)
}

// TestAWireBelowItsPromisedPaceIsRescuedOnAnUnattendedTask is the owner's own
// call, staged. The machine writes — there is no silence and no error — and it
// writes an eighth as fast as the machine the question was sent to. The rescue
// costs about what the call was going to cost anyway, and the call's own budget
// affords it; under the deleted window it did not, and a person watched 604
// tokens arrive over 86 seconds.
func TestAWireBelowItsPromisedPaceIsRescuedOnAnUnattendedTask(t *testing.T) {
	rig := slowWireRig(t, "slowwire/rescued")

	report := &HedgeReport{}
	ctx := WithHedgeReport(unattended(), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 0))

	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Hedged() {
		t.Fatal("a wire writing at an eighth of the pace it was asked for was never rescued")
	}
	if winner, _ := report.Lanes(); winner != "B" {
		t.Fatalf("winner = %q, want the answer from the machine that kept the pace", winner)
	}
	if tokens := answerTokens(response); tokens != 24 {
		t.Fatalf("the answer is %d tokens, want B's 24", tokens)
	}
	if got := rig.server.Requests("B"); got != 1 {
		t.Fatalf("Requests(B) = %d, want the one rescue the pace earned", got)
	}
}

// TestAWireBelowItsPaceWithNothingToSpendSaysSoAndIsSpent is the other half of
// the same call: nothing can be started, and a report is still a move.
//
// THREE THINGS ARE ASSERTED BECAUSE THREE THINGS USED TO BE MISSING. The row
// names the rail that really refused — the call's own budget, not some other
// request's allowance. The person is told the answer is arriving too slowly to
// read, which neither `writing` nor `all lanes slow` could honestly say. And the
// machine is written down as spent for this question, so no arm and no attempt
// of it goes back to the machine that earned the report.
func TestAWireBelowItsPaceWithNothingToSpendSaysSoAndIsSpent(t *testing.T) {
	read := loggingTo(t)
	rig := slowWireRig(t, "slowwire/nothing-to-spend")
	noRescues(t)
	told := listen(t)

	ctx := WithLaneChoice(unattended(), choiceFor(rig.model, 0))
	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if got := rig.server.Requests("B"); got != 0 {
		t.Fatalf("Requests(B) = %d, want no rescue from a call that may spend nothing", got)
	}
	rows := ended(read())
	if len(rows) != 1 {
		t.Fatalf("ended rows = %d, want the one call's row", len(rows))
	}
	if got := rows[0].Refused; got != planCannotPay {
		t.Fatalf("refused = %q, want %q — the rail that really refused", got, planCannotPay)
	}
	// AND IT IS THE LAST WORD THAT IS ASSERTED, not one somewhere in the list.
	// A phase said and then overwritten is a phase nobody read: this assertion
	// was `told.find(PhaseBelowPace)` and it passed while [hedgeRace.exhaust]
	// said `answering slowly` and the next statement said `all lanes slow` over
	// the top of it. See [heard.settled].
	if got := told.settled(); got != PhaseBelowPace {
		for _, news := range told.all() {
			t.Logf("PHASE %q detail=%q", news.Phase, news.Detail)
		}
		t.Fatalf("a person watching an answer crawl was left reading %q, want %q", got, PhaseBelowPace)
	}
}

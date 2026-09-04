package provider

import (
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
	SetHedgeBudget(lanes.NewBudget(1, 0))
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

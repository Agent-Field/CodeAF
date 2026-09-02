package provider

import (
	"math"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// ── THE STALL NOBODY COULD BE RESCUED FROM ──────────────────────────────────
//
// THE DEFECT THESE PIN, as it was reported: "stuck in a state where in the
// middle of thinking it just says still working, but at the bottom I see
// glm 5.3 friendli and t/s". A reasoning model writes its thinking on the same
// stream as its answer, and the watch counted those deltas as tokens. Past
// sixty-four of them — about two sentences of thought — every hedge was
// refused, because the commitment rule asks whether finishing here is cheaper
// than starting again and answers "stay" whenever nobody said how long the
// answer would be (which nothing in this build ever did). So the stall was
// left to the transport's own guard: forty-five seconds, or a hundred and fifty
// under keepalives, with nothing on the screen but three dots.
//
// The rule now counts the tokens A PERSON CAN READ. A run of thought is money
// already spent and nothing anybody would watch disappear, so it buys no
// commitment at all and a stall inside it is hedged like any other.

// TestAStallInsideTheThinkingIsRescued is the owner's report, staged: a
// hundred deltas of thought, then silence, with a healthy lane beside it.
func TestAStallInsideTheThinkingIsRescued(t *testing.T) {
	rig := newLaneRig(t, "reason/stall",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 1000,
			// A hundred thoughts and then nothing, before a word of answer.
			Reasoning: 100, Tokens: 40,
			StallAfter: 100, StallFor: 600 * time.Millisecond,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 2, 250)

	report := &HedgeReport{}
	ctx := WithHedgeReport(talking(), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 12*time.Millisecond))

	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Hedged() {
		t.Fatalf("a lane that went quiet sixty virtual seconds into its thinking was never rescued")
	}
	if reason := report.Reason(); reason != "drift" && reason != "gap" {
		t.Fatalf("reason = %q, want the drift test to have said so", reason)
	}
	winner, _ := report.Lanes()
	if winner != "B" {
		t.Fatalf("winner = %q, want the answer from B", winner)
	}
	if got := rig.server.Requests("B"); got != 1 {
		t.Fatalf("Requests(B) = %d, want exactly one rescue", got)
	}
	waitFor(t, func() bool { return rig.server.Cancels("A") == 1 })
	if tokens := answerTokens(response); tokens != 24 {
		t.Fatalf("the answer is %d tokens, want B's whole answer of 24", tokens)
	}
}

// TestTheThinkingIsNotSaidOutLoudSoItBuysNoCommitment is the same stall with
// the count that used to refuse it stated in the assertion: a hundred deltas
// had arrived and NONE of them were on the screen.
func TestTheThinkingIsNotSaidOutLoudSoItBuysNoCommitment(t *testing.T) {
	watch := lanes.NewWatch(
		lanes.Choice{Order: []string{"A", "B"}},
		believedAt(2, 250),
		time.Unix(0, 0),
	)
	now := time.Unix(0, 0)
	// A hundred deltas of thought, none of them visible, at the believed rate.
	for token := 1; token <= 100; token++ {
		now = now.Add(4 * time.Millisecond)
		if verdict := watch.Token(token, 0, now); verdict.Hedge {
			t.Fatalf("hedged at thought %d on an ordinary gap", token)
		}
	}
	// And then the stall. Sixty seconds of silence inside a run of thought is
	// judged by the liveness clock — the gap between two deltas, at the lane's
	// own believed rate — and no absolute anywhere.
	if verdict := watch.Silence(now.Add(60 * time.Second)); !verdict.Hedge {
		t.Fatalf("a sixty-second silence inside the thinking was not acted on")
	}
}

// TestAnAnswerOnTheScreenStillCommits is the other half of the same rule, and it
// is the one the change must not have broken.
//
// SIXTY-FOUR TOKENS ARE NOT A CONSTANT ANY MORE and the law they stood for is.
// What used to be a counter is the rewrite term of the same inequality: leaving
// costs a fresh first token plus writing again everything a person has already
// read, so an answer on the screen commits itself, later the longer it is. A
// second arm may still go out — the purse is what bounds arms, not a boolean —
// but THE VOICE DOES NOT MOVE and the words do not disappear: whichever arm
// wrote the first word a person could read is the arm they go on hearing, and
// the rest are cancelled the moment staying is cheaper than leaving.
func TestAnAnswerOnTheScreenStillCommits(t *testing.T) {
	rig := newLaneRig(t, "reason/committed",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 1000,
			Reasoning: 10, Tokens: 200,
			// Ten thoughts and eighty words in, and then it goes quiet.
			StallAfter: 90, StallFor: 400 * time.Millisecond,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 2, 250)

	// AND THE ALTERNATIVE WRITES AT A REAL MACHINE'S SPEED. The rig's other
	// lanes are scripted at two thousand tokens a second, which is the wire and
	// not a model: at that rate re-writing eighty words costs forty milliseconds
	// and nothing is ever worth staying for. Sixty a second is what the measured
	// sheet publishes, and it is what makes the rewrite term mean anything.
	choice := choiceFor(rig.model, 12*time.Millisecond)
	for index := range choice.Frontier {
		choice.Frontier[index].Rate = 60
	}

	report := &HedgeReport{}
	ctx := WithHedgeReport(talking(), report)
	ctx = WithLaneChoice(ctx, choice)

	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if report.Hedged() {
		t.Fatalf("eighty words on the screen bought no commitment; reason %q", report.Reason())
	}
	winner, _ := report.Lanes()
	if winner != "" && winner != "A" {
		t.Fatalf("the voice moved to %q; eighty words a person was reading were taken away", winner)
	}
	if tokens := answerTokens(response); tokens != 200 {
		t.Fatalf("the answer is %d tokens, want all 200 of A's", tokens)
	}
}

// TestAThinkingStallStillObeysTheBudget: the rule that unblocked the rescue
// did not unblock the spending. A budget with no bucket refuses it exactly as
// it refuses every other hedge.
func TestAThinkingStallStillObeysTheBudget(t *testing.T) {
	rig := newLaneRig(t, "reason/budget",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 1000,
			Reasoning: 100, Tokens: 24,
			StallAfter: 100, StallFor: 300 * time.Millisecond,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 2, 250)
	SetHedgeBudget(lanes.NewBudget(0, 0))

	report := &HedgeReport{}
	ctx := WithHedgeReport(talking(), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 12*time.Millisecond))

	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if report.Hedged() {
		t.Fatalf("a hedge went out on an exhausted budget")
	}
	if got := rig.server.Requests("B"); got != 0 {
		t.Fatalf("Requests(B) = %d, want none", got)
	}
	if tokens := answerTokens(response); tokens != 24 {
		t.Fatalf("the answer is %d tokens, want A's own 24 arriving late", tokens)
	}
}

// believedAt is one lane's posterior, stated: a median first token in
// milliseconds and a median rate in tokens a second.
func believedAt(ttft, rate float64) lanes.Belief {
	return lanes.Belief{
		TTFT: lanes.Posterior{X: math.Log(ttft), P: 0.04},
		Rate: lanes.Posterior{X: math.Log(rate), P: 0.04},
	}
}

// TestATaskLeafIsRescuedFromAThinkingStallToo: the rule is about the STREAM and
// not about who is reading it. A node working unattended stalls in its thinking
// exactly as a conversation does, it is measured and rescued the same way, and
// the only thing its role changes is that nothing about it is drawn on somebody
// else's status line (internal/lane's roles.go).
func TestATaskLeafIsRescuedFromAThinkingStallToo(t *testing.T) {
	rig := newLaneRig(t, "reason/leaf",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 1000,
			Reasoning: 100, Tokens: 40,
			StallAfter: 100, StallFor: 600 * time.Millisecond,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 2, 250)
	// A NODE NOBODY IS WATCHING IS WORTH NO MONEY AND IS STILL BOUNDED. λ is
	// zero for this role by construction, so nothing under the ceiling can ever
	// cross — and the ceiling is what acts, which is the whole of the claim. It
	// is stated here rather than waited out because the role's own is three
	// times a person's patience.
	rig.patience(t, 200*time.Millisecond)

	report := &HedgeReport{}
	ctx := WithHedgeReport(talking(), report)
	ctx = WithRole(ctx, lanes.RoleLeafUnattended)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 12*time.Millisecond))

	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Hedged() {
		t.Fatalf("a node's stalled thinking was left to the transport's last resort")
	}
	if tokens := answerTokens(response); tokens != 24 {
		t.Fatalf("the answer is %d tokens, want the rescuer's 24", tokens)
	}
	if lanes.RoleLeafUnattended.Visible() {
		t.Fatalf("an unattended node claimed somebody was reading it")
	}
}

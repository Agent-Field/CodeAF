package provider

import (
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// TestASlowToStartModelKeepsItsMoneyAndAnswersAlone is the policy the measured
// chat asked for: a model whose own recent answers took past [hedgeSlowTTFT]
// to say their first word is never hedged, because the second arm would be
// the same model's own queue paid a second time (the aforge issue #1007 live
// cell, third measurement: 68 of 71 rows hedged, the bill ≈1.8× the unhedged
// one, on deepseek-v4.1-flash's 4–15s first tokens).
//
// THE LEDGER IS PRIMED WITH THE MEASURED FACT, served by nobody in particular:
// observe records it as the model's latest sighting with no strike, which is
// what "the model is the slow thing" honestly looks like. The scenario below
// is otherwise the ordinary late-first-token hedge — A believed at twenty
// milliseconds and really at three hundred, so the hazard deadline passes with
// nobody speaking — so that what is being proved is only the gate, not a
// second scenario.
func TestASlowToStartModelKeepsItsMoneyAndAnswersAlone(t *testing.T) {
	read := loggingTo(t)
	rig := newLaneRig(t, "ttft/model-slow",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 300 * time.Millisecond, Rate: 2000, Tokens: 24}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)
	rig.client.velocity = newVelocityLedger()
	rig.client.velocity.observe(rig.model, "", 6*time.Second, 200, 20*time.Second, 0)

	ctx := WithLaneChoice(talking(), choiceFor(rig.model, 12*time.Millisecond))
	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}

	if got := rig.server.Requests("B"); got != 0 {
		t.Fatalf("Requests(B) = %d, want the model's own slow queue never bought twice", got)
	}
	if tokens := answerTokens(response); tokens != 24 {
		t.Fatalf("the answer is %d tokens, want A's whole answer of 24 rather than a rescue", tokens)
	}
	// AND THE ROW SAYS WHAT KEPT THE MONEY. Refused names the first reason a
	// rescue this question needed did not reach the wire — here the model's
	// own measured first-token wait, not the purse and not the room.
	rows := ended(read())
	if len(rows) != 1 {
		t.Fatalf("ended rows = %d, want the one arm the question stayed with", len(rows))
	}
	if got := rows[0].Refused; got != modelStartsSlow {
		t.Fatalf("refused = %q, want %q — the gate that kept the primary's money (row: %+v)", got, modelStartsSlow, rows[0])
	}
}

// TestAFastToStartModelIsHedgedAsEver is the gate's other half: a model whose
// measured first token sits under [hedgeSlowTTFT] is rescued exactly as it
// always was — the policy narrows the slowness bet to slow models and never
// touches fast ones.
func TestAFastToStartModelIsHedgedAsEver(t *testing.T) {
	rig := newLaneRig(t, "ttft/model-fast",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 300 * time.Millisecond, Rate: 2000, Tokens: 24}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)
	rig.client.velocity = newVelocityLedger()
	// A sighting under the gate: the most recent answer took half a second to
	// its first word, so a second arm can still genuinely beat a stall.
	rig.client.velocity.observe(rig.model, "A", 500*time.Millisecond, 200, 2*time.Second, 0)

	report := &HedgeReport{}
	ctx := WithHedgeReport(talking(), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 12*time.Millisecond))

	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if !report.Hedged() {
		t.Fatal("a model measured under the gate was not rescued; the policy must only narrow slow models")
	}
	waitFor(t, func() bool { return rig.server.Cancels("A") == 1 })
	if got := rig.server.Requests("B"); got != 1 {
		t.Fatalf("Requests(B) = %d, want the one rescue fast models still buy", got)
	}
}

// TestAnUnmeasuredModelFailsOpen is the policy's fail-open half: no sighting
// at all means the old behaviour — a late first token against a believed
// fast lane is rescued — because refusing a hedge on a number nobody
// measured would be declining rescues on a guess.
func TestAnUnmeasuredModelFailsOpen(t *testing.T) {
	rig := newLaneRig(t, "ttft/model-unknown",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 300 * time.Millisecond, Rate: 2000, Tokens: 24}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)
	rig.client.velocity = newVelocityLedger()

	report := &HedgeReport{}
	ctx := WithHedgeReport(talking(), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 12*time.Millisecond))

	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if !report.Hedged() {
		t.Fatal("a model with no sighting at all was not rescued; the gate is meant to fail open")
	}
	waitFor(t, func() bool { return rig.server.Cancels("A") == 1 })
	if got := rig.server.Requests("B"); got != 1 {
		t.Fatalf("Requests(B) = %d, want the one rescue an unmeasured model still buys", got)
	}
}

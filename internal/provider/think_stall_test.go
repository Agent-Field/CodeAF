package provider

import (
	"math"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/control"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// ── THE DURATION CLOCK, ON THE WIRE, ONCE THE MODEL HAS BEEN WATCHED ────────
//
// §B's second test asks whether a wait is past the (1 − p) quantile of the
// survival its clock reads, and for the duration clock that survival is
// [lanes.Thinks]. Nobody publishes how much one run of thought varies, so the
// spread stood on [lanes.SpreadFloor] for ever and the quantile sat at fifteen
// times the believed median — past every role's ceiling, at any amount of
// evidence. The gate could not close and the ceiling was the only thing that
// ever ended a hung thought (#316).
//
// IT IS OBSERVED WHERE IT IS NOT PUBLISHED, and these two are the wire's own
// account of what that buys and of what it must not cost. The first is the
// defect: a model this process has watched deliberate forty times goes quiet
// mid-thought, and the duration clock — not the ceiling — is what ends it. The
// second is the price nobody may pay for the first: the SAME belief, a thought
// that runs half again past its own quantile and is still writing, and nothing
// acts on it at all.
//
// THE LEDGER HERE IS THE REAL ONE, which is the whole point. `lanes.Thinks`
// reads the four-level hierarchy through [lanes.Hierarchy], the scripted ledger
// the rest of this file's scenarios use is not one, and a rig that scripted the
// think belief would be proving the arithmetic against a fixture rather than
// against the door `lanes.NoteThought` really writes through.

const (
	// thinkTypical is how long this model is watched deliberating, every time.
	// It is the rig's own milliseconds read as themselves: the duration clock
	// waits in seconds and a quarter of one is a whole scenario here.
	thinkTypical = 250 * time.Millisecond
	// thinkWatched is how many of those the process has seen. It is past the
	// point `bench/lanelab/gosim`'s TestWhenTheThinkGateCloses measures the gate
	// closing at, which is what makes this a test of the wire rather than a
	// second measurement of the arithmetic.
	thinkWatched = 40
	// thinkCeiling is the role's ceiling for these two scenarios, stated so the
	// wire does not really wait the ten seconds a person is given. Everything
	// below has to happen inside it or be refused inside it.
	thinkCeiling = 2 * time.Second
)

// TestAStalledThoughtIsActedOnBeforeTheCeiling is the defect, staged: a run of
// thought that stops, on a model whose thinking this process knows.
func TestAStalledThoughtIsActedOnBeforeTheCeiling(t *testing.T) {
	rig := newLaneRig(t, "think/stalled",
		// Five deltas of thought and then nothing, for longer than the ceiling:
		// no visible word ever arrives, so what is on the screen is what a
		// person reported — three dots.
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 1000,
			Reasoning: 20, Tokens: 24,
			StallAfter: 5, StallFor: 3 * time.Second,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rung := watchThinking(t, rig, thinkWatched)
	rig.patience(t, thinkCeiling)

	think := lanes.Thinks(rig.model, rung, time.Now())
	t.Logf("believed thought: median %.3fs, sigma %.3f, quantile %.3fs; the ceiling is %s",
		think.Quantile(0), think.Sigma, think.Quantile(deviateAt(2)), thinkCeiling)

	report := &HedgeReport{}
	ctx := WithHedgeReport(talking(), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 12*time.Millisecond))

	began := time.Now()
	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	took := time.Since(began)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Hedged() {
		t.Fatalf("a thought that stopped %s into a %s belief was never acted on at all",
			thinkTypical, thinkTypical)
	}
	// THE CLOCK IS THE CLAIM AND NOT MERELY THE TIMING. An act at the ceiling
	// is what this build already did; what #316 is about is the duration clock
	// deciding first, on a belief it measured itself.
	if reason := report.Reason(); reason != "long think" {
		t.Fatalf("reason = %q after %s, want the duration clock — %q is the bound firing, which is "+
			"what the think survival's floor left as the only thing that ever ended a hung thought",
			reason, took.Round(time.Millisecond), control.CeilingReason)
	}
	if took >= thinkCeiling {
		t.Fatalf("the rescue landed after %s, at or past the %s ceiling",
			took.Round(time.Millisecond), thinkCeiling)
	}
	if winner, _ := report.Lanes(); winner != "B" {
		t.Fatalf("winner = %q, want the answer from B", winner)
	}
	if tokens := answerTokens(response); tokens != 24 {
		t.Fatalf("the answer is %d tokens, want B's whole answer of 24", tokens)
	}
	t.Logf("the duration clock ended a stalled thought after %s, inside the %s ceiling",
		took.Round(time.Millisecond), thinkCeiling)
}

// TestALongLiveThoughtIsLeftAlone is the price nobody may pay for the test
// above.
//
// A GATE THAT CLOSES IS HALF OF §B AND THE PAYOFF TEST IS THE OTHER HALF. This
// thought runs well past its own abnormality quantile — the gate is open on it
// — and it is still WRITING, so leaving it would cost a whole fresh run of
// thought to buy back nothing. Nothing acts, the voice never moves, and the
// model's own answer arrives.
func TestALongLiveThoughtIsLeftAlone(t *testing.T) {
	rig := newLaneRig(t, "think/live",
		// Forty deltas of thought at a hundred a second — four tenths of a
		// second of deliberating, half again past what this model is believed to
		// need — and then its own answer, with no silence anywhere in it.
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 100,
			Reasoning: 40, Tokens: 24,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rung := watchThinking(t, rig, thinkWatched)
	rig.patience(t, thinkCeiling)

	think := lanes.Thinks(rig.model, rung, time.Now())
	if quantile := think.Quantile(deviateAt(2)); quantile > 400*time.Millisecond.Seconds() {
		t.Fatalf("this model's thinking is believed out to %.3fs, so a four-tenths thought is not "+
			"past its own quantile and this scenario proves nothing about the payoff test", quantile)
	}

	report := &HedgeReport{}
	ctx := WithHedgeReport(talking(), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 12*time.Millisecond))

	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if report.Hedged() {
		t.Fatalf("a thought that was still writing was rescued from itself; reason %q", report.Reason())
	}
	if winner, _ := report.Lanes(); winner != "" && winner != "A" {
		t.Fatalf("the voice moved to %q while the model it left was still thinking", winner)
	}
	if got := rig.server.Requests("B"); got != 0 {
		t.Fatalf("Requests(B) = %d, want none: nothing was wrong with A", got)
	}
	if tokens := answerTokens(response); tokens != 24 {
		t.Fatalf("the answer is %d tokens, want A's own 24", tokens)
	}
}

// watchThinking puts the REAL ledger behind the rig and shows it n runs of
// thought of [thinkTypical], through the door the transport writes through. It
// answers the effort rung they were filed under, which is the rung the plan
// will read them back at.
//
// THE ROWS COME FIRST AND THEY ARE WHAT KEEPS THE OTHER TWO CLOCKS QUIET. A
// hierarchy nobody has published to waits every lane against its own priors —
// a second to the first token, a gap of forty milliseconds — and the drift
// clock would then end these scenarios before the duration clock was asked
// anything. The rows say what the rig's lanes really do, which is what a sheet
// says about a real one.
func watchThinking(t *testing.T, rig *laneRig, n int) string {
	t.Helper()
	registry := lanes.Default()
	registry.SetLedger(nil)
	ledger := registry.Ledger()
	now := time.Now()
	for _, name := range []string{"A", "B"} {
		ledger.Prime(lanes.Row{
			ID: lanes.ID{Model: rig.model, Lane: name},
			At: now.Add(-time.Hour),
			// A first token this rig really delivers in milliseconds, and a
			// SLOW published rate: the gap clock's own quantile has to sit
			// outside the ceiling or it, and not the duration clock, is what
			// every silence below would be judged by.
			TTFTp50: 30, TTFTp75: 45, TTFTp90: 60, TTFTp99: 120,
			Ratep50: 1.5, Ratep75: 2, Ratep90: 2.4, Ratep99: 3,
		}, lanes.SheetWeight)
	}
	// A MINUTE APART, ENDING AT THE MOMENT, because a think chain ages like
	// every other belief and a history stamped all at once is one no session
	// ever has.
	rung := rig.client.recordedEffort(rig.model, callKnobs{})
	for i := range n {
		lanes.NoteThought(rig.model, rung, thinkTypical, now.Add(-time.Duration(n-i)*time.Minute))
	}
	return rung
}

// deviateAt is the z §B sizes the gate with for a request offering k alarm
// opportunities, which is what [control.Plan] derives from the request's own
// shape. It is spelled here so these scenarios can say what they are asserting
// against rather than quoting a number.
func deviateAt(k int) float64 {
	tail := 0.02 / float64(k)
	low, high := 0.0, 40.0
	for range 60 {
		middle := low + (high-low)/2
		if 1-normalBelow(middle) > tail {
			low = middle
		} else {
			high = middle
		}
	}
	return high
}

// normalBelow is Φ, the standard normal distribution function, which
// `internal/lane/control` keeps to itself.
func normalBelow(x float64) float64 { return 0.5 * (1 + math.Erf(x/math.Sqrt2)) }

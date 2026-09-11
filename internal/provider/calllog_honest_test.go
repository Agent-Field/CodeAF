package provider

import (
	"context"
	"strings"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE ROW IS HONEST ABOUT WHO ANSWERED, WHAT IT COST AND WHAT IT ASKED FOR ─
//
// The census over ten days of this build's own log (docs/design/recovery/
// census-20260910.md) found three things wrong with the record itself, and each
// of them made a later wave's number unmeasurable:
//
//   - `served` was empty on every row that never opened a stream, and every
//     per-lane belief was therefore keyed on `lane` — the machine ASKED FOR,
//     which on 3,728 of 10,107 finishes was not the machine that answered;
//   - a failed attempt recorded no cost and no tokens, so $201 of spend had
//     $0.00 attributed to anything that went wrong;
//   - `retry_after` was never present on any of 1,106 paced refusals, so no
//     repeated send could be checked against what the provider had asked for.
//
// These are staged against the real router stub rather than asserted on a
// hand-built record, because each of them is about a fact that only exists
// while a request is on a wire.

// TestTheRowNamesTheMachineThatAnsweredAndNotTheOneAskedFor stages the
// commonest disagreement in the live log: the preference ranks a full pool
// first, the router quietly serves the next machine, and the row has to say
// both.
func TestTheRowNamesTheMachineThatAnsweredAndNotTheOneAskedFor(t *testing.T) {
	read := loggingTo(t)
	rig := newLaneRig(t, "served/other",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			Paced: true, TTFT: 2 * time.Millisecond, Rate: 2000,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 12,
			PriceIn: 1e-6, PriceOut: 2e-6,
		}},
	)

	ctx := WithLaneChoice(talking(), choiceFor(rig.model, 0))
	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	rows := ended(read())
	if len(rows) != 1 {
		t.Fatalf("one answer should leave one end row, got %d", len(rows))
	}
	row := rows[0]
	if row.Lane != "A" {
		t.Errorf("the row says the preference asked for %q, want the head of the order", row.Lane)
	}
	if row.Served != "B" {
		t.Errorf("served = %q, want the machine that really answered", row.Served)
	}
	if row.Served == row.Lane {
		t.Error("the row filled `served` in from `lane`, which is the attribution the whole ledger was keyed on wrongly")
	}
	if row.TTFTms <= 0 {
		t.Error("the stream produced a first token and the row does not say when")
	}
}

// TestAPacedRefusalNamesItsPoolAndItsComebackTime is the other half: a machine
// this request DEMANDED refuses before any stream opens, so nothing on the wire
// names a provider — and the demand is the name, because a router asked for a
// single-machine `only` either answers from it or refuses.
func TestAPacedRefusalNamesItsPoolAndItsComebackTime(t *testing.T) {
	read := loggingTo(t)
	rig := newLaneRig(t, "paced/named",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			Paced: true, PacedFor: 30 * time.Second, TTFT: 2 * time.Millisecond, Rate: 2000,
		}},
	)

	// A demand and not a ranking: the stub falls back past a full pool for a
	// request that permits it, which is exactly why the live log's paced rows
	// are the ones with nowhere else to go.
	demand := lanes.Choice{Only: []string{"A"}}
	// AND THE CALLER'S OWN PATIENCE IS SHORT, because patience is not what this
	// test is about. Left to itself the attempt loop honours the thirty seconds
	// the pool named and spends its whole pacing budget doing so, which is two
	// minutes of a suite for an assertion about a field on the first row. How
	// long a paced call should go on trying is the recovery design's third and
	// fifth waves; what it has to WRITE DOWN while it does is this one.
	ctx, giveUp := context.WithTimeout(WithLaneChoice(talking(), demand), 2*time.Second)
	defer giveUp()
	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err == nil {
		t.Fatal("a full pool with nowhere to fall back to should have refused")
	}
	var paced bool
	for _, row := range ended(read()) {
		if row.Status != 429 {
			continue
		}
		paced = true
		if row.Served != "A" {
			t.Errorf("served = %q on a refusal from the one machine this request demanded, want %q", row.Served, "A")
		}
		if row.RetryAfterS != 30 {
			t.Errorf("retry_after = %v, want the thirty seconds the pool itself asked for", row.RetryAfterS)
		}
	}
	if !paced {
		t.Fatalf("no row recorded the refusal at all; rows: %d", len(ended(read())))
	}
}

// TestAFailedAttemptCarriesWhatItCost stages a reply that was generated,
// billed, and then judged unusable — the model's own tool grammar as text,
// which is one of the census's own signatures. The stream finished, so the
// usage frame arrived; what this asserts is that the row about the FAILURE
// carries it.
func TestAFailedAttemptCarriesWhatItCost(t *testing.T) {
	read := loggingTo(t)
	rig := newLaneRig(t, "failed/priced",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 16,
			Answer:  leakedGrammar,
			PriceIn: 1e-5, PriceOut: 2e-5,
		}},
	)

	ctx := WithLaneChoice(talking(), choiceFor(rig.model, 0))
	_, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"),
		ai.WithTools(machineryTools("web_search", "read")))
	if err == nil {
		t.Fatal("a reply that is the model's own grammar as text is not an answer and should have been cut")
	}
	if !strings.Contains(err.Error(), "internal markup") {
		t.Fatalf("the call failed for some other reason: %v", err)
	}
	var priced bool
	for _, row := range ended(read()) {
		if row.Error == "" {
			continue
		}
		priced = true
		if row.CompletionTokens <= 0 {
			t.Error("the failed attempt was generated and billed and the row says it produced no tokens")
		}
		if row.Cost <= 0 {
			t.Error("the failed attempt cost real money and the row says $0.00 — the census's finding 6, exactly")
		}
		if row.Served != "A" {
			t.Errorf("served = %q on a row about an answer this machine wrote", row.Served)
		}
	}
	if !priced {
		t.Fatal("the cut left no row carrying the error at all")
	}
}

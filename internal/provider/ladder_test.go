package provider

import (
	"context"
	"strings"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// ── THE LADDER, RUNG BY RUNG ────────────────────────────────────────────────
//
// One question escalates in one order and the order is written down
// (docs/ARCHITECTURE.md, "The ladder"):
//
//	1  the same model, another LANE — the watch's hedge
//	2  the same model, the remaining gate-passing lanes — the walk
//	3  the same model, a RELAXED request — and only for a refusal, never for
//	   slowness
//	4  another MODEL
//
// THE ORDER IS THE WHOLE POINT. Every rung past the first answers a slightly
// different question from the one that was asked, and the rungs are sorted by
// how different: a second machine behind the same model answers exactly the
// question; a request with its tools stripped answers a smaller one; another
// model answers somebody else's. A build that reached for rung three because
// rung two was never tried took the tools off a request that only needed a
// different endpoint.

// ladderChoice is a frontier of three lanes in a stated order, which is what
// the chooser hands a request it has an opinion about.
func ladderChoice(model string, _ time.Duration) lanes.Choice {
	return lanes.Choice{
		Order: []string{"A", "B", "C"},
		Frontier: []lanes.Scored{
			{ID: lanes.ID{Model: model, Lane: "A"}, TTFT: 2, Rate: 2000, Price: 0.01},
			{ID: lanes.ID{Model: model, Lane: "B"}, TTFT: 5, Rate: 2000, Price: 0.01},
			{ID: lanes.ID{Model: model, Lane: "C"}, TTFT: 5, Rate: 2000, Price: 0.01},
		},
	}
}

func TestARefusedRescueWalksToTheNextLaneRatherThanRelaxingTheRequest(t *testing.T) {
	told := listen(t)
	rig := newLaneRig(t, "ladder/walk",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 1000,
			Reasoning: 100, Tokens: 40,
			StallAfter: 100, StallFor: 900 * time.Millisecond,
		}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{FailWith: 400}},
		lanestub.Lane{Name: "C", Profile: lanestub.Profile{TTFT: 3 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 2, 250)

	report := &HedgeReport{}
	ctx := WithHedgeReport(context.Background(), report)
	ctx = WithLaneChoice(ctx, ladderChoice(rig.model, 12*time.Millisecond))

	response, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatalf("the ladder ran out before it reached a healthy lane: %v", err)
	}
	if got := answerTokens(response); got != 24 {
		t.Fatalf("the answer is %d tokens, want C's 24", got)
	}
	// RUNG ONE THEN RUNG TWO, and nothing else: the stalled lane, the rescue
	// that refused, and the machine that answered.
	for _, lane := range []string{"A", "B", "C"} {
		if got := rig.server.Requests(lane); got != 1 {
			t.Fatalf("Requests(%s) = %d, want exactly one", lane, got)
		}
	}
	// THE MODEL NEVER MOVED. Rung four is the only rung that changes what
	// somebody asked for, and it was never reached.
	for _, ask := range rig.server.Asks() {
		if ask.Model != rig.model {
			t.Fatalf("asked model %q, want the one the person chose (%q)", ask.Model, rig.model)
		}
	}
	// AND THE REQUEST WAS NEVER RELAXED on the way: a refusal from one machine
	// says nothing about the shape of the question.
	for _, news := range told.all() {
		if news.Phase == PhaseRetrying {
			t.Fatalf("relaxed the request while a lane behind the same model was still untried")
		}
		if news.Phase == PhaseSwitchingModel {
			t.Fatalf("changed the model while a lane behind it was still untried")
		}
	}
	// One story: the switch named where it went, and it went twice.
	switches := 0
	for _, news := range told.all() {
		if news.Phase == PhaseSwitching && (strings.EqualFold(news.Then, "B") || strings.EqualFold(news.Then, "C")) {
			switches++
		}
	}
	if switches == 0 {
		t.Fatalf("the walk was never said out loud: %v", told.story())
	}
}

func TestEveryLaneIsAskedBeforeAnythingElseIsTried(t *testing.T) {
	rig := newLaneRig(t, "ladder/relax",
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{FailWith: 400}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{FailWith: 400}},
		lanestub.Lane{Name: "C", Profile: lanestub.Profile{FailWith: 400}},
	)
	rig.believes("A", 2, 2000)

	ctx := WithLaneChoice(context.Background(), ladderChoice(rig.model, 12*time.Millisecond))
	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err == nil {
		t.Fatalf("three refusing lanes produced an answer")
	}
	// EVERY LANE WAS ASKED BEFORE THE SHAPE OF THE QUESTION WAS TOUCHED.
	for _, lane := range []string{"A", "B", "C"} {
		if rig.server.Requests(lane) == 0 {
			t.Fatalf("lane %s was never tried: %v", lane, rig.server.Served())
		}
	}
	// AND THE MODEL STILL NEVER MOVED. Three refusing machines are evidence
	// about three machines; the model a person chose is not something this
	// layer may change on it.
	for _, ask := range rig.server.Asks() {
		if ask.Model != rig.model {
			t.Fatalf("asked model %q, want %q", ask.Model, rig.model)
		}
	}
	// WHAT THIS FIXTURE CANNOT SHOW, said plainly rather than faked: rung
	// three's own gate is [Client.routingRefusal], and the stub's refusal body
	// names a provider, which is precisely the shape that gate reads as "the
	// router relayed somebody else's refusal" and declines. So the relax ladder
	// correctly does not run here, and the rung is proved where its gate is —
	// endpoints_test.go. What this scenario proves is the ORDER: every lane
	// behind the model was asked before anything else was tried at all.
	if got := len(rig.server.Asks()); got < 3 {
		t.Fatalf("%d requests went out, want one per lane before the ladder moved on", got)
	}
}

package provider

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/calllog"
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

// TestEveryStartRowGetsARowUnderIt is the law, and it is asserted over the
// WHOLE log rather than over one scenario, because the way it was broken was an
// exit nobody had thought of.
//
// Over the ten days to 2026-09-10, 527 of 16,921 attempts had a start row and
// nothing beside it — which every reader of this file, a person and the census
// alike, reads as a call that is still in flight. Six of them were one turn on
// a model the catalog holds no endpoints for, where the ladder ran out and
// returned without writing anything. Five separate comments in this package
// state the law; a law kept by every path remembering to keep it has one
// counter-example per exit, so the transport now closes what it opened
// (calllog.go's [callTrace.open]).
//
// IT IS A LAW ABOUT THE SETTLED LOG AND NOT ABOUT ONE INSTANT. A race the caller
// abandoned answers that caller at once and keeps its account behind them
// ([hedgeRace.accountForTheAbandoned]), so for a few milliseconds after a
// cancelled call returns there really is a start row with nothing under it. That
// is the trade and it is deliberate: the caller of a cancelled race is a person
// who has just typed a correction, and the drain used to cost them the whole of
// [lane.SpokenWithin]. So the tally is taken once the arms have reported —
// bounded by [abandonGrace], which is what bounds the accounting itself — and
// every other clause of the law is unchanged and asserted exactly as it was.
func TestEveryStartRowGetsARowUnderIt(t *testing.T) {
	for _, scene := range []struct {
		name  string
		lanes []lanestub.Lane
		ask   func(*testing.T, *laneRig) context.Context
	}{
		{
			name:  "a clean answer",
			lanes: []lanestub.Lane{{Name: "A", Profile: lanestub.Profile{TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 8}}},
			ask: func(_ *testing.T, rig *laneRig) context.Context {
				return WithLaneChoice(talking(), choiceFor(rig.model, 0))
			},
		},
		{
			name: "a pool that was full and had nowhere to fall back to",
			lanes: []lanestub.Lane{{Name: "A", Profile: lanestub.Profile{
				Paced: true, PacedFor: 30 * time.Second, TTFT: 2 * time.Millisecond, Rate: 2000,
			}}},
			ask: func(t *testing.T, _ *laneRig) context.Context {
				ctx, giveUp := context.WithTimeout(
					WithLaneChoice(talking(), lanes.Choice{Only: []string{"A"}}), time.Second)
				t.Cleanup(giveUp)
				return ctx
			},
		},
		{
			name: "a machine that refuses everything it is asked",
			lanes: []lanestub.Lane{{Name: "A", Profile: lanestub.Profile{
				FailWith: 404, TTFT: 2 * time.Millisecond, Rate: 2000,
			}}},
			ask: func(_ *testing.T, rig *laneRig) context.Context {
				return WithLaneChoice(talking(), choiceFor(rig.model, 0))
			},
		},
		{
			name: "a caller who gave up before the first token",
			lanes: []lanestub.Lane{{Name: "A", Profile: lanestub.Profile{
				TTFT: 5 * time.Second, Rate: 2000, Tokens: 8,
			}}},
			ask: func(t *testing.T, rig *laneRig) context.Context {
				ctx, giveUp := context.WithTimeout(
					WithLaneChoice(talking(), choiceFor(rig.model, 0)), 30*time.Millisecond)
				t.Cleanup(giveUp)
				return ctx
			},
		},
	} {
		t.Run(scene.name, func(t *testing.T) {
			read := loggingTo(t)
			rig := newLaneRig(t, "unclosed/"+strings.ReplaceAll(scene.name, " ", "-"), scene.lanes...)
			// The answer is not the assertion; the LOG is. Every one of these
			// scenes is allowed to fail, and three of them are meant to.
			_, _ = rig.client.CompleteWithMessages(scene.ask(t, rig), userMessages("hello"))

			tally := func() (map[string]int, map[string]int) {
				started, finished := map[string]int{}, map[string]int{}
				for _, row := range read() {
					if row.ID == "" {
						continue
					}
					if row.Phase == "start" {
						started[row.ID]++
						continue
					}
					finished[row.ID]++
				}
				return started, finished
			}
			// Settled, not instantaneous — see the law above. A scene whose rows
			// were all written before the call returned satisfies this on the
			// first look and pays nothing for it.
			waitFor(t, func() bool {
				started, finished := tally()
				if len(started) == 0 {
					return false
				}
				for id := range started {
					if finished[id] == 0 {
						return false
					}
				}
				return true
			})
			started, finished := tally()
			if len(started) == 0 {
				t.Fatal("nothing went out at all, so this scene proves nothing")
			}
			for id, opens := range started {
				if opens != 1 {
					t.Errorf("attempt %s opened %d times", id, opens)
				}
				switch finished[id] {
				case 1:
				case 0:
					t.Errorf("attempt %s has a start row and nothing under it, which reads as a call still in flight", id)
				default:
					t.Errorf("attempt %s was ended %d times", id, finished[id])
				}
			}
			for id := range finished {
				if started[id] == 0 {
					t.Errorf("attempt %s was ended without ever having gone out", id)
				}
			}
		})
	}
}

// TestAnAttemptNothingWroteIsClosedByTheTransport is the mechanism under the
// law above, tested at the seam rather than through a scenario — because the
// exits it guards are the ones nobody has thought of yet, and a test that could
// only reach today's exits would stop covering it the day a new one is written.
func TestAnAttemptNothingWroteIsClosedByTheTransport(t *testing.T) {
	read := loggingTo(t)
	client, _ := newTestClient(t, Config{Model: "vendor/model"})
	request := &ai.Request{Model: "vendor/model", Messages: []ai.Message{{Role: "user"}}}
	knobs := callKnobs{trace: newCallTrace()}

	// One attempt goes out and its path never writes it down; a second begins.
	knobs.trace.begin()
	client.record(recordFacts{
		ctx: context.Background(), request: request, knobs: knobs, stream: true,
		attempt: 1, began: logNow(), phase: calllog.PhaseStart,
	})
	first := knobs.trace.attemptID
	knobs.trace.begin()
	client.record(recordFacts{
		ctx: context.Background(), request: request, knobs: knobs, stream: true,
		attempt: 2, began: logNow(), phase: calllog.PhaseStart,
	})
	second := knobs.trace.attemptID

	// And then the call comes back to its door with the second still open.
	cancelled, giveUp := context.WithCancel(context.Background())
	giveUp()
	client.closeOpenAttempt(cancelled, knobs.trace, endedBy(cancelled))

	closed := map[string]string{}
	opened := map[string]bool{}
	for _, row := range read() {
		if row.Phase == calllog.PhaseStart {
			opened[row.ID] = true
			continue
		}
		closed[row.ID] = row.Ended
	}
	if len(opened) != 2 {
		t.Fatalf("two attempts went out and the log holds %d start rows", len(opened))
	}
	if closed[first] != endedHopped {
		t.Errorf("the attempt another one began over was closed as %q, want %q", closed[first], endedHopped)
	}
	if closed[second] != endedCancelled {
		t.Errorf("the attempt still open when the caller gave up was closed as %q, want %q", closed[second], endedCancelled)
	}
	// And closing twice writes nothing: a log that invented a second ending for
	// one attempt would be worse than the missing row it is here to prevent.
	before := len(read())
	client.closeOpenAttempt(context.Background(), knobs.trace, endedAbandoned)
	if after := len(read()); after != before {
		t.Errorf("closing a square log wrote %d more rows", after-before)
	}
}

// TestTheLosingArmsRowSaysItIsExhaustAndNotAFailure carries the watch's two
// readings (internal/provider's armwatch.go, wave R4) onto the row, which is the
// only place a census can reach them.
//
// A CANCELLED ARM AND AN ABANDONED CALL ARE THE SAME SENTENCE IN THIS FILE. Both
// say `context canceled`, and the first census read all 1,204 of them as
// failures — the largest cause family it found, and not one of them a thing that
// went wrong: they are the price of a race this build chose to run and won. The
// row is where the difference has to be written down, because by the time
// anything reads the log the race is long over.
func TestTheLosingArmsRowSaysItIsExhaustAndNotAFailure(t *testing.T) {
	read := loggingTo(t)
	rig := newLaneRig(t, "exhaust/row",
		// Thirty virtual seconds to A's first token, against five milliseconds
		// to B's: the rescue lands and A is cut off with nothing written.
		lanestub.Lane{Name: "A", Profile: lanestub.Profile{TTFT: 300 * time.Millisecond, Rate: 2000, Tokens: 24}},
		lanestub.Lane{Name: "B", Profile: lanestub.Profile{TTFT: 5 * time.Millisecond, Rate: 2000, Tokens: 24}},
	)
	rig.believes("A", 20, 2000)

	report := &HedgeReport{}
	ctx := WithHedgeReport(talking(), report)
	ctx = WithLaneChoice(ctx, choiceFor(rig.model, 12*time.Millisecond))
	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if !report.Hedged() {
		t.Fatal("the scenario did not hedge, so there is no losing arm to record")
	}
	// The loser's row is written where the loser finds out, which is a moment
	// after the caller already has its answer.
	waitFor(t, func() bool { return len(ended(read())) == 2 })

	var exhaust, answers int
	for _, row := range ended(read()) {
		if row.Exhaust {
			exhaust++
			if strings.TrimSpace(row.Error) == "" {
				t.Error("the exhaust row has no error on it, so there was nothing for a census to mistake")
			}
			continue
		}
		answers++
	}
	if exhaust != 1 || answers != 1 {
		t.Fatalf("the race left %d exhaust rows and %d answers, want one of each", exhaust, answers)
	}
}

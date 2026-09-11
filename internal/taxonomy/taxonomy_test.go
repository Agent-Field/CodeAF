package taxonomy

import (
	"testing"
	"time"
)

// ── WHAT EACH SHAPE IS ──────────────────────────────────────────────────────
//
// The table is the taxonomy itself, and the rows that matter most are the ones
// that used to come out the other way: a 400 from a named upstream, an empty
// 200, and a mangled tool call are all THE WIRE, and reading any of them as the
// model was 57–82% of the bill on three runs of a measured comparison.
func TestWhatEachShapeIs(t *testing.T) {
	for _, row := range []struct {
		name     string
		evidence Evidence
		want     Class
	}{
		{"a 400 the upstream refused", Evidence{Status: 400, Upstream: "Together"}, Transport},
		{"a mangled tool call", Evidence{Status: 400, Upstream: "Together", Malformed: true}, Transport},
		{"an empty 200", Evidence{Empty: true}, Transport},
		{"a deadline", Evidence{Timeout: true}, Transport},
		{"a silent subprocess", Evidence{Idle: true}, Transport},
		{"a cut stream", Evidence{Cut: true}, Transport},
		{"a pacing 429", Evidence{Status: 429}, Transport},
		{"an upstream 500", Evidence{Status: 500}, Transport},
		{"a shape the caller's own pattern knows", Evidence{Wire: true}, Transport},
		{"a 4xx that named nobody", Evidence{Status: 400}, Work},
		{"a check that found gaps", Evidence{Found: true}, Capability},
		{"a check that passed", Evidence{Passed: true}, Capability},
		{"nothing in particular", Evidence{}, Work},
	} {
		if got := classOf(row.evidence); got != row.want {
			t.Errorf("%s is %s, want %s", row.name, got, row.want)
		}
	}
}

// A 400 IS NOT A STATUS QUESTION, IT IS A SHAPE QUESTION, and the two halves of
// that go opposite ways. The upstream's refusal may well be served by somebody
// else; the router's own refusal of our bytes will not be served by anybody.
func TestTheSame400GoesTwoWaysOnWhetherAnUpstreamWasNamed(t *testing.T) {
	upstream := Classify(Evidence{Status: 400, Upstream: "Together", Attempt: 1}, Limits{})
	if !upstream.Retries() {
		t.Fatalf("an upstream's refusal did %q, want a retry somewhere else", upstream.Action)
	}
	ours := Classify(Evidence{Status: 400, Attempt: 1}, Limits{})
	if ours.Class != Work || ours.Action != ActionReport {
		t.Fatalf("the router's own refusal answered %s, want the work handed back", ours)
	}
}

// ── the transport ladder ────────────────────────────────────────────────────

// THE WAIT DOUBLES, AND THE CALLER'S OWN DEADLINE — NOT A COUNT — IS WHAT ENDS
// THE REQUEST. The count was `Limits.TransportAttempts` and it is deleted
// (docs/design/recovery/DESIGN.md §4): the transport under every caller was
// already bounded by the plan's deadline, so a second budget on the same axis
// multiplied rather than bounded. `OutOfTime` is how the caller says it is gone,
// and the thing given up on is still the REQUEST and never the tier.
func TestTheTransportLadderIsWalkedAndThenTheRequestIsGivenUp(t *testing.T) {
	limits := Limits{TransportBackoff: time.Second}
	for attempt, want := range map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second} {
		verdict := Classify(Evidence{Status: 503, Attempt: attempt}, limits)
		if !verdict.Retries() {
			t.Fatalf("attempt %d did %q, want a retry", attempt, verdict.Action)
		}
		if verdict.Backoff != want {
			t.Errorf("attempt %d waits %s, want %s", attempt, verdict.Backoff, want)
		}
		if !verdict.Rotate {
			t.Errorf("attempt %d was not asked to be served by somebody else", attempt)
		}
	}
	spent := Classify(Evidence{Status: 503, Attempt: 4, OutOfTime: true}, limits)
	if spent.Action != ActionGiveUp {
		t.Fatalf("the spent ladder did %q, want the request given up on", spent.Action)
	}
	// AND GIVING UP ON A REQUEST IS NOT GIVING UP ON A TURN, AND BUYS NOTHING.
	if spent.EndsTurn() {
		t.Error("a spent transport ladder was allowed to end a turn")
	}
	if spent.Escalates() {
		t.Error("a spent transport ladder bought a tier")
	}
}

// ── AND A SPENT LADDER IS NOT THE END WHEN THERE IS SOMEWHERE ELSE TO ASK ───
//
// A budget is spent on a MODEL, and a model is not the last thing there is. The
// measured failure was one 502 and three 429s ending a turn while a chain the
// person had configured was never asked, because the only road to it was a cut
// stream.
func TestASpentLadderMovesToTheNextModelWhenTheCallerHasOne(t *testing.T) {
	limits := Limits{}
	spent := Evidence{Status: 429, Upstream: "Together", Attempt: 4, OutOfTime: true}

	alone := Classify(spent, limits)
	if alone.Action != ActionGiveUp {
		t.Fatalf("with nowhere to go the spent ladder did %q, want the request given up on", alone.Action)
	}

	spent.FallbackAvailable = true
	moved := Classify(spent, limits)
	if !moved.Hops() {
		t.Fatalf("with a model left to ask the spent ladder did %q, want the step moved", moved.Action)
	}
	if moved.Class != Transport {
		t.Fatalf("the move was classified %q; nothing about who SERVED a request is evidence about who was asked", moved.Class)
	}
	// A MOVE IS NOT A PURCHASE and it does not end anything.
	if moved.Escalates() {
		t.Error("moving to another model bought a tier")
	}
	if moved.EndsTurn() {
		t.Error("moving to another model was allowed to end a turn")
	}
	if moved.Backoff != 0 {
		t.Errorf("the move waits %s; the wait belongs to the model that was being asked", moved.Backoff)
	}
	// AND IT IS NOT REACHED EARLY. A model with budget left is asked again on
	// the model it is on, chain or no chain.
	if early := Classify(Evidence{Status: 429, Upstream: "Together", Attempt: 3,
		FallbackAvailable: true}, limits); !early.Retries() {
		t.Fatalf("attempt 3 of 4 did %q with a chain in hand, want a retry on the same model", early.Action)
	}
}

// ONE BUDGET, TWO KINDS OF SPENDING. A cut stream is not evidence that the
// endpoint is failing — the request was served and the REPLY came apart — so it
// spends a shorter allowance with no wait in front of it, and arrives at the
// same three endings.
func TestACutStreamSpendsItsOwnAllowanceAndEndsTheSameWay(t *testing.T) {
	limits := Limits{TransportBackoff: time.Second}
	for _, shape := range []struct {
		name    string
		cut     Evidence
		allowed int
	}{
		{"silence that rerouted", Evidence{Cut: true, Rerouted: true}, SilentCutAttempts},
		{"silence that rerouted nothing", Evidence{Cut: true}, BlindCutAttempts},
		{"a reply that stopped being language", Evidence{Cut: true, Degenerate: true, Rerouted: true}, DegenerateCutAttempts},
	} {
		for spent := 1; spent < shape.allowed; spent++ {
			evidence := shape.cut
			evidence.Cuts = spent
			verdict := Classify(evidence, limits)
			if !verdict.Retries() {
				t.Fatalf("%s: cut %d of %d did %q, want a retry", shape.name, spent, shape.allowed, verdict.Action)
			}
			if verdict.Attempts != shape.allowed {
				t.Errorf("%s: the allowance reads %d, want %d", shape.name, verdict.Attempts, shape.allowed)
			}
			// NOTHING TO BACK OFF FROM. The endpoint answered, at once.
			if verdict.Backoff != 0 {
				t.Errorf("%s: cut %d waits %s", shape.name, spent, verdict.Backoff)
			}
		}
		full := shape.cut
		full.Cuts = shape.allowed
		if verdict := Classify(full, limits); verdict.Action != ActionGiveUp {
			t.Fatalf("%s: the spent allowance did %q with nowhere to go, want it given up on",
				shape.name, verdict.Action)
		}
		full.FallbackAvailable = true
		if verdict := Classify(full, limits); !verdict.Hops() {
			t.Fatalf("%s: the spent allowance did %q with a model left, want the step moved",
				shape.name, verdict.Action)
		}
	}
}

// ── the capability ladder ───────────────────────────────────────────────────

// K FINDINGS ON THE SAME TIER BUY ONE LIFT, AND NOT THE ONE BEFORE.
func TestFindingsBuyALiftOnlyAtTheCount(t *testing.T) {
	limits := Limits{SemanticFailures: 2}
	held := Classify(Evidence{Found: true, Refuted: 1}, limits)
	if held.Action != ActionHold {
		t.Fatalf("the first of two findings did %q, want a hold", held.Action)
	}
	bought := Classify(Evidence{Found: true, Refuted: 2}, limits)
	if !bought.Escalates() {
		t.Fatalf("the second finding did %q, want one tier bought", bought.Action)
	}
	if bought.Class != Capability {
		t.Fatalf("the purchase was classified %s, want capability", bought.Class)
	}
}

// AND A FINDING WITH THE WIRE UNDER IT SAYS SO IN ITS OWN REASON, so an autopsy
// can count the purchases this mechanism prevented.
func TestAFindingWithTheWireUnderItHoldsAndSaysWhy(t *testing.T) {
	verdict := Classify(Evidence{Found: true, Refuted: 0, TransportSeen: 4}, Limits{SemanticFailures: 1})
	if verdict.Action != ActionHold {
		t.Fatalf("a finding about a round that never ran did %q, want a hold", verdict.Action)
	}
	if verdict.Reason == "" || verdict.Reason == "the check found gaps" {
		t.Errorf("reason = %q, want it to name the wire", verdict.Reason)
	}
}

// THE LIFT COMES BACK DOWN. This is the half that did not exist: every lift this
// build bought was permanent, so one bad minute at a provider became the price
// of a whole run.
func TestAPassHandsTheLiftBack(t *testing.T) {
	back := Classify(Evidence{Passed: true, Escalated: true}, Limits{})
	if back.Action != ActionDeescalate {
		t.Fatalf("a pass on a lifted tier did %q, want the tier handed back", back.Action)
	}
	// A pass on a tier nothing lifted has nothing to hand back.
	flat := Classify(Evidence{Passed: true}, Limits{})
	if flat.Action != ActionHold {
		t.Fatalf("a pass on the ordinary tier did %q, want nothing", flat.Action)
	}
}

// AND THE CEILING IS ASKED BEFORE THE COUNT, so a verdict never says "escalate"
// on money that may not be spent.
func TestASpentCeilingReturnsTheWorkRatherThanBuyingAgain(t *testing.T) {
	limits := Limits{SemanticFailures: 1, TierCapUSD: 2}
	under := Classify(Evidence{Found: true, Refuted: 1, Escalated: true, SpentUSD: 1.99}, limits)
	if !under.Escalates() {
		t.Fatalf("under the ceiling the verdict did %q, want the round bought", under.Action)
	}
	over := Classify(Evidence{Found: true, Refuted: 9, Escalated: true, SpentUSD: 2}, limits)
	if over.Class != Work || over.Action != ActionReport {
		t.Fatalf("a spent ceiling answered %s, want the work handed back", over)
	}
	// NO CAP IS THE OTHER ANSWER AND IT IS ALLOWED. 0 means no ceiling, which is
	// what this build had before there was one.
	none := Classify(Evidence{Found: true, Refuted: 9, Escalated: true, SpentUSD: 900},
		Limits{SemanticFailures: 1, TierCapUSD: 0})
	if !none.Escalates() {
		t.Fatalf("with no ceiling set the verdict did %q, want the round bought", none.Action)
	}
}

// ── the tally ───────────────────────────────────────────────────────────────

// THE ROUND IS THE BRACKET. A finding is semantic only when the round it judged
// got to run, and the round's wire count is cleared either way so "on the same
// tier" means something.
func TestTheTallyKeepsTheWireApartFromTheModel(t *testing.T) {
	tally := &Tally{}
	tally.Wire()
	tally.Wire()
	if tally.Refuted() {
		t.Fatal("a finding about a round that lost two calls was counted as evidence about the model")
	}
	// THE ROUND WAS CLOSED BY THAT FINDING, so the next one is judged on a clean
	// round of its own. That is what makes the count mean "on the same tier"
	// rather than "ever".
	if !tally.Refuted() {
		t.Fatal("a finding about a round that ran cleanly was not counted")
	}
	wire, semantic, tainted := tally.Counts()
	if wire != 2 || semantic != 1 || tainted != 1 {
		t.Fatalf("wire = %d, semantic = %d, tainted = %d; want 2, 1, 1", wire, semantic, tainted)
	}
	// AND THE EVIDENCE THE POLICY READS CARRIES THE SEMANTIC COUNT AND NOT THE
	// TAINTED ONE.
	if got := tally.Evidence(); got.Refuted != 1 {
		t.Fatalf("the boundary would read %d findings, want the one that was about the model", got.Refuted)
	}
}

// A PASS WIPES THE SLATE. The work has been read and it holds, so nothing before
// it is evidence about the tier any more.
func TestAPassWipesTheSemanticCount(t *testing.T) {
	tally := &Tally{}
	tally.Refuted()
	tally.Refuted()
	tally.Passed()
	if _, semantic, _ := tally.Counts(); semantic != 0 {
		t.Fatalf("semantic = %d after a pass, want none", semantic)
	}
}

// AND THE SPEND FOLLOWS THE TIER. Money spent while nothing was lifted is the
// ordinary price of the work and belongs to somebody else's rail.
func TestOnlyALiftedTiersSpendCountsTowardTheCeiling(t *testing.T) {
	tally := &Tally{}
	tally.Spend(5)
	if got := tally.Evidence().SpentUSD; got != 0 {
		t.Fatalf("spent = %v on a tier nothing lifted, want none", got)
	}
	tally.Escalate()
	tally.Spend(5)
	if got := tally.Evidence().SpentUSD; got != 5 {
		t.Fatalf("spent = %v, want the lifted tier's 5", got)
	}
	tally.Deescalate()
	if got := tally.Evidence(); got.Escalated || got.SpentUSD != 0 {
		t.Fatalf("after the hand-back the tally still holds %+v", got)
	}
}

// A NIL TALLY IS A PIECE OF WORK NOBODY IS TALLYING, and every method tolerates
// it — a conversation carries none.
func TestANilTallyIsSafeEverywhere(t *testing.T) {
	var tally *Tally
	tally.Wire()
	tally.Round()
	tally.Passed()
	tally.Escalate()
	tally.Deescalate()
	tally.Spend(3)
	if tally.Refuted() || tally.Escalated() {
		t.Fatal("a nil tally answered as though it were keeping count")
	}
	if got := tally.Evidence(); got != (Evidence{}) {
		t.Fatalf("a nil tally reported %+v", got)
	}
}

// ── the limits ──────────────────────────────────────────────────────────────

// A CALLER THAT RESOLVED NOTHING STILL GETS A BOUNDED HARNESS, and the floors
// are deliberately the behaviour this build already had.
func TestUnsetLimitsFloorOntoWhatThisBuildAlreadyDid(t *testing.T) {
	got := Limits{}.Floored()
	if got.Patience != DefaultPatience {
		t.Errorf("patience = %v, want %v", got.Patience, DefaultPatience)
	}
	if got.TransportBackoff != DefaultTransportBackoff {
		t.Errorf("backoff = %s, want %s", got.TransportBackoff, DefaultTransportBackoff)
	}
	if got.SemanticFailures != DefaultSemanticFailures {
		t.Errorf("lift after = %d, want %d", got.SemanticFailures, DefaultSemanticFailures)
	}
	if got.TierCapUSD != 0 {
		t.Errorf("cap = %v, want the caller's own", got.TierCapUSD)
	}
}

// ── the registry ────────────────────────────────────────────────────────────

// EVERY CLASS HAS EXACTLY ONE POLICY, and an unregistered class hands the work
// back rather than panicking: a harness that cannot classify a failure must
// still be able to give it to somebody.
func TestAnUnclassifiableFailureIsHandedBackRatherThanDropped(t *testing.T) {
	registryMu.Lock()
	kept := policies[Work]
	delete(policies, Work)
	registryMu.Unlock()
	defer Register(kept)

	verdict := Classify(Evidence{}, Limits{})
	if verdict.Class != Work || verdict.Action != ActionReport {
		t.Fatalf("a class with no policy answered %s", verdict)
	}
}

// A ROUTING REFUSAL IS THE WIRE, AND OUR OWN BYTES ARE STILL THE WORK.
//
// The router's 404 when a list or an account setting emptied its endpoint set
// names no upstream, exactly as a request it rejected on its own bytes names
// none. The transport tells them apart at its refusal door and says so in
// Evidence.Routing; this is the reading of that one fact. The 2026-09-10 turn
// ended on the left-hand reading of the right-hand refusal.
func TestARoutingRefusalIsTransportAndAMalformedRequestIsWork(t *testing.T) {
	limits := Limits{}.Floored()
	routing := Classify(Evidence{Status: 404, Routing: true, Attempt: 1}, limits)
	if routing.Class != Transport || !routing.Retries() || !routing.Rotate {
		t.Fatalf("a routing refusal read as %s, want transport, retried somewhere else", routing)
	}
	if routing.EndsTurn() {
		t.Fatal("a routing refusal ended the turn")
	}
	spent := Classify(Evidence{Status: 404, Routing: true, Attempt: 2, OutOfTime: true, FallbackAvailable: true}, limits)
	if spent.Class != Transport || !spent.Hops() {
		t.Fatalf("a routing refusal with the budget spent read as %s, want a hop to the next model", spent)
	}
	ours := Classify(Evidence{Status: 400, Attempt: 1}, limits)
	if ours.Class != Work || ours.Action != ActionReport {
		t.Fatalf("a 400 about our own bytes read as %s, want the work's report", ours)
	}
}

// A PAUSED FIXED-PRICE WINDOW HAS NO RECOVERY MOVE. The dispatcher owns the
// only authorised metered-door switch; once it returns a pause, a fallback
// advertised by the caller must not turn that billing boundary into a model hop.
func TestAPlanPauseReportsEvenWhenAFallbackExists(t *testing.T) {
	verdict := Classify(Evidence{
		Status: 429, PlanPaused: true, Attempt: 1, FallbackAvailable: true,
	}, Limits{})
	if verdict.Class != Transport || verdict.Action != ActionReport || verdict.Reason != ReasonPlanPaused {
		t.Fatalf("a paused plan read as %s, want transport reported as paused", verdict)
	}
	if !verdict.EndsTurn() || verdict.Hops() || verdict.Retries() {
		t.Fatalf("a paused plan retained a recovery move: %s", verdict)
	}
}

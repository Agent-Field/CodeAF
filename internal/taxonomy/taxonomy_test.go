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

// N ATTEMPTS, DOUBLING, AND THEN THE REQUEST — NOT THE TIER — IS GIVEN UP ON.
func TestTheTransportLadderIsWalkedAndThenTheRequestIsGivenUp(t *testing.T) {
	limits := Limits{TransportAttempts: 4, TransportBackoff: time.Second}
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
	spent := Classify(Evidence{Status: 503, Attempt: 4}, limits)
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
	if got.TransportAttempts != DefaultTransportAttempts {
		t.Errorf("attempts = %d, want %d", got.TransportAttempts, DefaultTransportAttempts)
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

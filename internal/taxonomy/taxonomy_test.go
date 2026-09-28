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

// ── A NAMED REFUSAL IS A MOVE, WHATEVER NUMBER IT WORE ──────────────────────
//
// THE LAW: when the router relays somebody else's no and says whose, the fact is
// about that machine. The status belongs to the upstream and decides nothing —
// a 400, a 502 and a 429 from one pool are one fact and earn one answer.
//
// THE MEASURED FAILURE (2026-09-11 14:39–14:41). `Status == 429` was asked
// before `Upstream != ""`, so the commonest named refusal in ten days of the log
// was read as the account's own ceiling: eight sends to one pool over ninety
// seconds, with six other machines on the same model answering in under five.
func TestANamedRefusalIsTheMachinesWhateverStatusItWore(t *testing.T) {
	for _, status := range []int{400, 403, 404, 429, 500, 502, 503} {
		verdict := Classify(Evidence{
			Status: status, Upstream: "Wafer", FallbackAvailable: true, Attempt: 1,
		}, Limits{})
		if verdict.Class != Transport {
			t.Errorf("a %d relayed from a machine classified as %s, want %s", status, verdict.Class, Transport)
		}
		if verdict.Reason != ReasonRefused {
			t.Errorf("a %d relayed from a machine reads %q, want %q", status, verdict.Reason, ReasonRefused)
		}
		if !verdict.Rotate {
			t.Errorf("a %d relayed from a machine did not ask to be served by somebody else", status)
		}
	}
}

// AND A 429 THAT NAMED NOBODY IS THE ACCOUNT'S OWN CEILING, which is a different
// sentence and a different pair of moves: the wait it asked for, and then
// another model. It used to borrow "the provider could not serve it", which is
// the wrong claim twice — the provider can serve it, and nothing about it is the
// model's fault.
func TestAnUnnamedPaceIsTheAccountsOwnCeiling(t *testing.T) {
	verdict := Classify(Evidence{Status: 429, Attempt: 1, FallbackAvailable: true}, Limits{})
	if verdict.Class != Transport {
		t.Errorf("an account-wide pace classified as %s, want %s", verdict.Class, Transport)
	}
	if verdict.Reason != ReasonPaced {
		t.Errorf("an account-wide pace reads %q, want %q", verdict.Reason, ReasonPaced)
	}
	if verdict.Reason == ReasonRefused {
		t.Error("an account-wide pace was blamed on a machine nobody named")
	}
}

// AND THE PREDICATE IS THE ONE SPELLING OF IT. Three sites ask "is this refusal
// about a machine" — the classifier, the reason, and the transport's own walk —
// and a second spelling would be a second answer.
func TestNamedReadsTheUpstreamAndNothingElse(t *testing.T) {
	for _, row := range []struct {
		upstream string
		want     bool
	}{{"", false}, {"   ", false}, {"Wafer", true}} {
		if got := (Evidence{Upstream: row.upstream}).Named(); got != row.want {
			t.Errorf("Named() on upstream %q = %v, want %v", row.upstream, got, row.want)
		}
	}
}

// A PERSON ON ONE MACHINE IS THE CASE THE SHORT ALLOWANCE WAS WRONG ABOUT.
// `Rerouted` false is narrowed because the next attempt is drawn from the same
// pool by the same rules and buys nothing. With no pool at all — a person's own
// base url, a local server, one connected service — the next attempt is the
// only move there is, and the thing that mends a machine which answered nothing
// is time. So the allowance is longer AND the wait comes back.
func TestACutAgainstOneMachineAsksLongerAndWaitsBetweenAsks(t *testing.T) {
	limits := Limits{TransportBackoff: time.Second}
	if OneMachineCutAttempts <= BlindCutAttempts {
		t.Fatalf("one machine gets %d attempts, which is no more than the %d a blind cut in a pool gets",
			OneMachineCutAttempts, BlindCutAttempts)
	}
	for spent := 1; spent < OneMachineCutAttempts; spent++ {
		verdict := Classify(Evidence{Cut: true, OneMachine: true, Cuts: spent}, limits)
		if !verdict.Retries() {
			t.Fatalf("cut %d of %d did %q, want a retry", spent, OneMachineCutAttempts, verdict.Action)
		}
		if verdict.Attempts != OneMachineCutAttempts {
			t.Errorf("cut %d reads an allowance of %d, want %d", spent, verdict.Attempts, OneMachineCutAttempts)
		}
		// THE WAIT IS THE POINT. Asking the same server again the same instant
		// is not patience; it is the same request twice.
		if want := time.Second << (spent - 1); verdict.Backoff != want {
			t.Errorf("cut %d waits %s, want %s", spent, verdict.Backoff, want)
		}
	}
	// AND THE ENDING IS THE SAME ENDING. A spent allowance hops when there is a
	// next model and says so when there is not.
	full := Evidence{Cut: true, OneMachine: true, Cuts: OneMachineCutAttempts}
	if verdict := Classify(full, limits); verdict.Action != ActionGiveUp {
		t.Errorf("a spent allowance with no chain did %q, want %q", verdict.Action, ActionGiveUp)
	}
	full.FallbackAvailable = true
	if verdict := Classify(full, limits); verdict.Action != ActionHop {
		t.Errorf("a spent allowance with a chain did %q, want %q", verdict.Action, ActionHop)
	}
}

// AND A POOL KEEPS ITS OWN ANSWER. The field narrows nothing: a cut that had
// endpoint diversity still spends the short allowance with no wait, because
// what mends it is being served by somebody else.
func TestOneMachineChangesNothingForACutThatHadAPool(t *testing.T) {
	limits := Limits{TransportBackoff: time.Second}
	verdict := Classify(Evidence{Cut: true, Rerouted: true, Cuts: 1}, limits)
	if verdict.Attempts != SilentCutAttempts {
		t.Errorf("a rerouted cut reads an allowance of %d, want %d", verdict.Attempts, SilentCutAttempts)
	}
	if verdict.Backoff != 0 {
		t.Errorf("a rerouted cut waits %s, want no wait", verdict.Backoff)
	}
}

// A PERSON WAITING ON THEIR OWN MACHINE IS NOT GIVEN UP ON. Every other bound
// in this policy exists because the time could be spent on something else —
// another endpoint, another model, an ending that frees the person to go and
// fix it. A watched conversation against one machine with no chain has none of
// those, so it keeps asking and the person ends it when they choose.
func TestAWatchedWaitOnOneMachineIsNeverGivenUpOn(t *testing.T) {
	limits := Limits{TransportBackoff: time.Second}
	waiting := Evidence{Cut: true, OneMachine: true, Watched: true}
	for _, spent := range []int{1, 2, 5, 40, 4000} {
		evidence := waiting
		evidence.Cuts = spent
		verdict := Classify(evidence, limits)
		if !verdict.Retries() {
			t.Fatalf("ask %d did %q, want a retry for ever", spent, verdict.Action)
		}
		// NO DENOMINATOR, because there is no count to reach. The surface reads
		// this to know it must say the waiting some other way.
		if verdict.Attempts != 0 {
			t.Errorf("ask %d carries an allowance of %d, want none", spent, verdict.Attempts)
		}
		// AND IT SAYS SO IN A FIELD OF ITS OWN. Zero attempts already means
		// something older and different — an ordinary failure bounded by the
		// caller's deadline rather than by a count — so a caller reading the
		// two as one ending would end the deadline for every failure there is.
		if !verdict.Unbounded {
			t.Errorf("ask %d does not declare itself unbounded", spent)
		}
	}
	// AND THE DEADLINE DOES NOT END IT EITHER, which is the one place in this
	// policy where running out of time is not the last word.
	outOfTime := waiting
	outOfTime.Cuts, outOfTime.OutOfTime = 9, true
	if verdict := Classify(outOfTime, limits); !verdict.Retries() {
		t.Errorf("the give-up ended a watched wait: %q", verdict.Action)
	}
}

// AND THE THREE THINGS THAT END IT ARE EACH ENOUGH ON THEIR OWN. A chain the
// person configured is the move they asked for; nobody watching makes the same
// loop a hang; and a pool means the next ask is somewhere else already.
func TestEachMissingPieceEndsTheUnboundedWait(t *testing.T) {
	limits := Limits{TransportBackoff: time.Second}
	for _, shape := range []struct {
		name   string
		remove func(*Evidence)
	}{
		{"a chain to hop to", func(e *Evidence) { e.FallbackAvailable = true }},
		{"nobody watching", func(e *Evidence) { e.Watched = false }},
		{"a pool behind it", func(e *Evidence) { e.OneMachine, e.Rerouted = false, true }},
	} {
		evidence := Evidence{Cut: true, OneMachine: true, Watched: true, Cuts: 40}
		shape.remove(&evidence)
		verdict := Classify(evidence, limits)
		if verdict.Unbounded {
			t.Errorf("%s: still waiting for ever after 40 asks", shape.name)
		}
	}
}

// THE WAIT CLIMBS AND THEN HOLDS. Doubling away is a manner towards a shared
// service under strain; the machine here belongs to the person waiting on it,
// asking costs nothing, and a schedule that reached four minutes would turn a
// server that came back in ninety seconds into four more minutes of spinner.
func TestTheWaitOnOneMachineClimbsToACeilingAndStaysThere(t *testing.T) {
	cut := Evidence{Cut: true, OneMachine: true, Watched: true}
	base := time.Second
	var last time.Duration
	for ask := 1; ask <= 20; ask++ {
		wait := waitFor(cut, ask, base)
		if wait > OneMachineCutCeiling {
			t.Fatalf("ask %d waits %s, past the %s ceiling", ask, wait, OneMachineCutCeiling)
		}
		if ask > 1 && wait < last {
			t.Fatalf("ask %d waits %s, less than the %s before it", ask, wait, last)
		}
		last = wait
	}
	if last != OneMachineCutCeiling {
		t.Errorf("the schedule settled at %s, want the %s ceiling", last, OneMachineCutCeiling)
	}
	// AND A CUT WITH A POOL STILL WAITS NOT AT ALL, because what mends that one
	// is a different endpoint and it costs no time.
	if wait := waitFor(Evidence{Cut: true, Rerouted: true}, 4, base); wait != 0 {
		t.Errorf("a pooled cut waits %s, want none", wait)
	}
}

// AND IT HOLDS FOR EVER, NOT FOR TWENTY ASKS (#1358). The ramp was a signed
// shift, and past about thirty-five asks of a one-second base it wrapped to
// zero or below, which the turn loop reads as no wait at all: the unbounded
// wait turned back into a hot loop on its thirty-sixth ask. The test above
// stopped at twenty and never saw it.
func TestTheWaitOnOneMachineNeverWrapsToNothing(t *testing.T) {
	cut := Evidence{Cut: true, OneMachine: true, Watched: true}
	for ask := 5; ask <= 500; ask++ {
		if wait := waitFor(cut, ask, time.Second); wait != OneMachineCutCeiling {
			t.Fatalf("ask %d waits %s, want the %s ceiling", ask, wait, OneMachineCutCeiling)
		}
	}
	// A refusal has no ceiling, and its doubling saturates rather than wraps.
	for ask := 1; ask <= 500; ask++ {
		if wait := waitFor(Evidence{}, ask, time.Second); wait <= 0 {
			t.Fatalf("refusal %d waits %s, want a positive wait", ask, wait)
		}
	}
}

// AN ORDINARY FAILURE IS BOUNDED BY THE CALLER'S DEADLINE AND NOT BY A COUNT,
// which is what [Verdict.Attempts] of zero has meant since the count was
// removed. It is the reason the unbounded wait needs a field of its own: a
// caller that read zero attempts as "nothing may end this" would stop the
// deadline ending any failing request at all.
func TestZeroAttemptsIsNotTheSameClaimAsUnbounded(t *testing.T) {
	limits := Limits{TransportBackoff: time.Second}
	ordinary := Classify(Evidence{Status: 429, Attempt: 2}, limits)
	if !ordinary.Retries() {
		t.Fatalf("an ordinary refusal did %q, want a retry", ordinary.Action)
	}
	if ordinary.Attempts != 0 {
		t.Errorf("an ordinary refusal carries an allowance of %d, want none", ordinary.Attempts)
	}
	if ordinary.Unbounded {
		t.Error("an ordinary refusal declares itself unbounded, so no deadline could end it")
	}
}

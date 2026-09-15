package provider

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/lane/lanestub"
)

// ── A NAMED REFUSAL IS A MOVE, AND A VETO THAT DID NOT TAKE IS NOT ──────────
//
// THE LIVE CHAIN (2026-09-11 14:39:17 – 14:40:47, the owner's own task, tag
// `task` node 2, deepseek/deepseek-v4.1-flash). The router answered eight
// consecutive sends with `(via Wafer: … is temporarily rate-limited upstream)`,
// every one of them carrying `retry_after: 2`, while six other machines on the
// same model were answering inside five seconds — one of them thirty seconds
// earlier in the same minute. The gaps between the eight are the dispatcher's
// own doubling (0.7 s, 1.4, 2.8, 5.6, 11.2, 22.4, 44.8), which is what a call
// pays to ask THE SAME MACHINE again; the person read `waiting · rate limited ·
// 13m 37s` for ninety seconds and then the work hopped to a model nobody chose.
//
// TWO THINGS WERE WRONG AND THEY ARE TESTED APART:
//
//   - A refusal that NAMED a machine is that machine's, so the answer to it is
//     another machine and never a wait. That half already worked for a relayed
//     4xx and was denied to a 429 because the status was read instead of the
//     evidence.
//   - A VETO THAT DID NOT TAKE IS NOWHERE ELSE TO GO. The name in a relayed
//     refusal is the upstream's, and an upstream label is not always a name the
//     router will route around; when the next body names it and the same machine
//     answers again, the call has learned that routing cannot save it, and the
//     honest move is to hand the refusal back so the ladder and the session's one
//     model hop can run. It used to learn nothing and spend the whole deadline.

// wafered stages the live chain: one machine that refuses with its own name and
// its own comeback time and that the router will not route around
// ([lanestub.Lane.Unvetoable]), beside a healthy one the request could have had.
//
// The healthy machine is declared FIRST so that a call which escapes the veto
// lands on it — what a passing scenario looks like is a request that moved.
func wafered(t *testing.T, name string, unvetoable bool) (*laneRig, func() []time.Duration) {
	t.Helper()
	rig := newLaneRig(t, name,
		lanestub.Lane{Name: "Novita", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 8,
		}},
		lanestub.Lane{Name: "Wafer", Unvetoable: unvetoable, Profile: lanestub.Profile{
			Paced: true, PacedFor: 2 * time.Second,
			TTFT: 2 * time.Millisecond, Rate: 2000,
		}},
	)
	// THE WAITS ARE COLLECTED AND NOT SPENT. What this scenario is about is how
	// many of them there are; really sitting through a doubling ladder would be
	// ninety seconds of a suite to prove something about a branch.
	var mu sync.Mutex
	waits := []time.Duration{}
	rig.client.wait = func(_ context.Context, delay time.Duration) error {
		mu.Lock()
		waits = append(waits, delay)
		mu.Unlock()
		return nil
	}
	return rig, func() []time.Duration {
		mu.Lock()
		defer mu.Unlock()
		return append([]time.Duration(nil), waits...)
	}
}

// asking is the choice this scenario hands over: the refusing machine at the
// head, exactly as the live request's `lane` field said (`SiliconFlow` asked,
// `Wafer` served, eight times).
func asking(model string) lanes.Choice {
	choice := choiceFor(model, 0)
	choice.Order = []string{"Wafer", "Novita"}
	return choice
}

// TestAVetoThatDidNotTakeIsNowhereElseToGo is the live chain itself. The veto
// reaches the wire and the router serves the same machine anyway; the call must
// find that out ONCE and hand the refusal back, rather than discover it eight
// times with a doubling wait in front of each.
//
// ON dev THIS SCENARIO IS THE LIVE CHAIN EXACTLY: eight sends to Wafer and
// eighty-nine seconds of doubling, every body after the first carrying
// `ignore: [Wafer]` and being served by Wafer regardless. What ends it is
// neither the veto nor the ladder — it is a rescue arm demanding the healthy
// machine after a minute and a half.
func TestAVetoThatDidNotTakeIsNowhereElseToGo(t *testing.T) {
	rig, waits := wafered(t, "wafer/unvetoable", true)
	ctx := WithLaneChoice(WithPatientRateLimits(talking()), asking(rig.model))

	_, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))

	// THE FIRST SEND DISCOVERS THE MACHINE AND THE SECOND DISCOVERS THE VETO.
	// There is nothing a third could learn: the body already names every machine
	// this call has met, and the router has answered it with the same one.
	if sent := rig.server.Requests("Wafer"); sent > 2 {
		t.Errorf("the call sent %d requests to a machine it had already vetoed, want at most the one that proved the veto useless", sent)
	}
	if spent := total(waits()); spent > lanes.VisiblePatience {
		t.Errorf("the call waited %s on one machine, want no more than %s before the refusal is somebody else's to answer", spent, lanes.VisiblePatience)
	}
	for _, ask := range rig.server.Asks() {
		if len(ask.Ignore) > 0 && !namesEndpoint(ask.Ignore, "Wafer") {
			t.Errorf("a body vetoed %v, want the machine that actually answered", ask.Ignore)
		}
	}
	// AND WHATEVER THE ENDING IS, IT IS NOT A RAW STATUS. A refusal handed back
	// reaches the session, which offers another model; an answer means something
	// above found a machine. Either is fine and neither may take ninety seconds.
	if err != nil && !strings.Contains(err.Error(), "429") {
		t.Fatalf("the call failed with %v, want the pacing a model hop is offered on", err)
	}
}

// TestANamedRefusalGoesToAnotherMachineWithNoWait is the same chain on a router
// that honours the veto, which is the commoner shape and the one that must stay
// exactly as fast as it is: the machine that refused is off the next body, the
// next body goes out at once, and a healthy machine answers it.
func TestANamedRefusalGoesToAnotherMachineWithNoWait(t *testing.T) {
	rig, waits := wafered(t, "wafer/vetoable", false)
	ctx := WithLaneChoice(WithPatientRateLimits(talking()), asking(rig.model))

	if _, err := rig.client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatalf("a pool whose second machine was healthy did not answer: %v", err)
	}
	if got := rig.server.Requests("Novita"); got != 1 {
		t.Errorf("the healthy machine was asked %d times, want exactly the one move", got)
	}
	if spent := total(waits()); spent != 0 {
		t.Errorf("the call waited %s to go somewhere else; a move is not a wait", spent)
	}
}

// TestOnlyADemandExemptsAMachineFromTheVetoLaw is the law the exemption states,
// asked of the exemption itself.
//
// THE EXEMPTION IS FOR A REQUEST THAT DEMANDED ONE MACHINE, because the encoder
// leaves a demand down to its last name exactly as it stands and never vetoes it
// ([Client.dropRefusedHere]) — so that machine was never taken off anything and
// its refusal proves nothing about whether vetoes work.
//
// A CANDIDATE SET OF ONE IS NOT A DEMAND. It is this build's own belief about
// which machines are worth choosing between — an advisory ranking sent with
// fallbacks left on, every name of which the encoder vetoes freely — and a model
// this process has timed exactly one machine for has a set of one while the
// router still has the whole pool. The exemption used to ask [requestSet], which
// answers the WIDER question ("which machines may this request go to") and folds
// that belief in with the two real demands, so the narrowest belief this build
// can hold was exempted from the law its own doc comment described.
func TestOnlyADemandExemptsAMachineFromTheVetoLaw(t *testing.T) {
	t.Parallel()
	one := func(names ...string) *lanes.Choice {
		choice := &lanes.Choice{Order: names}
		for _, name := range names {
			choice.Frontier = append(choice.Frontier, lanes.Scored{ID: lanes.ID{Lane: name}})
		}
		return choice
	}
	demand := func(names ...string) *lanes.Choice {
		choice := one(names...)
		choice.Only = names
		return choice
	}
	for _, probe := range []struct {
		what  string
		knobs callKnobs
		want  bool
	}{
		{"a rescue's arm demands the machine it was sent to",
			callKnobs{hedgeLane: "Wafer"}, true},
		{"a person's strict pin demands the machine they named",
			callKnobs{laneChoice: demand("Wafer")}, true},
		{"a demand of two is not one machine wide",
			callKnobs{laneChoice: demand("Wafer", "Novita")}, false},
		{"a candidate set of one is a belief, not a demand",
			callKnobs{laneChoice: one("Wafer")}, false},
		{"a candidate set of two is not either",
			callKnobs{laneChoice: one("Wafer", "Novita")}, false},
		{"a call that named nothing demands nothing",
			callKnobs{}, false},
	} {
		if got := demandedThisOne(probe.knobs, "Wafer"); got != probe.want {
			t.Errorf("%s: the machine reads as demanded=%v, want %v", probe.what, got, probe.want)
		}
	}
	// AND A REFUSAL FROM NOBODY IS NOT A DEMAND FOR ANYBODY, whatever was asked
	// for: with no name on the refusal there is no machine for the law to be
	// about, so the exemption may not fire on the demand alone.
	if demandedThisOne(callKnobs{hedgeLane: "Wafer"}, "") {
		t.Error("a refusal that named nobody exempted the machine the request demanded")
	}
}

// TestOneMachineBelievedInIsStillNotADemand is the same law on the wire: a call
// whose whole candidate set is the machine that keeps refusing it still finds
// that out once and hands the refusal up, rather than paying for the discovery
// again.
func TestOneMachineBelievedInIsStillNotADemand(t *testing.T) {
	rig, waits := wafered(t, "wafer/onebelief", true)
	// The chooser has timed one machine and ranks only it. Nothing demands it:
	// `Only` is empty, so the body goes out with fallbacks on and the router is
	// free to serve anybody — and does, with the machine that keeps refusing.
	choice := asking(rig.model)
	choice.Order = []string{"Wafer"}
	choice.Frontier = choice.Frontier[:1]
	choice.Frontier[0].ID.Lane = "Wafer"
	ctx := WithLaneChoice(WithPatientRateLimits(talking()), choice)

	_, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))

	for _, ask := range rig.server.Asks() {
		if len(ask.Only) > 0 {
			t.Fatalf("the body demanded %v; this scenario is about an ADVISORY set and proves nothing if one is sent", ask.Only)
		}
	}
	if sent := rig.server.Requests("Wafer"); sent > 2 {
		t.Errorf("the call sent %d requests to a machine it had already vetoed, want at most the one that proved the veto useless", sent)
	}
	if spent := total(waits()); spent > lanes.VisiblePatience {
		t.Errorf("the call waited %s on one machine it merely believed in, want no more than %s", spent, lanes.VisiblePatience)
	}
	if err != nil && !strings.Contains(err.Error(), "429") {
		t.Fatalf("the call failed with %v, want the pacing a model hop is offered on", err)
	}
}

// total is how long a call asked to be paused for, all told.
func total(waits []time.Duration) time.Duration {
	var spent time.Duration
	for _, wait := range waits {
		spent += wait
	}
	return spent
}

// TestAnAccountWidePaceWaitsOnceAndThenHandsBack is the other half of the same
// defect. A 429 that names NOBODY on a router with a whole pool behind the
// model is the account's own ceiling: every machine is behind it, nothing can
// be taken off the next body, and a relaxed shape does not get under it. It
// earns exactly one wait — the comeback the refusal itself named — and then the
// refusal is handed back so the session can offer another model, which always
// beats a window.
func TestAnAccountWidePaceWaitsOnceAndThenHandsBack(t *testing.T) {
	rig, waits := wafered(t, "wafer/keypaced", false)
	rig.server.PacesTheKey(3 * time.Second)
	ctx := WithLaneChoice(WithPatientRateLimits(talking()), asking(rig.model))

	_, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
	if err == nil {
		t.Fatal("an account-wide ceiling has no machine to move to; the call should have handed it back")
	}
	if got := len(rig.server.Asks()); got != 2 {
		t.Errorf("the call sent the same bytes %d times, want the first and the one re-ask the refusal asked for", got)
	}
	if got := waits(); len(got) != 1 || got[0] != 3*time.Second {
		t.Errorf("the call waited %v, want exactly the three seconds the refusal named", got)
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("the call failed with %v, want the pacing a model hop is offered on", err)
	}
}

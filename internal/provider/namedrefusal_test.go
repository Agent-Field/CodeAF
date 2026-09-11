package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
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

// anonymousPace is the account's own ceiling: a 429 that names no machine at
// all and asks for a comeback in its own body, which is where this router puts
// it (`retry_after` was on not one of 1,106 paced rows' headers in ten days).
func anonymousPace(mu *sync.Mutex, sends *int, comeback time.Duration) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		*sends++
		mu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprintf(writer,
			`{"error":{"message":"rate limit exceeded","retry_after":%d}}`, int(comeback.Seconds()))
	})
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
// defect. A 429 that names NOBODY is the account's own ceiling: every machine
// answers it identically, so there is nothing to take off the next body and the
// next body is therefore the SAME BYTES TO THE SAME MACHINE. It earns exactly
// one wait — the comeback the refusal itself named — and then the refusal is
// handed back so the session can offer another model, which always beats a
// window.
func TestAnAccountWidePaceWaitsOnceAndThenHandsBack(t *testing.T) {
	var mu sync.Mutex
	sends := 0
	client := pacedClient(t, anonymousPace(&mu, &sends, 3*time.Second))
	client.velocity = newVelocityLedger()
	var waits []time.Duration
	client.wait = func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	}

	_, err := client.CompleteWithMessages(WithPatientRateLimits(talking()), userMessages("hello"))
	if err == nil {
		t.Fatal("an account-wide ceiling has no machine to move to; the call should have handed it back")
	}
	mu.Lock()
	got := sends
	mu.Unlock()
	if got != 2 {
		t.Errorf("the call sent the same bytes %d times, want the first and the one re-ask the refusal asked for", got)
	}
	if len(waits) != 1 || waits[0] != 3*time.Second {
		t.Errorf("the call waited %v, want exactly the three seconds the refusal named", waits)
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("the call failed with %v, want the pacing a model hop is offered on", err)
	}
}

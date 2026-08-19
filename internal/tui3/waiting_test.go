package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The wait for the first byte: what the pulse says while a model request is
// outstanding and the provider has not yet produced a single byte against it.
//
// Each of these asserts the LINE a person reads rather than the shape of the
// code under it, because the whole feature is a promise about what is on
// screen in the one state the surface used to say nothing at all about.

// newWaitingApp is a working turn pointed at a named model, with the wait
// anchored `waited` ago and nothing back from it.
func newWaitingApp(t *testing.T, waited time.Duration) *app {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "moonshot/kimi-k3"})
	a.state = stateWorking
	a.model = "moonshot/kimi-k3"
	a.awaited = time.Now().Add(-waited)
	return a
}

// A FAST PROVIDER NEVER SHOWS A CLOCK. Inside the grace the line is the
// ellipsis and nothing else — the state every ordinary turn passes through.
func TestAShortWaitPutsNoClockOnThePulse(t *testing.T) {
	a := newWaitingApp(t, waitingGrace-time.Second)
	line, ok := a.ellipsis()
	if !ok {
		t.Fatal("a working turn drew no indicator")
	}
	if strings.Contains(plain(line), "waiting") {
		t.Fatalf("a wait still inside its grace put a clock on screen: %q", plain(line))
	}
}

// Past the grace the line names what is being waited on and how long for.
func TestALongWaitNamesTheModelAndCountsUp(t *testing.T) {
	a := newWaitingApp(t, 12*time.Second)
	line, ok := a.ellipsis()
	if !ok {
		t.Fatal("a working turn drew no indicator")
	}
	text := plain(line)
	if !strings.HasSuffix(text, "waiting for kimi-k3 · 12s") {
		t.Fatalf("the wait did not name the model and the seconds: %q", text)
	}
	// The model is its BASENAME, the way the deck's chip spells it: the vendor
	// is routing and the person is waiting on a model.
	if strings.Contains(text, "moonshot/") {
		t.Fatalf("the wait spelled the model's full routing address: %q", text)
	}
	// It is the surface talking about itself, so it is dim.
	if !strings.Contains(line, sgr256(hueDim)) {
		t.Fatalf("the wait is not dim: %q", line)
	}
	// And it has NOT escalated: this is a long wait, not an abnormal one.
	if strings.Contains(text, waitLongWord) {
		t.Fatalf("twelve seconds escalated: %q", text)
	}
}

// Past the longer threshold the line says the plain fact outright, because by
// then the person is deciding whether to interrupt.
func TestAVeryLongWaitSaysNothingHasComeBack(t *testing.T) {
	a := newWaitingApp(t, 47*time.Second)
	line, _ := a.ellipsis()
	text := plain(line)
	if !strings.HasSuffix(text, "waiting for kimi-k3 · 47s · nothing has come back yet") {
		t.Fatalf("a forty-seven second wait did not escalate: %q", text)
	}
}

// A model the surface cannot name says so by saying nothing — the emptiness
// law — rather than leaving an empty slot after "waiting for".
func TestAWaitOnAnUnnamedModelLeavesNoEmptySlot(t *testing.T) {
	a := newWaitingApp(t, 12*time.Second)
	a.model = ""
	text := plain(mustLine(t, a))
	if !strings.HasSuffix(text, "waiting · 12s") {
		t.Fatalf("an unnamed model did not fall back to the bare word: %q", text)
	}
}

// A RUNNING CALL NEVER GETS A WAITING CLOCK. The call has its own spinner and
// its own count-up, and a second clock over the same wait would be the surface
// timing a `go test` and calling it a provider.
func TestARunningCallNeverGetsAWaitingClock(t *testing.T) {
	a := newWaitingApp(t, 3*time.Minute)
	a.entries = append(a.entries, entry{
		kind: entryTool, tool: "bash", status: toolRunning,
		turn: a.turn, began: time.Now().Add(-3 * time.Minute),
	})
	if words := a.waitingWords(); words != "" {
		t.Fatalf("a spinning call was given a provider's clock: %q", words)
	}
	// And the pulse stands down for the spinner entirely, as it always has.
	if line, ok := a.ellipsis(); ok {
		t.Fatalf("the pulse drew beside a spinning call: %q", plain(line))
	}
}

// A call that ENDS restarts the clock rather than handing its own runtime to
// it: the request that follows a three-minute `go test` has been outstanding
// for no time at all.
func TestACallEndingRestartsTheWaitRatherThanInheritingItsRuntime(t *testing.T) {
	a := newWaitingApp(t, 3*time.Minute)
	a.event(session.Event{Kind: session.EventToolEnd, Tool: "bash", CallID: "call_1"})
	if words := a.waitingWords(); words != "" {
		t.Fatalf("the request after a long call opened holding the call's runtime: %q", words)
	}
	if time.Since(a.awaited) > time.Second {
		t.Fatalf("the wait was not re-anchored on the call ending")
	}
}

// THE MOMENT THE STREAM SPEAKS THE LINE IS GONE. Every kind that is the
// provider answering stops the clock — the reply's text, the reasoning behind
// it, the marker that opened it, and a tool call arriving in any of its three
// phases.
func TestTheFirstThingTheStreamSaysStopsTheWait(t *testing.T) {
	spoke := []session.EventKind{
		session.EventTextDelta,
		session.EventReasoning,
		session.EventThinking,
		session.EventToolForming,
		session.EventToolAnnounced,
		session.EventToolBegin,
	}
	for _, kind := range spoke {
		a := newWaitingApp(t, time.Minute)
		a.event(session.Event{Kind: kind, Text: "x", Tool: "read", CallID: "call_1"})
		if !a.awaited.IsZero() {
			t.Fatalf("event kind %d left the wait running", kind)
		}
		if words := a.waitingWords(); words != "" {
			t.Fatalf("event kind %d left %q on screen", kind, words)
		}
	}
}

// An idle surface has no wait to report: the turn is over and nothing is
// outstanding.
func TestAnIdleSurfaceNeverWaitsOnAModel(t *testing.T) {
	a := newWaitingApp(t, time.Minute)
	a.state = stateIdle
	if words := a.waitingWords(); words != "" {
		t.Fatalf("an idle surface said %q", words)
	}
}

// The wait outranks the older silence suffix: only one of the two is ever on
// the line, and it is the one that names what is being waited on.
func TestTheWaitReplacesTheStillWorkingSuffix(t *testing.T) {
	a := newWaitingApp(t, time.Minute)
	a.lastDelta = time.Now().Add(-time.Minute)
	text := plain(mustLine(t, a))
	if strings.Contains(text, stillWorkingWord) {
		t.Fatalf("both answers to one question were on the line: %q", text)
	}
	if !strings.Contains(text, "waiting for kimi-k3") {
		t.Fatalf("the wait was not the one that survived: %q", text)
	}
}

// mustLine is the indicator, or a failure saying there was none.
func mustLine(t *testing.T, a *app) string {
	t.Helper()
	line, ok := a.ellipsis()
	if !ok {
		t.Fatal("a working turn drew no indicator")
	}
	return line
}

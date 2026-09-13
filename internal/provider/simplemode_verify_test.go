package provider

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/calllog"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THROWAWAY VERIFICATION for the simple routing mode: what the wire carries
// when the row reads `simple`. Kept as a law because the mode's whole promise
// is the shape of the request — bare without a pin, the pin's demand and
// nothing else with one.
func TestSimpleModeSendsOnlyThePersonsPin(t *testing.T) {
	server := prefRig(t)
	client := plainBase(t, server.URL(), server)
	client.config.Routing = StaticRouting(RoutingSimple)

	// No pin: the request carries no provider object at all, and nobody
	// hedges — one ask per turn.
	if _, err := client.CompleteWithMessages(talking(), userMessages("hello")); err != nil {
		t.Fatalf("unpinned simple turn failed: %v", err)
	}
	// A pin: the demand is the whole object.
	pinned(t, LanePin{Lane: "Harbor"})
	if _, err := client.CompleteWithMessages(talking(), userMessages("again")); err != nil {
		t.Fatalf("pinned simple turn failed: %v", err)
	}

	asks := server.Asks()
	if len(asks) != 2 {
		t.Fatalf("two turns produced %d asks, want exactly 2 (no hedging)", len(asks))
	}
	bare := asks[0]
	if bare.Sort != "" || bare.Only != nil || bare.Order != nil || bare.Ignore != nil || bare.MaxPrice != nil || bare.NoFallbacks {
		t.Fatalf("simple with no pin carried %+v, want a bare request", bare)
	}
	demand := asks[1]
	if len(demand.Only) != 1 || !strings.EqualFold(demand.Only[0], "Harbor") ||
		!demand.NoFallbacks || demand.Sort != "" || demand.MaxPrice != nil || demand.Order != nil {
		t.Fatalf("simple with a pin carried %+v, want only Harbor and fallbacks off", demand)
	}
}

// ── A PIN THE WIRE REFUSES, UNDER `simple` ──────────────────────────────────
//
// The loose end the mode shipped with. `simple` is the row that promises the
// wire carries the person's own instruction and nothing else, and the danger in
// that promise is the silent half: a demand that never goes out leaves the
// chrome saying `@deepseek` over a request the router answered from wherever it
// liked, which is the exact complaint the mode was written to close.
//
// So the pin is SENT, once, even when this process already believes the
// account's own settings exclude the machine — the wire is the only authority
// on that, the belief is a day old by design (internal/lane's account.go), and
// one refused round trip a run is a smaller price than a promise quietly
// broken. When the refusal comes back the pin is retired through the SAME door
// every other mode retires one through, so the person reads the one sentence
// this build has for it and the chrome stops naming the machine in the same
// breath.
func TestSimpleRoutingSendsThePinOnceThenRetiresItOutLoud(t *testing.T) {
	rig := newLaneRig(t, "simple/pin-refused", retiredLanes()...)
	forgotten(t)
	rig.client.config.Routing = StaticRouting(RoutingSimple)
	pinned(t, LanePin{Lane: "Ghost"})
	notes := &noticeLog{}
	turn := WithStreamObserver(talking(), notes.observe)

	// 1. THE DEMAND GOES OUT. Under `simple` the pin is the whole request.
	if _, err := rig.client.CompleteWithMessages(turn, userMessages("hello")); err != nil {
		t.Fatalf("the turn died on a refusal the widened retry was supposed to absorb: %v", err)
	}
	asks := rig.server.Asks()
	if len(asks) == 0 {
		t.Fatal("nothing reached the router")
	}
	if !demandedOnly(asks[0], "Ghost") {
		t.Fatalf("the first ask carried %+v, want the one machine the person pinned", asks[0])
	}
	before := len(asks)

	// 2. AND THE PERSON IS TOLD, in the sentence this build already has.
	said := notes.retired()
	if len(said) != 1 {
		t.Fatalf("the conversation was told %d times, want exactly once: %q", len(said), said)
	}
	if want := RetiredPinLine("Ghost"); said[0] != want {
		t.Fatalf("the conversation reads %q, want %q", said[0], want)
	}

	// 3. AND THE CHROME STOPS NAMING THE MACHINE IN THE SAME BREATH. This is
	//    the whole defect: a status line still reading `@ghost` over a bare
	//    request is the chrome promising a machine the wire was never asked for.
	if still := PinnedFor(rig.model); still != "" {
		t.Fatalf("the chrome would still say @%s after the wire retired the pin", still)
	}

	// 4. AND THE NEXT REQUEST GOES BARE rather than buying the same 404 again.
	if _, err := rig.client.CompleteWithMessages(turn, userMessages("again")); err != nil {
		t.Fatalf("the second turn failed: %v", err)
	}
	asks = rig.server.Asks()
	if len(asks) <= before {
		t.Fatal("the second turn sent nothing")
	}
	for _, later := range asks[before:] {
		if len(later.Only) != 0 || later.Sort != "" || len(later.Order) != 0 || later.MaxPrice != nil {
			t.Fatalf("a request after the retirement carried %+v, want a bare request", later)
		}
	}
	if said := notes.retired(); len(said) != 1 {
		t.Fatalf("the conversation was told %d times over two turns: %q", len(said), said)
	}
}

// AND A PIN THIS PROCESS ALREADY BELIEVES THE ACCOUNT EXCLUDES IS STILL SENT.
//
// The store is a belief carried between processes — `account-exclusions.json`,
// written a day at a time — and under every other row it is allowed to retire a
// strict pin before the call (lanes.go). Under `simple` it may not: the row's
// whole promise is that what a person wrote is what goes out, and a pin retired
// on a saved belief is a pin the person would watch fail with no request ever
// made. One refused round trip a run is what honesty costs here, and the
// sentence they read afterwards is the same one.
func TestSimpleRoutingStillAsksForAPinTheSavedExclusionsCover(t *testing.T) {
	rig := newLaneRig(t, "simple/pin-excluded", retiredLanes()...)
	forgotten(t)
	rig.client.config.Routing = StaticRouting(RoutingSimple)
	// The state the live run started in: a previous process wrote the machine
	// out of every model's serving set on the router's own sentence.
	lanes.ExcludeForAccount("Ghost", "the account's own settings exclude it")
	t.Cleanup(func() { lanes.ClearAccountExclusion("Ghost") })
	if !lanes.AccountExcludes("Ghost") {
		t.Fatal("the fixture did not write the exclusion it is about")
	}
	pinned(t, LanePin{Lane: "Ghost"})
	notes := &noticeLog{}
	turn := WithStreamObserver(talking(), notes.observe)

	if _, err := rig.client.CompleteWithMessages(turn, userMessages("hello")); err != nil {
		t.Fatalf("the turn failed: %v", err)
	}
	asks := rig.server.Asks()
	if len(asks) == 0 || !demandedOnly(asks[0], "Ghost") {
		t.Fatalf("the first ask carried %+v, want the pin sent to the one authority on it", asks)
	}
	if said := notes.retired(); len(said) != 1 {
		t.Fatalf("the conversation was told %d times about its own row, want exactly once: %q", len(said), said)
	}
	if still := PinnedFor(rig.model); still != "" {
		t.Fatalf("the chrome would still say @%s over a request that no longer asks for it", still)
	}
}

// installedRow states the routing row this process's unhanded clients answer to
// for the length of one test and puts it back afterwards — the knob is
// process-wide (velocity.go), so a test that left one set would route every
// test after it.
func installedRow(t *testing.T, strategy RoutingStrategy) {
	t.Helper()
	before, held := installedRoutingChoice()
	if !held {
		before = ""
	}
	InstallRouting(strategy)
	t.Cleanup(func() { InstallRouting(before) })
}

// ── THE ROW REACHES THE CLIENTS NOBODY HANDS ONE TO ─────────────────────────
//
// THE MEASURED FAILURE (2026-09-13). `internal/session` hands its own clients
// the row; every other client in the binary is assembled through
// [config.Config.ClientConfig], which carried no routing answer at all — the
// harness, the subharness, `read_document`, `view_image`, a panel's members.
// Each ran the ranked road whatever a person had written, and the ranked road
// may stand a strict pin down on a SAVED belief before any wire is asked
// (lanes.go's account-exclusion block). The retirement is process-wide, so a
// conversation running `simple` and doing everything right found its own pin
// already retired, went out bare, and left the chrome naming a machine the
// request had never asked for.
func TestAClientHandedNoRowAnswersTheOneThisProcessInstalled(t *testing.T) {
	server := prefRig(t)
	client := plainBase(t, server.URL(), server)
	installedRow(t, RoutingSimple)

	// A client handed nothing reads the installed row.
	client.config.Routing = nil
	if strategy := client.routingFor(IntentInteractive); strategy != RoutingSimple {
		t.Fatalf("a client handed no row routes on %q, want the row this process installed", strategy)
	}
	// And so does one handed a source that says nothing, which is how a session
	// with an unwritten row hands its answer down.
	client.config.Routing = StaticRouting("")
	if strategy := client.routingFor(IntentInteractive); strategy != RoutingSimple {
		t.Fatalf("a client handed an empty source routes on %q, want the installed row", strategy)
	}
	// A CALLER THAT SPOKE STILL WINS. An engine host and a task child carry
	// their parent's row explicitly, and a process-wide fallback may not
	// overrule somebody who said a word.
	client.config.Routing = StaticRouting(RoutingPrice)
	if strategy := client.routingFor(IntentInteractive); strategy != RoutingPrice {
		t.Fatalf("a client handed price routes on %q, want the answer it was handed", strategy)
	}
	// And with nothing installed and nothing handed down, every call takes the
	// row this build ships, whoever is waiting on it. It used to move with who
	// was waiting — price for an errand, latency for a person's own turn — and
	// that guess is what the shipped row replaces (velocity.go's
	// [DefaultRouting]).
	InstallRouting("")
	client.config.Routing = nil
	for _, intent := range []RoutingIntent{IntentBackground, IntentInteractive} {
		if strategy := client.routingFor(intent); strategy != DefaultRouting {
			t.Fatalf("an unwritten row routes intent %v on %q, want the shipped %q",
				intent, strategy, DefaultRouting)
		}
	}
}

// AND UNDER `simple` NOTHING STANDS A PIN DOWN ON A SAVED BELIEF.
//
// This is the defect above stated as the behaviour that closes it: with the row
// installed, the client that used to run the ranked road runs `simple` like
// every other, so the machine a person named is demanded and the wire is left
// to be the authority on whether the account can reach it. The contrast is the
// same draw with no row installed, which is the ranked road doing exactly what
// it is supposed to do.
func TestSimpleRoutingLeavesAPinTheRankedRoadWouldHaveStoodDown(t *testing.T) {
	rig := newLaneRig(t, "simple/pin-not-stood-down", retiredLanes()...)
	forgotten(t)
	lanes.ExcludeForAccount("Ghost", "the account's own settings exclude it")
	t.Cleanup(func() { lanes.ClearAccountExclusion("Ghost") })
	pinned(t, LanePin{Lane: "Ghost"})
	request := &ai.Request{Model: rig.model, Messages: userMessages("hello")}

	// THE RANKED ROAD, WHICH IS THE CONTRAST. It is NAMED rather than left to a
	// default: `latency` is the road where the saved exclusion stands the pin
	// down before the wire is asked, and the row nobody writes is `simple` now.
	installedRow(t, RoutingLatency)
	rig.client.config.Routing = nil
	if choice, made := rig.client.drawLaneChoice(turnKnobs(), rig.model, request); made && len(choice.Only) > 0 {
		t.Fatalf("the ranked road demanded %v for a machine the account excludes", choice.Only)
	}
	if !pinRetired("Ghost", rig.model) {
		t.Fatal("the ranked road did not stand the pin down, so this test is not about the contrast it names")
	}

	// AND `simple`, INSTALLED THE WAY THE SURFACE INSTALLS IT. The same client,
	// handed nothing, now demands the machine the person wrote.
	forgetRetiredPins()
	installedRow(t, RoutingSimple)
	choice, made := rig.client.drawLaneChoice(turnKnobs(), rig.model, request)
	if !made || len(choice.Only) != 1 || !strings.EqualFold(choice.Only[0], "Ghost") {
		t.Fatalf("under simple the draw came out %+v, want the one machine the person pinned", choice)
	}
	if pinRetired("Ghost", rig.model) {
		t.Fatal("under simple a saved belief stood the person's pin down with no wire asked")
	}
	if PinnedFor(rig.model) == "" {
		t.Fatal("the chrome would say nothing over a request that demands the machine")
	}
}

// turnKnobs is one call made for the person's own turn, which is the scope the
// `lane.talk` row governs. It is spelled here rather than [callKnobs] with its
// zero role because a zero role reads as a hidden background errand
// (internal/lane's RoleUnknown) and a draw asked about one of those is a draw
// asked the other question.
func turnKnobs() callKnobs { return callKnobs{role: lanes.RoleTalk} }

// ── THE ROW GOVERNS THE CALLS IT IS NAMED FOR ───────────────────────────────
//
// THE MEASURED FAILURE (2026-09-13, a real drive). One turn under `simple`,
// pinned to a machine the account excludes, paid THREE 404s: the turn on the
// chat model, the naming errand on the reflex tier's own model, and the memory
// reflex on a third model the pinned machine does not serve at all. A pairing
// is retired once, so each new model an errand reaches for is a fresh refused
// round trip — bought for calls nobody is reading, on machines nobody pinned.
//
// The row is `lane.talk` and the slot is the whole scope: the person's turn
// carries the demand and the errands beside it go bare.
func TestUnderSimpleOnlyThePersonsOwnTurnCarriesTheTalkPin(t *testing.T) {
	rig := newLaneRig(t, "simple/errands-go-bare", retiredLanes()...)
	forgotten(t)
	rig.client.config.Routing = StaticRouting(RoutingSimple)
	pinned(t, LanePin{Lane: "Ghost"})
	request := &ai.Request{Model: rig.model, Messages: userMessages("hello")}

	// EVERY ROLE A PERSON IS READING CARRIES THE DEMAND: the conversation's own
	// turn, a task room somebody is sitting in front of, and the headless
	// command they typed (cmd/aforge's execCallContext, which names the second
	// of these).
	for _, watched := range []lanes.Role{lanes.RoleTalk, lanes.RoleLeafAttached} {
		knobs := callKnobs{role: watched}
		choice, made := rig.client.drawLaneChoice(knobs, rig.model, request)
		if !made || len(choice.Only) != 1 || !strings.EqualFold(choice.Only[0], "Ghost") {
			t.Fatalf("a %q call drew %+v, want the one machine the person pinned", watched, choice)
		}
	}
	// EVERY ERRAND THE TURN RUNS BESIDE ITSELF. The roles are internal/lane's
	// own words for them, and the point of listing all four is that the scope is
	// read from the table rather than from a list kept here: whatever else is
	// added to that table arrives already answered.
	for _, errand := range []lanes.Role{
		lanes.RoleAuxiliary, lanes.RoleMemory, lanes.RoleRecall,
		lanes.RoleTool, lanes.RoleJudge, lanes.RoleLeafUnattended, lanes.RoleUnknown,
	} {
		knobs := callKnobs{role: errand}
		if choice, made := rig.client.drawLaneChoice(knobs, rig.model, request); made {
			t.Fatalf("a %q errand drew %+v, want no preference at all", errand, choice)
		}
	}
}

// ── THE ROW SAYS WHAT THE RETRY REALLY CARRIED ──────────────────────────────
//
// THE MEASURED ROW (2026-09-13). A pin was refused, retired, and the widened
// retry went out with no `provider` key at all — and its start row read
// `"lane":"DeepSeek","served":"DeepSeek","relaxed":["provider.require_parameters"]`.
// Every word of that is about the request before it. The lane and the served
// machine name the one machine that had just refused to answer; the relaxed
// list names a field a `simple` request never sends, while the field that
// actually came off was the demand.
func TestTheWidenedRetryOfARetiredPinIsLoggedAsTheBareRequestItIs(t *testing.T) {
	read := loggingTo(t)
	rig := newLaneRig(t, "simple/retry-row", retiredLanes()...)
	forgotten(t)
	rig.client.config.Routing = StaticRouting(RoutingSimple)
	pinned(t, LanePin{Lane: "Ghost"})

	if _, err := rig.client.CompleteWithMessages(talking(), userMessages("hello")); err != nil {
		t.Fatalf("the turn died on a refusal the widened retry was supposed to absorb: %v", err)
	}

	rows := read()
	var demand *calllog.Record
	var widened []calllog.Record
	for _, record := range rows {
		row := record
		switch {
		case row.Status == http.StatusNotFound && demand == nil:
			demand = &row
		case demand != nil && row.Relaxed != nil:
			widened = append(widened, row)
		}
	}
	// BOTH ROWS OF THE RETRY, and both of them on purpose: the start row and
	// the row that ends it are written from different hands, and until this
	// wave the second was built from the knobs the CALLER was still holding
	// rather than from the body that travelled (calllog.go's bodyKnobs).
	if demand == nil || len(widened) < 2 {
		t.Fatalf("the log holds no refused ask and no pair of widened rows to compare: %+v", rows)
	}
	// THE REFUSED ASK IS UNCHANGED. It really did demand the machine, so it
	// really does name it — this half is the control on the other.
	if !strings.EqualFold(demand.Lane, "Ghost") {
		t.Fatalf("the refused ask names lane %q, want the machine it demanded", demand.Lane)
	}
	for _, row := range widened {
		if row.Lane != "" {
			t.Fatalf("a row about the bare retry names lane %q, want no machine: the body asked for none", row.Lane)
		}
		if strings.EqualFold(row.Served, "Ghost") {
			t.Fatalf("a row about the bare retry says %q served it, and %q had refused the request before it",
				row.Served, row.Served)
		}
		if !slices.Contains(row.Relaxed, "provider.only") {
			t.Fatalf("the bare retry says it relaxed %v, want the demand it actually withdrew", row.Relaxed)
		}
		if slices.Contains(row.Relaxed, "provider.require_parameters") {
			t.Fatalf("the bare retry says it dropped %v, and a simple request never sent require_parameters",
				row.Relaxed)
		}
	}
}

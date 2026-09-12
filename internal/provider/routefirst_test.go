package provider

import (
	"net/http"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE GATE, MEASURED ──────────────────────────────────────────────────────
//
// The gate is the whole of the new `auto`: when it says the router is serving,
// no chooser runs and the router routes. These scenarios pin the three numbers
// a person would argue about — two strikes, not one; half an hour, not forever;
// a good answer counts back down, not up — because they are the difference
// between insurance and a second opinion nobody asked for.

func TestOneRefusalIsAQueueDrainingTwoIsATakeover(t *testing.T) {
	forgetRouterGates()
	t.Cleanup(forgetRouterGates)
	now := time.Now()

	gate := gateFor("openrouter/any-model")
	if gate.NoteRefused(now) {
		t.Fatal("one refusal armed the takeover; one is a queue draining somewhere")
	}
	if gate.TakenOver(now) {
		t.Fatal("one refusal and the gate reads as taken over")
	}
	if !gate.NoteRefused(now.Add(time.Minute)) {
		t.Fatal("the second refusal inside the window did not arm the takeover")
	}
	if !gate.TakenOver(now.Add(time.Minute)) {
		t.Fatal("two refusals and the gate still lends the router the road")
	}
}

func TestATakeoverLapsesOnItsOwnClock(t *testing.T) {
	forgetRouterGates()
	t.Cleanup(forgetRouterGates)
	now := time.Now()

	gate := gateFor("openrouter/any-model")
	gate.NoteRefused(now)
	gate.NoteRefused(now.Add(time.Minute))
	armed := now.Add(time.Minute)
	if !gate.TakenOver(armed) {
		t.Fatal("the takeover never armed")
	}
	if gate.TakenOver(armed.Add(takeoverWindow + time.Minute)) {
		t.Fatal("the window lapsed and the chooser still holds the road")
	}
}

func TestAHealthyAnswerCountsBackDown(t *testing.T) {
	forgetRouterGates()
	t.Cleanup(forgetRouterGates)
	now := time.Now()

	gate := gateFor("openrouter/any-model")
	gate.NoteRefused(now)
	// A refusal, then a whole answer: the queue drained. A second refusal
	// after it is the first of a new pair, not the second of the old one.
	gate.NoteHealthy(now.Add(time.Minute))
	if gate.NoteRefused(now.Add(2 * time.Minute)) {
		t.Fatal("a healthy answer between the refusals still armed the takeover")
	}
}

func TestTwoUnusableAnswersArmItToo(t *testing.T) {
	forgetRouterGates()
	t.Cleanup(forgetRouterGates)
	now := time.Now()

	gate := gateFor("openrouter/any-model")
	if gate.NoteBad(now) {
		t.Fatal("one bad answer armed the takeover")
	}
	if !gate.NoteBad(now.Add(time.Minute)) {
		t.Fatal("two answers that could not be used did not arm the takeover")
	}
}

func TestAnArmedGateEarnsItsSentenceOnce(t *testing.T) {
	forgetRouterGates()
	t.Cleanup(forgetRouterGates)
	now := time.Now()

	gate := gateFor("openrouter/any-model")
	gate.NoteRefused(now)
	if !gate.NoteRefused(now.Add(time.Minute)) {
		t.Fatal("the arming refusal answered false")
	}
	// Every later refusal inside the window is the same fact: the sentence is
	// owed once, and the retry that follows the arming call must not say it
	// again.
	if gate.NoteRefused(now.Add(2 * time.Minute)) {
		t.Fatal("a second refusal inside the window parked a second sentence")
	}
}

func TestAStaleStrikeIsForgotten(t *testing.T) {
	forgetRouterGates()
	t.Cleanup(forgetRouterGates)
	now := time.Now()

	gate := gateFor("openrouter/any-model")
	gate.NoteRefused(now)
	// A refusal an hour ago is not evidence about this afternoon.
	if gate.NoteRefused(now.Add(time.Hour)) {
		t.Fatal("a refusal beside an hour-old one armed the takeover; the window is not being pruned")
	}
}

func TestTheGateHandsBackACleanSlate(t *testing.T) {
	forgetRouterGates()
	t.Cleanup(forgetRouterGates)
	now := time.Now()

	gate := gateFor("openrouter/any-model")
	gate.NoteRefused(now)
	gate.NoteRefused(now.Add(time.Minute))
	// A takeover is thirty minutes; a strike is remembered for five. So the
	// strikes that armed it — and any the chooser collected while it held the
	// road — are all pruned before the window lapses, and the router's first
	// fresh refusal afterwards is one strike, not the second of a pair. The
	// 6× ratio between the two windows is what keeps the hand-back from
	// flapping; this scenario is why it may never shrink.
	lapse := now.Add(time.Minute).Add(takeoverWindow + time.Minute)
	if gate.TakenOver(lapse) {
		t.Fatal("the window lapsed and the gate still holds the road")
	}
	if gate.NoteRefused(lapse) {
		t.Fatal("the router's first refusal after a lapse re-armed the takeover; the old strikes were not pruned")
	}
}

func TestTheTwoModelsKeepTwoGates(t *testing.T) {
	forgetRouterGates()
	t.Cleanup(forgetRouterGates)
	now := time.Now()

	one := gateFor("openrouter/one-model")
	two := gateFor("openrouter/another-model")
	one.NoteRefused(now)
	one.NoteRefused(now.Add(time.Minute))
	if two.TakenOver(now.Add(time.Minute)) {
		t.Fatal("one model's bad afternoon took the other model's road with it")
	}
}

// ── THE SHAPE-AWARE REFUSAL DOOR ────────────────────────────────────────────
//
// The measured loop (live chat, 2026-09-12): a takeover armed; the chooser's
// narrowed demand answered 404; the gate counted THAT refusal, arming the
// road again; four "Retry 1/2: relaxed the endpoint filter" lines, each paying
// a full ~80k-token re-send, before the first rung went bare. The tests below
// pin the split: only a BARE attempt's refusal is evidence about the router's
// default, and the classifier reads the attempt's shape from the wire object
// the request actually went out with.

func TestANarrowedRefusalIsAboutOurChoiceNotTheRouter(t *testing.T) {
	forgetRouterGates()
	t.Cleanup(forgetRouterGates)
	client := &Client{config: Config{BaseURL: "https://openrouter.ai/api/v1"}}
	lanes.HeardPrefsCarried(client.config.BaseURL)
	const model = "openrouter/shape-gate"
	for range takeoverStrikes * 2 {
		if client.noteRouterRefusal(model, "brass", false) {
			t.Fatal("a refusal of our own demand claimed takeover")
		}
	}
	if gateFor(model).TakenOver(time.Now()) {
		t.Fatal("narrowed refusals took the road from the router")
	}
}

func TestBareRefusalsArmTheTakeover(t *testing.T) {
	forgetRouterGates()
	t.Cleanup(forgetRouterGates)
	client := &Client{config: Config{BaseURL: "https://openrouter.ai/api/v1"}}
	lanes.HeardPrefsCarried(client.config.BaseURL)
	const model = "openrouter/shape-gate"
	if client.noteRouterRefusal(model, "brass", true) {
		t.Fatal("one bare refusal armed the takeover; one is a queue draining")
	}
	if !client.noteRouterRefusal(model, "brass", true) {
		t.Fatal("the second bare refusal did not arm the takeover")
	}
	if !client.routingGateTakenOver(model) {
		t.Fatal("two bare refusals and the gate still lends the router the road")
	}
}

func TestANarrowedRefusalCannotHoldTheRoad(t *testing.T) {
	forgetRouterGates()
	t.Cleanup(forgetRouterGates)
	client := &Client{config: Config{BaseURL: "https://openrouter.ai/api/v1"}}
	lanes.HeardPrefsCarried(client.config.BaseURL)
	const model = "openrouter/shape-gate"
	if client.noteRouterRefusal(model, "brass", true) {
		t.Fatal("setup: one bare refusal armed")
	}
	if !client.noteRouterRefusal(model, "brass", true) {
		t.Fatal("setup: two bare refusals did not arm")
	}
	gate := gateFor(model)
	gate.mu.Lock()
	held := gate.until
	gate.mu.Unlock()
	if client.noteRouterRefusal(model, "brass", false) {
		t.Fatal("a narrowed refusal re-armed a held takeover")
	}
	gate.mu.Lock()
	after := gate.until
	gate.mu.Unlock()
	if !after.Equal(held) {
		t.Fatalf("a narrowed refusal moved the takeover's lapse from %v to %v", held, after)
	}
	if !client.routingGateTakenOver(model) {
		t.Fatal("an armed takeover vanished under a narrowed refusal")
	}
}

func TestTheThresholdCountsOnlyBareRefusals(t *testing.T) {
	forgetRouterGates()
	t.Cleanup(forgetRouterGates)
	client := &Client{config: Config{BaseURL: "https://openrouter.ai/api/v1"}}
	lanes.HeardPrefsCarried(client.config.BaseURL)
	const model = "openrouter/shape-gate"
	if client.noteRouterRefusal(model, "one", true) {
		t.Fatal("one bare refusal armed")
	}
	if client.noteRouterRefusal(model, "two", false) {
		t.Fatal("a narrowed refusal counted toward the bare threshold")
	}
	if !client.noteRouterRefusal(model, "three", true) {
		t.Fatal("the SECOND bare refusal did not finish the arming")
	}
	if !client.routingGateTakenOver(model) {
		t.Fatal("bare, narrowed, bare did not arm at exactly two bares")
	}
}

func TestTheClassifierSpeaksTheAttemptsShape(t *testing.T) {
	forgetRouterGates()
	t.Cleanup(forgetRouterGates)
	ledger, _ := testLedger()
	client := &Client{config: Config{BaseURL: "https://openrouter.ai/api/v1"}, velocity: ledger}
	lanes.HeardPrefsCarried(client.config.BaseURL)
	const model = "openrouter/shape-gate"
	paced := apiError(http.StatusTooManyRequests, []byte(
		`{"error":{"message":"Provider returned error"}}`))

	bare := client.refusalObject(&ai.Request{Model: model}, callKnobs{}, paced)
	if !bare.AskedBare {
		t.Fatal("a refusal with no membership-narrowing preference on the wire said the attempt was narrowed")
	}
	narrowed := client.refusalObject(&ai.Request{Model: model}, callKnobs{hedgeLane: "brass"}, paced)
	if narrowed.AskedBare {
		t.Fatal("a refusal of a rescue's one-machine demand said the attempt went out bare")
	}
}

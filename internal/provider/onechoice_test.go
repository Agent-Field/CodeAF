package provider

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// ── ONE CHOICE PER CALL, STREAMED OR NOT ────────────────────────────────────
//
// A lane choice is a SAMPLED decision — `internal/lane`'s seed mixes in the
// moment it is asked at — and one call encodes its request many times: once per
// attempt, once for each rung of the relaxation ladder, once more for the
// object the refusal door re-derives. While the encoder was free to draw one of
// its own, each of those encodes could be a different request to a different set
// of machines, and nothing above the encoder could name the set: the plan reads
// the call's choice ([requestSet]) and a choice the encoder kept to itself left
// the move generator walking a set the wire had never carried.
//
// BOTH SCENARIOS BELOW STAGE ONE FACT: THE BELIEF ARRIVES WHILE THE CALL IS IN
// FLIGHT. That is not a contrivance — it is the ordinary life of a call. The
// lane beat primes the ledger from the endpoints page on its own clock, and the
// call's own refusals are written into the belief as they happen
// (lanes.go's noteLaneRefused). A call that began knowing nothing about a model
// therefore very often knows something about it two attempts later, and the
// question this file settles is whether that changes the request already in
// flight. It must not: the watch, the plan and the body agree because there is
// ONE choice, drawn once, at the top of the call.

// movingLedger is a ledger that learns two lanes WHILE A CALL IS RUNNING. Cold
// it holds one belief, which is nothing to rank (internal/lane refuses to make
// a choice out of fewer than two); warm it holds three, which is a ranking.
//
// It is warmed by [warmingTransport] — the call's first request leaving — and
// not after a counted number of reads, because a read count is a claim about how
// many times the code under test happens to compose a preference object, which
// is the thing these scenarios must not depend on.
type movingLedger struct {
	mu   sync.Mutex
	warm bool
	cold []lanes.Belief
	hot  []lanes.Belief
}

func (l *movingLedger) Note(lanes.Sighting)       {}
func (l *movingLedger) NoteOutcome(lanes.Outcome) {}
func (l *movingLedger) Prime(lanes.Row, float64)  {}

func (l *movingLedger) warmUp() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warm = true
}

// warmed is the same field read the same way it is written. A scenario asks it
// after the call has ended, but the transport that warmed it ran on another
// goroutine, so an unguarded read here is a data race the detector is right
// about however settled the value looks.
func (l *movingLedger) warmed() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.warm
}

func (l *movingLedger) held() []lanes.Belief {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.warm {
		return l.hot
	}
	return l.cold
}

func (l *movingLedger) Belief(id lanes.ID) (lanes.Belief, bool) {
	for _, belief := range l.held() {
		if belief.ID == id {
			return belief, true
		}
	}
	return lanes.Belief{}, false
}

func (l *movingLedger) Beliefs(model string) []lanes.Belief {
	var found []lanes.Belief
	for _, belief := range l.held() {
		if belief.ID.Model == model {
			found = append(found, belief)
		}
	}
	return found
}

// refusingRouter is the rig both scenarios run on: three machines that all
// answer 500, so the call keeps its shape and keeps re-encoding it, and a
// ledger that warms up between the first attempt and the second.
func refusingRouter(t *testing.T) (*Client, *lanestub.Server, *movingLedger, string) {
	t.Helper()
	const model = "openrouter/scripted-model"
	healthy := lanestub.Profile{TTFT: time.Millisecond, Rate: 400, Tokens: 8, Tools: true}
	server := lanestub.New(model,
		lanestub.Lane{Name: "quicksilver", Profile: healthy},
		lanestub.Lane{Name: "brass", Profile: healthy},
		lanestub.Lane{Name: "molasses", Profile: healthy},
	)
	// A ROUTER HAVING AN AFTERNOON, WHICH NAMES NOBODY. It is the fault that
	// leaves the set alone: every attempt re-encodes the same request to the same
	// machines, so what differs between two bodies of this call is only what the
	// build decided to ask for — which is the whole of what is being measured. A
	// refusal that named a machine would strike it, and the bodies would then
	// differ for a reason that is nothing to do with the choice.
	server.RefusesWith(http.StatusInternalServerError, "the router had a moment", "")
	t.Cleanup(server.Close)
	ledger := &movingLedger{
		cold: []lanes.Belief{laneBelief(model, "quicksilver", 400, 70, 0.25)},
		hot: []lanes.Belief{
			laneBelief(model, "quicksilver", 400, 70, 0.25),
			laneBelief(model, "brass", 900, 60, 0.30),
			laneBelief(model, "molasses", 1500, 40, 0.20),
		},
	}
	client, err := NewClient(Config{
		APIKey:     "test-key",
		BaseURL:    server.URL(),
		Model:      model,
		Routing:    StaticRouting(RoutingLatency),
		HTTPClient: &http.Client{Transport: warmingTransport{inner: http.DefaultTransport, ledger: ledger}},
	})
	if err != nil {
		t.Fatal(err)
	}
	lanes.HeardPrefsCarried(server.URL())
	client.velocity = newVelocityLedger()
	client.wait = func(context.Context, time.Duration) error { return nil }
	lanes.Default().SetLedger(ledger)
	lanes.ForgetPrefixes()
	t.Cleanup(func() {
		lanes.Default().Reset()
		lanes.ForgetPrefixes()
	})
	return client, server, ledger, model
}

// warmingTransport is the seam the belief arrives through: the first request of
// the call leaves, and by the time anything is encoded again the ledger holds a
// ranking it did not hold when the call began.
//
// IT IS THE REQUEST AND NOT A COUNTED READ. A read count would be a claim about
// how many times the code under test happens to compose a preference object,
// which is exactly the thing these scenarios must not depend on; a round trip is
// something the scenario itself can point at.
type warmingTransport struct {
	inner  http.RoundTripper
	ledger *movingLedger
}

func (w warmingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := w.inner.RoundTrip(request)
	if strings.Contains(request.URL.Path, "/chat/completions") {
		w.ledger.warmUp()
	}
	return response, err
}

// namesTheSameMachines reports whether every body of one call asked for the same
// set of machines. It is the whole assertion of this file, said once.
//
// IT IS ABOUT THE BODIES THE PRIMARY SENDS, and both scenarios here have only a
// primary: a cold choice names no alternative, and the warm one cannot walk
// because nothing serves this model on a sheet the rig never publishes. A RESCUE
// ARM IS A DELIBERATE EXCEPTION to "one call, one set" — [hedgePreference]
// rewrites an arm's body to `Only=[its own lane]`, because an arm IS the machine
// it demanded — so if an arm ever fired here this would fail for a correct
// reason. The single arm is staged by omission rather than by construction, and
// saying so is cheaper than a scenario that guarantees it.
func namesTheSameMachines(t *testing.T, asks []lanestub.Ask) {
	t.Helper()
	if len(asks) < 2 {
		t.Fatalf("the call sent %d requests; this scenario needs at least two encodes to say anything", len(asks))
	}
	first := asks[0]
	for index, ask := range asks[1:] {
		if !sameNames(first.Order, ask.Order) || !sameNames(first.Only, ask.Only) {
			t.Fatalf("body 1 asked for order=%v only=%v and body %d asked for order=%v only=%v — "+
				"one call drew two different sets of machines, so the plan above it was walking a set the wire never named",
				first.Order, first.Only, index+2, ask.Order, ask.Only)
		}
	}
}

func sameNames(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

// TestOneCallAsksForOneSetOfMachinesHoweverOftenItIsEncoded is the streamed
// door's half. The choice is drawn before the first byte leaves and every
// re-encode under it reads that one answer — including the encodes that happen
// after the belief has changed underneath.
//
// THE BELIEF ARRIVING MID-CALL IS DELIBERATELY NOT ACTED ON. A call that began
// with nothing to prefer finishes with nothing to prefer; the next call is where
// the new belief is spent. That is the price of the watch, the plan and the body
// being one decision, and it is the right price: a ranking that appears on the
// third body of a call is a set nothing above the encoder has ever heard of.
func TestOneCallAsksForOneSetOfMachinesHoweverOftenItIsEncoded(t *testing.T) {
	client, server, ledger, _ := refusingRouter(t)

	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err == nil {
		t.Fatal("three machines answering 500 produced an answer")
	}
	if !ledger.warmed() {
		t.Fatal("the belief never warmed, so this scenario staged nothing")
	}
	namesTheSameMachines(t, server.Asks())
}

// TestACallThatHadABeliefAsksForItOnEveryBody is the other half of the same law
// and the reason the two are written together: "one choice per call" must not
// have been bought by sending no choice at all.
//
// The belief is there before the first byte leaves, so it reaches the wire on the
// first body — and on the tenth, unchanged, however many times the request has
// been re-encoded underneath.
func TestACallThatHadABeliefAsksForItOnEveryBody(t *testing.T) {
	client, server, ledger, _ := refusingRouter(t)
	ledger.warmUp()

	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err == nil {
		t.Fatal("a router answering 500 to everything produced an answer")
	}
	asks := server.Asks()
	namesTheSameMachines(t, asks)
	if len(asks[0].Order) == 0 {
		t.Fatalf("the belief ranked three machines and the wire asked for none: %+v", asks[0])
	}
}

// TestTheRawStreamDoorAlsoDecidesItsLaneOnce is the other door. Nothing about
// `provider.order` is the streamed loop's property: a call that reaches the wire
// through [Client.StreamComplete] is a call with a plan, a deadline and a set of
// machines exactly like any other, and it used to be the one shape that drew a
// fresh set inside every encode because nobody had stamped one for it.
func TestTheRawStreamDoorAlsoDecidesItsLaneOnce(t *testing.T) {
	client, server, ledger, _ := refusingRouter(t)

	chunks, errs := client.StreamComplete(context.Background(), "hello")
	for chunks != nil || errs != nil {
		select {
		case _, open := <-chunks:
			if !open {
				chunks = nil
			}
		case _, open := <-errs:
			if !open {
				errs = nil
			}
		}
	}
	if !ledger.warmed() {
		t.Fatal("the belief never warmed, so this scenario staged nothing")
	}
	namesTheSameMachines(t, server.Asks())
}

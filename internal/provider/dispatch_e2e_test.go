package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
)

// ── ONE DEADLINE BOUNDS EVERYTHING ──────────────────────────────────────────
//
// docs/design/recovery/DESIGN.md §7's acceptance, said as a scenario rather than
// as an argument. The worst case of the eleven controllers was a product nobody
// could state — 3 models × 4 attempts × 5 arms × 6 paced sends × 9 rungs — and
// what replaced it is one figure a person could be told: `lane.Role.GiveUp`.
// These two scenarios are what that has to mean on the wire.

// scriptedPool is a router whose machines each behave the way one row of the
// census behaves: a rate limit, an account policy refusal, a server error, or an
// answer. It records the lane and the exclusion list of every send, because the
// claims under test are about counts and about who was asked.
type scriptedPool struct {
	mu sync.Mutex
	// roster is every machine behind the model, in the order the router would
	// fall through them.
	roster []string
	// script is what each machine does. A machine missing from it answers.
	script map[string]int
	// asked is the lane each send reached, in order.
	asked []string
	// vetoed is the `provider.ignore` each send carried.
	vetoed [][]string
}

func (p *scriptedPool) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	body := make([]byte, 1<<16)
	read, _ := request.Body.Read(body)
	var wire wirePrefs
	_ = json.Unmarshal(body[:read], &wire)

	p.mu.Lock()
	vetoed := map[string]bool{}
	for _, name := range wire.Provider.Ignore {
		vetoed[strings.ToLower(name)] = true
	}
	allowed := p.roster
	if len(wire.Provider.Only) > 0 {
		allowed = wire.Provider.Only
	}
	lane := ""
	for _, name := range allowed {
		if !vetoed[strings.ToLower(name)] {
			lane = name
			break
		}
	}
	p.asked = append(p.asked, lane)
	p.vetoed = append(p.vetoed, append([]string(nil), wire.Provider.Ignore...))
	status := 0
	if lane != "" {
		status = p.script[lane]
	}
	p.mu.Unlock()

	if lane == "" {
		writer.WriteHeader(http.StatusNotFound)
		_, _ = writer.Write([]byte(`{"error":{"message":"No allowed providers are available for the selected model."}}`))
		return
	}
	switch status {
	case 0:
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer,
			`{"model":"openrouter/pool","provider":%q,"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`,
			lane)
	case http.StatusTooManyRequests:
		writer.WriteHeader(status)
		_, _ = fmt.Fprintf(writer,
			`{"error":{"message":"Provider returned error","metadata":{"provider_name":%q}}}`, lane)
	default:
		writer.WriteHeader(status)
		_, _ = fmt.Fprintf(writer,
			`{"error":{"message":"upstream said no","metadata":{"provider_name":%q}}}`, lane)
	}
}

func (p *scriptedPool) sends() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.asked...)
}

// TestOneDeadlineBoundsEverything is the acceptance of
// docs/design/recovery/DESIGN.md §7: over three hundred scripted pools, mixed
// however the census mixes them, NO CALL OUTLIVES ITS PLAN'S DEADLINE BY MORE
// THAN ONE [maxProviderWait] — the longest single wait this build will ever
// take — and no call sends more than its own machines and shapes can account for.
//
// THE CLOCK IS THE FICTION THE TEST STATES. [Client.wait] is stubbed, so the
// dispatcher charges itself for every wait it ASKED for (dispatch.go's `owed`)
// and the whole ladder runs in microseconds while believing it spent minutes.
// That is the only honest way to test a deadline: the alternative is a suite
// that really waits four and a half minutes to find out.
func TestOneDeadlineBoundsEverything(t *testing.T) {
	seed := rand.New(rand.NewSource(20260911))
	behaviours := []int{
		http.StatusTooManyRequests,
		http.StatusNotFound,
		http.StatusInternalServerError,
		http.StatusBadGateway,
	}
	for trial := range 300 {
		machines := 1 + seed.Intn(6)
		pool := &scriptedPool{script: map[string]int{}}
		for index := range machines {
			name := fmt.Sprintf("Machine%d", index)
			pool.roster = append(pool.roster, name)
			// One pool in four has a healthy machine somewhere in it, which is
			// the case the walk is for; the rest are the pools that end spent.
			if seed.Intn(4) == 0 {
				continue
			}
			pool.script[name] = behaviours[seed.Intn(len(behaviours))]
		}

		client := poolClient(t, fmt.Sprintf("deadline-%d", trial), pool)
		var asked []time.Duration
		client.wait = func(_ context.Context, delay time.Duration) error {
			asked = append(asked, delay)
			return nil
		}
		ctx := WithLaneChoice(context.Background(), lanes.Choice{Order: pool.roster})

		began := time.Now()
		_, _ = client.CompleteWithMessages(ctx, userMessages("hello"))
		real := time.Since(began)

		// WHAT THE CALL BELIEVES IT SPENT is the real clock plus every wait the
		// seam above made free. `lane.RoleUnknown` is the role a context with no
		// role carries, and its give-up is the deadline this call ran under.
		var believed time.Duration
		for _, delay := range asked {
			believed += delay
		}
		believed += real
		bound := lanes.RoleUnknown.GiveUp() + maxProviderWait
		if believed > bound {
			t.Fatalf("trial %d ran %s against a %s deadline (one wait of slack is %s): waits %v",
				trial, believed, lanes.RoleUnknown.GiveUp(), maxProviderWait, asked)
		}

		// AND THE SENDS ARE ACCOUNTED FOR. Machines times shapes, plus the one
		// legal repeat — the bound `control.Next` is written to keep, checked
		// here against the wire rather than against the generator.
		// Shapes are the request as written plus every rung the ladder has, which
		// is the widest this particular request could possibly be reshaped.
		sends := pool.sends()
		shapes := 1 + len(relaxRungs)
		if limit := machines*shapes + 1; len(sends) > limit {
			t.Fatalf("trial %d made %d sends over %d machines and %d shapes, bound %d: %v",
				trial, len(sends), machines, shapes, limit, sends)
		}
	}
}

// TestTheScreenshotScenarioAnswersThroughTheFourthMachine is the run that opened
// this design (docs/design/recovery/DESIGN.md §1, the 2026-09-10 screenshot),
// staged: an account-policy 404, a machine that rate limits, a second that rate
// limits, and a healthy fourth the sheet doubts.
//
// WHAT IT ASSERTS IS WHAT THE PERSON GOT. Four sends, each machine asked once,
// an answer, an ordinal they could count in, and NOT ONE router sentence on the
// screen. On the binary this design was written against it was nine identical
// sends to one machine over three minutes and then a turn that ended.
func TestTheScreenshotScenarioAnswersThroughTheFourthMachine(t *testing.T) {
	pool := &scriptedPool{
		roster: []string{"DeepInfra", "Io Net", "Fireworks", "GMICloud"},
		script: map[string]int{
			"DeepInfra": http.StatusNotFound,
			"Io Net":    http.StatusTooManyRequests,
			"Fireworks": http.StatusTooManyRequests,
		},
	}
	client := poolClient(t, "screenshot", pool)
	var asked []time.Duration
	client.wait = func(_ context.Context, delay time.Duration) error {
		asked = append(asked, delay)
		return nil
	}

	told := &heard{}
	previous := OnPhase(told.take)
	t.Cleanup(func() { OnPhase(previous) })

	ctx := WithLaneChoice(context.Background(), lanes.Choice{Order: pool.roster})
	response, err := client.CompleteWithMessages(ctx, userMessages("hello"))
	if err != nil {
		t.Fatalf("a pool with one healthy machine did not answer: %v", err)
	}
	if response == nil {
		t.Fatal("the call answered with nothing at all")
	}

	sends := pool.sends()
	if len(sends) != 4 {
		t.Fatalf("the call made %d sends, want one to each machine: %v", len(sends), sends)
	}
	seen := map[string]int{}
	for _, lane := range sends {
		seen[lane]++
	}
	for lane, count := range seen {
		if count != 1 {
			t.Errorf("%s was asked %d times — a machine that refused is not asked again", lane, count)
		}
	}
	if sends[len(sends)-1] != "GMICloud" {
		t.Errorf("the answer came from %q, want the one healthy machine", sends[len(sends)-1])
	}

	// AND IT WAS FAST, because a move to another machine costs no wait at all.
	// The whole recovery is under fifteen seconds of believed time against a
	// ninety-second give-up, where the measured run took three minutes.
	var believed time.Duration
	for _, delay := range asked {
		believed += delay
	}
	if believed > 15*time.Second {
		t.Errorf("the walk paid %s of waiting for three moves to other machines: %v", believed, asked)
	}

	// AND THE PERSON READ AN ORDINAL AND NO ROUTER SENTENCE.
	moving, ok := told.find(PhaseRetrying)
	if !ok {
		t.Fatal("a call that walked three refusing machines told the person nothing")
	}
	if !strings.Contains(moving.Detail, " of ") {
		t.Fatalf("the phase read %q, want an ordinal a person can count in", moving.Detail)
	}
	for _, news := range told.all() {
		for _, machinery := range []string{"429", "404", "API error", "No allowed providers"} {
			if strings.Contains(news.Detail, machinery) {
				t.Fatalf("%q reached the person in %q", machinery, news.Detail)
			}
		}
	}
}

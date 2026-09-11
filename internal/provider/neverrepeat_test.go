package provider

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// ── NEVER REPEAT ────────────────────────────────────────────────────────────
//
// The census (docs/design/recovery/census-20260910.md) counted 226 of 428
// multi-attempt chains that never left the lane they started on, and three that
// spent sixteen and seventeen consecutive sends on one machine over eleven
// minutes. Every one of those was this transport re-sending bytes it had
// encoded once, above the attempt loop, with the same `provider.order` on them.
//
// These scenarios are the law from the other side: what a person gets is an
// answer from a machine that had not refused yet, and what the wire gets is a
// different request every time.

// poolHandler is a fake router that answers by the preference it is given and
// records what it was asked. It is deliberately not lanestub: these scenarios
// are about the BYTES of each attempt, so the handler keeps the body of every
// request beside the lane it chose, which is the pairing the law is about.
type poolHandler struct {
	mu sync.Mutex
	// refusing is the machines that answer 429 naming themselves; everything
	// else answers.
	refusing map[string]bool
	// asked is (lane, body digest) in the order the wire saw them.
	asked []asked
	// lanes is the whole roster, in the order the router would fall back through.
	lanes []string
	// advisory makes `order` what it really is on the wire once
	// `allow_fallbacks` is true: a RANKING the router may ignore. With it set,
	// this handler serves the first machine it is not forbidden to serve,
	// whatever was asked for first — which is the live shape of the 2026-09-10
	// 22:20 run, where every attempt asked for DeepInfra and Parasail answered
	// all eighteen of them with a 429.
	advisory bool
}

type asked struct {
	lane string
	body string
	// sent is the preference object this attempt really carried, which is the
	// thing the law is about: a ranking the router may ignore proves nothing, and
	// a veto is what actually takes a machine off the table.
	sent wirePrefs
}

// wirePrefs is the `provider` object as it went out.
type wirePrefs struct {
	Provider struct {
		Order  []string `json:"order"`
		Only   []string `json:"only"`
		Ignore []string `json:"ignore"`
	} `json:"provider"`
}

func (h *poolHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	body := make([]byte, 1<<16)
	read, _ := request.Body.Read(body)
	body = body[:read]
	var wire wirePrefs
	_ = json.Unmarshal(body, &wire)

	h.mu.Lock()
	lane := h.pickLocked(wire.Provider.Order, wire.Provider.Only, wire.Provider.Ignore)
	digest := fmt.Sprintf("%x", sha256.Sum256(body))
	h.asked = append(h.asked, asked{lane: lane, body: digest, sent: wire})
	refusing := lane == "" || h.refusing[lane]
	h.mu.Unlock()

	if lane == "" {
		// The router's own sentence when every machine it may use is vetoed.
		writer.WriteHeader(http.StatusNotFound)
		_, _ = writer.Write([]byte(`{"error":{"message":"No allowed providers are available for the selected model."}}`))
		return
	}
	if refusing {
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprintf(writer,
			`{"error":{"message":"Provider returned error","metadata":{"provider_name":%q}}}`, lane)
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(writer,
		`{"model":"openrouter/pool","provider":%q,"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`,
		lane)
}

// pickLocked honours the preference the way the router does: a demand is a
// demand, a veto is absolute, and an order is a ranking over what is left.
func (h *poolHandler) pickLocked(order, only, ignore []string) string {
	allowed := h.lanes
	if len(only) > 0 {
		allowed = only
	}
	vetoed := map[string]bool{}
	for _, name := range ignore {
		vetoed[strings.ToLower(name)] = true
	}
	open := make([]string, 0, len(allowed))
	for _, name := range allowed {
		if !vetoed[strings.ToLower(name)] {
			open = append(open, name)
		}
	}
	if len(open) == 0 {
		return ""
	}
	if h.advisory {
		return open[0]
	}
	for _, wanted := range order {
		for _, name := range open {
			if strings.EqualFold(name, wanted) {
				return name
			}
		}
	}
	return open[0]
}

func (h *poolHandler) log() []asked {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]asked(nil), h.asked...)
}

// wire is the preference object one attempt carried.
func (h *poolHandler) wire(attempt int) wirePrefs {
	log := h.log()
	if attempt < 0 || attempt >= len(log) {
		return wirePrefs{}
	}
	return log[attempt].sent
}

// poolClient is a client pointed at one of these pools, with the waits collapsed
// so a scenario proves what it is about rather than how long a backoff is.
//
// EVERY SCENARIO GETS ITS OWN MODEL AND ITS OWN LEDGER, because both of the
// places a belief lands are process-wide on purpose — the strike ledger
// (velocity.go's [sharedVelocity]) and the registry's own (internal/lane). A
// scenario that inherited the one before it would open with its machines already
// refused and prove nothing about the walk it is here to prove.
func poolClient(t *testing.T, name string, handler *poolHandler) *Client {
	t.Helper()
	t.Setenv(home.EnvVar, t.TempDir())
	client, err := NewClient(Config{APIKey: "k", BaseURL: "https://openrouter.ai/api/v1",
		Model: "openrouter/" + name, HTTPClient: handlerClient(handler)})
	if err != nil {
		t.Fatal(err)
	}
	client.wait = func(context.Context, time.Duration) error { return nil }
	client.velocity = newVelocityLedger()
	lanes.HeardPrefsCarried(client.config.BaseURL)
	return client
}

// THE MEASURED CHAIN, END TO END. Two machines refuse and the third answers, in
// three sends, with each machine asked exactly once — where this transport used
// to spend all six of a watched call's attempts on whichever one the router
// happened to pick first.
func TestARefusedMachineIsNotAskedTwiceWhileAnotherIsAdmissible(t *testing.T) {
	handler := &poolHandler{
		lanes:    []string{"DeepInfra", "Fireworks", "GMICloud"},
		refusing: map[string]bool{"DeepInfra": true, "Fireworks": true},
	}
	client := poolClient(t, "never-repeat-walk", handler)

	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatalf("a pool with one healthy machine in it did not answer: %v", err)
	}

	log := handler.log()
	if len(log) != 3 {
		t.Fatalf("the call made %d sends, want one to each machine: %+v", len(log), log)
	}
	seen := map[string]int{}
	bodies := map[string]bool{}
	for _, one := range log {
		seen[one.lane]++
		bodies[one.lane+"|"+one.body] = true
	}
	for lane, count := range seen {
		if count != 1 {
			t.Errorf("%s was asked %d times, want once — a machine that refused is not asked again", lane, count)
		}
	}
	if len(bodies) != len(log) {
		t.Errorf("the same bytes went to the same machine twice: %+v", log)
	}
	if log[len(log)-1].lane != "GMICloud" {
		t.Errorf("the answer came from %q, want the one machine that had not refused", log[len(log)-1].lane)
	}
}

// AND THE PERSON IS TOLD WHILE IT HAPPENS, in the ordinal the waiting design
// asks for and with no status code anywhere near them.
func TestWalkingTheMachinesIsNarratedAsAnOrdinal(t *testing.T) {
	handler := &poolHandler{
		lanes:    []string{"DeepInfra", "Fireworks", "GMICloud"},
		refusing: map[string]bool{"DeepInfra": true, "Fireworks": true},
	}
	client := poolClient(t, "never-repeat-ordinal", handler)

	told := &heard{}
	previous := OnPhase(told.take)
	t.Cleanup(func() { OnPhase(previous) })

	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatalf("the call did not answer: %v", err)
	}

	// A MOVE IS NARRATED AS TRYING AGAIN AND NOT AS WAITING, because nothing is
	// being waited for: the machine that refused is off the next body and the
	// next body goes out at once.
	moving, ok := told.find(PhaseRetrying)
	if !ok {
		t.Fatal("a call that walked two saturated machines told the person nothing")
	}
	if !strings.Contains(moving.Detail, " of ") {
		t.Fatalf("the phase read %q, want an ordinal a person can count in", moving.Detail)
	}
	if !moving.Deadline.IsZero() && moving.Deadline.After(moving.Since) {
		t.Fatalf("a move promised a countdown to %s, and there is nothing to count down to", moving.Deadline)
	}
	if _, waited := told.find(PhasePaced); waited {
		t.Fatal("a call with somewhere else to go was drawn as waiting on a machine")
	}
	for _, news := range told.all() {
		if strings.Contains(news.Detail, "429") || strings.Contains(news.Detail, "API error") {
			t.Fatalf("a status code reached the person: %q", news.Detail)
		}
	}
}

// ── ORDER IS ADVISORY, SO THE EXCLUSION KEYS ON WHO ANSWERED ────────────────
//
// THE LIVE RUN (2026-09-10 22:20–22:34, four quick tasks). Every attempt asked
// for DeepInfra by name and the ROUTER served Parasail, which answered 429:
// eighteen identical sends on one node, eleven on another, eleven more served by
// Fireworks on a third. `provider.order` is a ranking the router may ignore once
// `allow_fallbacks` is on, so re-ordering the request excludes nothing at all.
// The next body has to carry `provider.ignore: [the machine that ANSWERED]`,
// which is the `(via X)` in the refusal and never the lane we asked for.
//
// AND THE LANE WE ASKED FOR IS NOT STRUCK. It never answered, so it has said
// nothing — striking it would take away the one machine we actually wanted on
// the evidence of a machine we did not.
func TestTheMachineThatAnsweredIsVetoedAndTheOneWeAskedForIsNot(t *testing.T) {
	handler := &poolHandler{
		advisory: true,
		lanes:    []string{"Parasail", "DeepInfra"},
		refusing: map[string]bool{"Parasail": true},
	}
	client := poolClient(t, "never-repeat-advisory", handler)
	choice := lanes.Choice{Order: []string{"DeepInfra"}}
	ctx := WithLaneChoice(context.Background(), choice)

	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatalf("a pool whose second machine was healthy did not answer: %v", err)
	}

	log := handler.log()
	if len(log) != 2 {
		t.Fatalf("the call made %d sends, want one refused and one answered: %+v", len(log), log)
	}
	if log[0].lane != "Parasail" || log[1].lane != "DeepInfra" {
		t.Fatalf("the router served %+v, want the fan-out then the machine we asked for", log)
	}
	first, second := handler.wire(0), handler.wire(1)
	if len(first.Provider.Ignore) != 0 {
		t.Fatalf("the first attempt already vetoed %v", first.Provider.Ignore)
	}
	if len(second.Provider.Ignore) != 1 || second.Provider.Ignore[0] != "Parasail" {
		t.Fatalf("the second attempt vetoed %v, want exactly the machine that answered", second.Provider.Ignore)
	}
	for _, name := range second.Provider.Ignore {
		if strings.EqualFold(name, "DeepInfra") {
			t.Fatal("the lane we asked for was struck for a refusal it never made")
		}
	}
	if len(second.Provider.Order) == 0 || !strings.EqualFold(second.Provider.Order[0], "DeepInfra") {
		t.Fatalf("the second attempt ordered %v, want the lane we asked for still first", second.Provider.Order)
	}
}

// alonePool answers one 429 naming itself and asking for three seconds, and
// then answers properly. It is the pool that is one machine wide.
func alonePool(t *testing.T) (*Client, func() int, *[]time.Duration) {
	t.Helper()
	var mu sync.Mutex
	served := 0
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		served++
		first := served == 1
		mu.Unlock()
		if first {
			writer.Header().Set("Retry-After", "3")
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = writer.Write([]byte(`{"error":{"message":"Provider returned error","metadata":{"provider_name":"Alone"}}}`))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"openrouter/pool","provider":"Alone","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`))
	})
	client := pacedClient(t, handler)
	client.velocity = newVelocityLedger()
	waits := &[]time.Duration{}
	client.wait = func(_ context.Context, delay time.Duration) error {
		*waits = append(*waits, delay)
		return nil
	}
	return client, func() int { mu.Lock(); defer mu.Unlock(); return served }, waits
}

// A CONVERSATION NEVER WAITS OUT A WINDOW WHILE ANOTHER MODEL EXISTS.
//
// THE OWNER'S RULING, 2026-09-10: nobody should ever work around a `too many
// requests` by switching models themselves. When every machine this request may
// go to is being held, the call gives the refusal straight back — `pacingExhausted`
// reads it as the pacing door it is (endpoints.go) and the session offers the
// next model, which beats any window.
func TestAWatchedCallWithEveryMachineHeldGivesUpAtOnce(t *testing.T) {
	client, served, waits := alonePool(t)
	ctx := WithLaneChoice(context.Background(), lanes.Choice{Only: []string{"Alone"}})
	_, err := client.CompleteWithMessages(ctx, userMessages("hello"))
	if err == nil {
		t.Fatal("the one machine was held; the call should have handed the refusal back")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("the call failed with %v, want the pacing the model hop is offered on", err)
	}
	if got := served(); got != 1 {
		t.Fatalf("the call made %d sends, want one: there was nowhere else to send it", got)
	}
	if len(*waits) != 0 {
		t.Fatalf("a person watching waited %v before being offered another model", *waits)
	}
}

// A POOL ONE MACHINE WIDE IS THE ONE LEGAL SAME-MACHINE MOVE, and it belongs to
// the call nobody is watching: a task node has a worktree of work behind it and
// no fallback to hop to, so it waits — for exactly as long as the machine asked,
// with that moment on the phase pipe so a surface can draw the countdown.
func TestAPatientCallWaitsTheWindowItWasGivenAndReAsksOnce(t *testing.T) {
	client, served, waits := alonePool(t)
	told := &heard{}
	previous := OnPhase(told.take)
	t.Cleanup(func() { OnPhase(previous) })

	// ONE MACHINE WIDE IS A DEMAND, which is what confines a request to a single
	// machine on the wire: `provider.only` with `allow_fallbacks: false`. Without
	// one the router may fall back and the honest move is still to go elsewhere.
	ctx := WithLaneChoice(WithPatientRateLimits(context.Background()),
		lanes.Choice{Only: []string{"Alone"}})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatalf("a pool that asked us to come back in three seconds did not answer: %v", err)
	}
	if got := served(); got != 2 {
		t.Fatalf("the call made %d sends, want exactly one re-ask", got)
	}
	if len(*waits) != 1 || (*waits)[0] != 3*time.Second {
		t.Fatalf("the call waited %v, want the three seconds the machine asked for", *waits)
	}
	paced, ok := told.find(PhasePaced)
	if !ok {
		t.Fatal("a call held for a machine's own wait was not narrated as paced")
	}
	if countdown := paced.Deadline.Sub(paced.Since); countdown <= 0 || countdown > 4*time.Second {
		t.Fatalf("the phase promised a countdown of %s, want the window the machine named", countdown)
	}
}

// AND THE WAIT IS READ OUT OF THE BODY WHEN NO HEADER CARRIED IT. `retry_after`
// was recorded on not one row in ten days while every fourth failure was a 429:
// this router names its comeback in the refusal envelope.
func TestTheComebackIsReadOutOfTheRefusalBody(t *testing.T) {
	for _, shape := range []struct {
		what string
		body string
	}{
		{"beside the error", `{"retry_after":4}`},
		{"on the error", `{"error":{"message":"slow down","retry_after":4}}`},
		{"in its metadata", `{"error":{"message":"slow down","metadata":{"retry_after":4}}}`},
		{"nowhere", `{"error":{"message":"slow down"}}`},
	} {
		got := retryAfterIn([]byte(shape.body))
		want := 4 * time.Second
		if shape.what == "nowhere" {
			want = 0
		}
		if got != want {
			t.Errorf("a comeback %s read as %s, want %s", shape.what, got, want)
		}
	}
}

// THE LAW ITSELF, over two hundred scripted pools. Whatever the roster, whatever
// refuses, no call ever sends the same bytes to the same machine twice while
// another machine is admissible.
func TestRetryNeverRepeatsAMachine(t *testing.T) {
	draws := rand.New(rand.NewSource(20260910))
	for round := 0; round < 200; round++ {
		roster := make([]string, 2+draws.Intn(4))
		refusing := map[string]bool{}
		for index := range roster {
			roster[index] = fmt.Sprintf("Machine%d", index)
			// Every machine but the last may refuse; a pool where everything
			// refuses is the one-machine-wide case and has its own scenario.
			if index < len(roster)-1 && draws.Intn(2) == 0 {
				refusing[roster[index]] = true
			}
		}
		handler := &poolHandler{lanes: roster, refusing: refusing}
		client := poolClient(t, fmt.Sprintf("never-repeat-round-%d", round), handler)
		_, _ = client.CompleteWithMessages(context.Background(), userMessages("hello"))

		seen := map[string]bool{}
		for _, one := range handler.log() {
			key := one.lane + "|" + one.body
			if seen[key] {
				t.Fatalf("round %d sent identical bytes to %s twice with %d machines in the pool: %+v",
					round, one.lane, len(roster), handler.log())
			}
			seen[key] = true
		}
	}
}

// ── THE SHEET'S DOUBT IS NOT A CLOSED DOOR ──────────────────────────────────

// THE SCREENSHOT, staged. A tool-carrying request meets a pool whose healthy
// machine is the one the sheet flags `Tools: false` — which is GMICloud on
// 2026-09-10, gated out of a request while it was serving that same task's tool
// calls six seconds at a time. It is answered, by that machine, and nothing a
// person can read says `API error`.
func TestATaskIsAnsweredByTheLaneTheSheetDoubts(t *testing.T) {
	rig := newLaneRig(t, "never-repeat-doubted",
		lanestub.Lane{Name: "Fireworks", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 2000, Tools: true, Uptime: 100, Paced: true,
		}},
		lanestub.Lane{Name: "DeepInfra", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 2000, Tools: true, Uptime: 100, Paced: true,
		}},
		lanestub.Lane{Name: "GMICloud", Profile: lanestub.Profile{
			TTFT: 2 * time.Millisecond, Rate: 2000, Tokens: 9, Tools: false, Uptime: 100,
		}},
	)
	rig.client.wait = func(context.Context, time.Duration) error { return nil }

	told := &heard{}
	previous := OnPhase(told.take)
	t.Cleanup(func() { OnPhase(previous) })

	response, err := rig.client.CompleteWithMessages(talking(), userMessages("use a tool"))
	if err != nil {
		t.Fatalf("a pool with one machine able to answer did not answer: %v", err)
	}
	if response == nil {
		t.Fatal("the call came back with no answer and no error")
	}
	if got := rig.server.Requests("GMICloud"); got == 0 {
		t.Fatal("the lane the sheet flags Tools:false was never asked, which is the measured defect")
	}
	for _, lane := range []string{"Fireworks", "DeepInfra"} {
		if got := rig.server.Requests(lane); got > 1 {
			t.Errorf("%s refused and was asked %d times", lane, got)
		}
	}
	if got := len(rig.server.Asks()); got > 4 {
		t.Errorf("the call made %d sends, want no more than one to each machine", got)
	}
	for _, news := range told.all() {
		if strings.Contains(news.Detail, "API error") {
			t.Fatalf("a router sentence reached the person: %q", news.Detail)
		}
	}
}

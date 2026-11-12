package session

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/credits"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// The fixtures here are UNIT DATA: the keys are this file's own strings, the
// provider is a real HTTP stub on loopback that answers by bearer, and nothing
// reaches a network or a model.

const (
	credKeyA   = "sk-or-v1-crewcred-key-a-0123456789"
	credKeyB   = "sk-or-v1-crewcred-key-b-9876543210"
	credKeyC   = "sk-or-v1-crewcred-key-c-5555555555"
	credPinned = "z-ai/glm-5.3-flash"
)

// bearerAsk is one request the stub received: the bearer and the model.
type bearerAsk struct{ bearer, model string }

// bearerStub answers each key with the status the test sets for it — 200,
// 401 a refused key, 402 an account out of credit — and runs during, when
// set, before it answers: the moment a request is in flight.
type bearerStub struct {
	*httptest.Server
	mu     sync.Mutex
	status map[string]int
	asked  []bearerAsk
	during func(bearer string)
}

func newBearerStub(t *testing.T, status map[string]int) *bearerStub {
	t.Helper()
	stub := &bearerStub{status: status}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !answersChatOnly(w, r) {
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(raw, &body)
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		stub.mu.Lock()
		stub.asked = append(stub.asked, bearerAsk{bearer, body.Model})
		during := stub.during
		stub.mu.Unlock()
		if during != nil {
			during(bearer)
		}
		stub.mu.Lock()
		status, ok := stub.status[bearer]
		stub.mu.Unlock()
		if !ok {
			status = http.StatusUnauthorized
		}
		w.Header().Set("Content-Type", "application/json")
		switch status {
		case http.StatusOK:
			_ = json.NewEncoder(w).Encode(map[string]any{"model": body.Model, "choices": []any{map[string]any{
				"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": "done"}}}})
		case http.StatusPaymentRequired:
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"error":{"message":"Insufficient credits. Add more using https://openrouter.ai/settings/credits","code":402}}`)
		default:
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"message":"No auth credentials found","code":401}}`)
		}
	}))
	t.Cleanup(stub.Close)
	return stub
}

func (s *bearerStub) requests() []bearerAsk {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]bearerAsk(nil), s.asked...)
}

func (s *bearerStub) set(bearer string, status int) {
	s.mu.Lock()
	s.status[bearer] = status
	s.mu.Unlock()
}

// onStub is the default service on the stub, carrying key.
func onStub(stub *bearerStub, key string) modelsource.Set {
	return modelsource.NewSet(modelsource.Connected{Source: modelsource.DefaultSource(stub.URL), Key: key, Address: stub.URL})
}

// credPrint is the dispatch print of key on the stub's default service.
func credPrint(stub *bearerStub, dir, key string) string {
	return config.CrewDispatchOf(dir, modelsource.Connected{Source: modelsource.DefaultSource(stub.URL), Key: key}).Cred
}

// bearerStubAgent is a managed conversation on the stub over a profile whose
// key is key A, with the crew routed over that profile's catalog.
func bearerStubAgent(t *testing.T, stub *bearerStub) (*Agent, string) {
	t.Helper()
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("CODEAF_BASE_URL", "")
	dir := t.TempDir()
	if err := config.WriteAPIKey(dir, credKeyA); err != nil {
		t.Fatal(err)
	}
	previous := config.CrewCatalog
	t.Cleanup(func() { config.CrewCatalog = previous })
	rows := []catalog.Model{
		{ID: credPinned, OpenWeights: true, PromptPrice: 1.5e-7, CompletionPrice: 5e-7,
			IntelligenceIndex: 41.8, CodingIndex: 71.5, AgenticIndex: 50.9, ContextLength: 1310720, Parameters: []string{"tools"}},
		{ID: credPinned + ":free", OpenWeights: true,
			IntelligenceIndex: 41.8, CodingIndex: 71.5, AgenticIndex: 50.9, ContextLength: 1310720, Parameters: []string{"tools"}},
	}
	config.CrewCatalog = func() []catalog.Model { return rows }
	agent, err := New(Config{
		Workspace: t.TempDir(), Model: "somelab/the-chat-model", ProfileDir: dir, Sources: onStub(stub, credKeyA),
		RouteCrew: func(ask config.CrewAsk) (crewroute.Decision, error) { return config.RouteCrew(dir, ask) },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	return agent, dir
}

// pinnedRun routes one task with every role pinned to GLM on openrouter for
// this task, so no role sharing the worker's model walks a ladder and the
// worker's own send is the only thing the facts decide.
func pinnedRun(t *testing.T, agent *Agent) *beltRun {
	t.Helper()
	sends := map[crewroute.Seat]string{crewroute.Worker: credPinned, crewroute.Planner: credPinned, crewroute.Checker: credPinned}
	crew, err := agent.routeTaskCrew(withCrewWish(t.Context(), crewWish{sends: sends}), 1, "fix: the parser crashes on empty input", "Traceback: ValueError")
	if err != nil {
		t.Fatal(err)
	}
	for _, seat := range crewroute.Seats {
		if pick := crew.current().Seat(seat); !pick.Pinned || pick.Send != credPinned || pick.Provider != "openrouter" {
			t.Fatalf("the %s was not pinned to %s on openrouter: %+v", seat, credPinned, pick)
		}
	}
	return &beltRun{row: 1, crew: crew}
}

// askPinned is the run engine's next call on the pinned worker.
func askPinned(t *testing.T, agent *Agent, run *beltRun) error {
	t.Helper()
	_, err := crewSeatCompleter{agent: agent, run: run}.CompleteWithMessages(seatCtx(crewroute.Worker),
		[]ai.Message{textMessage("user", "fix it")}, ai.WithModel(credPinned))
	return err
}

func wantStopped(t *testing.T, err error, action string) {
	t.Helper()
	var stopped crewStopped
	if !errors.As(err, &stopped) || stopped.action != action {
		t.Fatalf("the call ended %v, want %q", err, action)
	}
}

func failedOf(run *beltRun, send string) (crewFailure, bool) {
	run.crew.mu.Lock()
	defer run.crew.mu.Unlock()
	f, ok := run.crew.failed[send]
	return f, ok
}

// A KEY ROTATED WHILE THE CALL WAS IN FLIGHT IS NOT BLAMED FOR THE OLD KEY'S
// 401, and the same pin answers on it. The request leaves with key A; while
// it is in flight the person connects key B — the profile and the
// conversation's pool both move — and A's refusal comes back. The failure,
// and the router's log row, name A's print and never B's; the next explicit
// call on the same pinned worker reaches the stub on B, for the same model,
// and nothing else is asked.
func TestAKeyRotatedInFlightIsNotBlamedAndTheSamePinAnswersOnIt(t *testing.T) {
	stub := newBearerStub(t, map[string]int{credKeyA: http.StatusUnauthorized, credKeyB: http.StatusOK})
	agent, dir := bearerStubAgent(t, stub)
	var once sync.Once
	stub.during = func(bearer string) {
		if bearer != credKeyA {
			return
		}
		once.Do(func() {
			if err := config.WriteAPIKey(dir, credKeyB); err != nil {
				t.Error(err)
			}
			agent.SetSources(onStub(stub, credKeyB))
		})
	}
	run := pinnedRun(t, agent)
	wantStopped(t, askPinned(t, agent, run), "reconnect openrouter with /connect")

	printA, printB := credPrint(stub, dir, credKeyA), credPrint(stub, dir, credKeyB)
	if printA == "" || printA == printB || config.CrewDispatchFor(dir, "openrouter").Cred != printB {
		t.Fatalf("prints A %q B %q in force %q: the fixture did not rotate", printA, printB, config.CrewDispatchFor(dir, "openrouter").Cred)
	}
	failure, ok := failedOf(run, credPinned)
	if !ok || failure.kind != provider.RouteAuth || failure.sent.Cred != printA || failure.provider != "openrouter" || failure.at.IsZero() {
		t.Fatalf("the failure %+v, want an auth refusal stamped with A's print %q", failure, printA)
	}
	raw, err := os.ReadFile(config.ProfilePath(dir, "router-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	log := string(raw)
	if !strings.Contains(log, `"fail_cred":"`+printA+`"`) || strings.Contains(log, printB) {
		t.Fatalf("the router log does not blame A's print alone:\n%s", log)
	}
	if strings.Contains(log, credKeyA) || strings.Contains(log, credKeyB) {
		t.Fatal("the router log carries a key")
	}
	run.crew.mu.Lock()
	changes, changeCount := len(run.crew.changes), run.crew.changeCount
	run.crew.mu.Unlock()

	if err := askPinned(t, agent, run); err != nil {
		t.Fatalf("the explicit retry on B: %v (asked %+v)", err, stub.requests())
	}
	got := stub.requests()
	if len(got) != 2 || got[0] != (bearerAsk{credKeyA, credPinned}) || got[1] != (bearerAsk{credKeyB, credPinned}) {
		t.Fatalf("the stub was asked %+v, want A then B, both on %s", got, credPinned)
	}
	d := run.crew.current()
	if w := d.Seat(crewroute.Worker); d.Stopped != "" || !w.Pinned || w.Send != credPinned {
		t.Fatalf("after the repair the decision is %+v (worker %+v)", d, w)
	}
	run.crew.mu.Lock()
	defer run.crew.mu.Unlock()
	if len(run.crew.gone) != 0 || run.crew.bad[credPinned] || run.crew.failed[credPinned].kind != "" || !run.crew.started[credPinned] {
		t.Fatalf("after the repair gone %v bad %v failed %v started %v", run.crew.gone, run.crew.bad, run.crew.failed, run.crew.started)
	}
	if len(run.crew.changes) != changes || run.crew.changeCount != changeCount {
		t.Fatalf("the repair rewrote the history: %d/%d changes, want %d/%d", len(run.crew.changes), run.crew.changeCount, changes, changeCount)
	}
}

// pausedAdapter is the conversation's own provider adapter, held once at the
// moment it is handed a request: after the door chose it and the account it
// was chosen for, before it snapshots a bearer for the header. Everything
// else — its key, its header, what it reports — is the adapter's own.
type pausedAdapter struct {
	*provider.Client
	once            sync.Once
	reached, resume chan struct{}
}

func (p *pausedAdapter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	p.once.Do(func() {
		close(p.reached)
		<-p.resume
	})
	return p.Client.CompleteWithMessages(ctx, messages, options...)
}

// pauseTheAdapter puts a pause in front of the adapter the pinned send is
// answered by, wherever the agent keeps it.
func pauseTheAdapter(t *testing.T, agent *Agent) *pausedAdapter {
	t.Helper()
	pool := agent.clientPool
	client, _, _, want, _, err := pool.clientWithService(credPinned)
	if err != nil {
		t.Fatal(err)
	}
	inner, ok := unwrapCompleter(client).(*provider.Client)
	if !ok {
		t.Fatalf("the pinned send is answered by %T, not a provider adapter", client)
	}
	paused := &pausedAdapter{Client: inner, reached: make(chan struct{}), resume: make(chan struct{})}
	pool.mu.Lock()
	pool.clients[want] = paused
	pool.mu.Unlock()
	agent.mu.Lock()
	if wrapper, ok := agent.client.(sessionCompleter); ok && agent.clientAccount == want {
		wrapper.inner = paused
		agent.client = wrapper
	}
	agent.mu.Unlock()
	return paused
}

// A KEY HANDED TO THE LIVE ADAPTER AFTER THE DOOR CHOSE IT IS THE KEY THE
// REFUSAL IS STAMPED WITH, because that is the key the header carried. The
// door resolves the pinned send to the adapter on key A; before the adapter
// writes the request, key B is handed to it — the request leaves on B and B
// is refused. The stamp is the print of the bearer the stub actually
// received, which is B's: the account the adapter was chosen for would have
// said A, and the next call, on B again, would have been asked forever. Held
// on B, the retry asks nothing; once C is connected, the same pin is asked
// on C and answers.
func TestARefusalIsStampedWithTheBearerItsHeaderCarried(t *testing.T) {
	stub := newBearerStub(t, map[string]int{credKeyA: http.StatusOK, credKeyB: http.StatusUnauthorized, credKeyC: http.StatusOK})
	agent, dir := bearerStubAgent(t, stub)
	run := pinnedRun(t, agent)
	paused := pauseTheAdapter(t, agent)

	ended := make(chan error, 1)
	go func() { ended <- askPinned(t, agent, run) }()
	select {
	case <-paused.reached:
	case err := <-ended:
		t.Fatalf("the call ended before it reached its adapter: %v", err)
	}
	if err := agent.SetAPIKey(credKeyB); err != nil {
		t.Fatal(err)
	}
	close(paused.resume)
	wantStopped(t, <-ended, "reconnect openrouter with /connect")

	got := stub.requests()
	if len(got) != 1 || got[0].model != credPinned {
		t.Fatalf("the stub was asked %d requests, want the one pinned call", len(got))
	}
	header := credPrint(stub, dir, got[0].bearer)
	printA, printB := credPrint(stub, dir, credKeyA), credPrint(stub, dir, credKeyB)
	if header != printB {
		t.Fatalf("the header carried print %q, want B's %q: the fixture did not rotate the live adapter", header, printB)
	}
	failure, ok := failedOf(run, credPinned)
	if !ok || failure.kind != provider.RouteAuth || failure.provider != "openrouter" || failure.sent.Cred != header {
		t.Fatalf("the refusal was stamped %q (A is %q), want the header's print %q", failure.sent.Cred, printA, header)
	}
	raw, err := os.ReadFile(config.ProfilePath(dir, "router-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if log := string(raw); !strings.Contains(log, `"fail_cred":"`+header+`"`) || strings.Contains(log, printA) ||
		strings.Contains(log, credKeyA) || strings.Contains(log, credKeyB) {
		t.Fatal("the router log does not blame the header's print alone, or carries a key")
	}

	if err := config.WriteAPIKey(dir, credKeyB); err != nil {
		t.Fatal(err)
	}
	wantStopped(t, askPinned(t, agent, run), "reconnect openrouter with /connect")
	if n := len(stub.requests()); n != 1 {
		t.Fatalf("the refused key was asked again: %d requests", n)
	}

	if err := config.WriteAPIKey(dir, credKeyC); err != nil {
		t.Fatal(err)
	}
	if err := agent.SetAPIKey(credKeyC); err != nil {
		t.Fatal(err)
	}
	if err := askPinned(t, agent, run); err != nil {
		t.Fatalf("the explicit retry on C: %v", err)
	}
	got = stub.requests()
	if len(got) != 2 || got[1].model != credPinned || credPrint(stub, dir, got[1].bearer) != credPrint(stub, dir, credKeyC) {
		t.Fatalf("the stub was asked %d requests; want the same pin, on C, second", len(got))
	}
}

// A POOL THAT STILL HOLDS THE OLD KEY STAMPS THE OLD KEY. The profile already
// says B, but the conversation was never told and its pool sends A: the
// failure is A's, which the settings in force would have mislabelled B. And
// while the pool would send A again, the explicit retry asks nothing — the
// profile's new key says nothing about the client that would carry the call.
func TestAPoolStillHoldingTheOldKeyIsStampedAndHeldOnIt(t *testing.T) {
	stub := newBearerStub(t, map[string]int{credKeyA: http.StatusUnauthorized, credKeyB: http.StatusOK})
	agent, dir := bearerStubAgent(t, stub)
	run := pinnedRun(t, agent)
	if err := config.WriteAPIKey(dir, credKeyB); err != nil {
		t.Fatal(err)
	}
	wantStopped(t, askPinned(t, agent, run), "reconnect openrouter with /connect")
	if failure, _ := failedOf(run, credPinned); failure.sent.Cred != credPrint(stub, dir, credKeyA) || failure.sent == config.CrewDispatchFor(dir, "openrouter") {
		t.Fatalf("the failure was stamped %q, want A's print, not the profile's", failure.sent.Cred)
	}
	wantStopped(t, askPinned(t, agent, run), "reconnect openrouter with /connect")
	if got := stub.requests(); len(got) != 1 || got[0].bearer != credKeyA {
		t.Fatalf("the stub was asked %+v, want A's one refused call", got)
	}
	// Told, the pool moves to B, and the same pin answers there.
	agent.SetSources(onStub(stub, credKeyB))
	if err := askPinned(t, agent, run); err != nil {
		t.Fatalf("the retry once the pool holds B: %v", err)
	}
	if got := stub.requests(); len(got) != 2 || got[1] != (bearerAsk{credKeyB, credPinned}) {
		t.Fatalf("the stub was asked %+v, want B on %s second", got, credPinned)
	}
}

// THE SAME KEY, OR NO KEY, REPAIRS NOTHING. The refused key written again is
// the key that was refused; a profile and pool with no key in force are not a
// new credential. The pinned worker stays held on its action, and the stub is
// asked nothing more.
func TestTheSameKeyOrNoKeyLeavesTheRefusalHeldAndAsksNothing(t *testing.T) {
	stub := newBearerStub(t, map[string]int{credKeyA: http.StatusUnauthorized})
	agent, dir := bearerStubAgent(t, stub)
	run := pinnedRun(t, agent)
	wantStopped(t, askPinned(t, agent, run), "reconnect openrouter with /connect")

	if err := config.WriteAPIKey(dir, credKeyA); err != nil {
		t.Fatal(err)
	}
	agent.SetSources(onStub(stub, credKeyA))
	wantStopped(t, askPinned(t, agent, run), "reconnect openrouter with /connect")

	if err := config.WriteAPIKey(dir, ""); err != nil {
		t.Fatal(err)
	}
	agent.SetSources(onStub(stub, ""))
	wantStopped(t, askPinned(t, agent, run), "reconnect openrouter with /connect")
	if got := stub.requests(); len(got) != 1 {
		t.Fatalf("the stub was asked %+v, want only the first refused call", got)
	}
	if d := run.crew.current(); d.Stopped != "reconnect openrouter with /connect" {
		t.Fatalf("the held crew stopped on %q", d.Stopped)
	}
}

// CREDIT READ BEFORE A PAYMENT REFUSAL DOES NOT LIFT IT; credit read after it
// does. The account refuses for want of credit; a balance read that began
// before the refusal leaves the pin held and asks nothing. A positive balance
// read after it lets the same pin, on the same key, be asked once more.
func TestOnlyCreditReadAfterAPaymentRefusalAsksTheSamePinAgain(t *testing.T) {
	stub := newBearerStub(t, map[string]int{credKeyA: http.StatusPaymentRequired})
	agent, dir := bearerStubAgent(t, stub)
	run := pinnedRun(t, agent)
	before := time.Now()
	wantStopped(t, askPinned(t, agent, run), "add credit on openrouter to continue")

	if err := config.WriteCreditsReadingAt(dir, credKeyA, credits.Reading{Known: true}, before); err != nil {
		t.Fatal(err)
	}
	wantStopped(t, askPinned(t, agent, run), "add credit on openrouter to continue")
	if got := stub.requests(); len(got) != 1 {
		t.Fatalf("an old reading asked the stub again: %+v", got)
	}

	stub.set(credKeyA, http.StatusOK)
	if err := config.WriteCreditsReadingAt(dir, credKeyA, credits.Reading{Known: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := askPinned(t, agent, run); err != nil {
		t.Fatalf("the retry after credit was read: %v", err)
	}
	if got := stub.requests(); len(got) != 2 || got[1] != (bearerAsk{credKeyA, credPinned}) {
		t.Fatalf("the stub was asked %+v, want the same pin on the same key", got)
	}
	run.crew.mu.Lock()
	defer run.crew.mu.Unlock()
	if len(run.crew.broke) != 0 || run.crew.decision.Stopped != "" {
		t.Fatalf("after credit: broke %v stopped %q", run.crew.broke, run.crew.decision.Stopped)
	}
}

// ONLY THE ANSWERED REFUSAL IS LET GO. On one provider the pinned worker's
// refusal is answered by a new key, beside an older checkpoint's refusal that
// names no time and no key, a limit on another send, and a spend stop. The
// worker's own failure goes; the unanswered refusal keeps its provider gone
// and the stop said; the limit stands; a spend stop is never touched.
func TestOnlyTheAnsweredRefusalIsLetGo(t *testing.T) {
	for _, c := range []struct {
		name        string
		legacy      bool
		stopped     string
		wantGone    bool
		wantStopped string
	}{
		{"an unanswered refusal on the same provider", true, "reconnect openrouter with /connect", true, "reconnect openrouter with /connect"},
		{"nothing else refused on it", false, "reconnect openrouter with /connect", false, ""},
		{"a spend stop", false, "raise the task limit to continue", false, "raise the task limit to continue"},
	} {
		t.Run(c.name, func(t *testing.T) {
			stub := newBearerStub(t, map[string]int{credKeyB: http.StatusOK})
			agent, dir := bearerStubAgent(t, stub)
			run := pinnedRun(t, agent)
			crew := run.crew
			crew.mu.Lock()
			crew.failed = map[string]crewFailure{
				credPinned:   {kind: provider.RouteAuth, at: time.Now(), provider: "openrouter", sent: config.CrewDispatch{Cred: credPrint(stub, dir, credKeyA)}},
				"vendor/cap": {kind: provider.RouteQuota, at: time.Now(), provider: "openrouter"},
			}
			crew.bad = map[string]bool{credPinned: true, "vendor/cap": true}
			if c.legacy {
				crew.failed["vendor/old"] = crewFailure{kind: provider.RouteAuth, provider: "openrouter"}
				crew.bad["vendor/old"] = true
			}
			crew.gone, crew.broke = map[string]bool{"openrouter": true}, map[string]bool{}
			crew.decision.Stopped = c.stopped
			crew.mu.Unlock()

			if err := config.WriteAPIKey(dir, credKeyB); err != nil {
				t.Fatal(err)
			}
			agent.SetSources(onStub(stub, credKeyB))
			agent.liftAnsweredRefusals(run, credPinned)

			crew.mu.Lock()
			defer crew.mu.Unlock()
			if _, held := crew.failed[credPinned]; held || crew.bad[credPinned] {
				t.Fatalf("the answered refusal is still held: %v %v", crew.failed, crew.bad)
			}
			if _, held := crew.failed["vendor/cap"]; !held || !crew.bad["vendor/cap"] {
				t.Fatal("a limit was let go with the refused key")
			}
			if _, held := crew.failed["vendor/old"]; c.legacy && (!held || !crew.bad["vendor/old"]) {
				t.Fatal("a refusal no fact answers was let go")
			}
			if crew.gone["openrouter"] != c.wantGone || crew.decision.Stopped != c.wantStopped {
				t.Fatalf("gone %v stopped %q, want gone %v stopped %q", crew.gone, crew.decision.Stopped, c.wantGone, c.wantStopped)
			}
			if len(stub.requests()) != 0 {
				t.Fatalf("letting a refusal go asked the stub: %+v", stub.requests())
			}
		})
	}
}

// A CARRIED-ON TASK KEEPS WHAT ITS FAILURE WAS SENT WITH. The checkpoint
// keeps the failure's time, provider and print — never the key — and the
// crew restored from it lets the refusal go on the same facts.
func TestACheckpointKeepsTheFailuresDispatchFacts(t *testing.T) {
	stub := newBearerStub(t, map[string]int{credKeyA: http.StatusUnauthorized, credKeyB: http.StatusOK})
	agent, dir := bearerStubAgent(t, stub)
	run := pinnedRun(t, agent)
	wantStopped(t, askPinned(t, agent, run), "reconnect openrouter with /connect")
	failure, _ := failedOf(run, credPinned)

	record := run.crew.record()
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), credKeyA) {
		t.Fatal("the checkpoint carries a key")
	}
	if sent := record.FailedSent[credPinned]; sent.Cred != failure.sent.Cred || sent.Provider != "openrouter" || !sent.At.Equal(failure.at) {
		t.Fatalf("the checkpoint kept %+v, want %+v", sent, failure)
	}
	restored, err := agent.restoreTaskCrew(2, record)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture has no durable run row to checkpoint into; the restored
	// policy is what is under test.
	restored.persist = nil
	again := &beltRun{row: 2, crew: restored, recoveredCrew: record}
	if f, _ := failedOf(again, credPinned); f.sent != failure.sent || f.provider != failure.provider || !f.at.Equal(failure.at) {
		t.Fatalf("the restored failure %+v, want %+v", f, failure)
	}
	if err := config.WriteAPIKey(dir, credKeyB); err != nil {
		t.Fatal(err)
	}
	agent.SetSources(onStub(stub, credKeyB))
	if err := askPinned(t, agent, again); err != nil {
		t.Fatalf("the carried-on task on B: %v", err)
	}
	if got := stub.requests(); len(got) != 2 || got[1] != (bearerAsk{credKeyB, credPinned}) {
		t.Fatalf("the stub was asked %+v", got)
	}
}

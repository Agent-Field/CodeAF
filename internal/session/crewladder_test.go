package session

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// crewStub is the provider the owner's verification stood up: every paid id
// answers 402 out of credit (or 401, a refused key), and each free pool
// answers as the test says — 200, or 429 at its limit.
type crewStub struct {
	*httptest.Server
	mu    sync.Mutex
	asked []string
	paid  int
	free  map[string]int
	only  map[string]int
}

func newCrewStub(t *testing.T, paid int, free map[string]int) *crewStub {
	t.Helper()
	stub := &crewStub{paid: paid, free: free}
	stub.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !answersChatOnly(w, r) {
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(raw, &body)
		stub.mu.Lock()
		stub.asked = append(stub.asked, body.Model)
		status := stub.paid
		if strings.HasSuffix(body.Model, ":free") {
			status = stub.free[body.Model]
		}
		if forced, ok := stub.only[body.Model]; ok {
			status = forced
		}
		stub.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch status {
		case http.StatusOK:
			_ = json.NewEncoder(w).Encode(map[string]any{"model": body.Model, "choices": []any{map[string]any{
				"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": "done"}}}})
		case http.StatusPaymentRequired:
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"error":{"message":"Insufficient credits. Add more using https://openrouter.ai/settings/credits","code":402}}`)
		case http.StatusForbidden:
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"error":{"message":"this model is only available on agentic harnesses","code":403}}`)
		case http.StatusUnauthorized:
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"error":{"message":"No auth credentials found","code":401}}`)
		default:
			w.Header().Set("Retry-After", "3600")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"message":"Rate limit exceeded: free-models-per-day. ","code":429}}`)
		}
	}))
	t.Cleanup(stub.Close)
	return stub
}

func (s *crewStub) models() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.asked...)
}

// crewStubAgent is a conversation on the stub with the crew routed over a
// profile whose catalog holds a measured model, its free pool, and a free
// stranger that publishes too little to be a first pick.
func crewStubAgent(t *testing.T, stub *crewStub) (*Agent, string) {
	t.Helper()
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("CODEAF_BASE_URL", "")
	dir := t.TempDir()
	if err := config.WriteAPIKey(dir, "sk-or-v1-crewstub-0123456789"); err != nil {
		t.Fatal(err)
	}
	previous := config.CrewCatalog
	t.Cleanup(func() { config.CrewCatalog = previous })
	rows := []catalog.Model{
		{ID: "z-ai/glm-5.3-flash", OpenWeights: true, PromptPrice: 1.5e-7, CompletionPrice: 5e-7,
			IntelligenceIndex: 41.8, CodingIndex: 71.5, AgenticIndex: 50.9, ContextLength: 1310720, Parameters: []string{"tools"}},
		{ID: "z-ai/glm-5.3-flash:free", OpenWeights: true,
			IntelligenceIndex: 41.8, CodingIndex: 71.5, AgenticIndex: 50.9, ContextLength: 1310720, Parameters: []string{"tools"}},
		{ID: "vendor/stranger:free", ContextLength: 262144, CodingIndex: 40, Parameters: []string{"tools"}},
	}
	config.CrewCatalog = func() []catalog.Model { return rows }
	agent, err := New(Config{
		Workspace: t.TempDir(), Model: "somelab/the-chat-model", ProfileDir: dir,
		Sources:   modelsource.NewSet(modelsource.Connected{Source: modelsource.DefaultSource(stub.URL), Key: "sk-or-v1-crewstub-0123456789", Address: stub.URL}),
		RouteCrew: func(ask config.CrewAsk) (crewroute.Decision, error) { return config.RouteCrew(dir, ask) },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	return agent, dir
}

// askWorker routes one task and makes its worker's first call through the
// seat ladder, as the run engine does.
func askWorker(t *testing.T, agent *Agent, row uint64) (*beltRun, error) {
	t.Helper()
	crew, err := agent.routeTaskCrew(t.Context(), row, "fix: the parser crashes on empty input", "Traceback: ValueError")
	if err != nil {
		return nil, err
	}
	run := &beltRun{row: row, crew: crew}
	worker := crew.current().Seat(crewroute.Worker).Send
	_, err = crewSeatCompleter{agent: agent, run: run}.CompleteWithMessages(t.Context(),
		[]ai.Message{textMessage("user", "fix it")}, ai.WithModel(worker))
	return run, err
}

// THE OWNER'S STUB SEQUENCE, end to end: every paid call is out of credit.
// The worker's paid route refuses, the seat falls to the model's own free pool
// and the task proceeds; the NEXT task is routed (the paid route probed again,
// never refused at decision time) and, with that pool at its limit, falls to
// the next free pool; with every pool at its limit the task stops on the one
// action.
func TestTheSeatLadderReachesTheFreePoolsOnAnAccountOutOfCredit(t *testing.T) {
	free := map[string]int{"z-ai/glm-5.3-flash:free": http.StatusOK, "vendor/stranger:free": http.StatusOK}
	stub := newCrewStub(t, http.StatusPaymentRequired, free)
	agent, _ := crewStubAgent(t, stub)

	run, err := askWorker(t, agent, 1)
	if err != nil {
		t.Fatalf("task 1: %v (asked %v)", err, stub.models())
	}
	if got := stub.models(); len(got) != 2 || got[0] != "z-ai/glm-5.3-flash" || got[1] != "z-ai/glm-5.3-flash:free" {
		t.Fatalf("task 1 asked %v, want the paid route then its free pool", got)
	}
	line := run.crew.current().Line("", -1)
	if !strings.HasPrefix(line, "running on fallback crew") || !strings.Contains(line, "free routes in use (may log prompts)") {
		t.Errorf("task 1's line %q does not say the fallback and the free pool", line)
	}

	stub.mu.Lock()
	stub.asked, stub.free["z-ai/glm-5.3-flash:free"] = nil, http.StatusTooManyRequests
	stub.mu.Unlock()
	if _, err := askWorker(t, agent, 2); err != nil {
		t.Fatalf("task 2: %v (asked %v)", err, stub.models())
	}
	if got := stub.models(); len(got) < 2 || got[0] != "z-ai/glm-5.3-flash" || got[len(got)-1] != "vendor/stranger:free" {
		t.Fatalf("task 2 asked %v, want the paid probe, then the free pools down to the one that answers", got)
	}

	stub.mu.Lock()
	stub.free["vendor/stranger:free"] = http.StatusTooManyRequests
	stub.mu.Unlock()
	run, err = askWorker(t, agent, 3)
	var stopped crewStopped
	if !errors.As(err, &stopped) || stopped.action != "add credit on openrouter to continue" {
		t.Fatalf("task 3 ended %v, want the one action", err)
	}
	if got := run.crew.current().Stopped; got != "add credit on openrouter to continue" {
		t.Errorf("the decision records %q as where it stopped", got)
	}
}

// A REFUSED KEY STOPS ON RECONNECTING, and credit coming back is seen by the
// very next task: its first call probes the paid route, answers, and the
// crew runs as picked.
func TestARefusedKeyAsksToReconnectAndRestoredCreditIsSeenAtOnce(t *testing.T) {
	stub := newCrewStub(t, http.StatusUnauthorized, map[string]int{})
	agent, _ := crewStubAgent(t, stub)
	_, err := askWorker(t, agent, 1)
	var stopped crewStopped
	if !errors.As(err, &stopped) || stopped.action != "reconnect openrouter with /connect" {
		t.Fatalf("a refused key ended %v, want the reconnect action", err)
	}

	stub.mu.Lock()
	stub.paid, stub.asked = http.StatusOK, nil
	stub.mu.Unlock()
	run, err := askWorker(t, agent, 2)
	if err != nil {
		t.Fatalf("with the key good again: %v", err)
	}
	if got := stub.models(); len(got) != 1 || got[0] != "z-ai/glm-5.3-flash" {
		t.Errorf("the next task asked %v, want the paid route once", got)
	}
	if line := run.crew.current().Line("", -1); strings.Contains(line, "fallback") {
		t.Errorf("a recovered account still reads as fallback: %q", line)
	}
}

// NOTHING THIS TASK DOES ASKS A QUARANTINED ROUTE: a route that refused a
// model is left out of the next task's crew, and a call the run makes on a
// tier the crew does not seat — the same model by its tier's row — is handed
// a healthy route instead. The stub counts zero requests for it.
func TestNoCallReachesAQuarantinedRoute(t *testing.T) {
	stub := newCrewStub(t, http.StatusOK, map[string]int{})
	stub.only = map[string]int{"z-ai/glm-5.3-flash": http.StatusForbidden}
	agent, _ := crewStubAgent(t, stub)
	previous := config.CrewCatalog
	rows := append(previous(), catalog.Model{ID: "deepseek/deepseek-v4-flash", OpenWeights: true, PromptPrice: 8.246e-8, CompletionPrice: 1.6492e-7,
		IntelligenceIndex: 24.2, CodingIndex: 56.2, AgenticIndex: 22.2, ContextLength: 1048576, Parameters: []string{"tools"}})
	config.CrewCatalog = func() []catalog.Model { return rows }

	if _, err := askWorker(t, agent, 1); err != nil {
		t.Fatalf("task 1: %v (asked %v)", err, stub.models())
	}
	stub.mu.Lock()
	stub.asked = nil
	stub.mu.Unlock()
	run, err := askWorker(t, agent, 2)
	if err != nil {
		t.Fatalf("task 2: %v (asked %v)", err, stub.models())
	}
	// The run engine seats a tier the crew does not name on that tier's row.
	if _, err := (crewSeatCompleter{agent: agent, run: run}).CompleteWithMessages(t.Context(),
		[]ai.Message{textMessage("user", "a small errand")}, ai.WithModel("z-ai/glm-5.3-flash")); err != nil {
		t.Fatalf("the errand: %v", err)
	}
	// And the end-of-task helper — the run summary, a worker-role errand whose
	// floor is the conversation's model — with the person talking to the very
	// model whose route was quarantined.
	agent.SetModel("z-ai/glm-5.3-flash")
	if _, _, err := agent.callRole(t.Context(), roles.RoleWorker, agent.Model(), []ai.Message{textMessage("user", "summarise the run")}, ai.WithMaxTokens(320)); err != nil {
		t.Fatalf("the run summary: %v", err)
	}
	for _, model := range stub.models() {
		if model == "z-ai/glm-5.3-flash" {
			t.Fatalf("task 2 asked the quarantined route: %v", stub.models())
		}
	}
}

// A TASK THAT RAN ON ITS RESCUE AND FAILED ENDS ON THE CREDIT ACTION: a
// stronger crew is out of reach on an account out of credit, so the line
// never offers "/redo stronger" there.
func TestAFailedRescuedTaskEndsOnTheCreditAction(t *testing.T) {
	crew := &taskCrew{decision: crewroute.Decision{Class: crewroute.Bugfix}, broke: map[string]bool{"openrouter": true}}
	if got := crew.stoppedIfCutOff().Stopped; got != "add credit on openrouter to continue" {
		t.Errorf("a failed task on an account out of credit stops on %q", got)
	}
	clean := &taskCrew{decision: crewroute.Decision{Class: crewroute.Bugfix}}
	if got := clean.stoppedIfCutOff().Stopped; got != "" {
		t.Errorf("a failed task that saw no account cut off stops on %q, want the redo offer", got)
	}
}

// EVERY ROUTE DOWN ENDS IN SECONDS: paid calls out of credit, every free pool
// at its daily limit with a Retry-After of an hour. The seat tries at most
// three free pools, asks none of them twice — a patient task context
// notwithstanding — and stops on the one action.
func TestEveryRouteDownStopsWithinThreeFreePools(t *testing.T) {
	stub := newCrewStub(t, http.StatusPaymentRequired, map[string]int{})
	agent, _ := crewStubAgent(t, stub)
	previous := config.CrewCatalog
	rows := previous()
	for _, id := range []string{"a/one:free", "b/two:free", "c/three:free", "d/four:free", "e/five:free", "f/six:free"} {
		rows = append(rows, catalog.Model{ID: id, ContextLength: 262144, CodingIndex: 40, Parameters: []string{"tools"}})
	}
	config.CrewCatalog = func() []catalog.Model { return rows }

	crew, err := agent.routeTaskCrew(t.Context(), 1, "fix: the parser crashes on empty input", "Traceback")
	if err != nil {
		t.Fatal(err)
	}
	run := &beltRun{row: 1, crew: crew}
	began := time.Now()
	// EVERY SEAT, AND THE WORKER AGAIN: the engine asks each seat, and asks a
	// seat that failed again. A pool that answered 429 is rested for the
	// whole task, whichever seat meets it next.
	routed := crew.current()
	keys := []string{routed.Seat(crewroute.Worker).Send, routed.Seat(crewroute.Planner).Send,
		routed.Seat(crewroute.Checker).Send, routed.Seat(crewroute.Worker).Send}
	for i, key := range keys {
		_, err = crewSeatCompleter{agent: agent, run: run}.CompleteWithMessages(provider.WithPatientRateLimits(t.Context()),
			[]ai.Message{textMessage("user", "fix it")}, ai.WithModel(key))
		var stopped crewStopped
		if !errors.As(err, &stopped) || stopped.action != "add credit on openrouter to continue" {
			t.Fatalf("call %d on %s: every route down ended %v, want the one action", i, key, err)
		}
	}
	if took := time.Since(began); took > 30*time.Second {
		t.Errorf("reaching the action took %v", took)
	}
	asked := map[string]int{}
	for _, model := range stub.models() {
		if strings.HasSuffix(model, ":free") {
			asked[model]++
		}
	}
	if len(asked) > 3*len(crewroute.Seats) {
		t.Errorf("the seats walked %d free pools: %v", len(asked), asked)
	}
	for model, n := range asked {
		if n > 1 {
			t.Errorf("%s was asked %d times in one task through an hour's Retry-After", model, n)
		}
	}
}

// THE 403 HOME, END TO END: the worker's paid route refuses on this task and
// the seat moves to its free pool. Every call after that — the run's
// worker-role errand at a large answer ceiling, and a client built for the
// model and called directly, the lowest road there is — is refused at the
// provider's door without a request, though route health had been read (and
// cached) a moment before the refusal. The stub counts the seat's own first
// call and nothing else.
func TestNoRoadReachesARouteThatRefusedThisTask(t *testing.T) {
	stub := newCrewStub(t, http.StatusOK, map[string]int{"z-ai/glm-5.3-flash:free": http.StatusOK})
	stub.only = map[string]int{"z-ai/glm-5.3-flash": http.StatusForbidden}
	agent, dir := crewStubAgent(t, stub)
	if !config.CrewRouteAnswers(dir, "z-ai/glm-5.3-flash") {
		t.Fatal("the route was resting before anything refused it")
	}
	if _, err := askWorker(t, agent, 1); err != nil {
		t.Fatalf("task 1: %v (asked %v)", err, stub.models())
	}
	glm := func() int {
		n := 0
		for _, model := range stub.models() {
			if model == "z-ai/glm-5.3-flash" {
				n++
			}
		}
		return n
	}
	if n := glm(); n != 1 {
		t.Fatalf("the seat asked the paid route %d times, want its one first call (%v)", n, stub.models())
	}
	_, _, _ = agent.callRole(t.Context(), roles.RoleWorker, "z-ai/glm-5.3-flash",
		[]ai.Message{textMessage("user", "summarise the task")}, ai.WithMaxTokens(6400))
	client, wire, _, err := agent.completerFor("z-ai/glm-5.3-flash")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CompleteWithMessages(provider.WithCallTag(t.Context(), "worker"),
		[]ai.Message{textMessage("user", "one more errand")}, ai.WithModel(wire), ai.WithMaxTokens(6400))
	var resting errRouteResting
	if !errors.As(err, &resting) {
		t.Errorf("a direct call on the resting route ended %v, want it refused at the door (%v)", err, stub.models())
	}
	if n := glm(); n != 1 {
		t.Errorf("the resting route was asked %d times after the seat left it: %v", n-1, stub.models())
	}
}

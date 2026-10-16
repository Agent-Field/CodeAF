package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/provider"
)

// ── THE ORDINARY TURN, TWO MODELS, ONE ADMISSION AUTHORITY ──────────────────
//
// The provider's own tests prove that a refused send stops the next lane. This
// file proves it through the door a person uses: a real [Agent] built by [New],
// a real [provider.Client] on a loopback server, the observer and the owner on
// the SUBMISSION's context, and a configured second model that really would have
// answered. A refusal that was read like the wire (a "timeout" or an "overflow"
// in its words) would hop to that model and buy the very send the authority
// declined; the control run below proves the hop is real, so the refusal run's
// silence is a finding and not an accident of the fixture.

// admissionToken records one send's ending.
type admissionToken struct {
	mu   sync.Mutex
	ends []provider.TransportEnd
	fins []provider.FinancialObservation
}

func (t *admissionToken) EndTransport(e provider.TransportEnd) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ends = append(t.ends, e)
}

func (t *admissionToken) ObserveFinancial(o provider.FinancialObservation) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.fins = append(t.fins, o)
}

// admissionObserver is a pointer type, so it is comparable like a real authority.
// deny, when set, answers the nth ask (1-based); later asks go through.
type admissionObserver struct {
	mu     sync.Mutex
	asked  int
	infos  []provider.ActualAttemptInfo
	tokens []*admissionToken
	deny   func(n int) error
}

func (o *admissionObserver) BeforeAttempt(_ context.Context, info provider.ActualAttemptInfo) (provider.ActualAttemptToken, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.asked++
	o.infos = append(o.infos, info)
	if o.deny != nil {
		if err := o.deny(o.asked); err != nil {
			return nil, err
		}
	}
	token := &admissionToken{}
	o.tokens = append(o.tokens, token)
	return token, nil
}

func (o *admissionObserver) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.asked
}

// twoModelServer answers by the model named in each request body. The first
// model fails like a provider upstream (a routed 502 naming the machine); the
// second answers. Every request is counted per model whatever it was.
type twoModelServer struct {
	*httptest.Server
	mu     sync.Mutex
	byName map[string]int
	other  atomic.Int32
	paths  []string
}

func (s *twoModelServer) hits(model string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byName[model]
}

// sends is how many completion requests either model received.
func (s *twoModelServer) sends() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.byName {
		n += c
	}
	return n
}

func newTwoModelServer(t *testing.T, failing, answering string) *twoModelServer {
	t.Helper()
	s := &twoModelServer{byName: map[string]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			// A catalog, sheet or receipt route: counted, never served.
			s.other.Add(1)
			s.mu.Lock()
			s.paths = append(s.paths, r.Method+" "+r.URL.Path)
			s.mu.Unlock()
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var ask struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		_ = json.Unmarshal(body, &ask)
		s.mu.Lock()
		s.byName[ask.Model]++
		s.mu.Unlock()
		if ask.Model == failing {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprint(w, `{"error":{"message":"Provider returned error","code":502,"metadata":{"provider_name":"Together"}}}`)
			return
		}
		usage := `"usage":{"prompt_tokens":9,"completion_tokens":2,"total_tokens":11,"cost":0.003}`
		if ask.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"id\":\"gen-ok\",\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"hello there\"}}]}\n\n", ask.Model)
			fmt.Fprintf(w, "data: {\"id\":\"gen-ok\",\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n", ask.Model)
			fmt.Fprintf(w, "data: {\"id\":\"gen-ok\",\"model\":%q,\"choices\":[],%s}\n\n", ask.Model, usage)
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"gen-ok","model":%q,"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"hello there"}}],%s}`, ask.Model, usage)
	}))
	t.Cleanup(s.Close)
	return s
}

// twoModelAgent is the production constructor with two configured candidates and
// no optional paid helper: deterministic names, and no memory.
func twoModelAgent(t *testing.T, server *twoModelServer, first, second string) *Agent {
	t.Helper()
	agent, err := New(Config{
		Workspace:          t.TempDir(),
		Model:              first,
		ModelFallbacks:     []string{second},
		APIKey:             "test-key",
		BaseURL:            server.URL,
		DeterministicNames: true,
		System:             "SYSTEM",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(agent.SettleWrites)
	t.Cleanup(func() { _ = agent.Close() })
	chain, ok := agent.modelFallbackChain(first)
	if !ok || len(chain) != 1 || chain[0] != second {
		t.Fatalf("the agent's real client offers fallbacks %v (ok %v), want [%s]", chain, ok, second)
	}
	return agent
}

func submitObserved(t *testing.T, agent *Agent, observer provider.ActualAttemptObserver, owner provider.WorkOwner) []Event {
	t.Helper()
	ctx, err := provider.WithActualAttemptObserver(watchedContext(agent), observer)
	if err != nil {
		t.Fatal(err)
	}
	if ctx, err = provider.WithWorkOwner(ctx, owner); err != nil {
		t.Fatal(err)
	}
	events, err := agent.SubmitMessage(ctx, Submission{Text: "say hello"})
	if err != nil {
		t.Fatalf("SubmitMessage: %v", err)
	}
	return collect(t, events)
}

// THE CONTROL: with an authority that admits everything, the first model's
// transient failure really does move the turn to the second. Without this the
// refusal run below could pass only because no fallback was ever reachable.
func TestObservedTurnControlFirstModelFailureReallyHopsToTheSecond(t *testing.T) {
	const first, second = "ctl/first-model", "ctl/second-model"
	server := newTwoModelServer(t, first, second)
	agent := twoModelAgent(t, server, first, second)
	impatient(t, agent, turnLadderAttempts)
	observer := &admissionObserver{}
	owner := provider.WorkOwner{Kind: "turn", ID: "control"}

	collected := submitObserved(t, agent, observer, owner)

	if failure, failed := firstOfKind(collected, EventError); failed {
		t.Fatalf("the control turn failed although the second model answers: %v", failure.Err)
	}
	if _, done := firstOfKind(collected, EventTurnDone); !done {
		t.Fatalf("control never completed the ordinary turn: %v", kinds(collected))
	}
	if server.hits(first) == 0 || server.hits(second) == 0 {
		t.Fatalf("control hits first=%d second=%d, want both models really asked", server.hits(first), server.hits(second))
	}
	if observer.count() != server.hits(first)+server.hits(second) {
		t.Fatalf("observer admitted %d sends, the server received %d", observer.count(), server.hits(first)+server.hits(second))
	}
	for i, info := range observer.infos {
		if info.Owner != owner || info.ID == "" {
			t.Fatalf("send %d admitted with %+v, want the submission's owner and an id", i, info)
		}
	}
	seen := map[string]bool{}
	for _, info := range observer.infos {
		if seen[info.ID] {
			t.Fatalf("attempt id %q was admitted twice", info.ID)
		}
		seen[info.ID] = true
	}
}

// THE PROOF: the same turn, same two models, the authority refuses the FIRST
// attempt only and would allow every later one. Its refusal reads like a
// timeout or an overflow; it still ends the turn, with no HTTP on either
// model, one ask of the authority, and the original typed cause preserved.
func TestObservedTurnDenialDoesNotHopToTheSecondModelWhateverItsCauseSoundsLike(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
	}{
		// A wire-looking cause is the one the turn ladder would ASK AGAIN or HOP on.
		{"timeout-looking", errors.New("daily cap reached: read tcp: i/o timeout: connection reset by peer: 503 service unavailable")},
		// Overflow prose is not currently classified as compaction evidence;
		// this case guards against a future prose classifier bypassing admission.
		{"overflow-looking", errors.New("daily cap reached: request timed out; context length exceeded the maximum context window")},
	} {
		t.Run(tc.name, func(t *testing.T) { denialDoesNotHop(t, tc.cause) })
	}
}

func denialDoesNotHop(t *testing.T, cause error) {
	const first, second = "deny/first-model", "deny/second-model"
	server := newTwoModelServer(t, first, second)
	agent := twoModelAgent(t, server, first, second)
	impatient(t, agent, turnLadderAttempts)
	observer := &admissionObserver{deny: func(n int) error {
		if n == 1 {
			return cause
		}
		return nil // every later ask WOULD be allowed
	}}
	owner := provider.WorkOwner{Kind: "turn", ID: "denied"}

	collected := submitObserved(t, agent, observer, owner)
	agent.SettleWrites()

	failure, failed := firstOfKind(collected, EventError)
	if !failed {
		t.Fatalf("a refused first attempt did not end the turn; events were %v", kinds(collected))
	}
	if server.sends() != 0 {
		t.Fatalf("server received %d completions (first=%d second=%d); a refused turn must send nothing to either model", server.sends(), server.hits(first), server.hits(second))
	}
	// Anything else the server saw is an unpaid discovery GET (a catalog or
	// sheet read), never a completion and never a generation receipt.
	server.mu.Lock()
	for _, path := range server.paths {
		if !strings.HasPrefix(path, "GET ") || strings.Contains(path, "chat/completions") {
			server.mu.Unlock()
			t.Fatalf("unexpected non-completion traffic %v", server.paths)
		}
	}
	server.mu.Unlock()
	if got := observer.count(); got != 1 {
		t.Fatalf("the authority was asked %d times, want exactly once (no hop, no helper, no title, no retry)", got)
	}
	if got := observer.infos[0].Owner; got != owner {
		t.Fatalf("the single ask carried owner %+v, want %+v", got, owner)
	}
	if got := observer.infos[0].SelectedModel; got != first {
		t.Fatalf("the single ask was for %q, want the configured first model %q", got, first)
	}
	if len(observer.tokens) != 0 {
		t.Fatalf("a refused ask produced %d tokens", len(observer.tokens))
	}
	var denied *provider.AttemptDeniedError
	if !errors.As(failure.Err, &denied) {
		t.Fatalf("the turn failure %v lost its typed admission refusal", failure.Err)
	}
	if !errors.Is(failure.Err, cause) {
		t.Fatalf("the turn failure %v does not carry the authority's own cause", failure.Err)
	}
	// THE PERSON'S SENTENCE IS A SEPARATE FACT from the typed chain asserted
	// above and is deliberately only logged: today the turn reads "the request
	// did not get through" and the authority's reason lives in the typed cause.
	t.Logf("person-facing failure: Err=%q ErrKind=%q ErrStatus=%d ErrWords=%q", failure.Err.Error(), failure.ErrKind, failure.ErrStatus, failure.ErrWords)
	if _, retried := firstOfKind(collected, EventRetrying); retried {
		t.Fatalf("a refused attempt was announced as a retry or a move; events were %v", kinds(collected))
	}
	if agent.Model() != first {
		t.Fatalf("the session moved to %q after a refusal", agent.Model())
	}
}

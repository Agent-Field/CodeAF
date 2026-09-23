package session

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/codexauth"
	account "github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// leavesNoLaneNews makes a test that drives a real agent leave nothing behind
// for the next test's lane reader. A sighting posted while no reader is
// registered is HELD and handed to the next reader that registers
// ([OnLaneNews]), so an errand of this test's agent that finished after its
// reader was taken down would arrive in some later test as a second post. It
// is registered FIRST so that it runs LAST: after the agent has closed and
// after [hears] has put the previous reader back.
func leavesNoLaneNews(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		previous := OnLaneNews(func(LaneNews) {})
		laneNewsDesk.settled()
		OnLaneNews(previous)
	})
}

// rawCallLines is every call row in a journal exactly as it was written, so a
// test can say what the row does NOT carry — which the decoded struct, whose
// fields are all omitempty, cannot tell apart from an empty value.
func rawCallLines(t *testing.T, path string) []string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the journal: %v", err)
	}
	var lines []string
	for _, line := range strings.Split(string(content), "\n") {
		if strings.Contains(line, `"type":"call"`) {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestE2ACodexTurnsCallRowNamesCodexBesideTheBackendsModel(t *testing.T) {
	// E2 (#1391): through a real Agent, the real provider client and the real
	// Codex transport, every call row a Codex conversation journals says
	// `"endpoint":"Codex"` beside the model the backend answered with. Before
	// the fix the column was blank for exactly this service.
	//
	// E4: and the name is the RECORD's alone. The lane layer hears nothing about
	// a Codex answer — no lane news for the surface to draw beside the model,
	// no sighting in the provider's ledger — because no answer carried a name.
	leavesNoLaneNews(t)
	heard := hears(t)
	var turns atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/responses" {
			http.NotFound(writer, request)
			return
		}
		turn := turns.Add(1)
		writer.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(writer, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"endpoint-%d\",\"model\":\"gpt-5.5\",\"created_at\":1800000000}}\n\n", turn)
		fmt.Fprintf(writer, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"answer %d\"}\n\n", turn)
		fmt.Fprint(writer, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":9,\"output_tokens\":2}}}\n\n")
	}))
	defer backend.Close()
	t.Setenv("CODEAF_CODEX_BACKEND", backend.URL)
	profile := t.TempDir()
	if err := codexauth.Save(profile, codexauth.Tokens{AccessToken: "endpoint-access-token", RefreshToken: "endpoint-refresh-token", IDToken: "endpoint-identity-token", AccountID: "endpoint-account", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	listed := true
	if err := account.WriteSources(profile, []account.PersistedSource{{ID: "codex", Written: "codex", Key: codexauth.Sentinel, Listed: &listed}}); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	agent, err := New(Config{
		Workspace: t.TempDir(), Model: "codex/gpt-5.5", System: "Answer briefly.", SessionFile: journal,
		Sources: account.ResolveSources(profile, "", account.DefaultBaseURL),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	drainTurn(t, agent, "first question")
	drainTurn(t, agent, "second question")
	calls := journaledCalls(t, journal)
	if len(calls) < 2 {
		t.Fatalf("journaled %d call rows, want one per turn at least: %+v", len(calls), calls)
	}
	for _, call := range calls {
		if call.Role != "" {
			continue
		}
		if call.Endpoint != "Codex" || call.Model != "gpt-5.5" {
			t.Fatalf("a Codex turn's call row = %+v, want endpoint Codex beside model gpt-5.5", call)
		}
	}
	for _, news := range heard() {
		if strings.EqualFold(news.Lane, "Codex") || strings.EqualFold(news.Winner, "Codex") {
			t.Fatalf("a Codex answer was told to the surface as a lane: %+v", news)
		}
	}
	for _, model := range []string{"gpt-5.5", "codex/gpt-5.5"} {
		if sighting, ok := provider.LastServed(model); ok && strings.EqualFold(sighting.Provider, "Codex") {
			t.Fatalf("the provider's ledger holds a Codex lane for %s: %+v", model, sighting)
		}
	}
}

func TestE3ARouterStillNamesItsUpstreamAndASilentAnswerWritesNoEndpoint(t *testing.T) {
	// E3 (#1391), the control: nothing about a router's column moves. An answer
	// naming its upstream writes that name — and is still told to the lane
	// layer, which is what makes E2's silence a finding and not a deaf reader —
	// and an answer naming nobody writes NO endpoint key at all: the emptiness
	// law, never `"endpoint":""`.
	leavesNoLaneNews(t)
	heard := hears(t)
	var named atomic.Value
	named.Store("Alibaba")
	router := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || !strings.HasSuffix(request.URL.Path, "/chat/completions") {
			http.NotFound(writer, request)
			return
		}
		provider := ""
		if name, _ := named.Load().(string); name != "" {
			provider = fmt.Sprintf(`"provider":%q,`, name)
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(writer, "data: {\"id\":\"g\",\"object\":\"chat.completion.chunk\",\"model\":\"test/alpha\",%s\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":null}]}\n\n", provider)
		fmt.Fprintf(writer, "data: {\"id\":\"g\",\"object\":\"chat.completion.chunk\",\"model\":\"test/alpha\",%s\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":1,\"total_tokens\":10}}\n\n", provider)
		fmt.Fprint(writer, "data: [DONE]\n\n")
	}))
	defer router.Close()
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	agent, err := New(Config{
		Workspace: t.TempDir(), Model: "test/alpha", System: "Answer briefly.", SessionFile: journal,
		Sources: account.ResolveSources(t.TempDir(), "router-test-key", router.URL),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	drainTurn(t, agent, "a named answer")
	named.Store("")
	drainTurn(t, agent, "a silent answer")
	var own []string
	for _, line := range rawCallLines(t, journal) {
		if !strings.Contains(line, `"role":`) {
			own = append(own, line)
		}
	}
	if len(own) < 2 {
		t.Fatalf("journaled %d conversation call rows, want two: %v", len(own), own)
	}
	if !strings.Contains(own[0], `"endpoint":"Alibaba"`) {
		t.Fatalf("the named answer's row = %s, want the upstream it named", own[0])
	}
	if last := own[len(own)-1]; strings.Contains(last, `"endpoint"`) {
		t.Fatalf("the silent answer's row = %s, want no endpoint key at all", last)
	}
	told := false
	for _, news := range heard() {
		told = told || news.Lane == "Alibaba"
	}
	if !told {
		t.Fatalf("the router's named answer was not told to the lane layer: %+v", heard())
	}
}

func TestE3ADirectServiceThatDeclaredNoNameWritesNoEndpoint(t *testing.T) {
	// E3 (#1391), the other control: the declared name is Codex's alone. A
	// connected direct service that declared none, answering without naming a
	// machine, writes no endpoint key — its column stays exactly as it was.
	leavesNoLaneNews(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || !strings.HasSuffix(request.URL.Path, "/chat/completions") {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(writer, "data: {\"id\":\"d\",\"object\":\"chat.completion.chunk\",\"model\":\"box-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":null}]}\n\n")
		fmt.Fprint(writer, "data: {\"id\":\"d\",\"object\":\"chat.completion.chunk\",\"model\":\"box-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":1,\"total_tokens\":10}}\n\n")
		fmt.Fprint(writer, "data: [DONE]\n\n")
	}))
	defer server.Close()
	profile := t.TempDir()
	listed := true
	if err := account.WriteSources(profile, []account.PersistedSource{{ID: "custom", Written: "mybox", Address: server.URL, Key: "box-key", Listed: &listed}}); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	agent, err := New(Config{
		Workspace: t.TempDir(), Model: "mybox/box-model", System: "Answer briefly.", SessionFile: journal,
		Sources: account.ResolveSources(profile, "", account.DefaultBaseURL),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	drainTurn(t, agent, "a direct answer")
	lines := rawCallLines(t, journal)
	if len(lines) == 0 {
		t.Fatal("the direct service's turn journaled no call row")
	}
	for _, line := range lines {
		if strings.Contains(line, `"endpoint"`) {
			t.Fatalf("a direct service that declared no name wrote %s, want no endpoint key", line)
		}
	}
}

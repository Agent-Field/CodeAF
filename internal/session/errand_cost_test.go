package session

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// EVERY MODEL CALL AN ERRAND MAKES IS ITS COST. A model whose thinking eats the
// whole ceiling answers nothing at it; the adapter learns that and asks again
// with room. The first, superseded answer was paid for, so the errand's answer
// carries both calls' cost, which is the figure a caller books to the job.
func TestAnErrandCountsTheCallItsAdapterAskedAgain(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			MaxTokens int `json:"max_tokens"`
		}
		_ = json.Unmarshal(raw, &req)
		w.Header().Set("Content-Type", "text/event-stream")
		if calls.Add(1) == 1 {
			fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\"},\"finish_reason\":\"length\"}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":%d,\"cost\":0.003}}\n\ndata: [DONE]\n\n", req.MaxTokens)
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"{\\\"ok\\\":true}\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":40,\"cost\":0.001}}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	model := "stub/thinks-through-its-ceiling-" + strings.ReplaceAll(t.Name(), "/", "-")
	budget := &liveBudgetStub{cap: 1}
	agent, err := New(Config{Workspace: t.TempDir(), Model: "direct/" + model, JobBudget: budget,
		Sources: modelsource.NewSet(modelsource.Connected{Source: modelsource.Source{ID: "direct", Written: "direct", Address: server.URL}, Key: "k", Address: server.URL})})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	ask := errandAsk("cost_seam")
	ask.MaxTokens = 200
	got, err := agent.RunErrand(t.Context(), ask)
	if err != nil || got.Text != `{"ok":true}` || calls.Load() != 2 {
		t.Fatalf("answer=%+v err=%v calls=%d", got, err, calls.Load())
	}
	if got.Usage.CostUSD < 0.0039 || got.Usage.CostUSD > 0.0041 {
		t.Fatalf("the errand's cost is %v, not both calls' 0.004", got.Usage.CostUSD)
	}
	budget.mu.Lock()
	spent := budget.spent
	budget.mu.Unlock()
	if spent < 0.0039 || spent > 0.0041 {
		t.Fatalf("the job was charged %v, not both calls' 0.004", spent)
	}
}

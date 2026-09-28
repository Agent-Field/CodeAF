package run_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A call whose estimated reservation would cross the ceiling is refused
// before upstream billing; the real child, model API, worker and supervisor
// carry that refusal as a cost limit even below the metered ceiling.
func TestEstimatedModelRefusalEndsRunOnCostLimit(t *testing.T) {
	store := runOpenStore(t)
	program, setup, calling, _ := realChild(t, 0.06, "3")
	t.Setenv("FAKE_ENDING", "crash")
	setup.ModelPrice = func(string) (float64, float64, bool) { return 0, 0.00001, true }
	limits := run.Limits{CostUSD: 0.10}
	factory := run.DelegateFactory(store, t.TempDir(), program, setup, limits, nil)
	outcome, summary := run.Start(runContext(t), run.Spec{Store: store, Workspace: t.TempDir(), Slots: 1, Limits: limits, Factory: factory})
	if outcome != run.OutcomeLimit || summary.Limit != run.LimitCost || summary.USD != 0.06 || len(calling.seen()) != 1 {
		t.Fatalf("estimated refusal: outcome=%q limit=%q spent=%.2f upstream calls=%d; want cost limit at $0.06", outcome, summary.Limit, summary.USD, len(calling.seen()))
	}
}

// The conversation's real program door receives the supervisor's own summary
// after the child is refused; its model-free landing line names the limit and
// the row records the cost ending instead of a crash.
func TestEstimatedRefusalLandsAsConversationCostLimit(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv(delegateChildEnv, "1")
	t.Setenv("FAKE_CALLS", "3")
	t.Setenv("FAKE_ENDING", "crash")
	t.Setenv("FAKE_PROGRAM_NAME", "senior-dev")
	var upstream atomic.Int32
	var retryOffered atomic.Bool
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.Add(1)
		request, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		if strings.Contains(string(request), "ask whether to spend more") && !retryOffered.Swap(true) {
			args, _ := json.Marshal(map[string]string{
				"title": "Repair the parser", "summary": "Finish the parser repair",
				"brief": "Repair the parser and run its checks", "deliverable": "The parser repair",
				"acceptance": "The parser checks pass", "via": "senior-dev",
			})
			payload, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
				"index": 0, "finish_reason": "tool_calls", "delta": map[string]any{
					"role": "assistant", "tool_calls": []any{map[string]any{
						"index": 0, "id": "retry-1", "type": "function", "function": map[string]any{
							"name": "propose_task", "arguments": string(args),
						},
					}},
				},
			}}})
			_, _ = io.WriteString(w, "data: "+string(payload)+"\n\ndata: [DONE]\n\n")
			return
		}
		_, _ = io.WriteString(w, `data: {"id":"x","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"}}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"id":"x","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":10,"cost":0.06}}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer providerServer.Close()
	workspace := t.TempDir()
	place := session.Place{Dir: t.TempDir()}
	agent, err := session.New(session.Config{
		Workspace: workspace, Place: place, SessionFile: place.Transcript(),
		Model: "test/model", APIKey: "fixture-key", BaseURL: providerServer.URL,
		System: "Answer briefly.", Delegates: []delegate.Delegate{childProgram()},
		SpendRailUSD: 0.10,
		ModelPrice:   func(string) (float64, float64, bool) { return 0, 0.00001, true },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	updates, stop := agent.WatchTaskUpdates()
	defer stop()
	if _, _, _, err := agent.StartDelegate(context.Background(), "senior-dev", "repair the parser"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(10 * time.Second)
	for {
		select {
		case event := <-updates:
			if event.Kind != session.EventNotice || !strings.Contains(event.Text, "stopped at the conversation's $0.10 limit") {
				continue
			}
			if !strings.Contains(event.Text, "spent $0.06") {
				t.Fatalf("limit line does not carry metered spend: %q", event.Text)
			}
			settled := false
			for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
				for _, row := range agent.TaskIndex() {
					if row.Program == "senior-dev" && !row.Live() {
						if row.Ending != session.TaskEndingCostLimit {
							t.Fatalf("landed program row ending = %q, want cost limit", row.Ending)
						}
						settled = true
					}
				}
				if settled {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !settled {
				t.Fatal("the program row did not settle")
			}
			if upstream.Load() < 1 {
				t.Fatal("no model call reached the upstream fixture")
			}
			var hold []byte
			var err error
			for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
				hold, err = os.ReadFile(place.Transcript() + ".program-handoff.json")
				if err == nil {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err != nil || !strings.Contains(string(hold), `"verdict":"limit"`) {
				t.Fatalf("automatic hand-off hold = %q, %v; want a durable limit refusal", hold, err)
			}
			for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
				for _, entry := range agent.Transcript() {
					if entry.Tool == "propose_task" && strings.Contains(entry.Output, "ask the person first") {
						return
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatalf("the model's automatic retry was not refused: offered=%v transcript=%+v", retryOffered.Load(), agent.Transcript())
		case <-deadline:
			t.Fatalf("the real refusal produced no model-free cost-limit line: upstream=%d rows=%+v transcript=%+v", upstream.Load(), agent.TaskIndex(), agent.Transcript())
		}
	}
}

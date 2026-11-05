package session

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// This fixture also runs unchanged on app/v3: a person stops either a tool
// between requests or the following response while its tool arguments arrive.
func TestStopCutPairsToolsAndAcceptsNextTurn(t *testing.T) {
	for _, between := range []bool{false, true} {
		t.Run(fmt.Sprintf("between_calls=%t", between), func(t *testing.T) {
			t.Setenv("CODEAF_HOME", t.TempDir())
			t.Setenv("CODEAF_DAILY_BUDGET", "")
			profile := t.TempDir()
			if err := config.WriteDailyBudgetUSD(profile, .24); err != nil {
				t.Fatal(err)
			}
			ready := make(chan struct{})
			toolStarted := make(chan string, 1)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					fmt.Fprint(w, `{"data":{"total_cost":0}}`)
					return
				}
				var request ai.Request
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				assertStoppedToolPairs(t, request.Messages)
				n := calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				switch n {
				case 1:
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"completed\",\"type\":\"function\",\"function\":{\"name\":\"touch\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"cost\":0}}\n\ndata: [DONE]\n\n")
				case 2:
					if between { // The next explicit turn after stopping the tool.
						fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"resumed\"},\"finish_reason\":\"stop\"}],\"usage\":{\"cost\":0}}\n\ndata: [DONE]\n\n")
						return
					}
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"partial\",\"type\":\"function\",\"function\":{\"name\":\"touch\",\"arguments\":\"{\"}}]}}]}\n\n")
					w.(http.Flusher).Flush()
					close(ready)
					<-r.Context().Done()
				default:
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"resumed\"},\"finish_reason\":\"stop\"}],\"usage\":{\"cost\":0}}\n\ndata: [DONE]\n\n")
				}
			}))
			t.Cleanup(server.Close)
			client, err := provider.NewClient(provider.Config{APIKey: "fixture", BaseURL: server.URL, Model: "test/model", HTTPClient: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			a, _ := newTestAgent(t, client, func(c *Config) {
				c.OneModel = true
				c.DeterministicNames = true
				c.ProfileDir = profile
				c.SessionFile = filepath.Join(t.TempDir(), "chat.jsonl")
			})
			var held <-chan struct{}
			if between {
				held = make(chan struct{})
			}
			a.tools = append(a.tools, countingTool("touch", toolStarted, held))
			events := mustSubmit(t, a, "work")
			if between {
				select {
				case <-toolStarted:
				case <-time.After(3 * time.Second):
					t.Fatal("tool did not start")
				}
			} else {
				waitCloseRun(t, ready, "partial tool reply")
			}
			if err := a.StopWork(); err != nil {
				t.Fatal(err)
			}
			collect(t, events)
			waitFor(t, "stop joins", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return !a.workStopping })
			assertStoppedToolPairs(t, a.snapshot())
			a.SettleWrites()
			record := ReadTranscript(a.config.SessionFile)
			results, actualCalls := 0, 0
			for _, entry := range record.Entries {
				if entry.Role == "tool" && entry.Tool == "" {
					results++
					if entry.Answered {
						t.Error("plain result unexpectedly marked as a call")
					}
				}
				if entry.Tool != "" {
					actualCalls++
					if !entry.Answered {
						t.Errorf("unpaired actual call %q", entry.CallID)
					}
				}
			}
			if actualCalls != 1 || results != 1 {
				t.Fatalf("calls=%d results=%d, want one complete pair", actualCalls, results)
			}
			if err := config.WriteDailyBudgetUSD(profile, 1); err != nil {
				t.Fatal(err)
			}
			answer := ""
			for _, event := range collect(t, mustSubmit(t, a, "resume")) {
				answer += event.Text
				if event.Kind == EventError {
					t.Errorf("resume: %v", event.Err)
				}
			}
			if !strings.Contains(answer, "resumed") {
				t.Fatalf("next turn not accepted: %q", answer)
			}
			t.Logf("actual tool pair valid; %d plain tool-result row would falsely fail the old e2e checker", results)
		})
	}
}

func assertStoppedToolPairs(t *testing.T, messages []ai.Message) {
	t.Helper()
	pending := map[string]bool{}
	for _, m := range messages {
		if m.Role == "tool" {
			if !pending[m.ToolCallID] {
				t.Errorf("orphan result %q", m.ToolCallID)
			}
			delete(pending, m.ToolCallID)
			continue
		}
		if len(pending) > 0 {
			t.Errorf("unpaired calls before %s: %v", m.Role, pending)
		}
		for _, call := range m.ToolCalls {
			pending[call.ID] = true
		}
	}
	if len(pending) > 0 {
		t.Errorf("unpaired trailing calls: %v", pending)
	}
}

func TestBudgetStopExplainsIdleCrewOwner(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	budget := &liveBudgetStub{cap: 1}
	started := make(chan struct{})
	model := &scriptedCompleter{aside: func(messages []ai.Message) (*ai.Response, bool) {
		if isPlannerCall(messages) {
			return textResponse(`{"add":[{"id":"worker","title":"read notes","goal":"read notes"}]}`), true
		}
		if isNameCall(messages) || isSessionTitleCall(messages) {
			return textResponse("Read notes"), true
		}
		return nil, false
	}, steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}}
	a, _ := newTestAgent(t, model, func(c *Config) { c.JobBudget = budget })
	if _, err := a.RunOrchestrate(context.Background(), "read notes", "", 5); err != nil {
		t.Fatal(err)
	}
	waitCloseRun(t, started, "worker")
	budget.Spend(2)
	waitFor(t, "crew stopped", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return a.workStopped && !a.workStopping })
	for _, entry := range a.Transcript() {
		if strings.Contains(entry.Text, "job budget reached") {
			return
		}
	}
	t.Fatal("idle owner lost budget notice after worker stop")
}

// The crossing worker can finish before its owner's ledger watcher runs.
// A final crew report must not replace the durable daily-limit explanation.
func TestDailyCapFinishedCrewKeepsNotice(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	model := &scriptedCompleter{aside: func(messages []ai.Message) (*ai.Response, bool) {
		if isPlannerCall(messages) {
			return priced(textResponse(`{"add":[{"id":"worker","title":"read notes","goal":"read notes"}]}`), 0), true
		}
		if isNameCall(messages) || isSessionTitleCall(messages) {
			return priced(textResponse("Read notes"), 0), true
		}
		return nil, false
	}, steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return priced(textResponse("done"), .25), nil
	}}}
	a, _ := newTestAgent(t, model, func(c *Config) { c.DailyBudgetResolved = true; c.DailyBudgetUSD = .24 })
	id, err := a.RunOrchestrate(context.Background(), "read notes", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, a, id)
	FlushUsage()
	waitFor(t, "finished crew daily notice", func() bool {
		for _, e := range a.Transcript() {
			if strings.Contains(e.Text, "daily limit") && strings.Contains(e.Text, "/budget day") {
				return true
			}
		}
		return false
	})
}

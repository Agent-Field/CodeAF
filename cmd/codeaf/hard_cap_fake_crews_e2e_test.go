//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Drive the real v3 door as well as the recorder in fake mode. Every worker
// completes a tool pair, then waits for its first token when another crew banks
// the crossing charge. No configured credential or public endpoint is used.
func hardCapFakeCrews(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	t.Setenv("CODEAF_MODEL_POOL", "off")
	profile := v3Profile(t, map[string]any{config.KeyMemoryEnabled: "off"})
	if err := config.WriteDailyBudgetUSD(profile, hardCapDay); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ready := make(chan struct{}, 4)
	var mu sync.Mutex
	raised := false
	calls := map[string]int{}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `{"data":{"total_cost":0}}`)
			return
		}
		var req ai.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		pending := map[string]bool{}
		for _, m := range req.Messages {
			if m.Role == "tool" {
				if !pending[m.ToolCallID] {
					t.Errorf("orphan result %q", m.ToolCallID)
				}
				delete(pending, m.ToolCallID)
				continue
			}
			if len(pending) > 0 {
				t.Errorf("provider refused unpaired history: %v", pending)
			}
			for _, call := range m.ToolCalls {
				pending[call.ID] = true
			}
		}
		if len(pending) > 0 {
			t.Errorf("provider refused trailing calls: %v", pending)
		}
		raw, _ := json.Marshal(req.Messages)
		planner := strings.Contains(string(raw), "You plan an adaptive run")
		crew := ""
		for i := range 4 {
			marker := fmt.Sprintf("CREW_%d", i)
			if strings.Contains(string(raw), marker) {
				crew = marker
				break
			}
		}
		text := "Read notes"
		if planner {
			text = fmt.Sprintf(`{"add":[{"id":"worker","title":"read notes","goal":%q}]}`, crew+" read notes")
		}
		mu.Lock()
		resumed := raised
		if !planner && len(req.Tools) > 0 && crew != "" {
			calls[crew]++
		}
		n := calls[crew]
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if resumed {
			text = "CAP RAISED"
		} else if !planner && len(req.Tools) > 0 && crew != "" {
			if n == 1 {
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"read-notes\",\"type\":\"function\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\\\"notes.md\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"cost\":0}}\n\ndata: [DONE]\n\n")
				return
			}
			if n != 2 {
				t.Errorf("crew %s started call %d after cap", crew, n)
			}
			w.(http.Flusher).Flush()
			ready <- struct{}{}
			<-r.Context().Done()
			return
		}
		delta, _ := json.Marshal(text)
		fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}],\"usage\":{\"cost\":0}}\n\ndata: [DONE]\n\n", delta)
	}))
	t.Cleanup(fake.Close)
	var agents []*session.Agent
	defer func() {
		for _, a := range agents {
			_ = a.Close()
			a.SettleWrites()
		}
	}()
	for i := range 4 {
		workspace := filepath.Join(root, fmt.Sprint(i))
		if err := os.MkdirAll(workspace, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(workspace, "notes.md"), []byte("fixture notes"), 0600); err != nil {
			t.Fatal(err)
		}
		a, err := v3OpenSession(session.Config{Workspace: workspace, System: "SYSTEM", Model: "test/model", APIKey: "fixture", BaseURL: fake.URL, OneModel: true, DeterministicNames: true, ProfileDir: profile, SessionFile: filepath.Join(root, fmt.Sprintf("crew-%d.jsonl", i))})
		if err != nil {
			t.Fatal(err)
		}
		agents = append(agents, a)
		if _, err := a.RunOrchestrate(ctx, fmt.Sprintf("CREW_%d read notes", i), "test/model", 5); err != nil {
			t.Fatal(err)
		}
	}
	for range 4 {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Fatal("four fixture workers did not reach held calls")
		}
	}
	session.RecordUsage(session.UsageLedgerPath(), session.UsageLine{Model: "fixture crossing", Calls: 1, USD: .025})
	session.FlushUsage()
	hardCapWait(t, ctx, "all four persisted daily notices", func() bool {
		for _, a := range agents {
			found := false
			for _, e := range a.Transcript() {
				found = found || strings.Contains(e.Text, "daily limit") && strings.Contains(e.Text, "/budget day")
			}
			if !found {
				return false
			}
		}
		return true
	})
	hardCapPairs(t, root)
	if err := config.WriteDailyBudgetUSD(profile, 1); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	raised = true
	mu.Unlock()
	for _, a := range agents {
		events, err := a.Submit(ctx, "resume")
		if err != nil {
			t.Fatal(err)
		}
		answer := ""
		for _, event := range hardCapDrain(t, ctx, events, "fixture resume") {
			answer += event.Text
			if event.Kind == session.EventError {
				t.Errorf("resume: %v", event.Err)
			}
		}
		if !strings.Contains(answer, "CAP RAISED") {
			t.Fatalf("provider-shaped fixture did not accept next turn: %q", answer)
		}
	}
	hardCapPairs(t, root)
	t.Log("v3OpenSession: four real crews stopped, all four notices persisted, tool pairs valid, four resumed turns accepted")
}

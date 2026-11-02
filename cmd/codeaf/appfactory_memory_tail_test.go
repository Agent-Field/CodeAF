package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestMemoryCompileTailLifecycle(t *testing.T) {
	for _, end := range []string{"pause", "job end", "session close"} {
		t.Run(end, func(t *testing.T) {
			s := newLocalSetup(t)
			s.start()
			floor := s.backend.local.host.floor
			item, err := floor.Create(factory.Item{Repo: "acme/api", Title: "Cedar memory", Cap: 1, State: factory.StateNew})
			if err != nil {
				t.Fatal(err)
			}
			entered, cancelled := make(chan struct{}), make(chan struct{})
			var memoryCalls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodGet {
					fmt.Fprint(w, `{"data":{"total_cost":0.02,"tokens_prompt":20,"tokens_completion":5}}`)
					return
				}
				var body struct {
					Messages []struct{ Content string } `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					return
				}
				isMemory := false
				for _, m := range body.Messages {
					var p map[string]json.RawMessage
					if json.Unmarshal([]byte(m.Content), &p) == nil && p["messages"] != nil {
						isMemory = true
					}
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if isMemory {
					n := memoryCalls.Add(1)
					fmt.Fprintf(w, "data: {\"id\":\"gen-memory-%d\",\"model\":%q,\"choices\":[{\"delta\":{\"content\":\"{\\\"claims\\\":[]}\"}}]}\n\n", n, knowledge.Model)
					w.(http.Flusher).Flush()
					if n == 1 {
						close(entered)
					}
					<-r.Context().Done()
					if n == 1 {
						close(cancelled)
					}
					return
				}
				fmt.Fprintf(w, "data: {\"model\":%q,\"choices\":[{\"delta\":{\"content\":\"Cedar queues jobs.\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":5,\"cost\":0.01}}\n\ndata: [DONE]\n\n", knowledge.Model)
			}))
			t.Cleanup(server.Close)
			original := http.DefaultClient
			transport := http.DefaultTransport
			http.DefaultClient = &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
				clone := r.Clone(r.Context())
				u := *r.URL
				clone.URL = &u
				if strings.Contains(u.Host, "openrouter.ai") {
					clone.URL.Scheme = "http"
					clone.URL.Host = server.Listener.Addr().String()
				}
				return transport.RoundTrip(clone)
			})}
			t.Cleanup(func() { http.DefaultClient = original })
			meter := &memoryDelayedJobBudget{stageBudget: &stageBudget{floor: floor, item: item}}
			workspace := t.TempDir()
			agent, err := session.New(session.Config{Workspace: workspace, SessionFile: filepath.Join(workspace, "chat.jsonl"), ProfileDir: filepath.Join(s.home, "memory-profile"), Model: knowledge.Model, APIKey: "fixture", BaseURL: server.URL, JobBudget: meter, Unattended: true, DeterministicNames: true})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = agent.Close() })
			vault := agent.Knowledge()
			if err := vault.StartBuild(); err != nil {
				t.Fatal(err)
			}
			events, err := agent.Submit(context.Background(), "Cedar queues jobs.")
			if err != nil {
				t.Fatal(err)
			}
			drained := make(chan struct{})
			go func() {
				for range events {
				}
				close(drained)
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("memory call did not start")
			}
			select {
			case <-drained:
			case <-time.After(3 * time.Second):
				t.Fatal("step stream still blocked on memory after 3 seconds; want a 2 second cap")
			}
			select {
			case <-cancelled:
				t.Fatal("step-end cap cancelled compilation instead of letting it continue")
			default:
			}
			// A later finished turn must not stack a second compiler behind this one.
			next, err := agent.Submit(context.Background(), "Cedar queues jobs.")
			if err != nil {
				t.Fatal(err)
			}
			later := make(chan struct{})
			go func() {
				for range next {
				}
				close(later)
			}()
			select {
			case <-later:
			case <-time.After(3 * time.Second):
				t.Fatal("later turn stacked behind unfinished memory")
			}
			if got := memoryCalls.Load(); got != 1 {
				t.Fatalf("stacked memory calls=%d", got)
			}
			switch end {
			case "pause":
				if err := agent.StopWork(); err != nil {
					t.Fatal(err)
				}
			case "job end":
				if err := floor.Annotate(item.ID, func(it *factory.Item) error { it.State = factory.StateShipped; return nil }); err != nil {
					t.Fatal(err)
				}
			case "session close":
				if err := agent.Close(); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case <-cancelled:
			case <-time.After(3 * time.Second):
				t.Fatalf("%s did not cancel in-flight memory", end)
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				session.FlushUsage()
				lines, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
				if err != nil {
					t.Fatal(err)
				}
				usd, calls := 0.0, 0
				for _, line := range lines {
					if line.Workspace == workspace && line.Role == "memory" {
						usd += line.USD
						calls += line.Calls
					}
				}
				job, err := floor.Get(item.ID)
				if err != nil {
					t.Fatal(err)
				}
				spent := factory.JobSpent(job)
				if calls == 1 && math.Abs(usd-.02) < .000001 && math.Abs(spent-.04) < .000001 && math.Abs(job.MemorySpent-.02) < .000001 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("late memory accounting incomplete: calls=%d ledger=%.4f job=%.4f memory=%.4f want .02/.04/.02", calls, usd, spent, job.MemorySpent)
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err := vault.Flush(context.Background(), "fixture", true); err == nil {
				t.Fatal("ended owner admitted memory")
			}
			if got := memoryCalls.Load(); got != 1 {
				t.Fatalf("memory calls after %s=%d", end, got)
			}
			snapshot, err := vault.Snapshot()
			if err != nil || snapshot.Pending == 0 {
				t.Fatalf("cancelled compile lost its pending source: %+v %v", snapshot, err)
			}
		})
	}
}

// The ledger can be observable before the job meter finishes its separate write.
// Delay that public budget callback so the fixture exercises this order reliably.
type memoryDelayedJobBudget struct {
	*stageBudget
}

func (b *memoryDelayedJobBudget) Spend(usd float64) {
	if usd == .02 {
		time.Sleep(100 * time.Millisecond)
	}
	b.stageBudget.Spend(usd)
}

func (b *memoryDelayedJobBudget) SpendMemory(usd float64) {
	if usd == .02 {
		time.Sleep(100 * time.Millisecond)
	}
	b.stageBudget.SpendMemory(usd)
}

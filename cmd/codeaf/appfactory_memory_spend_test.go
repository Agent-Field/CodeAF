package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The provider's own counter is independent of the knowledge receipts and the
// Spending ledger. Both compilation and reading use their exported interfaces.
func TestMemorySpendAndLifecycle(t *testing.T) {
	s := newLocalSetup(t)
	t.Run("funnel law", checkMemoryFunnel)
	var calls atomic.Int64
	model := knowledge.Model
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Stream         bool            `json:"stream"`
			ResponseFormat json.RawMessage `json:"response_format"`
			Messages       []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		calls.Add(1)
		result := `{"subjects":[]}`
		var payload map[string]json.RawMessage
		if len(body.Messages) > 1 {
			_ = json.Unmarshal([]byte(body.Messages[1].Content), &payload)
		}
		if payload["messages"] != nil {
			var sources []knowledge.Source
			if err := json.Unmarshal(payload["messages"], &sources); err != nil || len(sources) == 0 {
				t.Errorf("sources: %s", payload["messages"])
				return
			}
			content, _ := json.Marshal(map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": "Cedar queues jobs.", "topic": "Queues", "scope": "Cedar", "kind": "fact", "evidence": []any{map[string]any{"source": sources[0].ID, "quote": "Cedar queues jobs."}}}}})
			result = string(content)
		}
		if len(body.ResponseFormat) == 0 {
			result = "Cedar queues jobs."
		}
		if body.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			chunk, _ := json.Marshal(map[string]any{"model": model, "choices": []any{map[string]any{"delta": map[string]any{"content": result}, "finish_reason": nil}}})
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			fmt.Fprintf(w, "data: {\"model\":%q,\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":4400,\"completion_tokens\":100,\"cost\":0.01}}\n\ndata: [DONE]\n\n", model)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": model, "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": result}}}, "usage": map[string]any{"prompt_tokens": 4400, "completion_tokens": 100, "cost": 0.01}})
	}))
	defer server.Close()
	original := http.DefaultTransport
	http.DefaultTransport = roundTripper(func(r *http.Request) (*http.Response, error) {
		copy := r.Clone(r.Context())
		u := *r.URL
		copy.URL = &u
		copy.URL.Scheme = "http"
		copy.URL.Host = server.Listener.Addr().String()
		return original.RoundTrip(copy)
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	vault := knowledge.Open(filepath.Join(s.home, "profile"))
	if err := vault.StartBuild(); err != nil {
		t.Fatal(err)
	}
	if err := vault.Capture(knowledge.Source{ID: "source", Session: "job-chat", Role: "user", TS: time.Now().UTC().Format(time.RFC3339Nano), Text: "Cedar queues jobs."}); err != nil {
		t.Fatal(err)
	}
	if err := vault.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	hits, err := vault.Search("queues", 5)
	if err != nil || len(hits) != 1 {
		t.Fatalf("memory output lost: %v %v", hits, err)
	}
	before := calls.Load()
	if err := vault.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != before {
		t.Errorf("unchanged input: calls %d -> %d", before, calls.Load())
	}
	if !session.FlushUsage() {
		t.Fatal("ledger did not flush")
	}
	lines, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var ledgerCalls int
	var usd float64
	for _, line := range lines {
		if line.Role == "memory" {
			ledgerCalls += line.Calls
			usd += line.USD
		}
	}
	t.Logf("provider: calls=%d USD=%.4f; memory ledger: calls=%d USD=%.4f", calls.Load(), float64(calls.Load())*.01, ledgerCalls, usd)
	if int64(ledgerCalls) != calls.Load() || fmt.Sprintf("%.4f", usd) != fmt.Sprintf("%.4f", float64(calls.Load())*.01) {
		t.Errorf("memory spend missing: provider calls=%d USD=%.4f; ledger calls=%d USD=%.4f", calls.Load(), float64(calls.Load())*.01, ledgerCalls, usd)
	}

	t.Run("owned jobs", func(t *testing.T) {
		st := s.start()
		floor := s.backend.local.host.floor
		for n := 0; n < 3; n++ {
			item, err := floor.Create(factory.Item{Repo: "acme/api", Title: "README memory", State: factory.StateNew, Cap: 5})
			if err != nil {
				t.Fatal(err)
			}
			meter := &stageBudget{floor: floor, item: item}
			workspace := t.TempDir()
			if err := os.MkdirAll(filepath.Join(workspace, "bucket"), 0700); err != nil {
				t.Fatal(err)
			}
			agent, err := session.New(session.Config{Workspace: workspace, SessionFile: filepath.Join(workspace, "bucket", "chat.jsonl"), ProfileDir: filepath.Join(s.home, "job-profile"), Model: model, APIKey: "fixture", BaseURL: server.URL, JobBudget: meter, Unattended: true, DeterministicNames: true})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { agent.Close() })
			vault := agent.Knowledge()
			if vault == nil {
				t.Fatal("job has no knowledge vault")
			}
			if err := vault.StartBuild(); err != nil {
				t.Fatal(err)
			}
			start := calls.Load()
			events, err := agent.Submit(context.Background(), "Cedar queues jobs.")
			if err != nil {
				t.Fatal(err)
			}
			for event := range events {
				if event.Kind == session.EventError {
					t.Fatalf("turn: %v", event.Err)
				}
			}
			if !session.FlushUsage() {
				t.Fatal("ledger flush")
			}
			made := calls.Load() - start
			var job api.Job
			s.must("factory.job", appwire.FactoryJobParams{Factory: st.Factory, Job: item.ID}, &job)
			lines, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			var ownedCalls, memoryCalls int
			var ownedUSD float64
			for _, line := range lines {
				if line.Workspace == workspace {
					ownedCalls += line.Calls
					ownedUSD += line.USD
					if line.Role == "memory" {
						memoryCalls += line.Calls
					}
				}
			}
			t.Logf("job %d: provider calls=%d USD=%.4f; ledger calls=%d USD=%.4f; job header USD=%.4f; memory calls=%d", n+1, made, float64(made)*.01, ownedCalls, ownedUSD, job.Cost, memoryCalls)
			if made == 0 || int64(ownedCalls) != made || fmt.Sprintf("%.4f", ownedUSD) != fmt.Sprintf("%.4f", float64(made)*.01) || fmt.Sprintf("%.4f", job.Cost) != fmt.Sprintf("%.4f", ownedUSD) || memoryCalls == 0 {
				t.Fatalf("job's provider, ledger and header disagree")
			}
			hits, err := vault.Search("queues", 5)
			if err != nil || len(hits) == 0 {
				t.Fatalf("job memory output lost: %v %v", hits, err)
			}
			before := calls.Load()
			if err := vault.Flush(context.Background(), "fixture", true); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != before {
				t.Fatal("unchanged job input spent again")
			}
			if err := agent.StopWork(); err != nil {
				t.Fatal(err)
			}
			if err := vault.Capture(knowledge.Source{ID: fmt.Sprintf("paused-%d", n), Session: "paused", Role: "user", TS: time.Now().UTC().Format(time.RFC3339Nano), Text: "Cedar queues jobs.", Job: true}); err != nil {
				t.Fatal(err)
			}
			if err := vault.Flush(context.Background(), "fixture", true); err == nil {
				t.Fatal("paused job admitted compilation")
			}
			if err := agent.Close(); err != nil {
				t.Fatal(err)
			}
			if err := vault.Flush(context.Background(), "fixture", true); err == nil {
				t.Fatal("ended job admitted compilation")
			}
			if calls.Load() != before {
				t.Fatal("memory spent during Pause or after the end")
			}
			inventory, err := session.KnowledgeHistory(workspace).Inventory(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, journal := range inventory {
				if strings.Contains(journal.ID, workspace) {
					t.Fatal("process worker admitted an owned journal")
				}
			}
			if _, err := os.Stat(agent.SessionPath() + ".knowledge-job"); err != nil {
				t.Fatal("job journal has no durable ownership marker")
			}
		}
	})
}

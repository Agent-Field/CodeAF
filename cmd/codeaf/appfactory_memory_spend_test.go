package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
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
	t.Run("funnel law", checkMemoryFunnel)
	var calls atomic.Int64
	var blockNext atomic.Bool
	var failUpdates atomic.Bool
	var richModel atomic.Bool
	entered := make(chan struct{})
	model := knowledge.Model
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Stream         bool            `json:"stream"`
			ResponseFormat json.RawMessage `json:"response_format"`
			Messages       []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[]}`)
			return
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
		if payload["messages"] != nil && blockNext.CompareAndSwap(true, false) {
			close(entered)
			<-r.Context().Done()
			return
		}
		if payload["messages"] != nil {
			var sources []knowledge.Source
			if err := json.Unmarshal(payload["messages"], &sources); err != nil || len(sources) == 0 {
				t.Errorf("sources: %s", payload["messages"])
				return
			}
			content, _ := json.Marshal(map[string]any{"claims": []any{map[string]any{"local_id": "c", "text": "Cedar queues jobs.", "topic": "Queues", "scope": "Cedar", "kind": "fact", "evidence": []any{map[string]any{"source": sources[0].ID, "quote": "Cedar queues jobs."}}}}})
			result = string(content)
			if failUpdates.Load() {
				result = "{broken"
			}
		}
		if richModel.Load() && len(body.Messages) > 1 {
			var payload map[string]any
			if err := json.Unmarshal([]byte(body.Messages[1].Content), &payload); err != nil {
				t.Error(err)
				return
			}
			content, err := json.Marshal(memoryRichReply(payload))
			if err != nil {
				t.Error(err)
				return
			}
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
	t.Cleanup(server.Close)
	original := http.DefaultTransport
	originalClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		if !strings.Contains(r.URL.Host, "openrouter.ai") {
			return original.RoundTrip(r)
		}
		copy := r.Clone(r.Context())
		u := *r.URL
		copy.URL = &u
		copy.URL.Scheme = "http"
		copy.URL.Host = server.Listener.Addr().String()
		return original.RoundTrip(copy)
	})}
	t.Cleanup(func() { http.DefaultClient = originalClient })
	s := newLocalSetup(t)
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
	offProfile := filepath.Join(s.home, "profile")
	if err := config.WriteMemoryEnabled(offProfile, false); err != nil {
		t.Fatal(err)
	}
	saved, err := vault.Snapshot()
	if err != nil || saved.Enabled || len(saved.Pages) != 1 {
		t.Fatalf("off hid saved knowledge: %+v %v", saved, err)
	}
	if _, err := vault.Read(hits[0].Page.ID); err != nil {
		t.Fatal(err)
	}
	if offHits, err := vault.Search("queues", 5); err != nil || len(offHits) != 1 {
		t.Fatalf("off search lost knowledge: %v %v", offHits, err)
	}
	if err := config.WriteMemoryEnabled(offProfile, true); err != nil {
		t.Fatal(err)
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
			workspace := testHome(t)
			if err := os.MkdirAll(filepath.Join(workspace, "bucket"), 0700); err != nil {
				t.Fatal(err)
			}
			place := session.Place{Dir: filepath.Join(workspace, "bucket", session.NewSessionID())}
			agent, err := session.New(session.Config{Workspace: workspace, Place: place, SessionFile: place.Transcript(), ProfileDir: filepath.Join(s.home, "job-profile"), Model: model, APIKey: "fixture", BaseURL: server.URL, JobBudget: meter, Unattended: true, DeterministicNames: true})
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
			if n == 0 {
				t.Run("delegated reports", func(t *testing.T) {
					place := session.Place{Dir: filepath.Dir(agent.SessionPath())}
					if err := os.MkdirAll(place.NodeJournals(), 0700); err != nil {
						t.Fatal(err)
					}
					report := `{"type":"message","role":"assistant","content":"Cedar queues jobs.","timestamp":` + fmt.Sprintf("%q", time.Now().UTC().Format(time.RFC3339Nano)) + `,"presentation":{"audience":"operational"}}` + "\n"
					if err := os.WriteFile(filepath.Join(place.NodeJournals(), "1.jsonl"), []byte(report), 0600); err != nil {
						t.Fatal(err)
					}
					before := calls.Load()
					events, err := agent.Submit(context.Background(), "Cedar queues jobs.")
					if err != nil {
						t.Fatal(err)
					}
					for range events {
					}
					inventory, err := session.KnowledgeHistory(workspace).Inventory(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					var task knowledge.Journal
					for _, journal := range inventory {
						if strings.HasPrefix(journal.Session, "task:") {
							task = journal
						}
					}
					if task.ID == "" || !task.Job {
						t.Fatalf("delegated job report missing ownership: %+v", inventory)
					}
					batch, err := session.KnowledgeHistory(workspace).Read(context.Background(), task, knowledge.JournalCursor{Scope: task.Project})
					if err != nil || len(batch.Sources) != 1 {
						t.Fatalf("delegated report: %+v %v", batch, err)
					}
					// Re-capturing the same immutable source proves it was consumed by
					// the owner's finished turn rather than left for idle polling.
					batch.Sources[0].Job = true
					if err := vault.Capture(batch.Sources[0]); err != nil {
						t.Fatal(err)
					}
					snapshot, err := vault.Snapshot()
					if err != nil || snapshot.Pending != 0 {
						t.Fatalf("delegated report stayed pending after owner's turn: pending=%d error=%v", snapshot.Pending, err)
					}
					if calls.Load() <= before {
						t.Fatal("owner did not compile delegated report")
					}
				})
			}
			if n == 0 {
				before := calls.Load()
				for _, state := range []string{"paused", "published"} {
					if err := floor.Annotate(item.ID, func(it *factory.Item) error {
						if state == "paused" {
							it.Stream = &factory.Stream{Paused: true}
						} else {
							it.State = factory.StateShipped
							it.Stream = nil
						}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
					if err := vault.Flush(context.Background(), "fixture", true); err == nil {
						t.Errorf("%s job admitted memory compilation", state)
					}
				}
				if calls.Load() != before {
					t.Error("memory spent for a paused or published job")
				}
				if err := floor.Annotate(item.ID, func(it *factory.Item) error { it.State = factory.StateNew; it.Stream = nil; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			if n == 0 {
				inventory, err := session.KnowledgeHistory(workspace).Inventory(context.Background())
				if err != nil || len(inventory) < 1 {
					t.Fatalf("job inventory: %v %v", inventory, err)
				}
				var parent string
				for _, journal := range inventory {
					if !strings.HasPrefix(journal.Session, "task:") {
						parent = journal.Session
					}
				}
				for extra := 0; extra < 65; extra++ {
					if err := vault.Capture(knowledge.Source{ID: fmt.Sprintf("large-%d", extra), Session: parent, Role: "user", TS: time.Now().UTC().Format(time.RFC3339Nano), Text: "Cedar queues jobs.", Job: true}); err != nil {
						t.Fatal(err)
					}
				}
				if err := vault.Flush(context.Background(), "fixture", true); err != nil {
					t.Fatal(err)
				}
				snapshot, err := vault.Snapshot()
				if err != nil || snapshot.Pending != 0 {
					t.Errorf("finished-turn compilation left captured sources unpublished: pending=%d error=%v", snapshot.Pending, err)
				}
			}
			before := calls.Load()
			if err := vault.Flush(context.Background(), "fixture", true); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != before {
				t.Fatal("unchanged job input spent again")
			}
			if n == 2 {
				blockNext.Store(true)
				interrupted, err := agent.Submit(context.Background(), "Cedar queues jobs.")
				if err != nil {
					t.Fatal(err)
				}
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("memory request did not reach pause barrier")
				}
				if err := agent.StopWork(); err != nil {
					t.Fatal(err)
				}
				drained := make(chan struct{})
				go func() {
					for range interrupted {
					}
					close(drained)
				}()
				select {
				case <-drained:
				case <-time.After(5 * time.Second):
					t.Fatal("Pause did not cancel the memory request")
				}
				before = calls.Load()
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
			if n == 2 {
				history := &memoryObservedHistory{History: session.KnowledgeHistory(workspace, filepath.Join(s.home, "job-profile")), passes: make(chan struct{}, 4)}
				ctx, cancel := context.WithCancel(context.Background())
				done := make(chan error, 1)
				worker := knowledge.Open(filepath.Join(s.home, "job-profile"))
				go func() {
					done <- worker.Run(ctx, history, func() string { return "fixture" })
				}()
				for pass := 0; pass < 2; pass++ {
					// Memory off inventories only when its saved-chat count is requested.
					if err := worker.AskDiscovery(); err != nil {
						cancel()
						t.Fatal(err)
					}
					select {
					case <-history.passes:
					case <-time.After(5 * time.Second):
						cancel()
						t.Fatal("background inventory did not run")
					}
				}
				cancel()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("background worker did not stop")
				}
				if calls.Load() != before {
					t.Fatal("process worker spent on a finished job")
				}
			}

			inventory, err := session.KnowledgeHistory(workspace).Inventory(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			for _, journal := range inventory {
				if strings.Contains(journal.ID, workspace) && !journal.Job {
					t.Fatal("process worker admitted an owned journal")
				}
			}
			if _, err := os.Stat(agent.SessionPath() + ".knowledge-job"); err != nil {
				t.Fatal("job journal has no durable ownership marker")
			}
		}
	})
	t.Run("captured job migration", func(t *testing.T) {
		v := knowledge.Open(testHome(t))
		if err := v.StartBuild(); err != nil {
			t.Fatal(err)
		}
		source := knowledge.Source{ID: "old", Session: "old-job", Role: "user", TS: time.Now().UTC().Format(time.RFC3339Nano), Text: "Cedar queues jobs."}
		history := memoryFixtureHistory{source: source, journal: knowledge.Journal{ID: "old-journal", Session: source.Session, Size: 1}}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- v.Run(ctx, history, func() string { return "" }) }()
		deadline := time.Now().Add(3 * time.Second)
		for {
			snapshot, err := v.Snapshot()
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			if snapshot.Build.Phase == knowledge.BuildFailed {
				break
			}
			if time.Now().After(deadline) {
				cancel()
				t.Fatal("old journal cursor never imported")
			}
			time.Sleep(10 * time.Millisecond)
		}
		cancel()
		<-done
		history.journal.Job = true
		observed := &memoryObservedHistory{History: history, passes: make(chan struct{}, 4)}
		ctx, cancel = context.WithCancel(context.Background())
		defer cancel()
		before := calls.Load()
		go func() { done <- v.Run(ctx, observed, func() string { return "fixture" }) }()
		for pass := 0; pass < 2; pass++ {
			select {
			case <-observed.passes:
			case <-time.After(5 * time.Second):
				cancel()
				t.Fatal("migrated inventory did not run")
			}
		}
		cancel()
		<-done
		if calls.Load() != before {
			t.Errorf("already captured ended job still spent: %d provider calls", calls.Load()-before)
		}
	})
	t.Run("unchanged failed input", func(t *testing.T) {
		v := knowledge.Open(testHome(t))
		if err := v.StartBuild(); err != nil {
			t.Fatal(err)
		}
		source := knowledge.Source{ID: "failed", Session: "ordinary-chat", Role: "user", TS: time.Now().UTC().Format(time.RFC3339Nano), Text: "Cedar queues jobs."}
		history := memoryFixtureHistory{source: source, journal: knowledge.Journal{ID: "failed-journal", Session: source.Session, Size: 1}}
		failUpdates.Store(true)
		defer failUpdates.Store(false)
		before := calls.Load()
		// Three-attempt rule: an unchanged failed input makes exactly three attempts,
		// ordinaryBuildAttemptLimit in internal/knowledge/build.go. Failures wait 5, 10
		// and 20 seconds, so a fourth attempt would come at about 35 seconds.
		ctx, cancel := context.WithTimeout(context.Background(), 38*time.Second)
		defer cancel()
		_ = v.Run(ctx, history, func() string { return "fixture" })
		if got := calls.Load() - before; got != 3 {
			t.Errorf("unchanged failed input spent %d model calls, want exactly 3 and no further retries", got)
		}
	})
	t.Run("unchanged refinement", func(t *testing.T) {
		richModel.Store(true)
		defer richModel.Store(false)
		v := knowledge.Open(testHome(t))
		if err := v.StartBuild(); err != nil {
			t.Fatal(err)
		}
		for _, round := range []string{"baseline", "added"} {
			for _, scope := range []string{"Cedar", "Maple", "Birch"} {
				if err := v.Capture(knowledge.Source{ID: scope + round, Session: "chat", Role: "user", TS: time.Now().UTC().Format(time.RFC3339Nano), Text: scope + " " + round + " guidance."}); err != nil {
					t.Fatal(err)
				}
			}
			if err := v.Flush(context.Background(), "fixture", true); err != nil {
				t.Fatal(err)
			}
		}
		before := calls.Load()
		if err := v.Flush(context.Background(), "fixture", true); err != nil {
			t.Fatal(err)
		}
		if calls.Load() != before {
			t.Errorf("unchanged sources launched %d further refinement calls", calls.Load()-before)
		}
		if err := v.StartBuild(); err != nil {
			t.Fatal(err)
		}
		if err := v.Flush(context.Background(), "fixture", true); err != nil {
			t.Fatal(err)
		}
		if calls.Load() == before {
			t.Error("explicit Build did not advance retained refinement")
		}

	})
}

// This model supplies three independent concepts so unfinished refinement cannot
// hide behind an empty subject list. The production compiler owns every stage.
func memoryRichReply(p map[string]any) map[string]any {
	if messages, ok := p["messages"].([]any); ok {
		claims := []any{}
		for _, raw := range messages {
			s := raw.(map[string]any)
			scope := strings.Fields(s["text"].(string))[0]
			claims = append(claims, map[string]any{"local_id": s["id"], "text": s["text"], "scope": scope, "topic": "Operations", "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": s["text"]}}})
		}
		return map[string]any{"claims": claims}
	}
	if catalog, ok := p["catalog"].([]any); ok {
		subjects := []any{}
		for _, raw := range catalog {
			n := raw.(map[string]any)
			subjects = append(subjects, map[string]any{"title": n["scope"].(string) + " guide", "scope": n["scope"], "notes": []any{n["id"]}})
		}
		return map[string]any{"subjects": subjects}
	}
	if catalog, ok := p["consolidation_catalog"].([]any); ok {
		groups := []any{}
		for _, raw := range catalog {
			d := raw.(map[string]any)
			groups = append(groups, map[string]any{"members": []any{d["id"]}, "title": d["title"], "scope": d["scope"], "purpose": d["purpose"], "folder": d["folder"]})
		}
		return map[string]any{"groups": groups}
	}
	if p["remaining_steps"] != nil {
		refs := []any{}
		for _, raw := range p["claims"].([]any) {
			refs = append(refs, raw.(map[string]any)["id"])
		}
		scope := p["subject"].(map[string]any)["scope"].(string)
		return map[string]any{"action": "write", "sections": []any{map[string]any{"heading": "Current operations", "paragraphs": []any{map[string]any{"text": scope + " baseline and added guidance.", "claims": refs}}}}}
	}
	if p["paragraphs"] != nil {
		return map[string]any{"checks": []any{map[string]any{"id": "P0", "supported": true, "useful": true}}}
	}
	return map[string]any{"sections": []any{}, "questions": []any{}, "conflicts": []any{}}
}

type memoryFixtureHistory struct {
	source  knowledge.Source
	journal knowledge.Journal
}

func (h memoryFixtureHistory) Inventory(context.Context) ([]knowledge.Journal, error) {
	return []knowledge.Journal{h.journal}, nil
}
func (h memoryFixtureHistory) Read(context.Context, knowledge.Journal, knowledge.JournalCursor) (knowledge.JournalBatch, error) {
	return knowledge.JournalBatch{Sources: []knowledge.Source{h.source}, Cursor: knowledge.JournalCursor{Offset: 1}}, nil
}

// Observe the real worker through its exported history interface, without a
// timer assumption about when its first or second inventory happens.
type memoryObservedHistory struct {
	knowledge.History
	passes chan struct{}
}

func (h *memoryObservedHistory) Inventory(ctx context.Context) ([]knowledge.Journal, error) {
	journals, err := h.History.Inventory(ctx)
	select {
	case h.passes <- struct{}{}:
	default:
	}
	return journals, err
}

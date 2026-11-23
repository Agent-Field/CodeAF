package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A provider receipt is the independent total: keeping, rejecting or losing
// the brief must leave exactly that amount on both Spending and the job.
func TestAppFactoryBriefChargesEachProviderCallOnce(t *testing.T) {
	for _, outcome := range []string{"kept with dropped claim", "rejected draft", "keep unavailable", "remote worker", "manager and title"} {
		t.Run(outcome, func(t *testing.T) {
			s := newLocalSetup(t)
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					fmt.Fprint(w, `{"data":[]}`)
					return
				}
				var request struct {
					Stream   bool `json:"stream"`
					Messages []struct {
						Content string `json:"content"`
					} `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				calls.Add(1)
				draft := briefDraft
				for _, message := range request.Messages {
					if strings.Contains(message.Content, "You name conversations.") {
						draft = "Retry accounting"
					}
				}
				if outcome == "rejected draft" {
					draft = `{"verdict":""}`
				}
				raw, _ := json.Marshal(map[string]any{"model": "deepseek/deepseek-v4.1-flash", "choices": []any{map[string]any{"delta": map[string]any{"content": draft}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 20, "completion_tokens": 5, "cost": 0.01}})
				if !request.Stream {
					_ = json.NewEncoder(w).Encode(map[string]any{"model": "deepseek/deepseek-v4.1-flash", "choices": []any{map[string]any{"message": map[string]any{"content": draft}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 20, "completion_tokens": 5, "cost": .01}})
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", raw)
			}))
			t.Cleanup(server.Close)
			st := s.start()
			floor := s.backend.local.host.floor
			var manager *session.Agent
			s.backend.open = func(ask appOpenAsk) (*appConn, error) {
				agent, err := session.New(session.Config{Workspace: ask.Workspace, SessionFile: ask.Transcript, ProfileDir: filepath.Join(s.home, "job-profile"), Model: "deepseek/deepseek-v4.1-flash", APIKey: "fixture", BaseURL: server.URL, JobChat: jobChatDoorOf(floor), Unattended: true, DeterministicNames: true})
				if err != nil {
					return nil, err
				}
				manager = agent
				return &appConn{Conv: agent, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() { _ = agent.Close() }}, nil
			}
			it, err := floor.Create(factory.Item{Tier: factory.TierOwner, Repo: "acme/api", Num: 7, Title: "Fix retries", Kind: factory.KindIssue, State: factory.StateNeedsYou, Cap: 1,
				Stream: &factory.Stream{Phases: []factory.Phase{{Name: "test", Kind: factory.StageCheck, State: factory.PhaseDone, Round: 1, Result: &factory.StageResult{Done: true, Tests: &factory.TestCounts{Passed: 3}}}}}})
			if err != nil {
				t.Fatal(err)
			}
			var worker *client.WorkerClient
			var held api.WorkerPull
			var baseline factory.Item
			if outcome == "remote worker" {
				makeJobManager(t, s, it.ID)
				var chats appwire.FactoryJobChats
				s.must("factory.job_chats", map[string]any{"factory": st.Factory, "job": it.ID}, &chats)
				// local_start also runs this app's worker. Only the simulated
				// remote machine may take this job, independent of test order.
				if err := floor.Annotate(it.ID, func(cur *factory.Item) error {
					cur.State = factory.StateQueued
					cur.Assignee = "owner"
					cur.WorktreeMachine = "remote-fixture"
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				service := httptest.NewServer(s.backend.local.host.handler)
				t.Cleanup(service.Close)
				worker = client.Open(service.URL, s.backend.local.host.token).Worker("")
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if _, err := worker.Attach(ctx, api.WorkerAttach{Machine: "remote-fixture", Name: "Remote worker", Handle: "owner", Slots: 1}); err != nil {
					t.Fatal(err)
				}
				var ok bool
				held, ok, err = worker.Pull(ctx)
				if err != nil || !ok || held.Job.ID != it.ID {
					current, _ := floor.Get(it.ID)
					t.Fatalf("pull: job=%d ok=%v err=%v; current state=%s machine=%s", held.Job.ID, ok, err, current.State, current.WorktreeMachine)
				}
				baseline, err = floor.Get(it.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			if outcome == "keep unavailable" {
				hub := s.backend.factoryHub()
				original := hub.client.Transport
				hub.client.Transport = roundTripper(func(r *http.Request) (*http.Response, error) {
					if strings.HasSuffix(r.URL.Path, "/brief/keep") {
						return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"fixture unavailable"}`))}, nil
					}
					if original == nil {
						return http.DefaultTransport.RoundTrip(r)
					}
					return original.RoundTrip(r)
				})
			}
			var got appwire.FactoryJobBriefWritten
			if outcome == "keep unavailable" {
				_, code, _ := s.call("factory.job_brief_write", map[string]any{"factory": st.Factory, "job": it.ID})
				if code == "" {
					t.Fatal("keep unexpectedly succeeded")
				}
			} else {
				s.must("factory.job_brief_write", map[string]any{"factory": st.Factory, "job": it.ID}, &got)
				if got.Kept != (outcome == "kept with dropped claim" || outcome == "remote worker" || outcome == "manager and title") {
					t.Fatalf("write: %+v", got)
				}
				if got.Kept && len(got.Drops) != 1 {
					t.Fatalf("drops: %+v", got.Drops)
				}
			}
			if outcome == "manager and title" {
				events, err := manager.Submit(context.Background(), "Repair retry cost accounting in the job header.")
				if err != nil {
					t.Fatal(err)
				}
				for event := range events {
					if event.Kind == session.EventError {
						t.Fatal(event.Err)
					}
				}
				// Managers already have a saved factory title. An unnamed owned
				// stage exercises real naming alongside the manager's turn.
				named, err := session.New(session.Config{Workspace: t.TempDir(), SessionFile: filepath.Join(t.TempDir(), "stage.jsonl"), ProfileDir: filepath.Join(s.home, "stage-profile"), Model: "deepseek/deepseek-v4.1-flash", APIKey: "fixture", BaseURL: server.URL, JobBudget: &stageBudget{floor: floor, item: it}, Unattended: true})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = named.Close() })
				events, err = named.Submit(context.Background(), "Repair retry cost accounting in the job header.")
				if err != nil {
					t.Fatal(err)
				}
				for event := range events {
					if event.Kind == session.EventError {
						t.Fatal(event.Err)
					}
				}
				deadline := time.Now().Add(5 * time.Second)
				for named.Title() != "Retry accounting" {
					if time.Now().After(deadline) {
						t.Fatalf("title did not finish: %q", named.Title())
					}
					time.Sleep(10 * time.Millisecond)
				}
				if err := named.Close(); err != nil {
					t.Fatal(err)
				}
				if err := manager.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if worker != nil {
				mirror, err := store.Open(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				if err := mirror.Save(baseline); err != nil {
					t.Fatal(err)
				}
				remote, err := session.New(session.Config{Workspace: t.TempDir(), SessionFile: filepath.Join(t.TempDir(), "worker.jsonl"), ProfileDir: filepath.Join(s.home, "worker-profile"), Model: "deepseek/deepseek-v4.1-flash", APIKey: "fixture", BaseURL: server.URL, JobBudget: &stageBudget{floor: mirror, item: baseline}, Unattended: true, DeterministicNames: true})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = remote.Close() })
				answer, err := remote.RunErrand(context.Background(), session.ErrandAsk{Purpose: "worker_fixture", Role: "brief", System: "Return a brief.", Messages: []session.ErrandMessage{{Role: "user", Text: "Write it."}}, MaxTokens: 100, Deadline: time.Second})
				if err != nil || answer.Usage.CostUSD != .01 {
					t.Fatalf("worker receipt: %+v %v", answer, err)
				}
				current, err := mirror.Get(it.ID)
				if err != nil {
					t.Fatal(err)
				}
				report, ok := factoryrun.Diff(baseline, current)
				if !ok || math.Abs(report.Cost-.01) > 1e-9 {
					t.Fatalf("worker delta: %+v", report)
				}
				if err := worker.Report(context.Background(), held.Lease.ID, api.FromRunReport(report)); err != nil {
					t.Fatal(err)
				}
			}
			if !session.FlushUsage() {
				t.Fatal("ledger did not flush")
			}
			lines, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			ledger := 0.0
			titleCalls := 0
			for _, line := range lines {
				if line.FactoryJob == factory.LedgerName(it) {
					ledger += line.USD
					if line.Role == "title" {
						titleCalls += line.Calls
					}
				}
			}
			var header api.Job
			s.must("factory.job", map[string]any{"factory": st.Factory, "job": it.ID}, &header)
			provider := float64(calls.Load()) * .01
			t.Logf("calls=%d provider=%.4f ledger=%.4f header=%.4f", calls.Load(), provider, ledger, header.Cost)
			// The model split is the same money, so it charges once too.
			final, err := floor.Get(it.ID)
			if err != nil {
				t.Fatal(err)
			}
			var split float64
			for _, row := range factory.ModelUses(final) {
				split += row.USD
			}
			if math.Abs(split-header.Cost) > 1e-9 {
				t.Errorf("model split %.4f, header %.4f: %+v", split, header.Cost, factory.ModelUses(final))
			}
			wantCalls, wantCost := int64(1), .01
			if worker != nil {
				wantCalls, wantCost = 2, .02
			}
			if outcome == "manager and title" {
				wantCalls, wantCost = 4, .04
				if titleCalls != 1 {
					t.Errorf("title calls=%d, want 1", titleCalls)
				}
			}
			if calls.Load() != wantCalls || math.Abs(ledger-wantCost) > 1e-9 || math.Abs(header.Cost-wantCost) > 1e-9 {
				t.Errorf("one call must charge once: provider=%.4f ledger=%.4f header=%.4f; want %.4f each", provider, ledger, header.Cost, wantCost)
			}
		})
	}
}

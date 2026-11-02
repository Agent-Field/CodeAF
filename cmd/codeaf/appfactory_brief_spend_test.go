package main

import (
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
	"github.com/Agent-Field/codeaf/internal/session"
)

// A provider receipt is the independent total: keeping, rejecting or losing
// the brief must leave exactly that amount on both Spending and the job.
func TestAppFactoryBriefChargesEachProviderCallOnce(t *testing.T) {
	for _, outcome := range []string{"kept with dropped claim", "rejected draft", "keep unavailable"} {
		t.Run(outcome, func(t *testing.T) {
			s := newLocalSetup(t)
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					fmt.Fprint(w, `{"data":[]}`)
					return
				}
				calls.Add(1)
				draft := briefDraft
				if outcome == "rejected draft" {
					draft = `{"verdict":""}`
				}
				raw, _ := json.Marshal(map[string]any{"model": "deepseek/deepseek-v4.1-flash", "choices": []any{map[string]any{"delta": map[string]any{"content": draft}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 20, "completion_tokens": 5, "cost": 0.01}})
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", raw)
			}))
			t.Cleanup(server.Close)
			st := s.start()
			floor := s.backend.local.host.floor
			s.backend.open = func(ask appOpenAsk) (*appConn, error) {
				agent, err := session.New(session.Config{Workspace: ask.Workspace, SessionFile: ask.Transcript, ProfileDir: filepath.Join(s.home, "job-profile"), Model: "deepseek/deepseek-v4.1-flash", APIKey: "fixture", BaseURL: server.URL, JobChat: jobChatDoorOf(floor), Unattended: true, DeterministicNames: true})
				if err != nil {
					return nil, err
				}
				return &appConn{Conv: agent, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() { _ = agent.Close() }}, nil
			}
			it, err := floor.Create(factory.Item{Repo: "acme/api", Num: 7, Title: "Fix retries", Kind: factory.KindIssue, State: factory.StateNeedsYou, Cap: 1,
				Stream: &factory.Stream{Phases: []factory.Phase{{Name: "test", Kind: factory.StageCheck, State: factory.PhaseDone, Round: 1, Result: &factory.StageResult{Done: true, Tests: &factory.TestCounts{Passed: 3}}}}}})
			if err != nil {
				t.Fatal(err)
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
				if got.Kept != (outcome == "kept with dropped claim") {
					t.Fatalf("write: %+v", got)
				}
				if got.Kept && len(got.Drops) != 1 {
					t.Fatalf("drops: %+v", got.Drops)
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
			for _, line := range lines {
				if line.FactoryJob == factory.LedgerName(it) {
					ledger += line.USD
				}
			}
			var header api.Job
			s.must("factory.job", map[string]any{"factory": st.Factory, "job": it.ID}, &header)
			provider := float64(calls.Load()) * .01
			t.Logf("calls=%d provider=%.4f ledger=%.4f header=%.4f", calls.Load(), provider, ledger, header.Cost)
			if calls.Load() != 1 || math.Abs(ledger-.01) > 1e-9 || math.Abs(header.Cost-.01) > 1e-9 {
				t.Errorf("one call must charge once: provider=%.4f ledger=%.4f header=%.4f; want 0.0100 each", provider, ledger, header.Cost)
			}
		})
	}
}

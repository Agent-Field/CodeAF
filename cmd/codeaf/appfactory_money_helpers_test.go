package main

import (
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestAppFactoryDebouncedEndMoney(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		name := "finished"
		if stopped {
			name = "stopped"
		}
		t.Run(name, func(t *testing.T) {
			s := newLocalSetup(t)
			var calls atomic.Int64
			server := moneyProvider(t, &calls)
			st := s.start()
			s.backend.open = func(ask appOpenAsk) (*appConn, error) {
				agent := moneyAgent(t, s, ask, server.URL)
				return &appConn{Conv: agent, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() { _ = agent.Close() }}, nil
			}
			floor := s.backend.local.host.floor
			it, err := floor.Create(factory.Item{Repo: "acme/api", Num: 7, Title: "Final brief accounting", Kind: factory.KindIssue, State: factory.StateNeedsYou, Cap: 1, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "proof", Kind: factory.StageCheck, State: factory.PhaseDone}}}})
			if err != nil {
				t.Fatal(err)
			}
			params := map[string]any{"factory": st.Factory, "job": it.ID}
			var chats appwire.FactoryJobChats
			s.must("factory.job_chats", params, &chats)
			if stopped {
				s.must("agents.stop", map[string]any{"id": chats.Agent}, &struct{}{})
			}
			// Keep the production lifecycle callback and debounce, shortening only
			// its wait so the test does not spend the user's debounce interval.
			s.backend.briefs.stop()
			s.backend.briefs = newBriefDebounce(10*time.Millisecond, s.backend.writeBriefLater)
			if err := floor.Update(it.ID, func(cur *factory.Item) error {
				cur.State = factory.StateShipped
				cur.Stream.Ended = time.Now()
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			var brief appwire.FactoryJobBrief
			for {
				s.must("factory.job_brief", params, &brief)
				if brief.Status == factory.BriefStatusReady || brief.Status == factory.BriefStatusFailed {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("debounced end did not finish: %+v", brief)
				}
				time.Sleep(10 * time.Millisecond)
			}
			want := .01
			if stopped {
				want = 0
				if brief.Failure != session.ErrandStopped {
					t.Errorf("stopped brief failure=%q, want stopped", brief.Failure)
				}
			}
			moneyTotals(t, s, st.Factory, it, &calls, want)
		})
	}
}

func TestAppFactoryPageAIMoneyStaysOutsideJob(t *testing.T) {
	s := newLocalSetup(t)
	var calls atomic.Int64
	server := moneyProvider(t, &calls)
	st := s.start()
	s.backend.open = func(ask appOpenAsk) (*appConn, error) {
		agent := moneyAgent(t, s, ask, server.URL)
		return &appConn{Conv: agent, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() { _ = agent.Close() }}, nil
	}
	it, err := s.backend.local.host.floor.Create(factory.Item{Repo: "acme/api", Num: 7, Title: "Viewed job", Kind: factory.KindIssue, State: factory.StateNeedsYou, Cap: 1})
	if err != nil {
		t.Fatal(err)
	}
	s.backend.mint = appMint
	var created struct {
		Agent appwire.Agent `json:"agent"`
	}
	s.must("agents.create", map[string]any{"workspace": t.TempDir()}, &created)
	for _, path := range []struct{ purpose, role string }{{"pipeline_ai", "insight_pipeline"}, {"view_topic", "insight_views"}} {
		var answer appwire.Errand
		s.must("agents.errand", appwire.ErrandParams{Agent: created.Agent.ID, Purpose: path.purpose, Role: path.role, System: "Answer briefly.", Messages: []appwire.ErrandMessage{{Role: "user", Text: "Summarize."}}, MaxTokens: 100}, &answer)
		if answer.Reason != "" || math.Abs(answer.Usage.CostUSD-.01) > 1e-9 {
			t.Fatalf("%s: %+v", path.purpose, answer)
		}
	}
	if !session.FlushUsage() {
		t.Fatal("ledger did not flush")
	}
	lines, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var ledger float64
	roles := map[string]int{}
	for _, line := range lines {
		ledger += line.USD
		roles[line.Role] += line.Calls
		if line.FactoryJob != "" {
			t.Errorf("page AI charged job %q", line.FactoryJob)
		}
	}
	day := time.Now().Format("2006-01-02")
	var spending appwire.SpendReportResult
	s.must("spend.report", map[string]any{"from": day, "to": day}, &spending)
	var header api.Job
	s.must("factory.job", map[string]any{"factory": st.Factory, "job": it.ID}, &header)
	if calls.Load() != 2 || math.Abs(ledger-.02) > 1e-9 || spending.Totals.CostUSD == nil || math.Abs(*spending.Totals.CostUSD-.02) > 1e-9 || header.Cost != 0 || roles["insight_pipeline"] != 1 || roles["insight_views"] != 1 {
		t.Fatalf("provider=%.4f ledger=%.4f spending=%+v job=%.4f roles=%v", float64(calls.Load())*.01, ledger, spending.Totals, header.Cost, roles)
	}
	t.Logf("provider=0.0200 ledger=%.4f Spending=%.4f viewed job=%.4f", ledger, *spending.Totals.CostUSD, header.Cost)
}

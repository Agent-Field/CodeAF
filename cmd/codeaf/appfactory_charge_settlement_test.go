package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestFactoryChargedCallsReachLedger(t *testing.T) {
	for _, mode := range []string{"failed turn", "failed errand", "unmetered turn", "unmetered errand", "unknown turn", "unknown errand", "unpriced receipt"} {
		t.Run(mode, func(t *testing.T) {
			s := newLocalSetup(t)
			st := s.start()
			floor := s.backend.local.host.floor
			it, err := floor.Create(factory.Item{Tier: factory.TierOwner, Repo: "acme/api", Num: 7, Title: "Repair accounting", Kind: factory.KindIssue, State: factory.StateNeedsYou, Cap: 10})
			if err != nil {
				t.Fatal(err)
			}
			failed := mode == "failed turn" || mode == "failed errand"
			unknown := mode == "unknown turn" || mode == "unknown errand" || mode == "unpriced receipt"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					if r.URL.Path == "/generation" && mode == "unpriced receipt" {
						fmt.Fprint(w, `{"data":{"tokens_prompt":20,"tokens_completion":5}}`)
					} else if r.URL.Path == "/generation" {
						fmt.Fprint(w, `{"data":{"total_cost":0.12,"tokens_prompt":20,"tokens_completion":5}}`)
					} else {
						fmt.Fprint(w, `{"data":[]}`)
					}
					return
				}
				var request struct {
					Stream bool `json:"stream"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				id := "gen-factory-charge"
				if unknown && mode != "unpriced receipt" {
					id = ""
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: {\"id\":%q,\"model\":\"deepseek/deepseek-v4.1-flash\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Done.\"}}]}\n\n", id)
				if failed {
					fmt.Fprint(w, "data: {\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"cost\":0.10},\"choices\":[]}\n\n")
					fmt.Fprint(w, "data: {\"error\":{\"code\":400,\"message\":\"fixture failure\"}}\n\n")
				} else {
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
				}
			}))
			t.Cleanup(server.Close)
			agent, err := session.New(session.Config{Workspace: t.TempDir(), SessionFile: filepath.Join(t.TempDir(), "stage.jsonl"), ProfileDir: filepath.Join(s.home, "stage-profile"), Model: "deepseek/deepseek-v4.1-flash", APIKey: "fixture", BaseURL: server.URL, JobBudget: &stageBudget{floor: floor, item: it}, Unattended: true, DeterministicNames: true, OneModel: true})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = agent.Close() })
			if mode == "failed errand" || mode == "unmetered errand" || mode == "unknown errand" {
				_, err = agent.RunErrand(context.Background(), session.ErrandAsk{Purpose: "charge_fixture", Role: "brief", System: "Answer briefly.", Messages: []session.ErrandMessage{{Role: "user", Text: "Finish."}}, MaxTokens: 100, Deadline: time.Second})
				if err != nil {
					t.Fatal(err)
				}
			} else {
				events, err := agent.Submit(context.Background(), "Finish.")
				if err != nil {
					t.Fatal(err)
				}
				for range events {
				}
			}
			deadline := time.Now().Add(4 * time.Second)
			for {
				if !session.FlushUsage() {
					t.Fatal("ledger flush timed out")
				}
				lines, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
				if err != nil {
					t.Fatal(err)
				}
				var cost float64
				rows, missing := 0, 0
				for _, line := range lines {
					if line.FactoryJob == factory.LedgerName(it) {
						cost += line.USD
						rows++
						if line.Unbilled {
							missing++
						}
					}
				}
				var header api.Job
				s.must("factory.job", map[string]any{"factory": st.Factory, "job": it.ID}, &header)
				var usage appwire.FactoryUsage
				day := time.Now().Format(time.DateOnly)
				s.must("factory.usage_by_model", map[string]any{"factory": st.Factory, "job": it.ID, "from": day, "to": day}, &usage)
				valid := rows == 1 && math.Abs(cost-.12) < 1e-9 && math.Abs(header.Cost-.12) < 1e-9
				if unknown {
					valid = rows == 1 && missing == 1 && cost == 0 && len(usage.Models) == 1 && usage.Models[0].CostUSD == nil && usage.Models[0].UnbilledCalls == 1
				}

				if valid {
					var spend appwire.SpendReportResult
					s.must("spend.report", map[string]any{"from": day, "to": day}, &spend)
					if unknown && (spend.Totals.UnbilledRecords != 1 || spend.Totals.CostUSD != nil) {
						t.Fatalf("Spending unknown price: %+v", spend.Totals)
					}
					if !unknown && (spend.Totals.CostUSD == nil || math.Abs(*spend.Totals.CostUSD-.12) > 1e-9) {
						t.Fatalf("Spending charge: %+v", spend.Totals)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("charge settlement: ledger=%.2f header=%.2f rows=%d unknown=%d usage=%+v", cost, header.Cost, rows, missing, usage.Models)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
	t.Run("queue overflow", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), session.UsageLedgerName)
		t.Cleanup(func() { session.StopUsageWriter(path) })
		const count = 20000
		for i := 0; i < count; i++ {
			session.RecordUsage(path, session.UsageLine{Model: "fixture", Calls: 1, USD: .01})
		}
		if !session.FlushUsage() {
			t.Fatal("ledger flush timed out")
		}
		rows, err := session.ReadUsage(path, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != count {
			t.Fatalf("charge rows on disk=%d, want %d", len(rows), count)
		}
	})
}

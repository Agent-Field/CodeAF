package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/route/judge"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// An AI filter's question goes out as the fixed system instruction and the
// user message, and what it cost is ONE row on the spend ledger under
// `factory filter`, which session counts as the factory's own AI.
func TestFactoryJudgeBooksItsQuestionOnTheLedger(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	type message struct{ Role, Content string }
	sent := make(chan []message, 1)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Messages []message }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		sent <- req.Messages
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "m/judge",
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": `{"yes":true,"why":"A small Safari crash with steps."}`}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 300, "completion_tokens": 12, "cost": 0.0007},
		})
	}))
	defer endpoint.Close()
	settings := config.Config{APIKey: "sk-test", BaseURL: endpoint.URL, Model: "profile/default", ProfileDir: t.TempDir()}
	j := factoryJudge(nil, func() (config.Config, error) { return settings, nil })
	ctx := judge.WithRuleModel(context.Background(), "m/judge")
	it := factory.Item{Repo: "acme/api", Title: "Login crashes on Safari", Body: "Steps attached.", Tier: factory.TierCollab}
	v, err := j.Judge(ctx, "Is this a small bug fix?", it)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Yes || v.Model != "m/judge" {
		t.Fatalf("verdict %+v; want a yes from m/judge", v)
	}
	got := <-sent
	if len(got) != 2 || got[0].Role != "system" || got[0].Content != judge.System || got[1].Role != "user" {
		t.Fatalf("sent %+v; want the fixed system instruction then the question", got)
	}
	if !session.FlushUsage() {
		t.Fatal("flush")
	}
	rows, err := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var booked []session.UsageLine
	for _, row := range rows {
		if row.Role == factoryJudgeName {
			booked = append(booked, row)
		}
	}
	if len(booked) != 1 || booked[0].USD != 0.0007 || booked[0].Calls != 1 {
		t.Fatalf("ledger rows under %q: %+v; want one row of $0.0007", factoryJudgeName, booked)
	}
	if !session.IsFactoryAIPurpose(factoryJudgeName) {
		t.Fatalf("%q is not counted as the factory's own AI", factoryJudgeName)
	}
}

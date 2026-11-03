package main

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestStepCostMatchesJobSpendLedgerAcrossResumedRounds(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"model":"deepseek/deepseek-v4.1-flash","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":5,"cost":0.01}}`)
	}))
	defer server.Close()
	st, _ := chatSpendFloor(t)
	it, err := st.Create(factory.Item{Repo: "acme/api", Num: 7, State: factory.StateRunning, Talk: filepath.Join(t.TempDir(), "manager.jsonl"), Stages: []factory.Stage{{Name: "proof", Kind: factory.StageChat, On: true}}, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "proof", Kind: factory.StageChat, Round: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	serverItem := api.ToItem(api.WorkerJob(it))
 baseline := api.ToItem(api.WorkerJob(it))
 flush := func() {
 t.Helper()
 paid,err:=st.Get(it.ID); if err!=nil {t.Fatal(err)}
 delta,_:=factoryrun.Diff(baseline,paid)
 wire:=api.ToWorkerReport(api.FromRunReport(delta))
 if err:=factoryrun.ApplyReport(&serverItem,wire,time.Now());err!=nil{t.Fatal(err)}
 // Transport idempotency handles Cost deltas. Table replacement itself
 // must also tolerate repeats without charging those rows again.
 if wire.Cost == 0 { if err:=factoryrun.ApplyReport(&serverItem,wire,time.Now());err!=nil{t.Fatal(err)} }
 baseline=api.ToItem(api.WorkerJob(paid))
 }
 // Two rounds; round two is interrupted and reopened with a new live meter.
	for admission, round := range []int{1, 2, 2, 0} {
		if err := st.Annotate(it.ID, func(cur *factory.Item) error {
			if round > 0 {
				cur.Stream.Phases[0].Round = round
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		current, _ := st.Get(it.ID)
		b := &stageBudget{floor: st, item: current, stage: "proof", round: round}
		cfg := session.Config{Workspace: t.TempDir(), SessionFile: filepath.Join(t.TempDir(), "stage.jsonl"), ProfileDir: t.TempDir(), Model: "deepseek/deepseek-v4.1-flash", APIKey: "fixture", BaseURL: server.URL, JobBudget: b, Unattended: true, DeterministicNames: true}
		if round == 0 {
			cfg.JobBudget = nil
			cfg.JobChat = jobChatDoorOf(st)
			cfg.SessionFile = it.Talk
		}
		agent, err := session.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		_, err = agent.RunErrand(context.Background(), session.ErrandAsk{Purpose: "step_cost_fixture", Role: "brief", System: "Return ok.", Messages: []session.ErrandMessage{{Role: "user", Text: "Check."}}, MaxTokens: 100, Deadline: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if round == 0 {
			_ = agent.Close()
            flush()
			continue
		}
		c := &stageConversation{agent: agent, budget: b}
		wantRound := .01
		if admission == 2 {
			wantRound = .02
		}
		if math.Abs(c.Spent()-wantRound) > 1e-9 {
			t.Fatalf("round %d cumulative report = %.4f, want %.4f", round, c.Spent(), wantRound)
		}
		res := factory.StageResult{Done: true, Spent: c.Spent(), SpendRecorded: true, Chat: fmt.Sprintf("round-%d", round)}
		_ = agent.Close()
		// A transport retry writes exactly the same report twice, through its codec.
		report := api.ToWorkerReport(api.FromRunReport(factory.RunReport{Stages: []factory.StageReport{{Name: "proof", Round: round, State: factory.PhaseRunning, Result: &res}}}))
		for n := 0; n < 2; n++ {
			if err := st.Annotate(it.ID, func(cur *factory.Item) error { return factoryrun.ApplyReport(cur, report, time.Now()) }); err != nil {
				t.Fatal(err)
			}
		}
		flush()
		// Reload the saved job for the next admission.
		it, err = st.Get(it.ID)
		if err != nil {
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
	for _, line := range lines {
		if line.FactoryJob == factory.LedgerName(it) {
			ledger += line.USD
		}
	}
	got := serverItem
	cost := 0.0
	for _, step := range api.FromItem(got).Steps {
		cost += step.Cost
	}
	if math.Abs(ledger-.04) > 1e-9 {
		t.Fatalf("tagged ledger = %.4f, want .0400", ledger)
	}
	if math.Abs(factory.JobSpent(got)-ledger)>1e-9 { t.Fatalf("job spend %.4f, tagged ledger %.4f",factory.JobSpent(got),ledger) }
 // The manager made one $.01 call; title and memory calls are absent.
	if math.Abs(cost-(ledger-.01)) > 1e-9 {
		t.Fatalf("step cost = %.4f, tagged job ledger = %.4f; manager spend = .0100; other non-step spend = 0", cost, ledger)
	}
}

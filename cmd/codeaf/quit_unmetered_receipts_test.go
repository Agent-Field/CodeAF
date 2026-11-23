package main

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

type unmeteredReceiptBudget struct{}

func (*unmeteredReceiptBudget) Spend(float64)      {}
func (*unmeteredReceiptBudget) Refusal() string    { return "" }
func (*unmeteredReceiptBudget) FactoryJob() string { return "acme/api#2332" }
func TestProcessCloseSettlesUnmeteredUsageReceipt(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	requested := make(chan struct{})
	release := make(chan struct{})
	var once, releaseOnce sync.Once
	finish := func() { releaseOnce.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/generation" {
			once.Do(func() { close(requested) })
			<-release
			fmt.Fprint(w, `{"data":{"total_cost":0.032296144,"tokens_prompt":116000,"tokens_completion":40}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"review2-unmetered-process\",\"model\":\"deepseek/deepseek-v4.1-flash\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"A whole answer.\"}}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	var stagePending atomic.Int32
	a, err := session.New(session.Config{Workspace: t.TempDir(), SessionFile: filepath.Join(t.TempDir(), "session.jsonl"), ProfileDir: t.TempDir(), APIKey: "fixture", BaseURL: server.URL, Model: "deepseek/deepseek-v4.1-flash", OneModel: true, Unattended: true, DeterministicNames: true, JobBudget: &unmeteredReceiptBudget{}, JobPendingSpend: func() func() { stagePending.Add(1); return func() { stagePending.Add(-1) } }})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ledger := session.UsageLedgerPath()
	total := func() float64 {
		session.FlushUsage()
		rows, e := session.ReadUsage(ledger, time.Time{})
		if e != nil {
			t.Fatal(e)
		}
		var got float64
		for _, r := range rows {
			if r.GenerationID == "review2-unmetered-process" {
				got += r.USD
			}
		}
		return got
	}
	defer func() {
		finish()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if got := total(); math.Abs(got-.032296144) < 1e-12 {
				t.Logf("after release expected=$0.032296144 recorded=$%.9f", got)
				session.StopUsageWriter(ledger)
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Error("receipt did not settle")
		session.StopUsageWriter(ledger)
	}()
	_, err = a.RunErrand(context.Background(), session.ErrandAsk{Purpose: "review_fixture", Role: "brief", System: "Answer briefly", Messages: []session.ErrandMessage{{Role: "user", Text: "Finish."}}, MaxTokens: 100, Deadline: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-requested:
	case <-time.After(5 * time.Second):
		t.Fatal("receipt not admitted")
	}
	if got := stagePending.Load(); got != 0 {
		t.Fatalf("unmetered receipt delayed Factory stage readiness: %d", got)
	}
	p := &v3Process{}
	p.track(a)
	closed := make(chan struct{})
	go func() { p.closeAll(); close(closed) }()
	select {
	case <-closed:
		got := total()
		t.Logf("real v3Process.closeAll returned while receipt pending; expected owed=$0.032296144 recorded=$%.9f", got)
		t.Error("quit returned before admitted unmetered receipt settled")
	case <-time.After(100 * time.Millisecond):
		finish()
		<-closed
		if got := total(); math.Abs(got-.032296144) > 1e-12 {
			t.Errorf("process close returned before banking: $%.9f", got)
		}
	}
}

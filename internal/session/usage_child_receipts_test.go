package session

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
)

func TestRetiredTaskChildUsageReceiptWait(t *testing.T) {
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
		fmt.Fprint(w, "data: {\"id\":\"review2-unmetered-process\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"A partial answer\"}}]}\n\ndata: {\"error\":{\"code\":400,\"message\":\"fixture cut\"}}\n\n")
	}))
	defer server.Close()
	a, err := New(Config{Workspace: t.TempDir(), SessionFile: filepath.Join(t.TempDir(), "session.jsonl"), ProfileDir: t.TempDir(), APIKey: "fixture", BaseURL: server.URL, Model: "deepseek/deepseek-v4.1-flash", OneModel: true, Unattended: true, DeterministicNames: true})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	ledger := UsageLedgerPath()
	total := func() float64 {
		FlushUsage()
		rows, e := ReadUsage(ledger, time.Time{})
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
				StopUsageWriter(ledger)
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Error("receipt did not settle")
		StopUsageWriter(ledger)
	}()
	child, _ := workerFor(t, a, taskSpec{title: "fixture child", brief: "b", acceptance: "a", depth: 1})
	defer child.Close()
	_, err = child.RunErrand(context.Background(), ErrandAsk{Purpose: "review_fixture", Role: "brief", System: "Answer briefly", Messages: []ErrandMessage{{Role: "user", Text: "Finish."}}, MaxTokens: 100, Deadline: time.Second})
	t.Logf("RunErrand partial response error=%v", err)
	select {
	case <-requested:
	case <-time.After(5 * time.Second):
		t.Fatal("receipt not admitted")
	}
	wait, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if err := child.WaitReceipts(wait); err != context.DeadlineExceeded {
		t.Fatalf("child pending receipt = %v", err)
	}
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	waitRoot, cancelRoot := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancelRoot()
	err = a.WaitReceipts(waitRoot)
	got := total()
	t.Logf("retired child: root wait=%v, owed=$0.032296144, recorded=$%.9f", err, got)
	if err != context.DeadlineExceeded {
		t.Error("root wait missed retired child's admitted receipt")
	}
}

package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestHardCapStopsProviderRetryAndFallback(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	started, release := make(chan struct{}), make(chan struct{})
	var sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if sends.Add(1) == 1 {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}
		http.Error(w, `{"error":{"message":"fixture unavailable","code":503}}`, http.StatusServiceUnavailable)
	}))
	defer server.Close()
	a, err := New(Config{Workspace: t.TempDir(), System: "SYSTEM", Model: "test/model", APIKey: "fixture", BaseURL: server.URL, DailyBudgetResolved: true, DailyBudgetUSD: .24, ModelFallbacks: []string{"test/fallback"}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.SettleWrites()
	defer a.Close()
	events, err := a.Submit(context.Background(), "work")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("no provider attempt")
	}
	RecordUsage(UsageLedgerPath(), UsageLine{Model: "other crew", Calls: 1, USD: .25})
	if !FlushUsage() {
		t.Fatal("ledger did not flush")
	}
	close(release)
	collect(t, events)
	if sends.Load() != 1 {
		t.Fatalf("stopped work retried or fell back: sends=%d", sends.Load())
	}
}

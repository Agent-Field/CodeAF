package session

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/provider"
)

func TestResumeBeforeReceiptKeepsKnownCapClosed(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	profile := t.TempDir()
	if err := config.WriteDailyBudgetUSD(profile, .24); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	receiptStarted := make(chan struct{})
	var once, receiptOnce sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			if r.URL.Path != "/generation" {
				http.NotFound(w, r)
				return
			}
			receiptOnce.Do(func() { close(receiptStarted) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			fmt.Fprint(w, `{"data":{"total_cost":0.25,"tokens_prompt":10,"tokens_completion":2}}`)
			return
		}
		n := sends.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			fmt.Fprint(w, "data: {\"id\":\"gen-cap\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"working\"}}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"cost\":0.25}}\n\n")
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
			case <-release:
			}
			return
		}
		fmt.Fprint(w, "data: {\"id\":\"gen-resumed\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"escaped\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"cost\":0}}\n\ndata: [DONE]\n\n")
	}))
	defer func() { unblock(); server.Close() }()
	client, err := provider.NewClient(provider.Config{APIKey: "fixture", BaseURL: server.URL, Model: "test/model", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := newTestAgent(t, client, func(c *Config) { c.ProfileDir = profile; c.OneModel = true })
	t.Cleanup(unblock)
	events := collect(t, mustSubmit(t, a, "work"))
	stopped := false
	for _, e := range events {
		stopped = stopped || errors.Is(e.Err, ErrSpendRail)
	}
	if !stopped {
		t.Fatal("fixture did not stop at measured $0.25")
	}
	select {
	case <-receiptStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("receipt not requested")
	}
	waitFor(t, "stopped work joins", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return !a.workStopping })
	resumed, err := a.Submit(context.Background(), "resume without raising")
	if err == nil {
		collect(t, resumed)
		t.Errorf("unchanged $0.24 cap reopened while known $0.25 receipt is pending; provider sends=%d", sends.Load())
	} else if !errors.Is(err, ErrSpendRail) {
		t.Errorf("wrong refusal: %v", err)
	}
	unblock()
	if err := a.WaitReceipts(t.Context()); err != nil {
		t.Fatal(err)
	}
	FlushUsage()
	spent, err := DailySpendUSD(UsageLedgerPath(), time.Now())
	if err != nil || spent != .25 {
		t.Errorf("late receipt accounting: spent=%v err=%v", spent, err)
	}
}

func TestKnownFreeAuxiliarySurvivesOtherConversationDayCap(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	var calls atomic.Int32
	model := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		calls.Add(1)
		return priced(textResponse("free"), 0), nil
	}}}
	a, _ := newTestAgent(t, model, func(c *Config) {
		c.OneModel = true
		c.DailyBudgetResolved = true
		c.DailyBudgetUSD = .24
		c.ModelPrice = func(string) (float64, float64, bool) { return 0, 0, true }
	})
	RecordUsage(UsageLedgerPath(), UsageLine{Model: "other-conversation", Calls: 1, USD: .25})
	if !FlushUsage() {
		t.Fatal("flush")
	}
	_, err := a.completeWithModel(context.Background(), purposeInherited, []ai.Message{textMessage("user", "free helper")}, "test/model")
	if err != nil || calls.Load() != 1 {
		t.Fatalf("known-free helper blocked by unrelated paid spend: err=%v calls=%d", err, calls.Load())
	}
}

func TestBudgetStopCancelsDetachedSummary(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	client := &scriptedCompleter{steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		close(started)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return priced(textResponse("released"), .01), nil
		}
	}}}
	a, _, _ := runSummaryFixture(t, client, func(c *Config) { c.OneModel = true; c.DailyBudgetResolved = true; c.DailyBudgetUSD = .24 })
	t.Cleanup(unblock)
	go func() { defer close(done); a.RefreshRunSummary(context.Background(), planRootID, time.Time{}) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("summary did not start")
	}
	RecordUsage(UsageLedgerPath(), UsageLine{Model: "other-crew", Calls: 1, USD: .25})
	FlushUsage()
	waitFor(t, "day-cap closes admission", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return a.workStopped })
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Error("day-cap StopWork left detached summary provider call running")
	}
	unblock()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("summary did not join")
	}
}

type idleBudgetCounter struct {
	liveBudgetStub
	reads atomic.Int32
}

func (b *idleBudgetCounter) Refusal() string { b.reads.Add(1); return b.liveBudgetStub.Refusal() }

func TestIdleWatcherDoesNotPollBudgetAndUnregisters(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	b := &idleBudgetCounter{liveBudgetStub: liveBudgetStub{cap: 1}}
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.JobBudget = b; c.DailyBudgetResolved = true; c.DailyBudgetUSD = .24 })
	budgetProgress.Lock()
	watchers := len(budgetProgress.watchers)
	calls := len(budgetProgress.calls)
	budgetProgress.Unlock()
	before := b.reads.Load()
	RecordUsage(UsageLedgerPath(), UsageLine{Model: "other", Calls: 1, USD: .25})
	FlushUsage()
	time.Sleep(250 * time.Millisecond)
	a.mu.Lock()
	stopped := a.workStopped
	a.mu.Unlock()
	if stopped || b.reads.Load() != before {
		t.Errorf("idle watcher stopped=%v budget reads=%d", stopped, b.reads.Load()-before)
	}
	_, retire := a.withBudgetProgress(context.Background())
	retire()
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "watcher unregistered", func() bool {
		budgetProgress.Lock()
		defer budgetProgress.Unlock()
		return len(budgetProgress.watchers) < watchers
	})
	budgetProgress.Lock()
	defer budgetProgress.Unlock()
	if len(budgetProgress.calls) != calls {
		t.Errorf("call progress leaked: before=%d after=%d", calls, len(budgetProgress.calls))
	}
}

func TestDayStopRespectsOtherAgentAuthority(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	agents := make([]*Agent, 4)
	events := make([]<-chan Event, 3)
	for i := range agents {
		started := make(chan struct{})
		model := &scriptedCompleter{steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(started)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-release:
				return priced(textResponse("done"), 0), nil
			}
		}}}
		a, _ := newTestAgent(t, model, func(c *Config) {
			c.OneModel = true
			c.DailyBudgetResolved = true
			c.DailyBudgetUSD = .24
			if i == 1 {
				c.DailyBudgetUSD = 1
			}
			if i == 2 {
				c.SpendPreauthorized = true
			}
		})
		agents[i] = a
		t.Cleanup(unblock)
		if i < 3 {
			events[i] = mustSubmit(t, a, "work")
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("provider not started")
			}
		}
	}
	RecordUsage(UsageLedgerPath(), UsageLine{Model: "crossing", Calls: 1, USD: .25})
	FlushUsage()
	collect(t, events[0])
	time.Sleep(150 * time.Millisecond)
	for i := 1; i < 4; i++ {
		agents[i].mu.Lock()
		stopped := agents[i].workStopped
		agents[i].mu.Unlock()
		if stopped {
			t.Errorf("unrelated agent %d stopped", i)
		}
	}
	unblock()
	collect(t, events[1])
	collect(t, events[2])
}

func TestStopRechecksRaiseAndEpoch(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	b := &liveBudgetStub{cap: .24, spent: .25}
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.JobBudget = b; c.SpendPreauthorized = true })
	why := a.jobBudgetBlock()
	b.mu.Lock()
	b.cap = 1
	b.mu.Unlock()
	if err := a.stopWorkEpoch(0, why); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	stopped := a.workStopped
	a.workEpoch = 1
	a.mu.Unlock()
	if stopped {
		t.Fatal("stop ignored raise")
	}
	b.mu.Lock()
	b.cap = .24
	b.mu.Unlock()
	if err := a.stopWorkEpoch(0, why); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.workStopped {
		t.Fatal("old epoch cancelled resumed work")
	}
}

func TestRunningChildHonorsOwnerDayCapRaise(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	profile := t.TempDir()
	if err := config.WriteDailyBudgetUSD(profile, .24); err != nil {
		t.Fatal(err)
	}
	started, cut, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	model := &scriptedCompleter{aside: func(messages []ai.Message) (*ai.Response, bool) {
		if isPlannerCall(messages) {
			return priced(textResponse(`{"add":[{"id":"worker","title":"read notes","goal":"read notes"}]}`), 0), true
		}
		if isNameCall(messages) || isSessionTitleCall(messages) {
			return priced(textResponse("Read notes"), 0), true
		}
		return nil, false
	}, steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		close(started)
		select {
		case <-ctx.Done():
			close(cut)
			return nil, ctx.Err()
		case <-release:
			return priced(textResponse("done"), 0), nil
		}
	}}}
	a, _ := newTestAgent(t, model, func(c *Config) { c.ProfileDir = profile; c.SessionFile = filepath.Join(t.TempDir(), "owner.jsonl") })
	t.Cleanup(unblock)
	if _, err := a.RunOrchestrate(context.Background(), "read notes", "", 5); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not start")
	}
	if err := config.WriteDailyBudgetUSD(profile, 1); err != nil {
		t.Fatal(err)
	}
	RecordUsage(UsageLedgerPath(), UsageLine{Model: "other-crew", Calls: 1, USD: .25})
	FlushUsage()
	select {
	case <-cut:
		t.Error("running child cancelled at old $0.24 cap after owner raised day cap to $1")
	case <-time.After(250 * time.Millisecond):
	}
	unblock()
}

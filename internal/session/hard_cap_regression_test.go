package session

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/orchestrate"
	"github.com/Agent-Field/codeaf/internal/provider"
)

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
	usageWatchers.Lock()
	watchers := len(usageWatchers.of)
	usageWatchers.Unlock()
	budgetProgress.Lock()
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
	_, retire := a.withBudgetProgress(context.Background(), "")
	retire()
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "watcher unregistered", func() bool {
		usageWatchers.Lock()
		defer usageWatchers.Unlock()
		return len(usageWatchers.of) < watchers
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

// The fixture exposes a measured, cancelled HTTP call with its receipt still pending.
func pendingReceiptFixture(t *testing.T, legacy bool) (*Agent, func()) {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	release := make(chan struct{})
	receipt := make(chan struct{})
	var once, receiptOnce sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			if r.URL.Path != "/generation" {
				http.NotFound(w, r)
				return
			}
			receiptOnce.Do(func() { close(receipt) })
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			fmt.Fprint(w, `{"data":{"total_cost":0.25,"tokens_prompt":10,"tokens_completion":2}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"gen-pending-cap\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"working\"}}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"cost\":0.25}}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() { unblock(); server.Close() })
	client, err := provider.NewClient(provider.Config{APIKey: "fixture", BaseURL: server.URL, Model: "test/model"})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := newTestAgent(t, client, func(c *Config) { c.OneModel = true; c.DailyBudgetResolved = true; c.DailyBudgetUSD = .4 })
	t.Cleanup(func() {
		unblock()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		a.WaitReceipts(ctx)
		FlushUsage()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = provider.WithReconcile(ctx, a.reconciled)
	observed := make(chan struct{})
	var observedOnce sync.Once
	ctx = provider.WithStreamObserver(ctx, func(event provider.StreamEvent) {
		if event.Delta != "" {
			observedOnce.Do(func() { close(observed) })
		}
	})
	if legacy {
		var err error
		ctx, err = provider.WithActualAttemptObserver(ctx, &admissionObserver{})
		if err != nil {
			t.Fatal(err)
		}
		ctx, err = provider.WithWorkOwner(ctx, provider.WorkOwner{Kind: "review", ID: "pending"})
		if err != nil {
			t.Fatal(err)
		}
	}
	if legacy {
		ctx = provider.WithBilling(ctx, func(b provider.Billed) {
			RecordUsage(a.config.usageLedger, UsageLine{Model: b.Model, Calls: 1, USD: b.Cost})
		})
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := a.completeWithModel(ctx, purposeInherited, []ai.Message{textMessage("user", "work")}, "test/model")
		t.Logf("fixture call ended: %v", err)
	}()
	waitCloseRun(t, observed, "streamed text after usage")
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not join")
	}
	select {
	case <-receipt:
	case <-time.After(3 * time.Second):
		t.Fatal("receipt did not start")
	}
	return a, unblock
}

func TestPendingLegacyBillingCountedOnce(t *testing.T) {
	a, unblock := pendingReceiptFixture(t, true)
	FlushUsage()
	spent, err := DailySpendUSD(a.config.usageLedger, time.Now())
	if err != nil || spent != .25 {
		data, _ := os.ReadFile(a.config.usageLedger)
		t.Fatalf("fixture ledger=%g err=%v raw=%s now=%v", spent, err, data, time.Now())
	}
	if err := a.dailyBudgetBlock(); err != nil {
		t.Errorf("single $0.25 call under $0.40 cap refused: ledger=$%.2f: %v", spent, err)
	}
	unblock()
	if err := a.WaitReceipts(t.Context()); err != nil {
		t.Fatal(err)
	}
	FlushUsage()
	spent, err = DailySpendUSD(a.config.usageLedger, time.Now())
	if err != nil || spent != .25 {
		t.Errorf("settled ledger=%g err=%v; want exactly one $0.25 charge", spent, err)
	}
}

func TestPendingPreviousDayDoesNotBlockNewDay(t *testing.T) {
	original := time.Local
	// Restore the zone only after receipt workers and the HTTP server have joined.
	t.Cleanup(func() { time.Local = original })
	a, unblock := pendingReceiptFixture(t, false)
	_, offset := time.Now().Zone()
	time.Local = time.FixedZone("next-day", offset+24*60*60)
	a.config.DailyBudgetUSD = .24
	spent, err := DailySpendUSD(a.config.usageLedger, time.Now())
	if err != nil || spent != 0 {
		t.Fatalf("new day ledger=%g err=%v", spent, err)
	}
	if err := a.dailyBudgetBlock(); err != nil {
		t.Errorf("ended previous-day call blocks fresh day: ledger=$%.2f: %v", spent, err)
	}
	unblock()
}

func TestFreeRequestCannotSubstitutePaidSeat(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	defaultServer := newForbiddenClientDoorServer(t)
	paidServer := newClientDoorServer(t, "paid answer")
	defaultSource := modelsource.DefaultSource(defaultServer.URL)
	directSource := modelsource.Source{ID: "direct", Written: "direct", Address: paidServer.URL}
	RecordUsage(UsageLedgerPath(), UsageLine{Model: "other", Calls: 1, USD: .25})
	FlushUsage()
	a, err := newTestProviderAgent(t, Config{Workspace: t.TempDir(), Model: "direct/stub/paid-seat", DailyBudgetResolved: true, DailyBudgetUSD: .24,
		ModelPrice: func(model string) (float64, float64, bool) {
			if model == "vendor/free" {
				return 0, 0, true
			}
			return .001, .002, true
		},
		Sources: modelsource.NewSet(modelsource.Connected{Source: defaultSource, Address: defaultServer.URL}, modelsource.Connected{Source: directSource, Key: "fixture", Address: paidServer.URL}),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	_, called, err := a.completeWithNamedModel(t.Context(), purposeInherited, []ai.Message{textMessage("user", "helper")}, "vendor/free")
	t.Logf("free request resolved called=%s err=%v calls=%d", called, err, len(paidServer.allCalls()))
	if len(paidServer.allCalls()) != 0 {
		t.Errorf("exhausted $0.24 day cap dispatched %d paid-seat calls: requested=vendor/free called=%s err=%v", len(paidServer.allCalls()), called, err)
	}
}

func TestKnownFreeHTTPHelperSurvivesDayCap(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	server := newClientDoorServer(t, "free answer")
	RecordUsage(UsageLedgerPath(), UsageLine{Model: "other", Calls: 1, USD: .25})
	FlushUsage()
	a, err := newTestProviderAgent(t, Config{Workspace: t.TempDir(), Model: "test/free", APIKey: "fixture", BaseURL: server.URL,
		DailyBudgetResolved: true, DailyBudgetUSD: .24, ModelPrice: func(string) (float64, float64, bool) { return 0, 0, true },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	_, err = a.completeWithModel(t.Context(), purposeInherited, []ai.Message{textMessage("user", "free helper")}, "test/free")
	if err != nil || len(server.allCalls()) != 1 {
		t.Errorf("known-free HTTP helper blocked: calls=%d err=%v", len(server.allCalls()), err)
	}
}

func TestChildReadsUnlimitedAndClosedOwner(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	profile := t.TempDir()
	if err := config.WriteDailyBudgetUSD(profile, 0); err != nil {
		t.Fatal(err)
	}
	a, dir := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.ProfileDir = profile })
	e := &orchestrateExec{agent: a, id: "pending-cap"}
	child, err := e.newChild(dir, orchestrate.Node{ID: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { child.Close(); child.SettleWrites() })
	if cap, err := child.dailyCeiling(); cap != 0 || err != nil {
		t.Fatalf("unlimited owner cap=%g err=%v", cap, err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteDailyBudgetUSD(profile, 1); err != nil {
		t.Fatal(err)
	}
	if cap, err := child.dailyCeiling(); cap != 1 || err != nil {
		t.Fatalf("closed owner cap=%g err=%v", cap, err)
	}
}

func TestModelGatePricesResolvedModel(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.DailyBudgetResolved = true
		c.DailyBudgetUSD = .24
		c.ModelPrice = func(model string) (float64, float64, bool) {
			if model == "vendor/free" {
				return 0, 0, true
			}
			if model == "vendor/paid" {
				return .001, .002, true
			}
			return 0, 0, false
		}
	})
	RecordUsage(a.config.usageLedger, UsageLine{Model: "earlier", Calls: 1, USD: .25})
	FlushUsage()
	ctx, retire := a.withBudgetProgress(context.Background(), "")
	defer retire()
	gate := a.config.routeGate("vendor/free", "free")
	for _, tc := range []struct {
		name, model string
		blocked     bool
	}{
		{"known-free", "free", false},
		{"paid-replacement", "vendor/paid", true},
		{"unknown-price", "vendor/unknown", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := gate(ctx, tc.model, string(purposeTurn))
			if (err != nil) != tc.blocked {
				t.Fatalf("model=%s blocked=%v err=%v", tc.model, tc.blocked, err)
			}
		})
	}
}

func TestDailyCrossingStopsToolStartedAfterTheWake(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	synctest.Test(t, func(t *testing.T) {
		defer StopUsageWriter(UsageLedgerPath())
		banked, resume := make(chan struct{}), make(chan struct{})
		releaseTool, toolStarted := make(chan struct{}), make(chan string, 1)
		var once sync.Once
		unblock := func() { once.Do(func() { close(resume) }) }
		defer unblock()
		defer close(releaseTool)
		var armed atomic.Bool
		model := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
			armed.Store(true)
			return priced(toolResponse("work", "touch", "{}"), .25), nil
		}}}
		a, _ := newTestAgent(t, model, func(c *Config) {
			c.OneModel = true
			c.Routing = provider.RoutingOff
			c.DailyBudgetResolved = true
			c.DailyBudgetUSD = .24
			c.SessionFile = filepath.Join(t.TempDir(), "chat.jsonl")
		})
		var pause sync.Once
		a.file.onWrite = func() {
			if armed.Load() {
				pause.Do(func() { close(banked); <-resume })
			}
		}
		a.tools = append(a.tools, countingTool("touch", toolStarted, releaseTool))
		events := mustSubmit(t, a, "work")
		<-banked
		if a.hasBudgetCall() {
			t.Fatal("fixture still has active model call")
		}
		if !StopUsageWriter(UsageLedgerPath()) {
			t.Fatal("ledger flush failed")
		}
		spent, err := DailySpendUSD(UsageLedgerPath(), time.Now())
		if err != nil || spent != .25 {
			t.Fatalf("fixture: recorded spend=%g err=%v", spent, err)
		}
		synctest.Wait()
		unblock()
		synctest.Wait()
		select {
		case <-toolStarted:
		default:
			t.Fatal("fixture: tool did not start")
		}
		time.Sleep(2 * time.Second)
		synctest.Wait()
		a.mu.Lock()
		stopped := a.workStopped
		a.mu.Unlock()
		if !stopped {
			t.Error("daily crossing wake consumed between call and tool; tool still running after 2s with no further ledger write")
		}
		RecordUsage(UsageLedgerPath(), UsageLine{Model: "later wake", Calls: 1, USD: .001})
		if !StopUsageWriter(UsageLedgerPath()) {
			t.Fatal("second ledger flush failed")
		}
		synctest.Wait()
		a.mu.Lock()
		recovered := a.workStopped
		a.mu.Unlock()
		if !recovered {
			t.Error("later wake did not stop tool")
		} else {
			t.Log("a later ledger write immediately recovers the lost stop")
		}
		a.StopWork()
		collect(t, events)
	})
}

func TestChildStartedCrewStopsAtDailyCrossing(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	started, cut, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer close(release)
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
	parent, _ := newTestAgent(t, model, func(c *Config) { c.DailyBudgetResolved = true; c.DailyBudgetUSD = .24; c.Routing = provider.RoutingOff })
	graph := parent.graph()
	graph.run = func(*TaskNode) {}
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "parent task", brief: "work", acceptance: "done"})
	child, err := parent.newTaskAgent(context.Background(), t.TempDir(), graph.node(id), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(child.SettleWrites)
	t.Cleanup(func() { child.Close() })
	if _, err := child.RunOrchestrate(context.Background(), "read notes", "", 5); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("crew not started")
	}
	RecordUsage(UsageLedgerPath(), UsageLine{Model: "crossing", Calls: 1, USD: .25})
	FlushUsage()
	select {
	case <-cut:
	case <-time.After(2 * time.Second):
		t.Error("child-started crew still running after daily crossing")
	}
}

// A missing wire price remains absent from the chat's money readings while
// its receipt is pending. A fresh submission can spend against that gap.
func TestResumeReopensCapWhileReceiptPending(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	release, receiptStarted := make(chan struct{}), make(chan struct{})
	finishReply := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/generation" {
			if r.URL.Query().Get("id") != "gen-pending-1" {
				fmt.Fprint(w, `{"data":{"total_cost":0,"tokens_prompt":1,"tokens_completion":1}}`)
				return
			}
			close(receiptStarted)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			fmt.Fprint(w, `{"data":{"total_cost":0.25,"tokens_prompt":10,"tokens_completion":2}}`)
			return
		}
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		n := sends.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"id\":\"gen-pending-%d\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"working\"}}]}\n\n", n)
		w.(http.Flusher).Flush()
		if n == 1 {
			<-r.Context().Done()
			return
		}
		select {
		case <-finishReply:
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\" done\"},\"finish_reason\":\"stop\"}],\"usage\":{\"cost\":0}}\n\ndata: [DONE]\n\n")
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { unblock(); server.Close() })
	client, err := provider.NewClient(provider.Config{APIKey: "fixture", BaseURL: server.URL, Model: "test/model", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := newTestAgent(t, client, func(c *Config) {
		c.OneModel = true
		c.Routing = provider.RoutingOff
		c.DailyBudgetResolved = true
		c.DailyBudgetUSD = .24
	})
	t.Cleanup(func() {
		unblock()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = a.WaitReceipts(ctx)
	})
	waitText := func(events <-chan Event) {
		t.Helper()
		for {
			select {
			case e, open := <-events:
				if !open {
					t.Fatal("stream ended before working text")
				}
				if e.Kind == EventTextDelta && e.Text == "working" {
					return
				}
			case <-time.After(3 * time.Second):
				t.Fatal("no working text")
			}
		}
	}
	events := mustSubmit(t, a, "work")
	waitText(events)
	if err := a.StopWork(); err != nil {
		t.Fatal(err)
	}
	first := collect(t, events)
	waitCloseRun(t, receiptStarted, "pending receipt")
	waitFor(t, "stopped work joins", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return !a.workStopping })
	if !FlushUsage() {
		t.Fatal("flush")
	}
	spent, err := DailySpendUSD(a.config.usageLedger, time.Now())
	if err != nil || spent != 0 || a.Usage().CostUSD != 0 {
		t.Fatalf("pending window: recorded=%g chat=%g err=%v", spent, a.Usage().CostUSD, err)
	}
	notice := ""
	for _, e := range first {
		if e.Kind == EventNotice {
			notice += e.Text
		}
		if errors.Is(e.Err, ErrSpendRail) {
			t.Fatalf("pending price closed cap: %v", e.Err)
		}
	}
	if notice != "everything running here was stopped, the reply with it" {
		t.Fatalf("pending chat notice=%q", notice)
	}
	for _, entry := range a.Transcript() {
		if strings.Contains(entry.Text, "daily limit") {
			t.Fatalf("pending window claims limit reached: %q", entry.Text)
		}
	}
	events = mustSubmit(t, a, "resume without raising")
	waitText(events)
	if sends.Load() != 2 {
		t.Fatalf("resume sends=%d", sends.Load())
	}
	t.Logf("pending window: chat keeps working and %q; recorded=$0.00, chat cost=$0.00, no daily-limit notice or pending-receipt warning; unchanged cap=$0.24 admits resume", notice)
	unblock()
	waitFor(t, "settled receipt closes admission while reply drains", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return a.workStopped })
	close(finishReply)
	got := collect(t, events)
	const usual = "daily limit reached · $0.25 spent of $0.24 · /budget day changes it"
	stopped, said, completedReply := false, "", false
	for _, e := range got {
		completedReply = completedReply || e.Kind == EventTextDelta && strings.Contains(e.Text, " done")
		stopped = stopped || errors.Is(e.Err, ErrSpendRail)
		if e.Kind == EventNotice {
			said += e.Text
		}
	}
	// The resumed call already supplied a token, so the receipt crossing lets it
	// finish. Its persisted limit notice belongs to the shared stop route.
	waitFor(t, "reply drain and notice", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return !a.workStopping })
	for _, entry := range a.Transcript() {
		if entry.Text == usual {
			said = usual
		}
	}
	if stopped || !completedReply || said != usual {
		t.Fatalf("settled receipt stop: rail=%v notice=%q", stopped, said)
	}
	waitFor(t, "receipt recorded", func() bool {
		FlushUsage()
		spent, err := DailySpendUSD(a.config.usageLedger, time.Now())
		return err == nil && spent == .25
	})
	if _, err := a.Submit(context.Background(), "still capped"); !errors.Is(err, ErrSpendRail) {
		t.Fatalf("receipt did not close cap: %v", err)
	}
	t.Logf("settled receipt: %s; recovery bound=%g seconds", said, provider.ReceiptWait.Seconds())
}

func TestDailyCrossingStopsRerunAfterIdleWake(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Place = Place{Dir: t.TempDir()}
		c.SessionFile = c.Place.Transcript()
		c.DailyBudgetResolved = true
		c.DailyBudgetUSD = .24
	})
	id := startJob(t, a, "if test -e review5-marker; then sleep 30; else touch review5-marker; fi")
	waitFor(t, "original command exits", func() bool { return !a.jobs.find(id).running() })
	facts, err := a.ProcessActions(context.Background(), id, a.jobs.find(id).info().started.UnixMilli())
	if err != nil || !facts.Rerun.Available {
		t.Fatalf("fixture rerun unavailable: %+v err=%v", facts.Rerun, err)
	}
	RecordUsage(a.config.usageLedger, UsageLine{Model: "fixture", Calls: 1, USD: .25})
	if !StopUsageWriter(a.config.usageLedger) {
		t.Fatal("ledger flush failed")
	}
	spent, err := DailySpendUSD(a.config.usageLedger, time.Now())
	if err != nil || spent != .25 {
		t.Fatalf("recorded spend=%g err=%v", spent, err)
	}
	time.Sleep(250 * time.Millisecond)
	a.mu.Lock()
	stopped := a.workStopped
	a.mu.Unlock()
	if stopped {
		t.Fatal("fixture: idle chat stopped before rerun")
	}
	original := a.jobs.find(id).info()
	rerun, err := a.RerunProcessAt(context.Background(), id, original.started.UnixMilli())
	if err != nil {
		t.Logf("rerun refused: %v", err)
		return
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && a.jobs.find(rerun.ID).running() {
		time.Sleep(10 * time.Millisecond)
	}
	if a.jobs.find(rerun.ID).running() {
		t.Error("rerun still running after 2s at recorded daily cap with no further ledger write")
	}
	RecordUsage(a.config.usageLedger, UsageLine{Model: "later wake", Calls: 1, USD: .001})
	if !StopUsageWriter(a.config.usageLedger) {
		t.Fatal("second ledger flush failed")
	}
	waitFor(t, "later ledger wake stops rerun", func() bool { return !a.jobs.find(rerun.ID).running() })
	t.Log("later ledger write stopped rerun")
}

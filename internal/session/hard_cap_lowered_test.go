package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// Timestamp at the provider boundary, including planners and auxiliary calls.
type capCallStarts struct {
	Completer
	mu sync.Mutex
	at []time.Time
}

func (s *capCallStarts) CompleteWithMessages(ctx context.Context, messages []ai.Message, opts ...ai.Option) (*ai.Response, error) {
	s.mu.Lock()
	s.at = append(s.at, time.Now())
	s.mu.Unlock()
	return s.Completer.CompleteWithMessages(ctx, messages, opts...)
}

func TestHardCapLoweredWhileCrewsWork(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	routing := provider.RoutingNow()
	provider.InstallRouting(provider.RoutingOff)
	t.Cleanup(func() { provider.InstallRouting(routing) })
	profile := t.TempDir()
	if err := config.WriteDailyBudgetUSD(profile, 1); err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		t.Cleanup(func() { StopUsageWriter(UsageLedgerPath()) })
		RecordUsage(UsageLedgerPath(), UsageLine{At: time.Now(), Model: "earlier today", Calls: 1, USD: .50})
		if !StopUsageWriter(UsageLedgerPath()) {
			t.Fatal("seed ledger did not flush")
		}
		release := make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		defer unblock()
		var owners []*Agent
		var runs []string
		var providers []*capCallStarts
		var cuts [3]atomic.Bool
		var raised atomic.Bool
		var completedReply atomic.Value
		for i := range 3 {
			started := make(chan struct{})
			// Decode real SSE in memory so first-token tracking uses the production
			// adapter while all waits remain controlled by the fake clock.
			client, err := provider.NewClient(provider.Config{APIKey: "fixture", BaseURL: "http://fixture.invalid", Model: "test/model", HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				reader, writer := io.Pipe()
				go func() {
					defer writer.Close()
					fmt.Fprint(writer, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"working\"}}]}\n\n")
					close(started)
					select {
					case <-r.Context().Done():
						cuts[i].Store(true)
					case <-release:
						fmt.Fprint(writer, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\" done\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":4,\"cost\":0.10}}\n\ndata: [DONE]\n\n")
					}
				}()
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: reader, Request: r}, nil
			})}})
			if err != nil {
				t.Fatal(err)
			}
			script := &scriptedCompleter{aside: func(messages []ai.Message) (*ai.Response, bool) {
				if raised.Load() {
					return priced(textResponse("CAP RAISED"), 0), true
				}
				if isPlannerCall(messages) {
					return priced(textResponse(`{"add":[{"id":"worker","title":"read notes","goal":"read notes"}]}`), 0), true
				}
				if isNameCall(messages) || isSessionTitleCall(messages) {
					return priced(textResponse("Read notes"), 0), true
				}
				return nil, false
			}, steps: []step{func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
				if i == 0 {
					response, err := client.CompleteWithMessages(ctx, messages)
					if err == nil && response != nil {
						completedReply.Store(response.Text())
					}
					return response, err
				}
				close(started)
				select {
				case <-ctx.Done():
					cuts[i].Store(true)
					return nil, ctx.Err()
				case <-release:
					return priced(textResponse("unexpectedly released waiting call"), 0), nil
				}
			}}}
			measured := &capCallStarts{Completer: script}
			a, _ := newTestAgent(t, measured, func(c *Config) {
				c.OneModel = true
				c.Routing = provider.RoutingOff
				c.DeterministicNames = true
				c.ProfileDir = profile
				c.SessionFile = filepath.Join(t.TempDir(), fmt.Sprintf("crew-%d.jsonl", i))
			})
			owners = append(owners, a)
			providers = append(providers, measured)
			id, err := a.RunOrchestrate(context.Background(), "read notes", "", 5)
			if err != nil {
				t.Fatal(err)
			}
			runs = append(runs, id)
			<-started
		}
		// Consume every setup receipt and wake before lowering: no later ledger
		// write may accidentally make a profile-only edit visible to the watcher.
		synctest.Wait()
		if !StopUsageWriter(UsageLedgerPath()) {
			t.Fatal("setup receipts did not flush")
		}
		time.Sleep(2 * budgetPollEvery)
		synctest.Wait()
		if err := config.WriteDailyBudgetUSD(profile, .24); err != nil {
			t.Fatal(err)
		}
		loweredAt := time.Now()
		time.Sleep(budgetPollEvery)
		synctest.Wait()
		for i := 1; i < 3; i++ {
			if !cuts[i].Load() {
				t.Errorf("crew %d waiting call not cancelled within %v of cap write", i, budgetPollEvery)
			}
		}
		if cuts[0].Load() {
			t.Error("already replying call cancelled by lowered cap")
		}
		// Try new work after the deadline while the replying call is still held.
		for i, a := range owners {
			events, err := a.Submit(context.Background(), "still capped")
			if err == nil {
				got := collect(t, events)
				if len(got) != 1 || !IsDailyLimit(got[0].Err) {
					t.Errorf("chat %d admitted capped message: %v", i, got)
				}
			} else if !errors.Is(err, ErrSpendRail) && !errors.Is(err, errWorkStopping) {
				t.Errorf("chat %d admission: %v", i, err)
			}
		}
		time.Sleep(2 * budgetPollEvery)
		synctest.Wait()
		if !StopUsageWriter(UsageLedgerPath()) {
			t.Fatal("waiting cancellations did not flush")
		}
		spent, err := DailySpendUSD(UsageLedgerPath(), time.Now())
		if err != nil || spent != .50 {
			t.Errorf("waiting calls added cost or reply settled early: spent=%g err=%v", spent, err)
		}
		unblock()
		synctest.Wait()
		for i, a := range owners {
			waitForRun(t, a, runs[i])
			waitFor(t, "budget stop joins", func() bool { a.mu.Lock(); defer a.mu.Unlock(); return !a.workStopping })
			if err := a.WaitReceipts(t.Context()); err != nil {
				t.Fatal(err)
			}
		}
		synctest.Wait()
		if !StopUsageWriter(UsageLedgerPath()) {
			t.Fatal("final receipts did not flush")
		}
		rows, err := ReadUsage(UsageLedgerPath(), time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		charged := 0
		spent = 0
		for _, row := range rows {
			spent += row.USD
			if row.USD == .10 {
				charged++
			}
		}
		if charged != 1 || spent < .5999 || spent > .6001 {
			t.Errorf("reply cost must be recorded once: charges=%d spent=%g rows=%+v", charged, spent, rows)
		}
		for i, a := range owners {
			notices := 0
			for _, entry := range a.Transcript() {
				if strings.Contains(entry.Text, "daily limit") && strings.Contains(entry.Text, "/budget day") {
					notices++
				}
			}
			if notices != 1 {
				t.Errorf("chat %d daily notices=%d, want exactly one; transcript=%+v", i, notices, a.Transcript())
			}
		}
		answer, _ := completedReply.Load().(string)
		if !strings.Contains(answer, "working done") {
			t.Errorf("replying crew lost completed answer: %q", answer)
		}
		for i, measured := range providers {
			measured.mu.Lock()
			for _, at := range measured.at {
				if !at.Before(loweredAt.Add(budgetPollEvery)) {
					t.Errorf("crew %d model admitted %v after cap write; bound=%v", i, at.Sub(loweredAt), budgetPollEvery)
				}
			}
			measured.mu.Unlock()
		}
		if err := config.WriteDailyBudgetUSD(profile, 1); err != nil {
			t.Fatal(err)
		}
		raised.Store(true)
		for i, a := range owners {
			answer := ""
			for _, event := range collect(t, mustSubmit(t, a, "resume")) {
				answer += event.Text
				if event.Kind == EventError {
					t.Errorf("raised chat %d: %v", i, event.Err)
				}
			}
			for _, entry := range a.Transcript() {
				answer += entry.Text
			}
			if !strings.Contains(answer, "CAP RAISED") {
				t.Errorf("raised chat %d did not answer: %q", i, answer)
			}
		}
	})
}

func TestHardCapLoweredWhileIdleRefusesNextMessageWithoutPolling(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	profile := t.TempDir()
	if err := config.WriteDailyBudgetUSD(profile, 1); err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		t.Cleanup(func() { StopUsageWriter(UsageLedgerPath()) })
		RecordUsage(UsageLedgerPath(), UsageLine{At: time.Now(), Model: "earlier today", Calls: 1, USD: .50})
		if !StopUsageWriter(UsageLedgerPath()) {
			t.Fatal("seed ledger did not flush")
		}
		model := &capCallStarts{Completer: &scriptedCompleter{}}
		// Observe the existing watcher's budget reads without replacing the daily
		// authority: the daily cap still comes from the selected profile on disk.
		budget := &idleBudgetCounter{liveBudgetStub: liveBudgetStub{cap: 5}}
		a, _ := newTestAgent(t, model, func(c *Config) {
			c.OneModel = true
			c.Routing = provider.RoutingOff
			c.ProfileDir = profile
			c.JobBudget = budget
		})
		synctest.Wait()
		before := budget.reads.Load()
		if err := config.WriteDailyBudgetUSD(profile, .24); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * budgetPollEvery)
		synctest.Wait()
		a.mu.Lock()
		stopped := a.workStopped
		a.mu.Unlock()
		if stopped || budget.reads.Load() != before {
			t.Errorf("idle cap edit started watcher: stopped=%v budget reads=%d", stopped, budget.reads.Load()-before)
		}
		events, err := a.Submit(context.Background(), "work")
		if err != nil {
			t.Fatalf("idle admission should report through events: %v", err)
		}
		got := collect(t, events)
		if len(got) != 1 || !errors.Is(got[0].Err, ErrSpendRail) || !IsDailyLimit(got[0].Err) {
			t.Fatalf("idle admission did not refuse daily cap: %+v", got)
		}
		if !strings.Contains(got[0].Err.Error(), "daily limit") || !strings.Contains(got[0].Err.Error(), "/budget day") {
			t.Errorf("missing daily-limit text: %v", got[0].Err)
		}
		synctest.Wait()
		model.mu.Lock()
		defer model.mu.Unlock()
		if len(model.at) != 0 {
			t.Errorf("idle capped message admitted model calls: %v", model.at)
		}
	})
}

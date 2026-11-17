package session

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The benchmark counts durable dollars, not elapsed time. Four real adaptive
// crews run child agents on the same cap, script and ledger at both revisions.
func TestHardCapOvershootMeasurement(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	release, cross := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	cancelled := make(chan struct{}, 3)
	var owners []*Agent
	for i := range 4 {
		ready := make(chan struct{})
		model := &scriptedCompleter{aside: func(messages []ai.Message) (*ai.Response, bool) {
			if isPlannerCall(messages) {
				return priced(textResponse(`{"add":[{"id":"worker","title":"read notes","goal":"read notes"}]}`), 0), true
			}
			if isNameCall(messages) || isSessionTitleCall(messages) {
				return priced(textResponse("Read notes"), 0), true
			}
			return nil, false
		}, steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(ready)
			cost := .10
			if i == 0 {
				select {
				case <-cross:
					cost = .25
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			} else {
				select {
				case <-ctx.Done():
					cost = .02
					cancelled <- struct{}{}
				case <-release:
				}
			}
			return priced(textResponse("work completed or partial work retained"), cost), nil
		}}}
		a, _ := newTestAgent(t, model, func(c *Config) {
			c.DailyBudgetResolved = true
			c.DailyBudgetUSD = .24
			c.SessionFile = filepath.Join(t.TempDir(), fmt.Sprintf("measured-crew-%d.jsonl", i))
		})
		owners = append(owners, a)
		if _, err := a.RunOrchestrate(context.Background(), "read notes", "", 5); err != nil {
			t.Fatal(err)
		}
		waitCloseRun(t, ready, "measured crew worker")
	}
	close(cross)
	stopped := 0
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
waiting:
	for stopped < 3 {
		select {
		case <-cancelled:
			stopped++
		case <-deadline.C:
			break waiting
		}
	}
	unblock()
	for _, a := range owners {
		if err := a.Close(); err != nil {
			t.Fatal(err)
		}
		a.SettleWrites()
	}
	if !FlushUsage() {
		t.Fatal("ledger did not flush")
	}
	rows, err := ReadUsage(UsageLedgerPath(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	var spent float64
	pricedCalls := 0
	for _, row := range rows {
		spent += row.USD
		if row.USD > 0 {
			pricedCalls++
		}
	}
	if pricedCalls != 4 {
		t.Fatalf("measured %d priced worker calls, want four", pricedCalls)
	}
	t.Logf("crews=4 cap=$0.24 recorded=$%.2f past-cap=$%.2f cancelled=%d/3 priced-worker-calls=%d", spent, spent-.24, stopped, pricedCalls)
}

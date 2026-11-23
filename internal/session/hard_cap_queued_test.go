package session

import (
	"context"
	"errors"
	"fmt"
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

func TestHardCapBeforeWorkerAdmissionNotifiesOwner(t *testing.T) {
	hardCapQueuedCrew(t, false, false)
}

func TestHardCapBeforePlannerAdmissionNotifiesOwner(t *testing.T) {
	hardCapQueuedCrew(t, true, false)
}

func TestHardCapFinishedPlannerNotifiesOwner(t *testing.T) {
	hardCapQueuedCrew(t, false, true)
}

func hardCapQueuedCrew(t *testing.T, beforePlanner, finishedPlanner bool) {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	routing := provider.RoutingNow()
	provider.InstallRouting(provider.RoutingOff)
	t.Cleanup(func() { provider.InstallRouting(routing) })
	profile := t.TempDir()
	if err := config.WriteDailyBudgetUSD(profile, .24); err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		t.Cleanup(func() { StopUsageWriter(UsageLedgerPath()) })
		gate, ready := make(chan struct{}), make(chan struct{})
		var once sync.Once
		release := func() { once.Do(func() { close(gate) }) }
		defer release()
		cross := func() {
			RecordUsage(UsageLedgerPath(), UsageLine{At: time.Now(), Model: "crossing crew", Calls: 1, USD: .25})
			if !StopUsageWriter(UsageLedgerPath()) {
				t.Error("crossing receipt did not flush")
			}
		}
		var raised atomic.Bool
		var owners []*Agent
		var runIDs []string
		var workers, planners [4]atomic.Int32
		for i := range 4 {
			started := make(chan struct{})
			model := &scriptedCompleter{aside: func(messages []ai.Message) (*ai.Response, bool) {
				if raised.Load() {
					return priced(textResponse("CAP RAISED"), 0), true
				}
				if isPlannerCall(messages) {
					planners[i].Add(1)
					if i == 3 {
						close(ready)
						<-gate
						cross()
					}
					if i == 3 && finishedPlanner {
						return priced(textResponse(`{"done":{"brief":"nothing to do"}}`), 0), true
					}
					return priced(textResponse(`{"add":[{"id":"worker","title":"read notes","goal":"read notes"}]}`), 0), true
				}
				if isNameCall(messages) || isSessionTitleCall(messages) {
					return priced(textResponse("Read notes"), 0), true
				}
				return nil, false
			}, steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				workers[i].Add(1)
				close(started)
				<-ctx.Done()
				return nil, ctx.Err()
			}}}
			a, _ := newTestAgent(t, model, func(c *Config) {
				c.OneModel = true
				c.Routing = provider.RoutingOff
				c.DeterministicNames = true
				c.ProfileDir = profile
				c.SessionFile = filepath.Join(t.TempDir(), fmt.Sprintf("crew-%d.jsonl", i))
				if i == 3 && beforePlanner {
					var priceOnce sync.Once
					c.ModelPrice = func(string) (float64, float64, bool) {
						priceOnce.Do(func() { close(ready); <-gate; cross() })
						return 0, 0, false
					}
				}
			})
			owners = append(owners, a)
			id, err := a.RunOrchestrate(context.Background(), "read notes", "", 5)
			if err != nil {
				t.Fatal(err)
			}
			runIDs = append(runIDs, id)
			if i < 3 {
				<-started
			}
		}
		<-ready
		synctest.Wait()
		release()
		synctest.Wait()
		// Let moving peers observe the crossing, then let every stop join.
		time.Sleep(2 * budgetPollEvery)
		synctest.Wait()
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
		if workers[3].Load() != 0 {
			t.Errorf("refused worker made %d model calls", workers[3].Load())
		}
		snap, ok := owners[3].OrchestrateSnapshot(runIDs[3])
		if !ok {
			t.Fatal("refused crew lost its snapshot")
		}
		events, err := owners[3].Submit(context.Background(), "still capped")
		if !errors.Is(err, ErrSpendRail) {
			t.Errorf("capped owner admission=%v, want ErrSpendRail", err)
			if err == nil {
				collect(t, events)
			}
		}
		if workers[3].Load() != 0 || planners[3].Load() != 0 && beforePlanner {
			t.Error("refused crew sent a model call")
		}
		if !beforePlanner && !finishedPlanner && len(snap.Nodes) == 0 {
			t.Errorf("worker admission was never attempted: %+v", snap)
		}

		wantPlanner := int32(1)
		if beforePlanner {
			wantPlanner = 0
		}
		if planners[3].Load() != wantPlanner {
			t.Errorf("last planner calls=%d, want %d", planners[3].Load(), wantPlanner)
		}
		if err := config.WriteDailyBudgetUSD(profile, 1); err != nil {
			t.Fatal(err)
		}
		raised.Store(true)
		got := collect(t, mustSubmit(t, owners[3], "resume"))
		for _, event := range got {
			if event.Kind == EventError {
				t.Errorf("raised chat error: %v", event.Err)
			}
		}
		answer := ""
		for _, entry := range owners[3].Transcript() {
			answer += entry.Text
		}
		if !strings.Contains(answer, "CAP RAISED") {
			t.Errorf("raised chat did not answer: %v", got)
		}
	})
}

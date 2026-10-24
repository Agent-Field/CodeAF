package run_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

type dailyAuthoritySeat struct{ calls atomic.Int32 }

func (s *dailyAuthoritySeat) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	s.calls.Add(1)
	return nil, errors.New("fake provider reached")
}

func TestRunFactoryCarriesDailyAuthority(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cap, spent float64
		allowed    bool
	}{{"chosen higher allowance", 1000, 600, true}, {"chosen lower allowance", 1, 1.5, false}, {"chosen unlimited", 0, 600, true}} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			profile := t.TempDir()
			t.Setenv("CODEAF_HOME", root)
			t.Setenv("CODEAF_PROFILE_DIR", profile)
			t.Setenv("CODEAF_TASK_BELT", "bash")
			t.Setenv("CODEAF_PLANDB_BIN", filepath.Join(root, "stub-codeaf"))
			t.Setenv("CODEAF_DAILY_BUDGET", "")
			t.Setenv("CODEAF_PREAUTHORIZE_SPEND", "")
			if err := config.WriteDailyBudgetUSD("", 500); err != nil {
				t.Fatal(err)
			}
			if err := config.WriteDailyBudgetUSD(profile, tc.cap); err != nil {
				t.Fatal(err)
			}
			session.RecordUsage(session.UsageLedgerPath(), session.UsageLine{At: time.Now(), USD: tc.spent, Model: "test/model", Calls: 1})
			session.FlushUsage()
			store, err := plandb.Open(filepath.Join(root, "plan.db"), "daily-authority", "root", "Work", "Work")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			seat := &dailyAuthoritySeat{}
			factory := run.CrewFactory(store, root, profile, run.Seats{One: "test/model"}, "", false, func(string) session.Completer { return seat })
			task := *store.Task(store.RootID())
			worker := factory(task)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			_, err = worker.Run(ctx, task)
			gotAllowed := seat.calls.Load() > 0
			t.Logf("profile cap=%g spent=%g provider calls=%d ending=%v", tc.cap, tc.spent, seat.calls.Load(), err)
			if !tc.allowed && !errors.Is(err, session.ErrSpendRail) {
				t.Fatalf("worker stopped for a reason other than its daily limit: %v", err)
			}
			if gotAllowed != tc.allowed {
				t.Fatalf("run factory's worker used wrong daily cap: reached provider=%t want=%t", gotAllowed, tc.allowed)
			}
		})
	}
}

func TestPreauthorizedRunPreservesTaskCaps(t *testing.T) {
	for _, limited := range []bool{false, true} {
		t.Run(map[bool]string{false: "daily preauthorization", true: "separate task cap"}[limited], func(t *testing.T) {
			root := t.TempDir()
			profile := t.TempDir()
			t.Setenv("CODEAF_HOME", root)
			t.Setenv("CODEAF_PROFILE_DIR", profile)
			t.Setenv("CODEAF_TASK_BELT", "bash")
			t.Setenv("CODEAF_PLANDB_BIN", filepath.Join(root, "stub-codeaf"))
			t.Setenv("CODEAF_DAILY_BUDGET", "")
			if err := config.WriteDailyBudgetUSD(profile, 1); err != nil {
				t.Fatal(err)
			}
			if err := config.WriteDailyBudgetUSD("", 1); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CODEAF_PREAUTHORIZE_SPEND", "")
			session.RecordUsage(session.UsageLedgerPath(), session.UsageLine{At: time.Now(), USD: 1.5, Model: "test/model", Calls: 1})
			session.FlushUsage()
			store, err := plandb.Open(filepath.Join(root, "plan.db"), "daily-authority", "root", "Work", "Work")
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			seat := &dailyAuthoritySeat{}
			factory := run.CrewFactory(store, root, profile, run.Seats{One: "test/model"}, "", true, func(model string) session.Completer {
				if !limited {
					return seat
				}
				guard := session.TaskSpendGuard(profile)
				guard.TaskCap = 0.01
				guard.TaskAction = "separate task cap"
				guard.Price = func(string) (float64, float64, float64, bool) { return 1, 1, 0, true }
				return guard.Wrap(model, seat)
			})
			task := *store.Task(store.RootID())
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			_, err = factory(task).Run(ctx, task)
			t.Logf("limited=%t calls=%d ending=%v", limited, seat.calls.Load(), err)
			if !limited && seat.calls.Load() != 1 {
				t.Fatalf("preauthorized run stopped before provider: %v", err)
			}
			var stopped session.ErrSpendStopped
			if limited && (!errors.As(err, &stopped) || stopped.Action != "separate task cap" || seat.calls.Load() != 0) {
				t.Fatalf("preauthorization lifted separate task limit: calls=%d err=%v", seat.calls.Load(), err)
			}
		})
	}
}

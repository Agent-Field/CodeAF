package session

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
)

// The default profile has an empty path at the real chat door. Its daily
// setting must remain live for both a conversation and a copied home errand.
func TestDefaultProfileDailyAuthorityStaysLive(t *testing.T) {
	for _, errand := range []bool{false, true} {
		for _, source := range []string{"saved", "environment"} {
			name := source + "/chat"
			if errand {
				name = source + "/errand"
			}
			t.Run(name, func(t *testing.T) {
				t.Setenv("CODEAF_HOME", t.TempDir())
				t.Setenv("CODEAF_PROFILE_DIR", "")
				t.Setenv("CODEAF_DAILY_BUDGET", "")
				setLimit := func(limit float64) {
					t.Helper()
					if source == "environment" {
						t.Setenv("CODEAF_DAILY_BUDGET", strconv.FormatFloat(limit, 'f', -1, 64))
					} else if err := config.WriteDailyBudgetUSD("", limit); err != nil {
						t.Fatal(err)
					}
				}
				setLimit(1)
				settings, err := config.LoadKeyless()
				if err != nil {
					t.Fatal(err)
				}
				if settings.ProfileDir != "" || settings.DailyBudgetUSD != 1 {
					t.Fatalf("unexpected default settings: profile=%q day=%g", settings.ProfileDir, settings.DailyBudgetUSD)
				}
				c := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil }}}
				// Match v3ConfigFor: it carries ProfileDir but no resolved daily figure.
				a, err := New(Config{
					Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM",
					Interactive: true, OneModel: true, Errand: errand,
					ProfileDir: settings.ProfileDir, completer: c,
				})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(a.SettleWrites)
				t.Cleanup(func() { _ = a.Close() })
				recordUsage(t, UsageLedgerPath(), UsageLine{At: time.Now(), USD: 1.5, Calls: 1, Model: "test/model"})
				events := collect(t, mustSubmit(t, a, "work"))
				if len(events) != 1 || !errors.Is(events[0].Err, ErrSpendRail) || c.requests() != 0 {
					t.Fatalf("daily limit bypassed: events=%v calls=%d", kinds(events), c.requests())
				}
				setLimit(2)
				events = collect(t, mustSubmit(t, a, "work"))
				if c.requests() != 1 || len(events) == 0 || events[len(events)-1].Kind != EventTurnDone {
					t.Fatalf("live daily increase did not release the same root: events=%v calls=%d", kinds(events), c.requests())
				}
			})
		}
	}
}

// Both production child constructors must carry the selected authority, even
// when it is unlimited, without lending either settings tool to the child.
func TestTaskConstructorsCarryDailyAuthority(t *testing.T) {
	for _, checker := range []bool{false, true} {
		kind := "worker"
		if checker {
			kind = "checker"
		}
		for _, tc := range []struct {
			name                string
			limit, spent        float64
			authorized, blocked bool
		}{
			{"higher selected limit", 1000, 600, false, false},
			{"lower selected limit", 1, 1.5, false, true},
			{"selected unlimited", 0, 600, false, false},
			{"authorized daily spending", 1, 1.5, true, false},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				t.Setenv("CODEAF_HOME", t.TempDir())
				t.Setenv("CODEAF_DAILY_BUDGET", "")
				if err := config.WriteDailyBudgetUSD("", 500); err != nil {
					t.Fatal(err)
				}
				profile := t.TempDir()
				if err := config.WriteDailyBudgetUSD(profile, tc.limit); err != nil {
					t.Fatal(err)
				}
				c := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil }}}
				parent, _ := newTestAgent(t, c, func(cfg *Config) {
					cfg.ProfileDir = profile
					cfg.OneModel = true
					cfg.SpendPreauthorized = tc.authorized
				})
				g := parent.graph()
				g.run = func(*TaskNode) {}
				id := g.reserve()
				g.admit(id, taskSpec{title: "work", brief: "answer", acceptance: "done"})
				var child *Agent
				var err error
				if checker {
					child, err = parent.newAuditAgent(t.TempDir(), g.node(id), plainDoor(auditReadCommands), "")
				} else {
					child, err = parent.newTaskAgent(context.Background(), t.TempDir(), g.node(id), "")
				}
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(child.SettleWrites)
				t.Cleanup(func() { _ = child.Close() })
				if child.config.ProfileDir != "" || len(child.settingsTools()) != 0 {
					t.Fatal("child gained settings access")
				}
				// Only the carried value may answer now, including an explicit zero.
				// Reading the environment or default profile would fail the high case.
				t.Setenv("CODEAF_DAILY_BUDGET", "1")
				recordUsage(t, UsageLedgerPath(), UsageLine{At: time.Now(), USD: tc.spent, Calls: 1, Model: "test/model"})
				events := collect(t, mustSubmit(t, child, "work"))
				blocked := len(events) == 1 && errors.Is(events[0].Err, ErrSpendRail)
				calls := 1
				if tc.blocked {
					calls = 0
				}
				if blocked != tc.blocked || c.requests() != calls {
					t.Fatalf("wrong carried authority: events=%v calls=%d wantBlocked=%t wantCalls=%d", kinds(events), c.requests(), tc.blocked, calls)
				}
			})
		}
	}
}

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

type capTestCompleter struct{ calls int }

func (c *capTestCompleter) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	c.calls++
	usd := 0.01
	return &ai.Response{Usage: &ai.Usage{Cost: &usd}}, nil
}

func appDayProfile(t *testing.T) string {
	t.Helper()
	profile := t.TempDir()
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	return profile
}

func TestAppSettingsDayLimitReachesAnAlreadyConstructedRealGuard(t *testing.T) {
	profile := appDayProfile(t)
	if err := config.WriteDailyBudgetUSD(profile, 500); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	row, _ := json.Marshal(session.UsageLine{At: now, Day: now.Format("2006-01-02"), USD: 2})
	path := session.UsageLedgerPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(row, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	guard := session.CrewSpendGuard(profile, crewroute.Decision{}, true)
	guard.Price = func(string) (float64, float64, float64, bool) { return 1e-7, 1e-7, 0, true }
	calls := &capTestCompleter{}
	wrapped := guard.Wrap("fixture", calls)
	backend := newFakeBackend(newAppRecorder(), nil)
	defer backend.Close()
	for _, tc := range []struct {
		cap       float64
		wantCalls int
		blocked   bool
	}{{1, 0, true}, {3, 1, false}} {
		if _, err := backend.SetSettings(appwire.SettingsPatch{DayCapUSD: &appwire.Figure{USD: &tc.cap}}); err != nil {
			t.Fatal(err)
		}
		_, err := wrapped.CompleteWithMessages(t.Context(), []ai.Message{}, ai.WithMaxTokens(1))
		if (err != nil) != tc.blocked || calls.calls != tc.wantCalls {
			t.Fatalf("cap%v: %v calls%d", tc.cap, err, calls.calls)
		}
	}
	if guard.TaskCap != config.CrewTaskCapDefault {
		t.Fatal("task limit changed")
	}
	backend2 := newFakeBackend(newAppRecorder(), nil)
	defer backend2.Close()
	got, err := backend2.Settings()
	if err != nil || got.DayCapUSD == nil || *got.DayCapUSD != 3 {
		t.Fatalf("reopen: %+v,%v", got, err)
	}
}

func TestAppOpenMigratesBeforeTheEngineCanStart(t *testing.T) {
	profile := appDayProfile(t)
	if err := os.WriteFile(config.BudgetConfigPath(profile), []byte(`{"daily_budget_usd":500}`), 0600); err != nil {
		t.Fatal(err)
	}
	called := false
	backend := newFakeBackend(newAppRecorder(), func(appOpenAsk) (*appConn, error) {
		called = true
		got, err := config.DailyBudgetUSDAt(profile)
		if err != nil || got != 20 {
			t.Fatalf("opened before migration: %v,%v", got, err)
		}
		return nil, errors.New("fixture ends before opening")
	})
	defer backend.Close()
	_, _, _ = backend.ensureLocked(&appAgent{view: appwire.Agent{ID: "fixture"}})
	if !called {
		t.Fatal("open road did not reach fixture")
	}
}

func TestAppBadDayLimitPreservesResumeAndExplicitSetRepairsNumericRow(t *testing.T) {
	profile := appDayProfile(t)
	if err := os.WriteFile(config.BudgetConfigPath(profile), []byte(`{"daily_budget_usd":null,"other":42}`), 0600); err != nil {
		t.Fatal(err)
	}
	backend := newFakeBackend(newAppRecorder(), nil)
	defer backend.Close()
	if err := backend.state.write(func(f *appStateFile) error { f.ResumeAfterQuit = []string{"fixture"}; return nil }); err != nil {
		t.Fatal(err)
	}
	backend.resumeAfterQuit()
	var kept int
	_ = backend.state.read(func(f *appStateFile) { kept = len(f.ResumeAfterQuit) })
	if kept != 1 {
		t.Fatal("lost unresumed work on budget failure")
	}
	cap := 12.0
	got, err := backend.SetSettings(appwire.SettingsPatch{DayCapUSD: &appwire.Figure{USD: &cap}})
	if err != nil || got.DayCapUSD == nil || *got.DayCapUSD != 12 {
		t.Fatalf("repair: %+v,%v", got, err)
	}
	raw, err := os.ReadFile(config.BudgetConfigPath(profile))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil || fields["other"] != float64(42) {
		t.Fatalf("unrelated setting lost: %s", raw)
	}
}

func TestHeadlessRunRefusesUnreadableDailyAccounting(t *testing.T) {
	profile := appDayProfile(t)
	t.Setenv("CODEAF_PREAUTHORIZE_SPEND", "")
	if err := config.WriteDailyBudgetUSD(profile, 20); err != nil {
		t.Fatal(err)
	}
	path := session.UsageLedgerPath()
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	bound, err := runSpendBound(doRequest{}, profile, time.Now())
	if err == nil || bound.usd != 0 {
		t.Fatalf("unreadable ledger granted bound %+v,%v", bound, err)
	}
}

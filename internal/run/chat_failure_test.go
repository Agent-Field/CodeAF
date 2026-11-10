package run_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestChatEngineCarriesTheDailyLimitFailure(t *testing.T) {
	root, profile := t.TempDir(), t.TempDir()
	t.Setenv("CODEAF_HOME", root)
	t.Setenv("CODEAF_PROFILE_DIR", profile)
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", filepath.Join(root, "stub-codeaf"))
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	t.Setenv("CODEAF_PREAUTHORIZE_SPEND", "")
	if err := config.WriteDailyBudgetUSD(profile, 1); err != nil {
		t.Fatal(err)
	}
	session.RecordUsage(session.UsageLedgerPath(), session.UsageLine{At: time.Now(), USD: 1.5, Model: "test/model", Calls: 1})
	session.FlushUsage()
	seat := &dailyAuthoritySeat{}
	summary := run.ChatEngine.Start(runContext(t), session.RunSpec{
		Store: runOpenStore(t), Workspace: t.TempDir(), ProfileDir: profile,
		OneModel:     "test/model",
		CompleterFor: func(string) session.Completer { return seat },
	})
	if seat.calls.Load() != 0 {
		t.Fatal("a capped task reached the provider")
	}
	if !strings.Contains(summary.Failure, "daily limit reached · $1.50 spent of $1 · /budget day changes it") {
		t.Fatalf("chat engine discarded the worker's daily refusal: %q", summary.Failure)
	}
}

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// settingsBackend is a backend over a profile and a home of its own.
func settingsBackend(t *testing.T) *appBackend {
	t.Helper()
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	t.Setenv(home.EnvVar, t.TempDir())
	backend := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { backend.Close() })
	backend.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	return backend
}

// THE GENERAL ROWS READ THEIR DEFAULTS UNTIL SET, and a set row reads back.
func TestAppSettingsGeneralRowsAndPins(t *testing.T) {
	backend := settingsBackend(t)
	got, err := backend.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if got.Appearance != appwire.AppearanceSystem || got.TextSize != appwire.TextMedium || got.OpenAtLogin || !got.DockBadge || got.PinnedModels == nil {
		t.Fatalf("the defaults = %+v", got)
	}
	dark, off := appwire.AppearanceDark, false
	got, err = backend.SetSettings(appwire.SettingsPatch{Appearance: &dark, DockBadge: &off})
	if err != nil || got.Appearance != dark || got.DockBadge {
		t.Fatalf("after the set = %+v, %v", got, err)
	}
	got, err = backend.PinModels([]string{"deepseek/deepseek-v4", "qwen/qwen3.5"})
	if err != nil || len(got.PinnedModels) != 2 || got.PinnedModels[1] != "qwen/qwen3.5" {
		t.Fatalf("pinned = %+v, %v", got.PinnedModels, err)
	}
}

// AN ALWAYS-ALLOW RULE IS LISTED AND REMOVED FROM ITS OWN ROW, leaving the
// others as they were.
func TestAppRulesListAndRemove(t *testing.T) {
	backend := settingsBackend(t)
	profile := config.ProfileDir()
	if err := config.RememberToolApproval(profile, "read", "allow"); err != nil {
		t.Fatal(err)
	}
	if err := config.RememberBashApproval(profile, "git status*"); err != nil {
		t.Fatal(err)
	}
	rules, err := backend.Rules()
	if err != nil || len(rules) != 2 {
		t.Fatalf("rules = %+v, %v", rules, err)
	}
	if err := backend.RemoveRule("bash:git status*"); err != nil {
		t.Fatal(err)
	}
	rules, _ = backend.Rules()
	if len(rules) != 1 || rules[0].ID != "tool:read" {
		t.Fatalf("after the removal = %+v", rules)
	}
	var wire *appwire.Error
	if err := backend.RemoveRule("tool:write"); err == nil || !errors.As(err, &wire) || wire.Code != appwire.CodeNotFound {
		t.Fatalf("removing a rule nobody wrote = %v", err)
	}
}

// THE SPENDING EXPORT IS THE LEDGER'S CALLS IN THE RANGE, as CSV.
func TestAppSpendExportWritesTheRange(t *testing.T) {
	backend := settingsBackend(t)
	day := time.Date(2026, 10, 14, 12, 0, 0, 0, time.Local)
	path := session.UsageLedgerPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, line := range []session.UsageLine{
		{At: day, Day: "2026-10-14", Model: "deepseek/deepseek-v4", Calls: 1, USD: 0.25},
		{At: day.AddDate(0, 0, 3), Day: "2026-10-17", Model: "qwen/qwen3.5", Calls: 1, USD: 1},
	} {
		raw, _ := json.Marshal(line)
		b.Write(raw)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 10, 14, 0, 0, 0, 0, time.Local)
	out, err := backend.ExportSpend(from, from.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "deepseek/deepseek-v4") || strings.Contains(text, "qwen/qwen3.5") || !strings.HasPrefix(text, "at,day,") {
		t.Fatalf("the export:\n%s", text)
	}
}

func TestAppRulesMetadataRestoreAndExpiry(t *testing.T) {
	b := settingsBackend(t)
	now := time.Date(2026, 10, 12, 14, 2, 0, 0, time.UTC)
	b.now = func() time.Time { return now }
	profile := config.ProfileDir()
	origin := config.RuleOrigin{Agent: "api-docs", Place: "api", At: now.Add(-time.Hour)}
	if err := config.RememberBashApproval(profile, "git status*", origin); err != nil {
		t.Fatal(err)
	}
	if err := config.RememberToolApproval(profile, "read", "allow"); err != nil {
		t.Fatal(err)
	}
	if _, err := config.UseApprovalRule(profile, "bash:git status*", now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.FlushRuleUsage(profile) })
	rules, err := b.Rules()
	if err != nil || len(rules) != 2 || rules[0].ID != "bash:git status*" || rules[0].Origin != origin || rules[0].Uses != 1 || !rules[0].LastUsed.Equal(now) || rules[0].Text == "" {
		t.Fatalf("metadata = %+v, %v", rules, err)
	}
	if err := b.RemoveRule(rules[0].ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := b.Rules(); len(got) != 1 {
		t.Fatalf("removed = %+v", got)
	}
	now = now.Add(4 * time.Second)
	if err := b.RestoreRule(rules[0].ID); err != nil {
		t.Fatal(err)
	}
	got, err := b.Rules()
	if err != nil || len(got) != 2 || got[0] != rules[0] {
		t.Fatalf("restored = %+v, %v", got, err)
	}
	if err := b.RemoveRule(rules[0].ID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Second)
	if err := b.RestoreRule(rules[0].ID); err == nil {
		t.Fatal("expired Undo restored a rule")
	}
}

func TestAppRuleRestoreKeepsPriorityAndRejectsReplacement(t *testing.T) {
	b := settingsBackend(t)
	profile := config.ProfileDir()
	raw := "deny git push*, allow git *, prompt go test*"
	if err := applyRow(profile, config.KeyBashApprovals, raw); err != nil {
		t.Fatal(err)
	}
	if err := b.RemoveRule("bash:git *"); err != nil {
		t.Fatal(err)
	}
	if err := b.RestoreRule("bash:git *"); err != nil {
		t.Fatal(err)
	}
	if got := config.BashApprovalsAt(profile); got != raw {
		t.Fatalf("priority changed: %s", got)
	}
	if err := b.RemoveRule("bash:git *"); err != nil {
		t.Fatal(err)
	}
	if err := applyRow(profile, config.KeyBashApprovals, "deny git *"); err != nil {
		t.Fatal(err)
	}
	if err := b.RestoreRule("bash:git *"); err == nil {
		t.Fatal("Undo overwrote a new rule")
	}
}

// A terminal answer uses the same metadata writer as the desktop answer.
func TestTerminalAlwaysRecordsConversationAndFolder(t *testing.T) {
	profile := t.TempDir()
	agent := &namedRuleGate{title: "api-docs"}
	if err := bankToolApproval(agent, "/work/acme/api", profile, false)("edit"); err != nil {
		t.Fatal(err)
	}
	if err := bankBashApproval(agent, "/work/acme/api", profile, false)("go test*"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"tool:edit", "bash:go test*"} {
		got, err := config.RuleMetadataAt(profile, id)
		if err != nil || got.Origin.Agent != "api-docs" || got.Origin.Place != "api" || got.Origin.At.IsZero() {
			t.Fatalf("terminal %s = %+v, %v", id, got, err)
		}
	}
}

type namedRuleGate struct {
	title  string
	policy *approval.Policy
}

func (g *namedRuleGate) ShortTitle() string                   { return g.title }
func (g *namedRuleGate) SetApprovalPolicy(p *approval.Policy) { g.policy = p }

// A writer queued behind another process must check the row after that write.
func TestAppRestoreChecksConflictUnderProfileLock(t *testing.T) {
	for _, kind := range []string{"bash", "tool"} {
		t.Run(kind, func(t *testing.T) {
			b := settingsBackend(t)
			profile := config.ProfileDir()
			key, initial, replacement, id := config.KeyBashApprovals, "allow go test*", "deny go test*", "bash:go test*"
			if kind == "tool" {
				key, initial, replacement, id = config.KeyToolApprovals, "bash:allow", "bash:deny", "tool:bash"
			}
			if err := applyRow(profile, key, initial); err != nil {
				t.Fatal(err)
			}
			if err := b.RemoveRule(id); err != nil {
				t.Fatal(err)
			}
			lock, err := os.OpenFile(config.BudgetConfigPath(profile)+".lock", os.O_CREATE|os.O_RDWR, 0600)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			if err := filelock.Lock(lock, true, false); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- b.RestoreRule(id) }()
			deadline := time.Now().Add(time.Second)
			for {
				stack := make([]byte, 1<<20)
				n := runtime.Stack(stack, true)
				if strings.Contains(string(stack[:n]), "config.lockProfileConfig(") {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("restore did not reach profile lock")
				}
				runtime.Gosched()
			}
			raw, _ := json.Marshal(map[string]string{key: replacement})
			if err := os.WriteFile(config.BudgetConfigPath(profile), raw, 0600); err != nil {
				t.Fatal(err)
			}
			if err := filelock.Unlock(lock); err != nil {
				t.Fatal(err)
			}
			err = <-done
			var wire *appwire.Error
			if !errors.As(err, &wire) || wire.Code != appwire.CodeConflict {
				t.Fatalf("restore = %v, want conflict", err)
			}
			if got := settingsRow(profile, key); got != replacement {
				t.Fatalf("replacement overwritten: %s", got)
			}
		})
	}
}

func TestAppMultipleUndosKeepRelativePriority(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprint(reverse), func(t *testing.T) {
			b := settingsBackend(t)
			profile := config.ProfileDir()
			raw := "deny A*, allow B*"
			if err := applyRow(profile, config.KeyBashApprovals, raw); err != nil {
				t.Fatal(err)
			}
			ids := []string{"bash:A*", "bash:B*"}
			for _, id := range ids {
				if err := b.RemoveRule(id); err != nil {
					t.Fatal(err)
				}
			}
			if reverse {
				ids[0], ids[1] = ids[1], ids[0]
			}
			for _, id := range ids {
				if err := b.RestoreRule(id); err != nil {
					t.Fatal(err)
				}
			}
			if got := config.BashApprovalsAt(profile); got != raw {
				t.Fatalf("priority = %q, want %q", got, raw)
			}
		})
	}
}

func TestTerminalAlwaysSurvivesMalformedMetadata(t *testing.T) {
	for _, kind := range []string{"tool", "bash"} {
		t.Run(kind, func(t *testing.T) {
			profile := t.TempDir()
			if err := os.WriteFile(filepath.Join(profile, "rule-metadata.json"), []byte("{broken"), 0600); err != nil {
				t.Fatal(err)
			}
			gate := &namedRuleGate{title: "api"}
			save := bankToolApproval(gate, t.TempDir(), profile, false)
			name := "edit"
			if kind == "bash" {
				save = bankBashApproval(gate, t.TempDir(), profile, false)
				name = "go test*"
			}
			if err := save(name); err != nil {
				t.Fatal(err)
			}
			if gate.policy == nil {
				t.Fatal("committed permission did not refresh the live gate")
			}
			decision := gate.policy.Check("edit", nil)
			if kind == "bash" {
				decision = gate.policy.CheckBash("go test ./tiny")
			}
			if decision.Action != approval.ActionAllow {
				t.Fatalf("saved permission inactive: %+v", decision)
			}
		})
	}
}

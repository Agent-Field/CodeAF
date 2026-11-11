package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

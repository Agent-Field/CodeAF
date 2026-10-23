package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// An external settings writer never increments this process's counter. Atomic
// replacement also exercises the way another terminal's settings panel saves.
func writeExternalCrewSettings(t *testing.T, path string, values map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(time.Second)
	if old, err := os.Stat(path); err == nil {
		stamp = old.ModTime().Add(time.Second)
	}
	tmp := path + ".external"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(tmp, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func TestRunningCrewReadsSettingsChangedByAnotherProcess(t *testing.T) {
	profile, workspace := t.TempDir(), t.TempDir()
	source, err := v3RolesSource(workspace, profile)
	if err != nil {
		t.Fatal(err)
	}
	generation := config.SettingsGeneration()
	writeExternalCrewSettings(t, config.BudgetConfigPath(profile), map[string]string{config.KeyTierReflexModel: "vendor/first", config.KeyTierLowModel: "vendor/cheap"})
	if config.SettingsGeneration() != generation {
		t.Fatal("fixture used in-process writer")
	}
	resolve := func(role roles.Role, want string) {
		t.Helper()
		got, err := roles.Resolve(roles.Source(source), role, "vendor/chat")
		if err != nil || got != want {
			t.Fatalf("role=%s model=%q want=%q error=%v", role, got, want, err)
		}
	}
	resolve(roles.RoleReflex, "vendor/first")
	resolve(roles.RoleTitle, "vendor/cheap")
	// Same-length replacements must invalidate both the crew snapshot and the
	// config parser without requiring a process restart or a file-size change.
	writeExternalCrewSettings(t, config.BudgetConfigPath(profile), map[string]string{config.KeyTierReflexModel: "vendor/other", config.KeyTierLowModel: "vendor/newer"})
	resolve(roles.RoleReflex, "vendor/other")
	resolve(roles.RoleTitle, "vendor/newer")
	writeExternalCrewSettings(t, config.ProjectConfigPath(workspace), map[string]string{config.KeyTierLowModel: "vendor/project"})
	resolve(roles.RoleTitle, "vendor/project")
	if err := os.Remove(config.ProjectConfigPath(workspace)); err != nil {
		t.Fatal(err)
	}
	resolve(roles.RoleTitle, "vendor/newer")
	if err := os.Remove(config.BudgetConfigPath(profile)); err != nil {
		t.Fatal(err)
	}
	resolve(roles.RoleReflex, config.DefaultReflexModel)
}

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
)

// Contract 4: Windows health uses the shared shell resolver, including its
// explicit-override refusal, instead of accepting a WSL launcher from PATH.
func TestSetupContractWindowsGitBash(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	oldRun, oldLook := readinessRun, readinessLookPath
	readinessRun = func(context.Context, string, ...string) (string, error) { return "configured", nil }
	readinessLookPath = func(name string) (string, error) { return name, nil }
	t.Cleanup(func() { readinessRun, readinessLookPath = oldRun, oldLook })
	bash := filepath.Join(t.TempDir(), "bash.exe")
	if err := os.WriteFile(bash, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	b := newFakeBackend(newAppRecorder(), nil)
	defer b.Close()
	for _, state := range []string{"ok", "error"} {
		override := bash
		if state == "error" {
			override += ".missing"
		}
		t.Setenv("CODEAF_BASH", override)
		health, err := b.HealthCheck()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, check := range health.Checks {
			if check.Name == "Git Bash" {
				found = true
				if check.State != state || !strings.Contains(check.Fix, "CODEAF_BASH") {
					t.Fatalf("Git Bash = %+v", check)
				}
			}
		}
		if !found {
			t.Fatal("Windows health has no Git Bash check")
		}
	}
}

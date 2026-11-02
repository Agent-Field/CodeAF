package github

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

func TestAppGitHubHandoffOwnsTheRunnerConnection(t *testing.T) {
	t.Setenv("GH_TOKEN", "shell-account")
	t.Setenv("GITHUB_TOKEN", "other-shell-account")
	profile := t.TempDir()
	if err := config.WriteGitHubToken(profile, "profile-account"); err != nil {
		t.Fatal(err)
	}
	keys := config.NewAppCredentials()
	keys.Set("github", "app-assignee")
	defer config.UseAppCredentials(keys)()
	if token, via := TokenAt(context.Background(), profile); token != "app-assignee" || via != factory.ViaToken {
		t.Fatalf("handoff = %q via %q", token, via)
	}
	keys.Set("github", "")
	if token, via := TokenAt(context.Background(), profile); token != "" || via != "" {
		t.Fatal("removed app account fell back to another person's credential")
	}
}

// fakeGH stands a gh in for the real one that runs script.
func fakeGH(t *testing.T, script string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	was := forge.LookPath
	forge.LookPath = func(string) (string, error) { return path, nil }
	t.Cleanup(func() { forge.LookPath = was; forgetGHToken() })
	forgetGHToken()
}

// A GH THAT HANGS (a keychain prompt nobody answers, a dead network) NEVER
// HOLDS A READ: every read of the factory asks for the token, once per job,
// and each once waited for gh to give up. The answer is gh's last one, asked
// again behind the reads.
func TestAHangingGHNeverHoldsTheReadsThatAskForTheToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	profile := t.TempDir()
	if err := config.WriteGitHubViaGH(profile); err != nil {
		t.Fatal(err)
	}
	fakeGH(t, "sleep 60")
	began := time.Now()
	for range 20 {
		TokenAt(context.Background(), profile)
	}
	if took := time.Since(began); took > time.Second {
		t.Fatalf("20 token reads with gh hanging took %v", took)
	}
	fakeGH(t, "echo gh-account")
	if token, via := TokenAt(context.Background(), profile); token != "gh-account" || via != factory.ViaGH {
		t.Fatalf("a gh that answers at once: %q via %q", token, via)
	}
}

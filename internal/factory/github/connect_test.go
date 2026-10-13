package github

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
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

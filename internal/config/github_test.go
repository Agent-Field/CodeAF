package config

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// A GitHub token the person typed is one value in the profile; saying yes to
// gh replaces it, and a token replaces that yes.
func TestGitHubTokenAndViaReplaceEachOther(t *testing.T) {
	dir := t.TempDir()
	if GitHubTokenAt(dir) != "" || GitHubViaAt(dir) != "" {
		t.Fatal("an empty profile holds a GitHub connection")
	}
	if err := WriteGitHubToken(dir, "  ghp_typed_by_person  "); err != nil {
		t.Fatal(err)
	}
	if got := GitHubTokenAt(dir); got != "ghp_typed_by_person" {
		t.Fatalf("token = %q", got)
	}
	if err := WriteGitHubViaGH(dir); err != nil {
		t.Fatal(err)
	}
	if GitHubTokenAt(dir) != "" || GitHubViaAt(dir) != "gh" {
		t.Fatalf("yes to gh left token %q via %q", GitHubTokenAt(dir), GitHubViaAt(dir))
	}
	if err := WriteGitHubToken(dir, "ghp_again"); err != nil {
		t.Fatal(err)
	}
	if GitHubViaAt(dir) != "" || GitHubTokenAt(dir) != "ghp_again" {
		t.Fatalf("a token left via %q", GitHubViaAt(dir))
	}
}

// THE APP'S GITHUB KEY IS MEMORY ONLY: it answers the reader, and no writer
// puts a token in the profile while the app holds the keys. The yes to gh is
// consent, not a key, so the app's profile keeps that alone, and takes it back.
func TestAppGitHubKeyNeverReachesTheProfile(t *testing.T) {
	dir := t.TempDir()
	creds := NewAppCredentials()
	defer UseAppCredentials(creds)()
	creds.Set("github", "opaque-app-gh-7c21")
	if got := GitHubTokenAt(dir); got != "opaque-app-gh-7c21" {
		t.Fatalf("the app's key lost to %q", got)
	}
	if err := WriteGitHubToken(dir, "ghp_written"); !errors.Is(err, ErrAppKeys) {
		t.Fatalf("a token write was answered %v", err)
	}
	if err := WriteGitHubViaGH(dir); err != nil || GitHubViaAt(dir) != "gh" {
		t.Fatalf("the yes to gh was not kept: %v, via %q", err, GitHubViaAt(dir))
	}
	if err := ForgetGitHubViaGH(dir); err != nil || GitHubViaAt(dir) != "" {
		t.Fatalf("the yes to gh was not taken back: %v, via %q", err, GitHubViaAt(dir))
	}
	raw, _ := os.ReadFile(BudgetConfigPath(dir))
	if strings.Contains(string(raw), "opaque-app") || strings.Contains(string(raw), "ghp_") {
		t.Fatalf("a GitHub key reached the profile: %s", raw)
	}
}

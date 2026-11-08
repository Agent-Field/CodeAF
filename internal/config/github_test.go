package config

import (
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

// A TOKEN PASTED INTO AN APP-RUN ENGINE OUTLIVES THE LAUNCH IN ITS PROFILE
// (live e2e round 9: held in memory alone, it was lost at every desk launch).
// The app's own key still answers the reader first and never reaches the
// profile. The yes to gh is consent, not a key, and is kept and taken back.
func TestAppKeepsAPastedGitHubTokenInTheProfileForTheNextLaunch(t *testing.T) {
	dir := t.TempDir()
	creds := NewAppCredentials()
	defer UseAppCredentials(creds)()
	creds.Set("github", "opaque-app-gh-7c21")
	if got := GitHubTokenAt(dir); got != "opaque-app-gh-7c21" {
		t.Fatalf("the app's key lost to %q", got)
	}
	// A pasted token is kept in the profile too, owner-readable only, and the
	// app's own key still answers first.
	if err := WriteGitHubToken(dir, "ghp_written"); err != nil {
		t.Fatalf("a token write was answered %v", err)
	}
	if got := GitHubTokenAt(dir); got != "opaque-app-gh-7c21" {
		t.Fatalf("the profile's copy won over the app's key: %q", got)
	}
	if info, err := os.Stat(BudgetConfigPath(dir)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the profile holding the token is %v, %v; want 0600", info.Mode().Perm(), err)
	}
	// The next launch, where the app hands nothing back, reads it from there.
	next := NewAppCredentials()
	restore := UseAppCredentials(next)
	if got := GitHubTokenAt(dir); got != "ghp_written" {
		t.Fatalf("the next launch lost the token: %q", got)
	}
	restore()
	if err := WriteGitHubViaGH(dir); err != nil || GitHubViaAt(dir) != "gh" {
		t.Fatalf("the yes to gh was not kept: %v, via %q", err, GitHubViaAt(dir))
	}
	if err := ForgetGitHubViaGH(dir); err != nil || GitHubViaAt(dir) != "" {
		t.Fatalf("the yes to gh was not taken back: %v, via %q", err, GitHubViaAt(dir))
	}
	raw, _ := os.ReadFile(BudgetConfigPath(dir))
	if strings.Contains(string(raw), "opaque-app") {
		t.Fatalf("the app's own key reached the profile: %s", raw)
	}
}

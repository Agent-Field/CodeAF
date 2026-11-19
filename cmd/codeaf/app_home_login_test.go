package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
)

// `codeaf app --home <dir>` moves codeaf's state to <dir> and leaves the
// person's login home where it was, because the dot-folders of the person's
// other programs are not codeaf's to move. Nothing else about the person's
// environment changes, and a child process inherits both answers.
func TestAppHomeFlagMovesTheStateAndKeepsTheLoginHome(t *testing.T) {
	login := t.TempDir()
	state := filepath.Join(t.TempDir(), "app-state")
	t.Setenv("HOME", login)
	t.Setenv(home.EnvVar, "")
	t.Setenv(home.LoginEnvVar, "")

	restore, err := useAppHome(state)
	if err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv(home.EnvVar); got != state {
		t.Fatalf("CODEAF_HOME = %q, want %q", got, state)
	}
	if got, err := home.Login(); err != nil || got != login {
		t.Fatalf("the login home under --home = %q err = %v, want the real one %q", got, err, login)
	}
	if got := home.Dir(); got != state {
		t.Fatalf("the state root under --home = %q, want %q", got, state)
	}
	if got := home.OwnLogin(login); got != state {
		t.Fatalf("codeaf's own skills folder under --home reads from %q, want the app's folder %q", got, state)
	}
	restore()
	if got, err := home.Login(); err != nil || got != state {
		t.Fatalf("the login home after the app returned = %q err = %v, want the quarantine %q", got, err, state)
	}
}

func TestAppWithoutHomeFlagLeavesTheEnvironmentAlone(t *testing.T) {
	login := t.TempDir()
	t.Setenv("HOME", login)
	t.Setenv(home.EnvVar, "")
	t.Setenv(home.LoginEnvVar, "")

	restore, err := useAppHome("  ")
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	if value, set := os.LookupEnv(home.LoginEnvVar); set && value != "" {
		t.Fatalf("a run with no --home recorded a login home: %q", value)
	}
	if got := os.Getenv(home.EnvVar); got != "" {
		t.Fatalf("a run with no --home set CODEAF_HOME to %q", got)
	}
	if got, err := home.Login(); err != nil || got != login {
		t.Fatalf("the login home with no --home = %q err = %v, want %q", got, err, login)
	}
}

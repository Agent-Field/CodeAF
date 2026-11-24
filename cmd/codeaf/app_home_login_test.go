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

// THE APP'S --HOME TAKES EFFECT BEFORE MAIN READS THE STATE ROOT, because the
// legacy-folder adoption, the root's first witness and the crash-telemetry
// consent all read it first. Every way the flag is spelled gives the same home,
// and no other command and no other spelling of the app moves anything.
func TestAppHomeFlagIsReadBeforeTheStateRootIsRead(t *testing.T) {
	login := t.TempDir()
	state := filepath.Join(t.TempDir(), "app-state")
	t.Setenv("HOME", login)
	for _, args := range [][]string{
		{"app", "--home", state},
		{"app", "--home=" + state},
		{"app", "-home", state},
		{"app", "--turn-long", "5s", "--home", state},
	} {
		t.Setenv(home.EnvVar, "")
		t.Setenv(home.LoginEnvVar, "")
		useAppHomeEarly(args)
		if got := home.Dir(); got != state {
			t.Errorf("%v: the state root is %q, want %q", args, got, state)
		}
		if got, err := home.Login(); err != nil || got != login {
			t.Errorf("%v: the login home is %q err = %v, want the real one %q", args, got, err, login)
		}
	}
}

func TestAppHomeFlagIsLeftAloneByEveryOtherCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, args := range [][]string{
		nil,
		{"app"},
		{"app", "--schema"},
		{"chat", "--home", "/elsewhere"},
		{"exec", "--home", "/elsewhere", "say hi"},
		{"version"},
	} {
		t.Setenv(home.EnvVar, "")
		t.Setenv(home.LoginEnvVar, "")
		useAppHomeEarly(args)
		if got := os.Getenv(home.EnvVar); got != "" {
			t.Errorf("%v set CODEAF_HOME to %q", args, got)
		}
		if got := os.Getenv(home.LoginEnvVar); got != "" {
			t.Errorf("%v recorded a login home %q", args, got)
		}
	}
}

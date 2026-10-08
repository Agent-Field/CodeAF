package main

// The Clone door: a repository with no checkout is cloned into the factory's
// own folder and recorded, over gh when gh is here, else git with the token in
// the child's environment. Every program is a fake on PATH in a temp dir:
// NEVER A REAL NETWORK CALL.

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// cloneRig is a store watching acme/api and a bin folder first on PATH.
func cloneRig(t *testing.T) (*store.Store, string, string) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	return st, bin, filepath.Join(t.TempDir(), "said")
}

// fakeProgram writes a shell script named name into bin that records its
// arguments and the named variables into said, one per line, then makes the
// folder its last argument names a checkout (or fails, when fail is set).
func fakeProgram(t *testing.T, bin, name, said, vars, fail string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"echo \"args: $*\" >> '" + said + "'\n"
	for _, v := range strings.Fields(vars) {
		script += "echo \"" + v + "=$" + v + "\" >> '" + said + "'\n"
	}
	if fail != "" {
		script += "echo '" + fail + "' >&2\nexit 1\n"
	} else {
		script += "for last; do :; done\nmkdir -p \"$last/.git\"\n"
	}
	if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readSaid(t *testing.T, said string) string {
	t.Helper()
	b, _ := os.ReadFile(said)
	return string(b)
}

func TestFactoryCloneDoorUsesGhAndRecordsTheCheckout(t *testing.T) {
	st, bin, said := cloneRig(t)
	fakeProgram(t, bin, "gh", said, "GH_TOKEN GIT_TERMINAL_PROMPT", "")
	keep := forge.LookPath
	forge.LookPath = exec.LookPath
	t.Cleanup(func() { forge.LookPath = keep })

	door := factoryCloneDoor(st, func(context.Context) string { return "tok-secret" })
	dir, err := door(context.Background(), "api")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(st.Root(), "repos", "acme", "api")
	if dir != want {
		t.Fatalf("cloned into %q, want %q", dir, want)
	}
	got := readSaid(t, said)
	if !strings.Contains(got, "args: repo clone acme/api "+want+"\n") {
		t.Fatalf("gh was asked:\n%s", got)
	}
	if !strings.Contains(got, "GH_TOKEN=tok-secret") || !strings.Contains(got, "GIT_TERMINAL_PROMPT=0") {
		t.Fatalf("gh's environment did not carry the token and no prompt:\n%s", got)
	}
	if strings.Contains(strings.SplitN(got, "\n", 2)[0], "tok-secret") {
		t.Fatal("the token is on gh's command line")
	}
	// THE CLONE IS THE CHECKOUT from now on, for every reader of it.
	if st.CheckoutDir("acme/api") != want || factoryRepoDirs(st, "")("api") != want {
		t.Fatalf("the clone was not recorded: %q", st.CheckoutDir("api"))
	}
	// A KNOWN FOLDER IS NEVER CLONED AGAIN.
	again, err := door(context.Background(), "acme/api")
	if err != nil || again != want || strings.Count(readSaid(t, said), "args:") != 1 {
		t.Fatalf("a second ask = %q, %v, gh asked %d times", again, err, strings.Count(readSaid(t, said), "args:"))
	}
}

func TestFactoryCloneDoorFallsBackToGitWithTheTokenInTheEnvironment(t *testing.T) {
	st, bin, said := cloneRig(t)
	fakeProgram(t, bin, "git", said, "GIT_CONFIG_COUNT GIT_CONFIG_KEY_0 GIT_CONFIG_VALUE_0", "")
	keep := forge.LookPath
	forge.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { forge.LookPath = keep })

	dir, err := factoryCloneDoor(st, func(context.Context) string { return "tok-secret" })(context.Background(), "acme/api")
	if err != nil {
		t.Fatal(err)
	}
	got := readSaid(t, said)
	if !strings.Contains(got, "args: clone https://github.com/acme/api.git "+dir+"\n") {
		t.Fatalf("git was asked:\n%s", got)
	}
	header := "AUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:tok-secret"))
	for _, want := range []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.https://github.com/.extraheader", "GIT_CONFIG_VALUE_0=" + header} {
		if !strings.Contains(got, want) {
			t.Fatalf("git's environment is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(strings.SplitN(got, "\n", 2)[0], "tok-secret") {
		t.Fatal("the token is on git's command line")
	}
	if st.CheckoutDir("api") != dir {
		t.Fatal("the git clone was not recorded")
	}
}

// A FAILED CLONE SAYS GIT'S LAST LINE WITHOUT THE TOKEN, leaves no folder and
// records nothing.
func TestFactoryCloneDoorFailureSaysWhyAndKeepsNothing(t *testing.T) {
	st, bin, said := cloneRig(t)
	fakeProgram(t, bin, "gh", said, "", "fatal: repository not found tok-secret")
	keep := forge.LookPath
	forge.LookPath = exec.LookPath
	t.Cleanup(func() { forge.LookPath = keep })

	_, err := factoryCloneDoor(st, func(context.Context) string { return "tok-secret" })(context.Background(), "acme/api")
	if err == nil || err.Error() != "could not clone acme/api · repository not found …" {
		t.Fatalf("a failed clone said %v", err)
	}
	if _, serr := os.Stat(factoryCloneFolder(st.Root(), "acme/api")); !errors.Is(serr, os.ErrNotExist) {
		t.Fatalf("a failed clone left its folder: %v", serr)
	}
	if st.CheckoutDir("api") != "" {
		t.Fatal("a failed clone was recorded")
	}
	// A name no watched repository has is refused before anything runs.
	if _, err := factoryCloneDoor(st, nil)(context.Background(), "nope"); err == nil || !strings.Contains(err.Error(), "watch it in the repos list first") {
		t.Fatalf("an unknown short name = %v", err)
	}
}

// LAUNCH REFUSES AN ITEM WHOSE REPOSITORY HAS NO CHECKOUT, and passes one
// that has, whatever asked for it.
func TestFactoryLaunchNeedsACheckout(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{Repo: "api", Title: "one", State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	var launched []int
	seam := factoryNeedsCheckout(factory.Seam{Launch: func(id int) error { launched = append(launched, id); return nil }}, st, factoryRepoDirs(st, ""))
	if err := seam.Launch(it.ID); err == nil || err.Error() != "not run · codeaf does not know where api is checked out" || len(launched) != 0 {
		t.Fatalf("a launch with no checkout = %v, launched %v", err, launched)
	}
	if err := st.SetCheckout("api", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := seam.Launch(it.ID); err != nil || len(launched) != 1 {
		t.Fatalf("a launch with a checkout = %v, launched %v", err, launched)
	}
}

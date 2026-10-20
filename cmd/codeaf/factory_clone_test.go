package main

// The Clone door and the checkout door: a repository with no checkout is
// cloned into the factory's own folder and recorded, over git with the token
// in the child's environment and never over gh; every start asks the checkout
// door first, which refuses only what cannot be cloned; and the runner clones
// on the first step. Programs are fakes on PATH in a temp dir, or real git
// cloning a bare repository on disk that https://github.com/ is rewritten to:
// NEVER A REAL NETWORK CALL.

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
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
// pause makes it sleep that long first, so two callers overlap.
func fakeProgram(t *testing.T, bin, name, said, vars, fail string, pause ...string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"echo \"args: $*\" >> '" + said + "'\n"
	for _, v := range strings.Fields(vars) {
		script += "echo \"" + v + "=$" + v + "\" >> '" + said + "'\n"
	}
	for _, p := range pause {
		script += "sleep " + p + "\n"
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

// THE CLONE GOES OVER GIT AND HTTPS EVEN WHEN GH IS HERE: gh follows the
// person's git_protocol and may go over ssh with a key the factory does not
// hold. The token rides an http header in git's environment, never its
// command line, and the clone is the checkout from then on.
func TestFactoryCloneDoorClonesOverGitWithTheTokenInTheEnvironment(t *testing.T) {
	st, bin, said := cloneRig(t)
	fakeProgram(t, bin, "git", said, "GIT_CONFIG_COUNT GIT_CONFIG_KEY_0 GIT_CONFIG_VALUE_0 GIT_TERMINAL_PROMPT", "")
	ghSaid := filepath.Join(t.TempDir(), "gh-said")
	fakeProgram(t, bin, "gh", ghSaid, "", "")

	door := factoryCloneDoor(st, func(context.Context) string { return "tok-secret" })
	dir, err := door(context.Background(), "api")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(st.Root(), "repos", "acme", "api")
	if dir != want {
		t.Fatalf("cloned into %q, want %q", dir, want)
	}
	if readSaid(t, ghSaid) != "" {
		t.Fatalf("gh was asked to clone:\n%s", readSaid(t, ghSaid))
	}
	got := readSaid(t, said)
	if !strings.Contains(got, "args: clone https://github.com/acme/api.git "+dir+"\n") {
		t.Fatalf("git was asked:\n%s", got)
	}
	header := "AUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:tok-secret"))
	for _, want := range []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.https://github.com/.extraheader", "GIT_CONFIG_VALUE_0=" + header, "GIT_TERMINAL_PROMPT=0"} {
		if !strings.Contains(got, want) {
			t.Fatalf("git's environment is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(strings.SplitN(got, "\n", 2)[0], "tok-secret") {
		t.Fatal("the token is on git's command line")
	}
	// THE CLONE IS THE CHECKOUT from now on, for every reader of it.
	if st.CheckoutDir("acme/api") != want || factoryRepoDirs(st, "")("api") != want {
		t.Fatalf("the clone was not recorded: %q", st.CheckoutDir("api"))
	}
	// A KNOWN FOLDER IS NEVER CLONED AGAIN.
	again, err := door(context.Background(), "acme/api")
	if err != nil || again != want || strings.Count(readSaid(t, said), "args:") != 1 {
		t.Fatalf("a second ask = %q, %v, git asked %d times", again, err, strings.Count(readSaid(t, said), "args:"))
	}
}

// TWO STARTS OF ONE REPOSITORY CLONE IT ONCE: the second waits for the first
// and answers the folder it recorded, rather than cloning into the same
// folder and taking the first one's clone away when it fails.
func TestFactoryCloneDoorClonesARepositoryOnceForTwoStarts(t *testing.T) {
	st, bin, said := cloneRig(t)
	fakeProgram(t, bin, "git", said, "", "", "0.3")
	door := factoryCloneDoor(st, func(context.Context) string { return "tok-secret" })

	var wg sync.WaitGroup
	dirs := make([]string, 2)
	errs := make([]error, 2)
	for i := range dirs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			dirs[i], errs[i] = door(context.Background(), "acme/api")
		}(i)
	}
	wg.Wait()
	want := factoryCloneFolder(st.Root(), "acme/api")
	for i := range dirs {
		if errs[i] != nil || dirs[i] != want {
			t.Fatalf("start %d = %q, %v; want %q", i, dirs[i], errs[i], want)
		}
	}
	if n := strings.Count(readSaid(t, said), "args: clone"); n != 1 {
		t.Fatalf("git cloned %d times for two starts:\n%s", n, readSaid(t, said))
	}
}

// A FAILED CLONE SAYS GIT'S LAST LINE WITHOUT THE TOKEN, leaves no folder and
// records nothing.
func TestFactoryCloneDoorFailureSaysWhyAndKeepsNothing(t *testing.T) {
	st, bin, said := cloneRig(t)
	fakeProgram(t, bin, "git", said, "", "fatal: repository not found tok-secret")

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

// THE CHECKOUT DOOR SAYS here, will_clone OR missing, and only missing is an
// error: the NeedsCheckout naming why, in the words the start refuses with.
func TestFactoryCheckoutDoorSaysHereWillCloneOrMissing(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"acme/api", "acme/web"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCheckout("acme/web", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	dirs := factoryRepoDirs(st, "")
	connected := factoryCheckoutDoor(st, dirs, func(context.Context) string { return "tok" })
	offline := factoryCheckoutDoor(st, dirs, func(context.Context) string { return "" })
	for _, c := range []struct {
		name  string
		door  func(context.Context, string) (factory.CheckoutState, error)
		repo  string
		state factory.CheckoutState
		words string
	}{
		{"a known checkout", offline, "acme/web", factory.CheckoutHere, ""},
		{"a known checkout by its short name", offline, "web", factory.CheckoutHere, ""},
		{"a watched repository and a token", connected, "api", factory.CheckoutWillClone, ""},
		{"any GitHub name and a token", connected, "acme/other", factory.CheckoutWillClone, ""},
		{"no token", offline, "acme/api", factory.CheckoutMissing, "acme/api isn't checked out on this machine · " + factory.CheckoutNeedsGitHub},
		{"not a GitHub repository", connected, "notes", factory.CheckoutMissing, "notes isn't checked out on this machine · " + factory.CheckoutNotOnGitHub},
		{"no repository", connected, "", "", ""},
	} {
		state, err := c.door(context.Background(), c.repo)
		if state != c.state {
			t.Errorf("%s: state %q, want %q", c.name, state, c.state)
		}
		if c.words == "" {
			if err != nil {
				t.Errorf("%s: %v", c.name, err)
			}
			continue
		}
		var needs factory.NeedsCheckout
		if !errors.As(err, &needs) || err.Error() != c.words {
			t.Errorf("%s: err %v, want the NeedsCheckout %q", c.name, err, c.words)
		}
	}
}

// LAUNCH REFUSES ONLY WHAT CANNOT BE CLONED, and passes a repository it will
// clone and one that is checked out, whatever asked for it.
func TestFactoryLaunchRefusesOnlyWhatCannotBeCloned(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	// The store files a short name under the one watched repository that has
	// it, so the refusal names acme/api.
	it, err := st.Create(factory.Item{Repo: "api", Title: "one", State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	token := ""
	var launched []int
	seam := factoryCheckouts(factory.Seam{Launch: func(id int) error { launched = append(launched, id); return nil }}, st, factoryRepoDirs(st, ""), func(context.Context) string {
		mu.Lock()
		defer mu.Unlock()
		return token
	})
	setToken := func(s string) { mu.Lock(); token = s; mu.Unlock() }
	if seam.CheckoutOf == nil {
		t.Fatal("the seam has no checkout door")
	}
	var needs factory.NeedsCheckout
	if err := seam.Launch(it.ID); !errors.As(err, &needs) || err.Error() != "acme/api isn't checked out on this machine · "+factory.CheckoutNeedsGitHub || len(launched) != 0 {
		t.Fatalf("a launch that cannot clone = %v, launched %v", err, launched)
	}
	setToken("tok")
	if err := seam.Launch(it.ID); err != nil || len(launched) != 1 {
		t.Fatalf("a launch the runner will clone for = %v, launched %v", err, launched)
	}
	setToken("")
	if err := st.SetCheckout("acme/api", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := seam.Launch(it.ID); err != nil || len(launched) != 2 {
		t.Fatalf("a launch with a checkout = %v, launched %v", err, launched)
	}
}

// githubOnDisk makes a bare repository holding one commit with marker in it,
// at <remotes>/acme/ledger.git, and points git's https://github.com/ at
// <remotes>/ for the rest of the test (GIT_CONFIG_GLOBAL), so a clone the
// factory makes of acme/ledger is a real git clone that never leaves the disk.
func githubOnDisk(t *testing.T, marker string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on this machine")
	}
	remotes := t.TempDir()
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, []byte("[url \"file://"+remotes+"/\"]\n\tinsteadOf = https://github.com/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	seed := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", seed)
	if err := os.WriteFile(filepath.Join(seed, marker), []byte("here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("-C", seed, "add", marker)
	git("-C", seed, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "first")
	git("clone", "-q", "--bare", seed, filepath.Join(remotes, "acme", "ledger.git"))
}

// ledgerFloor is a store watching acme/ledger and acme/gone (which is on no
// disk), with no checkout of either and no workspace that could stand for one.
func ledgerFloor(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "factory"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"acme/ledger", "acme/gone"}); err != nil {
		t.Fatal(err)
	}
	return st
}

// ledgerJob launches a one-check job on repo through the runner this process
// builds, with no workspace, and answers its id.
func ledgerJob(t *testing.T, st *store.Store, repo, check string) int {
	t.Helper()
	r := buildFactoryRunner(st, st, "", t.TempDir(), nil, nil)
	stopAll(t, st, r)
	id, err := st.Add(context.Background(), factory.Item{
		Title: "prove it is there", Repo: repo, Tier: factory.TierOwner,
		Stages: []factory.Stage{{Name: "test", Kind: factory.StageCheck, Ask: check, On: true}, {Name: factory.ApproveName, Kind: factory.StageGate, On: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := factory.LocalSeam(st, time.Now(), factory.WithRunner(r)).Launch(id); err != nil {
		t.Fatal(err)
	}
	return id
}

// A START WITH NO CHECKOUT CLONES ON THE FIRST STEP AND THE ROUND RUNS IN THE
// CLONE: the runner's whole road with no model, from the launch to the proof,
// where the check finds a file only the clone has. The job's log says it
// cloned, and the clone is the repository's checkout from then on.
func TestARunWithNoCheckoutClonesAndRunsInTheClone(t *testing.T) {
	githubOnDisk(t, "LEDGER_ONLY_IN_THE_CLONE")
	t.Setenv("GH_TOKEN", "tok-secret")
	st := ledgerFloor(t)
	id := ledgerJob(t, st, "acme/ledger", "test -f LEDGER_ONLY_IN_THE_CLONE")
	it := (runRig{st: st}).waitFor(t, id, factory.StateLanded)
	if len(it.Proof) != 1 || !it.Proof[0].OK {
		t.Fatalf("the check did not run in the clone: proof %+v", it.Proof)
	}
	want := factoryCloneFolder(st.Root(), "acme/ledger")
	if got := st.CheckoutDir("acme/ledger"); got != want {
		t.Fatalf("the clone was recorded as %q, want %q", got, want)
	}
	said := false
	for _, l := range it.Stream.Log {
		said = said || l.Text == "cloning acme/ledger…"
	}
	if !said {
		t.Fatalf("the job's log never said it cloned: %+v", it.Stream.Log)
	}
}

// A CLONE THAT FAILS FAILS THE ROUND WITH THE DOOR'S SENTENCE: the job stops
// with that failure, its step never runs anywhere else, and nothing is
// recorded as the repository's checkout.
func TestARunWhoseCloneFailsSaysWhy(t *testing.T) {
	githubOnDisk(t, "README")
	st := ledgerFloor(t)
	id := ledgerJob(t, st, "acme/gone", "true")
	deadline := time.Now().Add(15 * time.Second)
	for {
		it, err := st.Get(id)
		if err == nil && it.Failure != nil {
			if !strings.HasPrefix(it.Failure.Detail, "could not clone acme/gone · ") || len(it.Proof) != 0 {
				t.Fatalf("the round failed with %+v, proof %+v", it.Failure, it.Proof)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the job never failed: %+v", it)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if st.CheckoutDir("acme/gone") != "" {
		t.Fatal("a failed clone was recorded as the checkout")
	}
}

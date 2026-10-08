package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// runRig is a store, a git checkout named `api` used as the workspace, and an
// item on it whose one stage is a check that runs `true`: no model anywhere.
type runRig struct {
	st        *store.Store
	workspace string
}

func newRunRig(t *testing.T) runRig {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "factory"))
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(t.TempDir(), "api")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("git"); err == nil {
		if out, err := exec.Command("git", "-C", workspace, "init", "-q").CombinedOutput(); err != nil {
			t.Fatalf("git init: %v\n%s", err, out)
		}
	}
	return runRig{st: st, workspace: workspace}
}

func (g runRig) checkItem(t *testing.T) int {
	t.Helper()
	id, err := g.st.Add(context.Background(), factory.Item{
		Title: "prove it builds", Repo: "api", Tier: factory.TierOwner, Gate: factory.GateShip,
		Stages: []factory.Stage{{Name: "test", Kind: factory.StageCheck, Ask: "true", On: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (g runRig) waitFor(t *testing.T, id int, want factory.State) factory.Item {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		it, err := g.st.Get(id)
		if err == nil && it.State == want {
			return it
		}
		if time.Now().After(deadline) {
			t.Fatalf("item %d never became %s; it is %s (%q)", id, want, it.State, it.Question)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// stopAll stops whatever r still holds, so no round outlives the temp dirs.
func stopAll(t *testing.T, st *store.Store, r *factoryrun.Runner) {
	t.Cleanup(func() {
		items, _ := st.List()
		for _, it := range items {
			_ = r.Stop(it.ID)
		}
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
			busy := false
			for _, it := range items {
				busy = busy || r.Running(it.ID)
			}
			if !busy {
				return
			}
		}
	})
}

// TestFactoryRunnerCarriesEveryExecutorThisProcessHas pins the wiring: chat,
// check and post each have an executor, a gate has none (the loop answers it),
// the benches are the default, the money is the pool, and a floor that is not
// connected to GitHub has no source for a post to write through.
func TestFactoryRunnerCarriesEveryExecutorThisProcessHas(t *testing.T) {
	t.Setenv(factoryBenchesEnv, "")
	g := newRunRig(t)
	r := buildFactoryRunner(g.st, g.workspace, t.TempDir(), nil)
	opts := r.Opts()
	for _, kind := range []factory.StageKind{factory.StageChat, factory.StageCheck, factory.StagePost} {
		if opts.Exec[kind] == nil {
			t.Errorf("no executor for a %s stage", kind)
		}
	}
	if _, ok := opts.Exec[factory.StageGate]; ok {
		t.Error("a gate was given an executor; the loop answers a gate itself")
	}
	if opts.Benches != factoryBenchesDefault || factoryBenchesDefault != 4 {
		t.Errorf("benches = %d, want the default 4", opts.Benches)
	}
	if _, ok := opts.Pool.(*factoryrun.Money); !ok {
		t.Errorf("the pool is %T, not the money", opts.Pool)
	}
	if opts.Rail == nil || opts.SpentToday == nil || opts.RepoDir == nil || opts.Recipe == nil || opts.Source == nil {
		t.Fatal("a reading the loop needs was left nil")
	}
	if dir := opts.RepoDir("api"); dir != g.workspace {
		t.Errorf("the checkout of api is %q, want the workspace %q", dir, g.workspace)
	}
	if src := opts.Source("acme/api"); src != nil {
		t.Errorf("a floor with no token and nothing watched has a source: %T", src)
	}
	if len(opts.Recipe("api").Stages) == 0 {
		t.Error("a repository with no recipe file has no stages; it should run the default recipe")
	}
}

// TestAnOwnerLaunchRunsACheckItemToLanded is the whole road with no model: the
// owner's seam launches, the check runs `true` in the checkout, and the item
// lands with its claim shown.
func TestAnOwnerLaunchRunsACheckItemToLanded(t *testing.T) {
	g := newRunRig(t)
	r := buildFactoryRunner(g.st, g.workspace, t.TempDir(), nil)
	stopAll(t, g.st, r)
	seam := factory.LocalSeam(g.st, time.Now(), factory.WithRunner(r))
	for _, door := range []string{"launch", "stop", "pause", "answer", "steer", "signoff", "sendback", "reverify"} {
		if !seam.Has(door) {
			t.Errorf("the owner's floor has no %s", door)
		}
	}
	id := g.checkItem(t)
	if err := seam.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitFor(t, id, factory.StateLanded)
	if len(it.Proof) != 1 || !it.Proof[0].OK || it.Proof[0].Text != "true passes" {
		t.Fatalf("proof = %+v", it.Proof)
	}
	if !strings.HasPrefix(it.Proof[0].Evidence, "exit 0") {
		t.Fatalf("the claim's evidence is %q, not the exit code", it.Proof[0].Evidence)
	}
}

// TestANonOwnerLaunchReachesTheOwnerAndLands: a window that does not run the
// floor launches through the mailbox, the owner's drain carries it through
// the real runner, and the item lands. A second launch of the same item hears
// the runner's own refusal back.
func TestANonOwnerLaunchReachesTheOwnerAndLands(t *testing.T) {
	g := newRunRig(t)
	window := factory.LocalSeam(g.st, time.Now(), factory.WithMailbox(g.st.Mailbox()))
	owner := buildFactoryRunner(g.st, g.workspace, t.TempDir(), nil)
	stopAll(t, g.st, owner)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go drainFactoryMailbox(ctx, g.st.Mailbox(), owner, 5*time.Millisecond)

	id := g.checkItem(t)
	if err := window.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.waitFor(t, id, factory.StateLanded)
	if err := window.Launch(id); err == nil || !strings.Contains(err.Error(), "has landed · sign it off, or send it back") {
		t.Fatalf("a second launch from the window = %v, not the runner's own sentence", err)
	}
}

// TestARefusedLaunchFromAWindowSaysTheRunnersReason: the day rail's refusal
// comes back to the window that asked, word for word.
func TestARefusedLaunchFromAWindowSaysTheRunnersReason(t *testing.T) {
	g := newRunRig(t)
	r := factoryrun.New(factoryrun.Options{Store: g.st, Rail: func() float64 { return 1 }, SpentToday: func() float64 { return 2 }})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go drainFactoryMailbox(ctx, g.st.Mailbox(), r, 5*time.Millisecond)
	window := factory.LocalSeam(g.st, time.Now(), factory.WithMailbox(g.st.Mailbox()))
	id := g.checkItem(t)
	err := window.Launch(id)
	if err == nil || err.Error() != "the day rail is $1 and today's spend has reached it" {
		t.Fatalf("the window heard %v", err)
	}
	if it, _ := g.st.Get(id); it.State != factory.StateNew {
		t.Fatalf("a refused launch left the item %s", it.State)
	}
}

// TestTheFloorHasOneRunner: the run lock is held by one process at a time.
func TestTheFloorHasOneRunner(t *testing.T) {
	g := newRunRig(t)
	first, ok := tryFactoryRunLock(g.st)
	if !ok {
		t.Fatal("nobody holds the floor, and the lock was refused")
	}
	other, err := store.Open(g.st.Root())
	if err != nil {
		t.Fatal(err)
	}
	if second, ok := tryFactoryRunLock(other); ok {
		second.Close()
		first.Close()
		t.Skip("this platform's lock does not exclude within one process")
	}
	first.Close()
	again, ok := tryFactoryRunLock(other)
	if !ok {
		t.Fatal("the lock was not given back when its holder let go")
	}
	again.Close()
}

// TestTheBenchesPinIsRegisteredAndRead: the pin is on the plumbing list, a
// whole number above nothing is obeyed, and anything else is the default.
func TestTheBenchesPinIsRegisteredAndRead(t *testing.T) {
	if !slices.Contains(config.OperatorEnvPins, factoryBenchesEnv) {
		t.Fatalf("%s is not registered in internal/config's settings.go", factoryBenchesEnv)
	}
	for value, want := range map[string]int{"": 4, "2": 2, "0": 4, "-3": 4, "many": 4, " 7 ": 7} {
		t.Setenv(factoryBenchesEnv, value)
		if got := factoryBenches(); got != want {
			t.Errorf("%s=%q gives %d benches, want %d", factoryBenchesEnv, value, got, want)
		}
	}
}

// TestTheSeamOptionFollowsWhoRunsTheFloor: with no runner in this process the
// seam option is the mailbox, which still draws all eight runner doors; with
// one, it is the runner's own doors.
func TestTheSeamOptionFollowsWhoRunsTheFloor(t *testing.T) {
	g := newRunRig(t)
	factoryRunner.mu.Lock()
	held := factoryRunner.r
	factoryRunner.r = nil
	factoryRunner.mu.Unlock()
	t.Cleanup(func() {
		factoryRunner.mu.Lock()
		factoryRunner.r = held
		factoryRunner.mu.Unlock()
	})
	doors := []string{"launch", "stop", "pause", "answer", "steer", "signoff", "sendback", "reverify"}
	window := factory.LocalSeam(g.st, time.Now(), factoryRunnerDoors(g.st))
	for _, door := range doors {
		if !window.Has(door) {
			t.Errorf("a window that does not run the floor has no %s", door)
		}
	}
	if factoryRunnerDoors(nil) != nil {
		t.Error("no store still bound a mailbox")
	}
	factoryRunner.mu.Lock()
	factoryRunner.r = buildFactoryRunner(g.st, g.workspace, t.TempDir(), nil)
	factoryRunner.mu.Unlock()
	owner := factory.LocalSeam(g.st, time.Now(), factoryRunnerDoors(g.st))
	for _, door := range doors {
		if !owner.Has(door) {
			t.Errorf("the window that runs the floor has no %s", door)
		}
	}
}

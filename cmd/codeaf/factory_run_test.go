package main

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	factoryworker "github.com/Agent-Field/codeaf/internal/factory/worker"
	"github.com/Agent-Field/codeaf/internal/session"
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
		// ONE COMMIT, because an item's rounds run in a worktree made from
		// the checkout, and a repository with no commit has nothing to make
		// one from.
		for _, args := range [][]string{
			{"init", "-q"},
			{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "-q", "--allow-empty", "-m", "first"},
		} {
			if out, err := exec.Command("git", append([]string{"-C", workspace}, args...)...).CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
	}
	return runRig{st: st, workspace: workspace}
}

func (g runRig) checkItem(t *testing.T) int {
	t.Helper()
	id, err := g.st.Add(context.Background(), factory.Item{
		Title: "prove it builds", Repo: "api", Tier: factory.TierOwner,
		Stages: []factory.Stage{{Name: "test", Kind: factory.StageCheck, Ask: "true", On: true}, {Name: factory.ApproveName, Kind: factory.StageGate, On: true}},
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
	r := buildFactoryRunner(g.st, g.st, g.workspace, t.TempDir(), nil, nil)
	opts := r.Opts()
	for _, kind := range []factory.StageKind{factory.StageChat, factory.StageCheck, factory.StagePost} {
		if opts.Exec[kind] == nil {
			t.Errorf("no executor for a %s stage", kind)
		}
	}
	if _, ok := opts.Exec[factory.StageGate]; ok {
		t.Error("a gate was given an executor; the loop answers a gate itself")
	}
	// The mirror runner's benches are the shared ceiling, not the env pin or the
	// default of 2: the lease cap governs, so a raise after attach finds room.
	if factoryBenchesDefault != 15 || opts.Benches != api.MaxRunnerSlots {
		t.Errorf("benches = %d, want the shared ceiling %d", opts.Benches, api.MaxRunnerSlots)
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
	if opts.Workdir == nil {
		t.Fatal("the runner has no Workdir: items would work in the person's checkout")
	}
	if _, err := exec.LookPath("git"); err == nil {
		id := g.checkItem(t)
		it, _ := g.st.Get(id)
		dir, err := opts.Workdir(it)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(g.st.Root(), "work", "api-"+strconv.Itoa(id)); dir != want {
			t.Errorf("the item works in %q, want its own worktree %q", dir, want)
		}
		it, _ = g.st.Get(id)
		if it.Stream == nil || len(it.Stream.Log) == 0 || it.Stream.Log[len(it.Stream.Log)-1].Text != "branch: factory/"+strconv.Itoa(id)+"-prove-it-builds" {
			t.Errorf("the item's log does not name its branch: %+v", it.Stream)
		}
	}
	if src := opts.Source("acme/api"); src != nil {
		t.Errorf("a floor with no token and nothing watched has a source: %T", src)
	}
	if len(opts.Recipe(factory.Item{Repo: "api"}).Stages) == 0 {
		t.Error("a repository with no recipe file has no stages; it should run the default recipe")
	}
}

// TestAnOwnerLaunchRunsACheckItemToLanded is the whole road with no model: the
// owner's seam launches, the check runs `true` in the checkout, and the item
// lands with its claim shown.
func TestAnOwnerLaunchRunsACheckItemToLanded(t *testing.T) {
	g := newRunRig(t)
	r := buildFactoryRunner(g.st, g.st, g.workspace, t.TempDir(), nil, nil)
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

// A LAUNCH OPENS NO MANAGER TURN BEFORE THE FIRST STAGE (the owner's run of
// 2026-10-19, where a turn asked to shape the run stopped on a card nobody
// answered and the plan never ran). The runner is wired with this process's
// manager turns, whose every road fails the test if it is taken, and the item
// still lands on its own stages; the turns stay wired for the inbox.
func TestALaunchGivesTheManagerNoTurnBeforeTheFirstStage(t *testing.T) {
	g := newRunRig(t)
	var mu sync.Mutex
	turns := 0
	manager := &shapeTurns{
		dirs: func(string) string { return g.workspace },
		live: func(string, session.RunDoor) (managerTurn, bool) {
			mu.Lock()
			turns++
			mu.Unlock()
			return nil, false
		},
		open: func(context.Context, factory.Item, session.RunDoor, bool) (managerTurn, error) {
			mu.Lock()
			turns++
			mu.Unlock()
			return nil, errors.New("a launch opened the manager")
		},
	}
	r := buildFactoryRunner(g.st, g.st, g.workspace, t.TempDir(), nil, manager)
	stopAll(t, g.st, r)
	if opts := r.Opts(); opts.Inbox == nil {
		t.Fatal("the manager turns are not wired for the inbox")
	}
	seam := factory.LocalSeam(g.st, time.Now(), factory.WithRunner(r))
	id := g.checkItem(t)
	if err := seam.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitFor(t, id, factory.StateLanded)
	mu.Lock()
	defer mu.Unlock()
	if turns != 0 {
		t.Fatalf("the launch asked the manager for %d turns, want none", turns)
	}
	if len(it.Proof) != 1 || !it.Proof[0].OK {
		t.Fatalf("the first stage did not run: proof %+v", it.Proof)
	}
}

// workOn serves g's floor on loopback with the lease's doors, and runs a
// worker loop against it whose runner is this process's own (no model: the
// rig's items are checks). It answers the floor as a window reaches it.
func (g runRig) workOn(t *testing.T, r func(mirror *store.Store) *factoryrun.Runner) (factory.Seam, *factoryrun.Runner) {
	t.Helper()
	seam := factory.LocalSeam(g.st, time.Now(), factoryRunnerDoors(g.st))
	handler := serve.New(seam, serve.Config{Address: "http://test", Store: g.st, PullWait: 50 * time.Millisecond})
	server := httptest.NewServer(handler)
	mirror, err := store.Open(filepath.Join(t.TempDir(), "mirror"))
	if err != nil {
		t.Fatal(err)
	}
	runner := r(mirror)
	var credential string
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		factoryworker.Run(ctx, factoryworker.Options{
			Dial:   func(int) *client.WorkerClient { return client.Open(server.URL, "").Worker(credential) },
			Keep:   func(_ int, w *client.WorkerClient) { credential = w.Credential() },
			Attach: api.WorkerAttach{Name: "test", Handle: "me", TakesUnassigned: map[string]bool{lease.EveryRepo: true}},
			Runner: runner, Mirror: mirror, Slots: 1, ReportEvery: 10 * time.Millisecond, BeatEvery: 10 * time.Millisecond,
		})
	}()
	t.Cleanup(func() { cancel(); <-done; server.Close(); handler.Close() })
	return client.Open(server.URL, "").Seam(), runner
}

// TestAWindowsLaunchRunsOnTheWorkerAndLands: a window's launch reaches the
// factory, which queues the job; the worker pulls it, its runner runs the
// check in this machine's checkout, the reports land on the floor, and the
// job waits at its approve step. A second launch hears the refusal back.
func TestAWindowsLaunchRunsOnTheWorkerAndLands(t *testing.T) {
	g := newRunRig(t)
	window, _ := g.workOn(t, func(mirror *store.Store) *factoryrun.Runner {
		return buildFactoryRunner(mirror, g.st, g.workspace, t.TempDir(), nil, nil)
	})
	id := g.checkItem(t)
	if err := window.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitFor(t, id, factory.StateLanded)
	if len(it.Proof) != 1 || !it.Proof[0].OK || it.Proof[0].Text != "true passes" {
		t.Fatalf("proof = %+v", it.Proof)
	}
	if err := window.Launch(id); err == nil || !strings.Contains(err.Error(), "has landed · approve it, or request changes") {
		t.Fatalf("a second launch from the window = %v, not the runner's own sentence", err)
	}
}

// TestARunTheWorkerRefusesGoesBackWithItsReason: the day rail is the
// worker's; a job it cannot start goes back to backlog with the reason on its
// log, where the window that pressed run reads it.
func TestARunTheWorkerRefusesGoesBackWithItsReason(t *testing.T) {
	g := newRunRig(t)
	window, _ := g.workOn(t, func(mirror *store.Store) *factoryrun.Runner {
		return factoryrun.New(factoryrun.Options{Store: mirror, Rail: func() float64 { return 1 }, SpentToday: func() float64 { return 2 }})
	})
	id := g.checkItem(t)
	if err := window.Launch(id); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		it, _ := g.st.Get(id)
		if it.State == factory.StateNew && it.Stream != nil && len(it.Stream.Log) > 0 &&
			strings.Contains(it.Stream.Log[len(it.Stream.Log)-1].Text, "the day rail is $1 and today's spend has reached it") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the refused job is %s with log %+v", it.State, it.Stream)
		}
	}
}

// TestTheBenchesPinIsRegisteredAndRead: the pin is on the plumbing list, a
// whole number above nothing is obeyed, and anything else is the default.
func TestTheBenchesPinIsRegisteredAndRead(t *testing.T) {
	if !slices.Contains(config.OperatorEnvPins, factoryBenchesEnv) {
		t.Fatalf("%s is not registered in internal/config's settings.go", factoryBenchesEnv)
	}
	for value, want := range map[string]int{"": factoryBenchesDefault, "2": 2, "0": factoryBenchesDefault, "-3": factoryBenchesDefault, "many": factoryBenchesDefault, " 7 ": 7, strconv.Itoa(api.MaxRunnerSlots + 1): api.MaxRunnerSlots} {
		t.Setenv(factoryBenchesEnv, value)
		if got := factoryBenches(); got != want {
			t.Errorf("%s=%q gives %d benches, want %d", factoryBenchesEnv, value, got, want)
		}
	}
}

// TestEveryWindowHangsTheLeaseDoors: no process owns the floor, so every
// window's seam has all eight runner doors over its store, and no store binds
// none.
func TestEveryWindowHangsTheLeaseDoors(t *testing.T) {
	g := newRunRig(t)
	window := factory.LocalSeam(g.st, time.Now(), factoryRunnerDoors(g.st))
	for _, door := range []string{"launch", "stop", "pause", "hold", "answer", "steer", "signoff", "sendback", "reverify"} {
		if !window.Has(door) {
			t.Errorf("a window has no %s", door)
		}
	}
	if factoryRunnerDoors(nil) != nil {
		t.Error("no store still bound runner doors")
	}
}

// A DROP NEVER DELETES A FOLDER A RUNNER ON THIS MACHINE STILL WORKS IN. A
// live worker's mirror of the job holds the delete back until it is gone; a
// mirror left by a process that died does not.
func TestFactoryDropWaitsForThisMachinesRunner(t *testing.T) {
	root := t.TempDir()
	mirror := func(pid string) string {
		dir := filepath.Join(root, "workers", pid, "0123abcd")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "7.json")
		if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if factoryMirrorHolds(root, 7) || !factoryRunnerLetGo(root, 7, 0) {
		t.Fatal("a job no worker holds is held")
	}
	mirror("2147483646")
	if factoryMirrorHolds(root, 7) {
		t.Fatal("a dead worker's mirror holds the job")
	}
	live := mirror(strconv.Itoa(os.Getpid()))
	if !factoryMirrorHolds(root, 7) || factoryRunnerLetGo(root, 7, 0) {
		t.Fatal("a live worker's mirror does not hold the job")
	}
	if factoryMirrorHolds(root, 8) {
		t.Fatal("another job is held by this one's mirror")
	}
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	if !factoryRunnerLetGo(root, 7, 0) {
		t.Fatal("the job is still held after its worker let go")
	}
}

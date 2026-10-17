package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/processgroup"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE RUNNER, WIRED: WHERE THE FLOOR'S JOBS RUN ─────────────────────────
//
// internal/factory/run's Runner moves a job through its steps. NO PROCESS
// OWNS THE FLOOR. A job runs wherever a worker pulls it: every codeaf with a
// factory (the one it serves itself, the one another codeaf on this machine
// serves, or the one attach.json names) runs a worker loop against that
// factory ([startFactoryWorker], internal/factory/worker), unless --no-worker.
// One process holds worker.lock for this home; the others share its registration
// and wait to take over with its saved credentials when that process stops.
// The loop attaches once, long-polls for a job, runs it with this machine's
// Runner over a mirror of the job, reports what the runner wrote, heartbeats,
// and says done.
//
// EVERY WINDOW'S VERBS GO TO THE FACTORY, never to a runner: the factory's
// runner doors are the lease's ([factoryRunnerDoors], internal/factory/lease's
// Doors), which change the job's state and leave a signal on its lease for
// the worker that holds it. They only write the store and leases.json, so
// every process on this machine can hang them; there is no per-job run lock or
// mailbox.

// factoryBenchesEnv pins how many items run at once on this machine's floor.
const factoryBenchesEnv = "CODEAF_FACTORY_BENCHES"

// factoryNoWorkerEnv is --no-worker: this machine runs no job.
const factoryNoWorkerEnv = "CODEAF_FACTORY_NO_WORKER"

// factoryBenchesDefault is how many items run at once when nothing pins it.
// The manual quotes it (internal/manual/chat/factory.md).
const factoryBenchesDefault = api.DefaultRunnerSlots

// factoryStageWall is the most one stage conversation runs for, which is also
// what makes it an unattended conversation that keeps going until its work is
// done (internal/session's Budget): nobody is at its keyboard.
const factoryStageWall = 2 * time.Hour

// factoryProcess is what this process built for the floor once: the manager's
// shaping door every window's page uses, and the worker loop's stop.
var factoryProcess struct {
	once         sync.Once
	mu           sync.Mutex
	shape        func(ctx context.Context, id int) (string, error)
	mirror       string
	workerShape  func(ctx context.Context, id int) (string, error)
	workerShapes map[string]func(context.Context, int) (string, error)
	stop         context.CancelFunc
	parent       session.Config
}

// factoryShapeHere is this process's Shape door, nil before
// [startFactoryWorker] built it (a window with no conversation config).
func factoryShapeHere() func(ctx context.Context, id int) (string, error) {
	factoryProcess.mu.Lock()
	defer factoryProcess.mu.Unlock()
	return factoryProcess.shape
}

// factoryRunnerDoors is the seam option every window's floor hangs: the
// lease's doors over st, nil for no floor.
func factoryRunnerDoors(st *store.Store) factory.LocalOption {
	if st == nil {
		return nil
	}
	desk, err := lease.Open(st, lease.Options{})
	if err != nil {
		return nil
	}
	return factory.WithRunner(lease.NewDoors(desk))
}

// startFactoryWorker builds this process's shaping door and, unless
// --no-worker, waits for the home's runner lock before starting worker slots.
// ONE RUNNER PER HOME, STOPPED WITH THE PROCESS ([stopFactoryHere]). parent is the
// conversation config the launch built, which every stage conversation is
// opened from.
func startFactoryWorker(st *store.Store, workspace, profileDir string, parent session.Config) {
	if st == nil {
		return
	}
	// THE WORKER OUTLIVES A WINDOW. A later launch may pin its model, so new
	// manager and stage conversations must not keep the first window's crew.
	setFactoryProcess(func() { factoryProcess.parent = parent })
	factoryProcess.once.Do(func() {
		turns := factoryShapeTurnsFrom(st, workspace, profileDir, factoryWorkerParent)
		setFactoryProcess(func() { factoryProcess.shape = factoryShapeDoor(st, workspace, profileDir, turns) })
		if strings.TrimSpace(env.Get(factoryNoWorkerEnv)) == "1" {
			return
		}
		ctx, cancel := context.WithCancel(context.Background())
		setFactoryProcess(func() { factoryProcess.stop = cancel })
		guard.Go("factory/worker", func() {
			credentials := factorySessions(newFactorySessions())
			if a, ok := readFactoryAttachment(); ok && a.Token != "" {
				// The session `codeaf runner join` kept for this machine.
				credentials.SetFactorySession(a.URL, a.Token)
			}
			host := &appFactoryHost{ctx: ctx, floor: st, workspace: workspace, profile: profileDir, credentials: credentials, parent: factoryWorkerParent, defaultTarget: func() (factoryTarget, error) { return resolveFactory(st, factorySeam(st, workspace, profileDir)) }}
			host.workers()
		})
	})
}

// factoryWorkerParent reads the latest launch under the worker's lock, so a
// conversation opened after a one-model launch inherits that launch's pin.
func factoryWorkerParent() session.Config {
	factoryProcess.mu.Lock()
	defer factoryProcess.mu.Unlock()
	return factoryProcess.parent
}

// setFactoryModelPin publishes a launch's pin when this process has no local
// conversation config, keeping triage on the same source as manager and stage calls.
func setFactoryModelPin(model string, oneModel bool) {
	setFactoryProcess(func() {
		factoryProcess.parent.Model = model
		factoryProcess.parent.OneModel = oneModel
	})
}

// clearDeadFactoryMirrors removes the mirrors of worker processes that are
// gone: a process killed mid-job never removed its own, and its jobs went
// back to the queue when their leases expired.
func clearDeadFactoryMirrors(dir string) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if pid, err := strconv.Atoi(e.Name()); err == nil && pid > 0 && !processgroup.ProcessAlive(pid) {
			_ = os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
}

// stopFactoryHere ends this process's worker loop and takes down the factory
// it serves, serve.json with it. A job the loop held goes back to the queue
// when its lease expires, for the next worker to go on with.
func stopFactoryHere() {
	var stop context.CancelFunc
	setFactoryProcess(func() { stop = factoryProcess.stop })
	if stop != nil {
		stop()
	}
	closeFactoryServingHere()
}

// setFactoryProcess changes or reads [factoryProcess] under its lock.
func setFactoryProcess(change func()) {
	factoryProcess.mu.Lock()
	defer factoryProcess.mu.Unlock()
	change()
}

// buildFactoryRunner is the runner with every executor this process can
// provide, over items (the worker's mirror of the jobs it leased) and floor
// (this machine's factory folder: its checkouts, worktrees and GitHub token).
// It starts nothing.
//
// SPEND IS WRITTEN ONCE. A stage conversation is an ordinary conversation and
// its calls are already rows on the spend ledger, under that conversation; a
// `factory run` row beside them would count every dollar twice. So the money
// is handed no ledger: what a round spent goes on the item (its spend, its
// cap, today's rail) and nowhere else.
func buildFactoryRunner(items, floor *store.Store, workspace, profileDir string, maker factoryrun.ConversationMaker, shape *shapeTurns) *factoryrun.Runner {
	dirs := factoryRepoDirs(floor, workspace)
	recipe := func(repo string) factory.Recipe {
		return factoryItemRecipe(dirs, factory.Item{Repo: repo})
	}
	source := factorySource(floor, profileDir)
	money := factoryrun.NewMoney(items, nil, time.Now)
	// EVERY ITEM WORKS IN ITS OWN WORKTREE, never in the person's checkout:
	// `<factory folder>/work/<repo>-<number>` on `factory/<number>-<slug>`,
	// made from the checkout the first round that needs it. Its `branch:`
	// line goes on the item's log through the store, and a `pr` pushes that
	// branch before it opens the pull request.
	workdirs := factoryWorkdirs(items, floor)
	// A PUSH GOES AS THE PERSON: git is handed the profile's GitHub token,
	// the one the source writes with, never in its arguments.
	git := factoryrun.ExecGit{}
	pushGit := factoryrun.ExecGit{Token: func() string {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		t, _ := factorygithub.TokenAt(ctx, profileDir)
		return t
	}}
	// EVERY POST CITES ITS CONSENT: the post executor reads the job's
	// approval from this machine's factory record and writes its trailer.
	history, _ := record.Open(floor, record.Options{})
	opts := factoryrun.Options{
		Store: items,
		Exec: map[factory.StageKind]factoryrun.Executor{
			factory.StageChat:  factoryChatStage(items, profileDir, factoryrun.NewChatExecutor(maker)),
			factory.StageCheck: factoryrun.NewCheckExecutor(factoryrun.CheckOptions{}),
			factory.StagePost: factoryrun.NewPostExecutorWith(factoryrun.PostOptions{
				Record: history,
				Source: source,
				Policy: func(repo string) []string { return recipe(repo).Policy },
				Push:   factoryrun.GitPush(pushGit),
			}),
			// A GATE IS A PERSON, and the loop answers it before it would look
			// for an executor, so there is none to give.
		},
		Benches:    factoryBenches(),
		Rail:       money.Rail,
		SpentToday: money.SpentToday,
		RepoDir:    dirs,
		Workdir: func(it factory.Item) (string, error) {
			return workdirs.For(it, dirs(it.Repo))
		},
		Recipe:    recipe,
		Source:    source,
		Publisher: factoryPublisher(profileDir),
		// APPROVING PUBLISHES (internal/factory/run's publish.go): the job's
		// worktree is committed with git, its branch pushed as the person,
		// and the pull request opened through source.
		Push: factoryrun.GitPush(pushGit),
		Git:  git,
		// EVENTS ARE DROPPED. The loop writes every move to the store before
		// it would send one, and the worker reports what it wrote, so nothing
		// is waiting to hear them.
		Events: nil,
		Pool:   money,
		// THE ITEM'S OWN CONVERSATION IS THE MANAGER OF ITS RUN: made at
		// launch the way `T` makes it, the lead of the item's team, told every
		// stage, and listened to for the brief, the steer and an answer.
		Manager: managerMaker(floor, workspace, profileDir),
		Talk:    sessionTalk{},
	}
	// AND THE MANAGER SHAPES THE RUN: one turn of its conversation at the
	// launch of an item it never shaped, before the first stage
	// (factory_shape.go). Nil shapes nothing; the recipe stands. What the
	// person says during the run is the manager's own turn, never a second one.
	if shape != nil {
		opts.Shape = shape.Shape
		// `shape steps` while this worker holds the job is the same turn the
		// floor's Shape door runs, bounded to the steps not yet started.
		opts.ShapeNow = func(ctx context.Context, it factory.Item, inRun bool) (factory.RunEdit, string, error) {
			return shape.run(ctx, it, shapeAsk, inRun)
		}
		// AND THE MANAGER IS THE INBOX: every question a step asks is given to
		// it first, one turn of its conversation (factory_inbox.go).
		opts.Inbox = shape.Inbox
	}
	return factoryrun.New(opts)
}

// factoryWorkdirs is the jobs' worktrees under this machine's factory folder's
// `work`; the `branch:` line goes on the job's log in items.
func factoryWorkdirs(items, floor *store.Store) *factoryrun.Workdirs {
	w := factoryrun.NewWorkdirs(filepath.Join(floor.Root(), "work"), factoryrun.ExecGit{})
	w.Log = factoryrun.StoreLog(items, time.Now)
	return w
}

// factoryBenches is how many items run at once: [factoryBenchesEnv] when it
// is a whole number above nothing, else [factoryBenchesDefault].
func factoryBenches() int {
	if n, err := strconv.Atoi(strings.TrimSpace(env.Get(factoryBenchesEnv))); err == nil && n > 0 {
		return n
	}
	return factoryBenchesDefault
}

// factorySource answers the GitHub source a post stage writes through, built
// at the moment of asking over the profile's token and the watched
// repositories (the refresh doors' reason: a token connected a minute ago is
// the one to use). NIL IS NOT CONNECTED, and a post stage then says so.
func factorySource(st *store.Store, profileDir string) func(repo string) factory.Source {
	return func(string) factory.Source {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		src := factoryGitHub(ctx, st, func(ctx context.Context) string {
			t, _ := factorygithub.TokenAt(ctx, profileDir)
			return t
		})
		// A typed nil inside the interface would read as a source.
		if src == nil {
			return nil
		}
		return src
	}
}

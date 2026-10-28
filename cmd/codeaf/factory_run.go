package main

import (
	"context"
	"fmt"
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
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	factoryworker "github.com/Agent-Field/codeaf/internal/factory/worker"
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

// factoryStageWall is the most one stage conversation runs for: the runner's
// own bound on a round (internal/factory/run's StageWall), handed to the
// conversation as its budget so the tasks it hands out are bounded by it too.
const factoryStageWall = factoryrun.StageWall

// factoryProcess is what this process built for the floor once: the launch's
// conversation config the worker opens from, and the worker loop's stop.
var factoryProcess struct {
	once   sync.Once
	mu     sync.Mutex
	stop   context.CancelFunc
	parent session.Config
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
	return factory.WithRunner(lease.NewDoors(desk).WithStartRead(func(ctx context.Context, repo string) (pipeline.File, error) {
		return factoryPipelineRead(ctx, st, repo, st.CheckoutDir(repo))
	}))
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
		// IN THE APP PROCESS NO LOOP STARTS HERE, AND NO DEFAULT TARGET MAY SERVE.
		// This worker's target is resolveFactory, which serves a factory of its
		// own when none is live: in the app that wrote a token-less serve.json
		// with this very pid before the app's own cold resume ran, and the
		// resume then refused its own process's service. The explicit
		// factory.local_run starts the app's loop (appFactoryHost.workers).
		if strings.TrimSpace(env.Get(factoryNoWorkerEnv)) == "1" || !factoryBackgroundHere() {
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
func buildFactoryRunner(items, floor *store.Store, workspace, profileDir string, maker factoryrun.ConversationMaker, turns *shapeTurns) *factoryrun.Runner {
	dirs := factoryRepoDirs(floor, workspace)
	// EVERY BOUND ON A JOB IS THE FILE IT RUNS: the factory's copy it carries
	// first, else main's file here ([factoryItemRecipe]). No store copy is
	// read: a job the factory started under its copy carries it.
	recipe := func(it factory.Item) factory.Recipe {
		return factoryItemRecipe(nil, dirs, it)
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
				Policy: func(it factory.Item) []string { return recipe(it).Policy },
				Push:   factoryrun.GitPush(pushGit),
			}),
			// A GATE IS A PERSON, and the loop answers it before it would look
			// for an executor, so there is none to give.
		},
		// THE LEASE CAP IS THE GOVERNOR, not this number. The mirror runner only
		// runs jobs a lease handed it, and the factory refuses leases beyond the
		// owner's saved slots, so a raise after attach must find benches free.
		// A fixed factoryBenches() here was the old two-job bottleneck.
		Benches:    api.MaxRunnerSlots,
		Rail:       money.Rail,
		SpentToday: money.SpentToday,
		RepoDir:    dirs,
		// AND A REPOSITORY WITH NO CHECKOUT HERE IS CLONED FIRST, into this
		// machine's factory folder, over the token the push uses
		// (factory_clone.go).
		Workdir:   factoryRunWorkdir(floor, dirs, workdirs, factoryTokenOf(profileDir)),
		Recipe:    recipe,
		Source:    source,
		Publisher: factoryPublisher(profileDir),
		// APPROVING PUBLISHES (internal/factory/run's publish.go): the job's
		// worktree is committed with git, its branch pushed as the person,
		// and the pull request opened through source.
		Push: factoryrun.GitPush(pushGit),
		Git:  pushGit,
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
	// THE LAUNCH GIVES THE MANAGER NO TURN: the run starts at its first stage
	// with the stages as they stand, and what the person says during the run
	// is the manager's own turn, never a second one. The manager's
	// conversation is the INBOX: every question a step asks is given to it
	// first, one turn of its conversation (factory_inbox.go); nil turns give
	// it none.
	if turns != nil {
		opts.Inbox = turns.Inbox
	}
	return factoryrun.New(opts)
}

// factoryWorkdirs is the jobs' worktrees under this machine's factory folder's
// `work`; the `branch:` line goes on the job's log in items.
func factoryWorkdirs(items, floor *store.Store) *factoryrun.Workdirs {
	w := factoryrun.NewWorkdirs(filepath.Join(floor.Root(), "work"), factoryrun.ExecGit{})
	w.Log = factoryrun.StoreLog(items, time.Now)
	w.Prefix = func(it factory.Item) string { return factory.NormalizeHandle(it.Person()) }
	return w
}

// factoryJobFolders is a drop's reach on this machine (serve's JobFolders):
// the job's worktree under this factory folder's `work`, exactly where the
// runner made it, and its local branch, in the checkout the factory knows for
// the job's repository. A job that ran on another machine has neither here,
// so a drop finds nothing to read or delete for it.
type factoryJobFolders struct {
	floor *store.Store
	work  *factoryrun.Workdirs
}

func newFactoryJobFolders(floor *store.Store) factoryJobFolders {
	return factoryJobFolders{floor: floor, work: factoryWorkdirs(floor, floor)}
}

func (f factoryJobFolders) Work(it factory.Item) (factoryrun.Work, error) {
	return f.work.Work(it, f.floor.CheckoutDir(it.Repo))
}

// Discard deletes the job's local work once nothing on this machine runs it.
// THE FOLDER IS NEVER DELETED UNDER A RUNNING STEP: a drop takes the job's
// lease away, and the worker holding it learns that at its next heartbeat and
// stops its runner, so the delete waits for that worker to let go. A worker
// still holding it when the wait ends keeps the folder, and the drop says so.
func (f factoryJobFolders) Discard(it factory.Item) error {
	if !factoryRunnerLetGo(f.floor.Root(), it.ID, factoryLetGoWait) {
		return fmt.Errorf("%s was still running on this machine", it.Ref())
	}
	return f.work.Discard(it, f.floor.CheckoutDir(it.Repo))
}

// factoryLetGoWait bounds that wait: the worker hears the stop at its next
// heartbeat and then gives its runner [factoryworker.StopWait] to stop.
const factoryLetGoWait = lease.HeartbeatEvery + factoryworker.StopWait + time.Second

// factoryRunnerLetGo waits, at most wait, until no worker on this machine
// holds job id, and answers whether none does.
func factoryRunnerLetGo(root string, id int, wait time.Duration) bool {
	deadline := time.Now().Add(wait)
	for factoryMirrorHolds(root, id) {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
	return true
}

// factoryMirrorHolds says whether a live worker process keeps job id in its
// mirror, `<floor>/workers/<pid>/<factory>/<id>.json`: a worker writes it when
// it takes the job and removes it only after its runner has stopped. Every
// factory's mirror is read, because a worker of this machine's factory dials
// whatever address it was given; another factory's job with the same number
// only makes the wait longer, never the delete earlier.
func factoryMirrorHolds(root string, id int) bool {
	held, _ := filepath.Glob(filepath.Join(root, "workers", "*", "*", strconv.Itoa(id)+".json"))
	for _, path := range held {
		pid, err := strconv.Atoi(filepath.Base(filepath.Dir(filepath.Dir(path))))
		if err == nil && pid > 0 && processgroup.ProcessAlive(pid) {
			return true
		}
	}
	return false
}

// factoryBenches is how many items this worker asks to run at once before the
// owner's saved number is known: [factoryBenchesEnv] when it is a whole number
// above nothing, else [factoryBenchesDefault], never above api.MaxRunnerSlots.
// Once the factory has a saved value, that wins and this is only the first ask.
func factoryBenches() int {
	if n, err := strconv.Atoi(strings.TrimSpace(env.Get(factoryBenchesEnv))); err == nil && n > 0 {
		return min(n, api.MaxRunnerSlots)
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

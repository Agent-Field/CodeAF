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
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE RUNNER, WIRED: WHICH PROCESS RUNS THE FLOOR'S ITEMS ────────────────
//
// internal/factory/run's Runner moves an item through its stages. This file
// builds the ONE runner a machine has and decides which process holds it.
//
// THE OWNER IS THE PROCESS THAT HOLDS THE FLOOR'S RUN LOCK (`run.lock` in the
// factory folder), taken where the GitHub poll starts: the session host on the
// ordinary launch, the window itself on a launch with no host. It is held for
// the life of the process, so two windows can never run one item twice. A
// process that finds the lock taken looks again every [factoryWatchEvery] and
// becomes the owner when the old one goes.
//
// EVERY OTHER WINDOW ONLY QUEUES. Its Launch writes the item `queued`
// ([factory.QueueLaunch]) and the owner's scan, every [factoryQueueEvery],
// launches each queued item it is not already running. Its other runner doors
// (stop, pause, answer, steer, sign-off, send back, check again) are absent,
// because only the owner holds the item's round.

// factoryBenchesEnv pins how many items run at once on this machine's floor.
const factoryBenchesEnv = "CODEAF_FACTORY_BENCHES"

// factoryBenchesDefault is how many items run at once when nothing pins it.
// The manual quotes it (internal/manual/chat/factory.md).
const factoryBenchesDefault = 4

// factoryQueueEvery is how often the owner looks for items another window
// queued. A variable so a test can shorten it.
var factoryQueueEvery = 3 * time.Second

// factoryStageWall is the most one stage conversation runs for, which is also
// what makes it an unattended conversation that keeps going until its work is
// done (internal/session's Budget): nobody is at its keyboard.
const factoryStageWall = 2 * time.Hour

// factoryRunner is this process's runner, nil until it owns the floor.
var factoryRunner struct {
	once sync.Once
	mu   sync.Mutex
	r    *factoryrun.Runner
	// lock is the open run.lock, kept referenced for the life of the
	// process: a file nobody holds is closed by the collector, and its lock
	// with it.
	lock *os.File
}

// factoryRunnerHere is this process's runner, or nil when another process
// runs the floor.
func factoryRunnerHere() *factoryrun.Runner {
	factoryRunner.mu.Lock()
	defer factoryRunner.mu.Unlock()
	return factoryRunner.r
}

// setFactoryRunner makes r this process's runner and keeps its lock open.
func setFactoryRunner(r *factoryrun.Runner, lock *os.File) {
	factoryRunner.mu.Lock()
	defer factoryRunner.mu.Unlock()
	factoryRunner.r, factoryRunner.lock = r, lock
}

// startFactoryRunner builds this process's runner over st when it can take
// the floor's run lock, and starts the scan that picks up what other windows
// queued. It answers the runner, or nil when another process owns the floor
// for now (a small loop keeps trying). ONCE PER PROCESS, STOPPED WITH THE
// PROCESS. parent is the conversation config the launch built, which every
// stage conversation is opened from.
func startFactoryRunner(st *store.Store, workspace, profileDir string, parent session.Config) *factoryrun.Runner {
	if st == nil {
		return nil
	}
	factoryRunner.once.Do(func() {
		take := func() bool {
			lock, ok := tryFactoryRunLock(st)
			if !ok {
				return false
			}
			r := buildFactoryRunner(st, workspace, profileDir, factoryStageMaker(st, workspace, profileDir, parent))
			setFactoryRunner(r, lock)
			guard.Go("factory/run-queue", func() {
				scanFactoryQueue(context.Background(), st, r, factoryQueueEvery)
			})
			return true
		}
		if take() {
			return
		}
		guard.Go("factory/run-owner", func() {
			for {
				time.Sleep(factoryWatchEvery)
				if take() {
					return
				}
			}
		})
	})
	return factoryRunnerHere()
}

// factoryRunnerDoors is the seam option for this process: the runner's eight
// doors when it runs the floor, the queued launch otherwise.
func factoryRunnerDoors() factory.LocalOption {
	if r := factoryRunnerHere(); r != nil {
		return factory.WithRunner(r)
	}
	return factory.WithQueuedLaunch()
}

// tryFactoryRunLock takes the floor's run lock without waiting, and answers
// the open file that holds it.
func tryFactoryRunLock(st *store.Store) (*os.File, bool) {
	root := strings.TrimSpace(st.Root())
	if root == "" {
		return nil, false
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, false
	}
	f, err := os.OpenFile(filepath.Join(root, "run.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false
	}
	if err := filelock.Lock(f, true, true); err != nil {
		f.Close()
		return nil, false
	}
	return f, true
}

// buildFactoryRunner is the runner with every executor this process can
// provide. It starts nothing.
//
// SPEND IS WRITTEN ONCE. A stage conversation is an ordinary conversation and
// its calls are already rows on the spend ledger, under that conversation; a
// `factory run` row beside them would count every dollar twice. So the money
// is handed no ledger: what a round spent goes on the item (its spend, its
// cap, today's rail) and nowhere else.
func buildFactoryRunner(st *store.Store, workspace, profileDir string, maker factoryrun.ConversationMaker) *factoryrun.Runner {
	dirs := factoryRepoDirs(st, workspace)
	recipe := func(repo string) factory.Recipe {
		return factoryItemRecipe(dirs, factory.Item{Repo: repo})
	}
	source := factorySource(st, profileDir)
	money := factoryrun.NewMoney(st, nil, time.Now)
	return factoryrun.New(factoryrun.Options{
		Store: st,
		Exec: map[factory.StageKind]factoryrun.Executor{
			factory.StageChat:  factoryChatStage(st, profileDir, factoryrun.NewChatExecutor(maker)),
			factory.StageCheck: factoryrun.NewCheckExecutor(factoryrun.CheckOptions{}),
			factory.StagePost: factoryrun.NewPostExecutor(source, func(repo string) []string {
				return recipe(repo).Policy
			}),
			// A GATE IS A PERSON, and the loop answers it before it would look
			// for an executor, so there is none to give.
		},
		Benches:    factoryBenches(),
		Rail:       money.Rail,
		SpentToday: money.SpentToday,
		RepoDir:    dirs,
		Recipe:     recipe,
		Source:     source,
		// EVENTS ARE DROPPED. The loop writes every move to the store before
		// it would send one, and every window reads the store, so nothing on
		// this machine is waiting to hear them.
		Events: nil,
		Pool:   money,
	})
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

// scanFactoryQueue launches, every `every`, each item another window queued
// that r is not already running, until ctx ends.
func scanFactoryQueue(ctx context.Context, st *store.Store, r *factoryrun.Runner, every time.Duration) {
	for {
		pickUpFactoryQueue(st, r)
		t := time.NewTimer(every)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// pickUpFactoryQueue is one pass of the scan. A queued item goes back to new
// first, because the runner launches only from new, and is launched at once;
// when the launch is refused (the day rail, a stranger's write) the refusal
// is the item's own log line, so the window that queued it can read why.
func pickUpFactoryQueue(st *store.Store, r *factoryrun.Runner) {
	items, err := st.List()
	if err != nil {
		return
	}
	for _, it := range items {
		if it.State != factory.StateQueued || r.Running(it.ID) {
			continue
		}
		taken := false
		_ = st.Update(it.ID, func(cur *factory.Item) error {
			if cur.State != factory.StateQueued {
				return nil
			}
			cur.State, taken = factory.StateNew, true
			return nil
		})
		if !taken {
			continue
		}
		if err := r.Launch(it.ID); err != nil {
			now := time.Now()
			_ = st.Update(it.ID, func(cur *factory.Item) error {
				if cur.Stream == nil {
					cur.Stream = &factory.Stream{}
				}
				cur.Stream.Log = append(cur.Stream.Log, factory.LogLine{At: now, Tone: "fail", Text: "did not start: " + err.Error()})
				return nil
			})
		}
	}
}

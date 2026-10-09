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
// EVERY OTHER WINDOW ASKS. Its eight runner doors (run, stop, pause,
// answer, steer, approve, request changes, re-run checks) post an ask to the store's
// mailbox and wait for the reply (internal/factory's [factory.WithMailbox]);
// the owner drains the mailbox every [factoryMailboxEvery], carries each ask
// through its runner's own door and writes the door's answer back. So every
// verb works from every window, and a refusal reads the same wherever the key
// was pressed. The owner's own window keeps the direct doors.

// factoryBenchesEnv pins how many items run at once on this machine's floor.
const factoryBenchesEnv = "CODEAF_FACTORY_BENCHES"

// factoryBenchesDefault is how many items run at once when nothing pins it.
// The manual quotes it (internal/manual/chat/factory.md).
const factoryBenchesDefault = 4

// factoryMailboxEvery is how often the owner drains the mailbox other windows
// post their asks to. A window waits [factory.MailboxWait] for its reply, which
// is several of these. A variable so a test can shorten it.
var factoryMailboxEvery = time.Second

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
	// shape is the floor's Shape door this process carries for every window
	// (factory_shape.go's [shapeDoor]), nil until it owns the floor.
	shape func(ctx context.Context, id int) (string, error)
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

// factoryShapeHere is this process's Shape door, or nil when another process
// runs the floor (a window then asks that one through the mailbox).
func factoryShapeHere() func(ctx context.Context, id int) (string, error) {
	factoryRunner.mu.Lock()
	defer factoryRunner.mu.Unlock()
	return factoryRunner.shape
}

// startFactoryRunner builds this process's runner over st when it can take
// the floor's run lock, and starts the drain that answers what other windows
// ask of it. It answers the runner, or nil when another process owns the floor
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
			turns := factoryShapeTurns(st, workspace, profileDir, parent)
			r := buildFactoryRunner(st, workspace, profileDir, factoryStageMaker(st, workspace, profileDir, parent), turns)
			// AND THE MANAGER READS THE ISSUE WHEN ITS PAGE OPENS, in this
			// process for every window (factory_shape.go's [shapeDoor]).
			shape := factoryShapeDoor(st, workspace, profileDir, turns)
			// AN ASK THE PREVIOUS OWNER NEVER ANSWERED IS NOT CARRIED OUT NOW:
			// its window has already said nobody answered.
			mb := st.Mailbox()
			_ = mb.Clear()
			setFactoryRunner(r, lock)
			setFactoryShape(shape)
			guard.Go("factory/run-mailbox", func() {
				drainFactoryMailbox(context.Background(), mb, r, shape, factoryMailboxEvery)
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

// factoryRunnerDoors is the seam option for this process over st: the
// runner's eight doors when it runs the floor, the same eight through st's
// mailbox otherwise.
func factoryRunnerDoors(st *store.Store) factory.LocalOption {
	if r := factoryRunnerHere(); r != nil {
		return factory.WithRunner(r)
	}
	// A typed nil inside the interface would read as a mailbox.
	if mb := st.Mailbox(); mb != nil {
		return factory.WithMailbox(mb)
	}
	return nil
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
func buildFactoryRunner(st *store.Store, workspace, profileDir string, maker factoryrun.ConversationMaker, shape *shapeTurns) *factoryrun.Runner {
	dirs := factoryRepoDirs(st, workspace)
	recipe := func(repo string) factory.Recipe {
		return factoryItemRecipe(dirs, factory.Item{Repo: repo})
	}
	source := factorySource(st, profileDir)
	money := factoryrun.NewMoney(st, nil, time.Now)
	// EVERY ITEM WORKS IN ITS OWN WORKTREE, never in the person's checkout:
	// `<factory folder>/work/<repo>-<number>` on `factory/<number>-<slug>`,
	// made from the checkout the first round that needs it. Its `branch:`
	// line goes on the item's log through the store, and a `pr` pushes that
	// branch before it opens the pull request.
	workdirs := factoryWorkdirs(st)
	git := factoryrun.ExecGit{}
	opts := factoryrun.Options{
		Store: st,
		Exec: map[factory.StageKind]factoryrun.Executor{
			factory.StageChat:  factoryChatStage(st, profileDir, factoryrun.NewChatExecutor(maker)),
			factory.StageCheck: factoryrun.NewCheckExecutor(factoryrun.CheckOptions{}),
			factory.StagePost: factoryrun.NewPostExecutorWith(factoryrun.PostOptions{
				Source: source,
				Policy: func(repo string) []string { return recipe(repo).Policy },
				Push:   factoryrun.GitPush(git),
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
		Recipe: recipe,
		Source: source,
		// EVENTS ARE DROPPED. The loop writes every move to the store before
		// it would send one, and every window reads the store, so nothing on
		// this machine is waiting to hear them.
		Events: nil,
		Pool:   money,
		// THE ITEM'S OWN CONVERSATION IS THE MANAGER OF ITS RUN: made at
		// launch the way `T` makes it, the lead of the item's team, told every
		// stage, and listened to for the brief, the steer and an answer.
		Manager: managerMaker(st, workspace, profileDir),
		Talk:    sessionTalk{},
	}
	// AND THE MANAGER SHAPES THE RUN: one turn of its conversation at launch,
	// before the first stage, and one on each thing the person says during
	// the run (factory_shape.go). Nil shapes nothing; the recipe stands.
	if shape != nil {
		opts.Shape = shape.Shape
		opts.Reshape = shape.Reshape
		// AND THE MANAGER IS THE INBOX: every question a step asks is given to
		// it first, one turn of its conversation (factory_inbox.go).
		opts.Inbox = shape.Inbox
	}
	return factoryrun.New(opts)
}

// factoryWorkdirs is the items' worktrees under the factory folder's `work`.
func factoryWorkdirs(st *store.Store) *factoryrun.Workdirs {
	w := factoryrun.NewWorkdirs(filepath.Join(st.Root(), "work"), factoryrun.ExecGit{})
	w.Log = factoryrun.StoreLog(st, time.Now)
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

// drainFactoryMailbox answers, every `every`, what other windows asked of r,
// until ctx ends.
func drainFactoryMailbox(ctx context.Context, mb *store.Mailbox, r factory.RunnerDoors, shape func(context.Context, int) (string, error), every time.Duration) {
	for {
		answerFactoryAsks(mb, r, shape, time.Now())
		t := time.NewTimer(every)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// answerFactoryAsks is one pass of the drain: every ask taken, oldest first,
// is carried through r's door and its reply written. AN ASK NOBODY IS WAITING
// FOR ANY MORE IS NOT CARRIED OUT: its window stopped waiting after
// [factory.MailboxWait] and told the person the runner did not answer, and a
// launch or a stop that happened anyway a minute later would make that
// sentence a lie.
//
// A SHAPING TURN IS CARRIED BESIDE THE DRAIN ([factory.VerbShape]): it takes up
// to a minute, and a stop pressed meanwhile must not wait behind it. Its window
// waits [factory.ShapeMailboxWait] for the reply.
func answerFactoryAsks(mb *store.Mailbox, r factory.RunnerDoors, shape func(context.Context, int) (string, error), now time.Time) {
	asks, err := mb.Take()
	if err != nil {
		return
	}
	for _, ask := range asks {
		if !ask.At.IsZero() && now.Sub(ask.At) > factory.MailboxWait {
			continue
		}
		if ask.Verb == factory.VerbShape && shape != nil {
			guard.Go("factory/shape", func() {
				line, err := shape(context.Background(), ask.ID)
				reply := factory.Reply{Line: line}
				if err != nil {
					reply.Err = err.Error()
				}
				_ = mb.Answer(ask.Seq, reply)
			})
			continue
		}
		_ = mb.Answer(ask.Seq, factory.Carry(r, ask))
	}
}

// setFactoryShape records the shape door the owner answers the mailbox with.
func setFactoryShape(shape func(ctx context.Context, id int) (string, error)) {
	factoryRunner.mu.Lock()
	defer factoryRunner.mu.Unlock()
	factoryRunner.shape = shape
}

// Package run drives an item across its stages: the runner is code, and a
// stage is the only place a model is asked anything.
//
// THE RUNNER ENFORCES THE KNOBS AND THE MODEL ONLY FILLS A STAGE. A stage's
// `until` and `max` are loops the runner drives with [factory.Met]; a gate
// waits for the person; a check runs a command and keeps its exit code as the
// evidence; a post goes through the item's source and keeps the receipt. The
// ask is a brief and a brief is never trusted: what makes a stage "followed"
// is its exit, read by code.
//
// This file is the contract every lane codes against. The loop, the four
// executors, the money and the wiring each live in their own file and own
// nothing in here. A lane that needs a field the contract lacks adds it here,
// additively, and says so.
package run

import (
	"context"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// Executor runs one round of one stage for one item and reports what came of
// it. It honours ctx: a cancelled ctx is a stop or a pause, and the executor
// returns without a result. A nil Executor for a kind means the runner has no
// way to run that kind: the stage fails with a sentence, never a panic.
type Executor interface {
	Run(ctx context.Context, job Job) (factory.StageResult, error)
}

// ExecutorFunc adapts a func to [Executor].
type ExecutorFunc func(ctx context.Context, job Job) (factory.StageResult, error)

// Run calls f.
func (f ExecutorFunc) Run(ctx context.Context, job Job) (factory.StageResult, error) {
	return f(ctx, job)
}

// Job is everything one round of one stage may read.
type Job struct {
	// Item is the item as it stands when the round starts.
	Item factory.Item
	// Stage is the stage being run and Index its place in Item.Stages.
	Stage factory.Stage
	Index int
	// Round counts from 1; the runner raises it on each loop of `until`.
	Round int
	// Notes are the person's notes on the item (Item.Notes) and every note a
	// prior stage left, in order: the brief a chat stage opens with.
	Notes []string
	// Prior holds the results of every stage that ran before this one, in
	// stage order, so a review reads what write produced and a post reads
	// whether the checks were green.
	Prior []factory.StageResult
	// Dir is the repository's checkout on this machine, "" when unknown.
	Dir string
	// Steer delivers the person's words while the round runs; a chat stage
	// hands them to its conversation, a check ignores them.
	Steer <-chan string
	// Log appends one line to the item's stream, timestamped by the runner.
	Log func(line string)
	// Room records the round's conversation on its phase the moment it is
	// made, so the item page can walk into a stage WHILE IT RUNS rather than
	// only once the round has ended. nil records nothing; the result's Chat
	// still lands with the result.
	Room func(chat string)
	// Spend reports model cost the round incurred, in dollars, as it happens.
	Spend func(usd float64)
}

// EventKind names what moved on an item.
type EventKind string

const (
	EventQueued  EventKind = "queued"
	EventStarted EventKind = "started" // a stage began a round
	EventRound   EventKind = "round"   // a stage looped
	EventWaiting EventKind = "waiting" // a gate or a question holds the item
	EventDone    EventKind = "done"    // a stage met its until
	EventFailed  EventKind = "failed"  // a stage could not be run or hit max
	EventLanded  EventKind = "landed"  // every stage done; the proof sheet is up
	EventShipped EventKind = "shipped"
	EventStopped EventKind = "stopped"
	EventPaused  EventKind = "paused"
	EventResumed EventKind = "resumed"
)

// Event is one movement, for the surface and for tests. The store already
// holds the truth; events only say that it changed.
type Event struct {
	Item  int
	Stage string
	Kind  EventKind
	At    time.Time
	Text  string
}

// Options builds a [Runner].
type Options struct {
	Store *store.Store
	// Exec maps a stage kind to what runs it. A missing kind fails its stage.
	Exec map[factory.StageKind]Executor
	// Benches is how many items may run at once; 0 is unbounded.
	Benches int
	// Rail answers today's day rail in dollars, 0 for none; Launch refuses
	// when today's spend has reached it.
	Rail func() float64
	// SpentToday answers what the floor has spent today, for the rail.
	SpentToday func() float64
	// RepoDir maps a repo's short name to its checkout, "" when unknown.
	RepoDir func(repo string) string
	// Recipe answers the repo's recipe (its file, else the default), which
	// the plan stage's edits are bounded by.
	Recipe func(repo string) factory.Recipe
	// Source answers the item's source for a post stage, nil when none.
	Source func(repo string) factory.Source
	// Events receives every movement; nil drops them. Sends never block the
	// loop: the runner drops an event the receiver is not ready for.
	Events chan<- Event
	// Clock is time.Now unless a test says otherwise.
	Clock func() time.Time
	// Pool is the money (money.go): what a round's dollars are written
	// through and whether the item's cap is reached. Nil is the loop's own
	// plain reckoning: Stream.Spent grows by what is reported, and the cap is
	// reached when it is spent.
	Pool Pool
}

// Pool is what the loop needs of the money, and money.go's *Money answers
// it. THE LOOP ASKS AFTER EVERY ROUND whether the cap is reached, and when it
// is, it parks the item on CapQuestion's sentence; the money decides nothing.
type Pool interface {
	// Spend is the Job.Spend hook for one item.
	Spend(id int) func(usd float64)
	// CapReached says whether the item has spent what its cap allows.
	CapReached(it factory.Item) bool
	// CapQuestion is the sentence the item waits on, and its QKind.
	CapQuestion(it factory.Item) (q string, kind string)
}

// Runner is the one per process. Its doors are the engine half of
// [factory.Seam]: Launch, Stop, Pause, Answer, Steer, SignOff, SendBack and
// Reverify, bound by the wiring lane onto the local seam.
//
// The loop lane fills this type in loop.go: the fields here are the contract's
// and may grow there.
type Runner struct {
	opts Options

	// loopOnce makes floor, the state machine's own state (loop.go), on the
	// first door anybody calls, so New stays the one-line constructor the
	// lanes were handed.
	loopOnce sync.Once
	floor    *floorLoop
}

// New makes a runner. It starts nothing until Launch.
func New(opts Options) *Runner { return &Runner{opts: opts} }

// Opts is the runner's options as given.
func (r *Runner) Opts() Options { return r.opts }

// Running says whether this runner holds the item: on a bench, in its queue,
// or waiting on a person. The wiring lane added it, additively, so the process
// that owns the floor can tell a `queued` mark another window wrote (which it
// must pick up) from an item it already queued itself (which it must not
// launch twice).
func (r *Runner) Running(id int) bool {
	return r.loop().ctl(id) != nil
}

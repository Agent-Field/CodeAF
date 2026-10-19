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
	"github.com/Agent-Field/codeaf/internal/factory/record"
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
	// Last is this same stage's result from the round before, nil on round 1.
	// A chat stage that loops on `until clean`, `green` or `proven` reads it
	// to fix what that round found before it looks again. The rounds lane
	// added it, additively.
	Last *factory.StageResult
	// Dir is the repository's checkout on this machine, "" when unknown.
	Dir string
	// Steer delivers the person's words while the round runs; a chat stage
	// hands them to its conversation, a check ignores them.
	Steer <-chan string
	// Models delivers a model the person chose for the job while the round
	// runs; a chat stage switches its conversation to it for its next turn, a
	// check ignores it. The round opened on Item.Model.
	Models <-chan string
	// Log appends one line to the item's stream, timestamped by the runner.
	Log func(line string)
	// Spend reports model cost the round incurred, in dollars, as it happens.
	Spend func(usd float64)
	// Resume names the conversation a pause cut short (a chat stage's
	// Phase.Chat), "" for a round that starts fresh. A chat stage then
	// continues that conversation with a short carry-on note instead of
	// opening a new one, and opens fresh only when it cannot be reopened.
	Resume string
	// CarryOn is what the carried-on chat is told besides the carry-on note:
	// the answers given while the step was paused ([factory.Phase.Carry]).
	CarryOn []string
	// Opened is told the conversation's id the moment a chat stage has one,
	// so the item keeps the stage's chat before the round ends and a pause or
	// a restart can come back to it. Nil is nobody listening.
	Opened func(chat string)
	// Ask puts a question the round's step asked to the item's manager first
	// (inbox.go) and answers the answer in words, the manager's or, when the
	// manager sent it on, the person's. It waits as long as that takes, until
	// ctx ends. Nil is a round with nobody to ask. The inbox lane added it,
	// additively.
	Ask func(ctx context.Context, q factory.Asked) (string, error)

	// dirErr is why Options.Workdir could not answer a folder. The round is
	// then not run: it fails with this sentence, because a round run in the
	// person's checkout instead is the very thing the worktree is for.
	dirErr error
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
	// Workdir answers the folder an item's rounds run in: its own worktree
	// on its own branch (workdir.go), so no item ever works in the person's
	// checkout and two items never share one. When it is set, a Job's Dir
	// comes from it and its error fails the round with its sentence; when it
	// is nil, Dir is RepoDir's. The workdir lane added it, additively.
	Workdir func(it factory.Item) (dir string, err error)
	// Recipe answers the repo's recipe (its file, else the default), which
	// the plan stage's edits are bounded by.
	Recipe func(repo string) factory.Recipe
	// Source answers the item's source for a post stage, nil when none.
	Source func(repo string) factory.Source
	// Publisher resolves the assignee's account and one credential for both
	// the push and forge write. The server never supplies a publishing token.
	// Nil retains Source and Push for embedded runners.
	Publisher func(context.Context, factory.Item) (PublishTarget, error)
	// Push publishes an item's branch from its folder when a person's
	// approval is carried out ([Runner.Publish]); the wire hands [GitPush]
	// with the person's token. Nil publishes nothing: an approval then fails
	// with the sentence that this machine cannot push.
	Push func(dir, branch string) error
	// Git commits an approved item's worktree ([Runner.Publish]); nil is
	// [ExecGit].
	Git Git
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
	// Manager answers the item's manager conversation (manager.go): the item's
	// own conversation, made the way the floor's `T` makes it when the item
	// has none, and named the lead of the item's team either way. Launch calls
	// it; nil makes none, and an item with no conversation runs unmanaged.
	// The manager lane added it, additively.
	Manager func(ctx context.Context, it factory.Item) (transcript string, err error)
	// Talk is the manager conversation's file, read and written: the runner's
	// lines go in through it, and what the person typed comes out. Nil is a
	// runner that neither reports nor listens.
	Talk Talk
	// TalkPoll is how often a running item reads its manager conversation for
	// a steer or an answer; 0 is two seconds.
	TalkPoll time.Duration
	// THERE IS NO TURN AT LAUNCH: a run starts with the stages the item has,
	// which are the repository's recipe unless the person, or the manager on
	// the person's own words through its `factory_run`, changed them first. A
	// turn the runner gave the manager before the first stage held the plan
	// behind a conversation nobody had asked anything of (the owner's run of
	// 2026-10-19), so it is gone.
	//
	// THERE IS NO TURN ON A STEER: words the person types into the manager's
	// conversation during a run are already a turn of the manager there, and
	// it reshapes the stages not yet started with its own `factory_run`.
	//
	// ShapeWait is how long a manager turn on a step's question (Inbox) may
	// take before it is let go; 0 is a minute.
	ShapeWait time.Duration
	// Inbox is the manager's turn on a question a step asked (inbox.go): one
	// turn of the item's manager conversation that answers the question, or
	// sends it on to the person with the reason. Nil, a turn that fails and a
	// turn that answers nothing all send the question to the person. The
	// inbox lane added it, additively.
	Inbox func(ctx context.Context, it factory.Item, q factory.Asked) (InboxReply, error)
	// Record receives a step_failed event when a step errors. Nil keeps the
	// failure on the job and writes no audit line. The worker's mirror
	// leaves this nil; the factory that accepts the report writes the event.
	Record *record.Record
}

// PublishTarget keeps one attempt's GitHub writes on the same person's account.
type PublishTarget struct {
	Source factory.Source
	Push   func(dir, branch string) error
}

// InboxReply is the manager's answer to a step's question: Answer is its
// answer in words, and "" sends the question on to the person, with Why the
// manager's reason.
type InboxReply struct {
	Answer string
	Why    string
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
// or waiting on a person. A worker reads it to know when the job it pulled
// has ended (internal/factory/worker).
func (r *Runner) Running(id int) bool {
	return r.loop().ctl(id) != nil
}

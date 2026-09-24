package session

// The chat's task door on the run engine: a
// `/task` under the bash belt starts a RUN rather than a node of this session's
// own tree. The conversation opens or reuses the store its plan lives in, seeds
// the work from the person's own sentence, hands the store to the run engine in
// a goroutine, and answers AT ONCE with the id the store knows the work by — so
// the person's conversation stays usable while the run goes, and the run's own
// page is the store's root rather than a node this session hosts.
//
// ── WHY THE ENGINE IS A SEAM AND NOT AN IMPORT ──
//
// The run engine ([internal/run]) is built ON this package: its crew factory
// hands every task to [NewBeltWorker] and its landing is [LandRunTree]. A door
// here that imported it would be the cycle the Go compiler refuses, so the
// engine is reached through [RunEngine] — a small interface this package owns,
// implemented by the engine and registered by it ([RegisterRunEngine]) — and a
// build with no engine registered answers the LEGACY road, which is the whole
// of what an unset switch does anyway.
//
// ── OWNERSHIP IS PER PROCESS ──
//
// The running run is held on the Agent ([beltRun]) and nowhere else. A second
// `/task` while one is live adds its work to that same store rather than
// opening a second one, because one store is one run (`plandb`'s own law: a
// store belongs to one root), and the becomes-live child is dispatched by the
// supervisor already turning. A conversation that has no live run seeds a fresh
// store the way [TaskGraph.planSeed] seeds one — adopting a live store it finds,
// archiving a finished one beside the session folder — so a resumed
// conversation keeps reading its own plan.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// runCostLeft is what a run is handed of the dollar limit the person set: the
// limit less what the conversation has already spent. THE LIMIT IS READ THROUGH
// [Agent.railCap], the one place that decides which of the person's dollar
// limits is the smaller, so a run and an adaptive run cannot come to disagree
// about it. Zero means no limit. A spent or overspent limit becomes the smallest
// positive figure rather than zero because the run engine reads zero as
// unlimited; its existing limit ending then stops the run before a second paid
// call if admission did not already refuse the turn.
func runCostLeft(limit, spent float64) float64 {
	if limit <= 0 {
		return 0
	}
	left := limit - spent
	if left <= 0 {
		return math.SmallestNonzeroFloat64
	}
	return left
}

// RunSpec is one run as the door hands it to the engine: the store to drive,
// the working copy its workers share, the run's own words, the conversation's
// two limits, and the provider its workers are seated on.
type RunSpec struct {
	// Store is the plan the run drives. The door opened it and keeps it open
	// for the life of the run; the engine reads and writes it like any other
	// writer of the store.
	Store *plandb.Store
	// Workspace is the run's own working copy, the directory every worker
	// types in and the landing commits.
	Workspace string
	// Title and Brief are the run's own words: the title names the root row,
	// and the brief is the assignment the root worker reads.
	Title string
	Brief string
	// Slots is how many workers run at once. CostUSD is what is left of the
	// smaller dollar limit the person set on the conversation, so the run and
	// conversation spend from the same finite allowance.
	Slots   int
	CostUSD float64
	// Elapsed is the conversation time still available when this run starts.
	// Zero means no time ceiling, matching the run engine's Limits contract.
	Elapsed time.Duration
	// StepsPerTask is the per-task step cap, the same figure a node of this
	// session's own tree carries.
	StepsPerTask int
	// ProfileDir is the person's profile directory, read by the engine's crew
	// factory to seat a task on the model its role rides.
	ProfileDir string
	// WorkModel and PlanModel are the two seats the conversation resolved for
	// this run: the work seat every leaf rides and the plan seat every planner
	// rides. The engine's crew factory seats those two roles on them rather
	// than asking the profile again, so the seat a conversation's task runs in
	// is the seat the conversation's own ladder says. They are read off the
	// conversation's role ladder ([roles.TierModel] through Config.RolesSource)
	// — the same rows its own planner and worker calls resolve through — and
	// empty means the ladder holds no row for that tier, which falls to the
	// profile's tier the way an empty seat always has.
	//
	// Only these two travel: the careful row a check rides and the small row a
	// probe rides are named by no door and stay the profile's.
	WorkModel string
	PlanModel string
	// CompleterFor answers the provider a worker is seated on. The door hands
	// the conversation's own — a run worker's calls go out the way the
	// conversation's do — and a nil one lets the engine build each worker's
	// client itself.
	CompleterFor func(model string) Completer
	// Serves answers whether this conversation's services can take a call on a
	// model ([ServesModel], read live). A delegated run's model API asks it of
	// every model the program names, and answers a model nothing here can
	// reach on the run's work seat instead. Nil answers yes for every model.
	Serves func(model string) bool
	// OnSpend observes the reconciled cumulative run spend while work is live.
	OnSpend func(float64)
	// Delegate, when set, is the program this run's root task is handed to
	// instead of a bash worker (delegate_door.go). No key goes with it: the
	// program reaches a model only through the API codeaf serves the run. Nil is
	// every run the conversation's own workers drive.
	Delegate *delegate.Delegate
	// PlainFolder says the delegated run's folder has no git history, so the
	// program is started with its own flags for one
	// (delegate.Delegate.PlainFolder). False for every other run.
	PlainFolder bool
}

// RunLimit is which bound a person set ended a run. The engine's outcome word
// is one sentence for every limit; it is the exit ladder's own word and the
// ladder keeps its one rung, so this fact is what says which limit fired. It
// is set where the run decides the limit was reached and read where the ending
// is drawn; it is never parsed back out of a sentence.
type RunLimit string

const (
	// RunLimitTime is the elapsed limit: the session's own time bound, of
	// which a run is given what is left.
	RunLimitTime RunLimit = "time"
	// RunLimitCost is the run's spend ceiling, counted while the work is still
	// going.
	RunLimitCost RunLimit = "cost"
)

// RunSummary is what a run came to, folded onto the words this package reads:
// the engine's outcome word, the root's result, the run's size, and the limit
// that ended it when one did.
type RunSummary struct {
	Outcome string
	Result  string
	// Limit is empty on every run that did not end on a bound its person set.
	Limit RunLimit
	// Cut is every task the run's own ending cut mid-flight, by store id: the
	// same typed fact as the limit, read where the run recorded it. A joined
	// row in this set is drawn with the run's own ending and never as a fault.
	Cut   []string
	Nodes int
	Steps int
	USD   float64
}

// RunLanding is what the run's landing answered: the branch the working copy's
// work was committed on, the paths that commit carried, and the sentence saying
// why it refused. A landing names a branch or says what stopped it.
type RunLanding struct {
	Branch  string
	Changed []string
	Refused string
	// Home is how the work came home, in the landing road's own outcome words
	// ([mergeMerged] and its kin), set by this door once the run's copy has been
	// brought back to its ground ([Agent.landBeltRun]). Empty is an engine's own
	// landing, which commits on the copy's branch and merges nothing.
	Home string
}

// RunEngine is the run engine as this door reaches it. Start drives one store
// to an outcome and answers what came of it; Land commits the run's working
// copy onto its branch and answers where the work went.
type RunEngine interface {
	Start(ctx context.Context, spec RunSpec) RunSummary
	Land(ctx context.Context, store *plandb.Store, workspace, rootID string) (RunLanding, error)
}

// chatRunEngine is the registered engine, set once by [RegisterRunEngine] and
// read on every bash-belt `/task`. Nil is a build with no engine linked — every
// test binary that never asks for one, and every door that never imported the
// engine — and the door answers the legacy road for it.
var chatRunEngine RunEngine

// RegisterRunEngine installs the run engine the chat's task door reaches. It is
// called by the engine's own package at load, so a binary that links the engine
// gets the run road and one that does not gets the legacy one.
func RegisterRunEngine(engine RunEngine) { chatRunEngine = engine }

// beltRunOutcomeDone is the engine's word for a run that finished whole, and it
// is the one outcome this door reads as a landing rather than a failure. It is
// spelled here rather than imported because the outcome ladder is the engine's
// and this package cannot reach it.
const beltRunOutcomeDone = "done"

// beltRunSummaryDeadline is the most a landing waits for its one final
// summary refresh before preserving the outcome note it already knows. IT IS
// SIZED TO A REAL CALL: four short lines on the worker model come back in two
// to four seconds, and a deadline under that would make the refresh a thing
// that never happens outside a test. A variable only so a test can shorten it.
var beltRunSummaryDeadline = 6 * time.Second

// beltRun is one live run this conversation started: the store it drives, the
// root it was seeded under, and the row the conversation knows it by. It is held
// on the Agent and nowhere else, so ownership of a running run is this
// process's.
type beltRun struct {
	plan  *planState
	store *plandb.Store
	root  string
	row   uint64
	title string
	// workspace is the run's own copy, the directory every worker types in, and
	// ground is the folder that copy was cut from and comes home to. tree is the
	// copy as the ground ladder made it, kept so the run's landing is the ladder's
	// own ([Agent.landBeltRun]).
	workspace string
	ground    string
	tree      taskTree
	// joined is every hand-off that joined this run after it started, by the
	// number its row wears. Each was published as a running row of its own, and
	// each is settled with the run ([Agent.settleBeltRun]); it is written and
	// read under [Agent.beltMu].
	joined []uint64
	// cut ends the context the run's workers and every call they have out run
	// under, and stopped and stopReason say a PERSON ended it and in what words
	// (stoprun.go). cut is set once before the run starts; the other two are
	// written and read under [Agent.beltMu].
	cut        context.CancelFunc
	stopped    bool
	stopReason string
	// born is when this run started, off the conversation's own clock, and it is
	// what the run's row in the work tree ages from ([Agent.beltRunWorkingNow]).
	// It is the same reading the row published to the surface carries, so the
	// tree and the row cannot disagree about when the work began.
	born time.Time
	// delegate is the program this run's root is handed to, nil for a run the
	// conversation's own workers drive; startSha is the commit the copy stood on
	// the moment the run began, the point a tree program's commits are squashed
	// back to at landing (delegate_door.go).
	delegate *delegate.Delegate
	startSha string
	// plain is a tree program working in a folder with no git history
	// ([delegateOnPlainFolder]): it is told so on its line, and its landing
	// commits nothing, because the work is already where it belongs.
	plain bool
}

// startTaskRun is StartTask's second road, taken whenever the bash belt is asked
// for and a run engine is linked. It seeds or reuses the conversation's store,
// adds this brief's work to it, publishes the row a surface draws, and starts
// the engine in a goroutine the moment the run is new. Every refusal falls back
// to the legacy road rather than inventing a sentence of its own, so a
// conversation the run road cannot serve gets exactly the door it always had.
func (a *Agent) startTaskRun(ctx context.Context, brief string, solo bool, question string) (uint64, string, string, error) {
	engine := chatRunEngine
	g := a.graph()
	if engine == nil || g == nil {
		return a.startTaskLegacy(ctx, brief, solo)
	}
	path := g.planPath()
	if path == "" {
		return a.startTaskLegacy(ctx, brief, solo)
	}

	id := g.reserve()
	title := taskPersonTitle(brief)
	stand := taskStand{dir: a.config.Workspace, mode: TaskModeWorktree}
	if err := a.startKnownTaskRun(ctx, id, title, brief, nil, stand, question); err != nil {
		return a.startTaskLegacy(ctx, brief, solo)
	}
	return id, title, "", nil
}

// standsElsewhereError is the one refusal that STAYS AT THE RUN'S DOOR: a task
// handed off while other work is underway shares that work's copy, and a copy is
// of one folder. It says both folders and what to do, because the conversation
// that reads it has no other way to learn why a good proposal was turned back.
// Every other failure of this door (no engine, no store, a copy that would not
// cut) falls through to the shipped road, which cuts its own copy from the same
// stand and so honours the ground the person approved.
type standsElsewhereError struct{ underway, asked string }

func (e standsElsewhereError) Error() string {
	return "the work already underway is in a copy of " + e.underway + ", and this task is about " + e.asked +
		": tasks that run together share one copy of one folder. Propose it again when that work has ended"
}

// An approved hand-off under the bash belt belongs to the run store and never to the session tree.
func (a *Agent) startKnownTaskRun(ctx context.Context, id uint64, title, brief string, dependsOn []uint64, stand taskStand, question string) error {
	return a.startKnownTaskRunVia(ctx, id, title, brief, dependsOn, stand, question, nil)
}

// startKnownTaskRunVia is [Agent.startKnownTaskRun] with the worker named: nil
// is the conversation's own bash worker, and a program is the one the root task
// is handed to (delegate_door.go). One body serves both because a
// delegated run IS a run — the store, the copy, the row and the stop road are
// the same — and a second body would be two roads that must stay in step.
func (a *Agent) startKnownTaskRunVia(ctx context.Context, id uint64, title, brief string, dependsOn []uint64, stand taskStand, question string, via *delegate.Delegate) error {
	engine := chatRunEngine
	g := a.graph()
	if engine == nil || g == nil || g.planPath() == "" {
		return errors.New("the run road is unavailable")
	}
	path := g.planPath()
	storeID := strconv.FormatUint(id, 10)
	dependencies := make([]plandb.Dependency, 0, len(dependsOn))
	for _, dependency := range dependsOn {
		dependencies = append(dependencies, plandb.Dependency{TaskID: strconv.FormatUint(dependency, 10)})
	}

	a.beltMu.Lock()
	live := a.beltRun
	a.beltMu.Unlock()
	if live != nil {
		return a.joinBeltRun(g, live, id, title, brief, dependencies, stand, via)
	}

	plan, store, err := a.openBeltRunStore(g, path, storeID, title, brief)
	if err != nil {
		return err
	}
	if question = strings.TrimSpace(question); question != "" {
		if _, err := store.Revise(store.RootID(), plandb.TaskPatch{Question: &question}); err != nil {
			_ = store.Close()
			return err
		}
	}
	tree, err := prepareTaskTreeOn(ctx, a.config.Place, a.config.Workspace, a.journalID(), id, title, stand)
	if err != nil {
		_ = store.Close()
		return err
	}
	// THE COPY IS A SHELL WORKER'S, so its landing stages the tree's own status:
	// a run's workers edit through bash and fill no write ledger.
	tree.bashBelt = true
	// THE RUN'S CONTEXT IS ONE A PERSON'S STOP CAN CUT. It outlives the turn that
	// started it, which is the caller's business (task.go hands this door a
	// context no turn's ending cancels); what it must not outlive is the person
	// saying stop, and until this cancel was kept nothing could say it (stoprun.go).
	runCtx, cut := context.WithCancel(ctx)
	born := a.taskClockNow()
	run := &beltRun{
		plan: plan, store: store, root: store.RootID(), row: id, title: title,
		workspace: tree.dir, ground: canonicalPath(stand.dir), tree: tree, cut: cut,
		born: born, delegate: via, startSha: delegateStartSha(tree, via),
		plain: delegateOnPlainFolder(tree, via),
	}
	a.installBeltRun(g, run)
	// THE COPY IS WRITTEN DOWN IN THE SAME BREATH THE RUN IS PUBLISHED, because
	// the branch it names exists only in this variable until it is: the road that
	// cut it minted the name at random and wrote it nowhere ([runCopyOf] says the
	// whole of why). A run published without it is a run nobody can carry on.
	a.publishRunRow(g, TaskNotice{
		ID: id, Title: title, State: TaskRunning, StartedAt: born,
		Copy: runCopyOf(tree),
	})

	go a.driveBeltRun(runCtx, engine, run, a.beltRunSpec(run, brief))
	return nil
}

// joinBeltRun is the second task of a live run. The store holds one root, so the
// new work is a child of it — normalizeSpec's own law for a task that names no
// parent — and the supervisor already turning finds it ready on its next pass.
// Nothing opens a second store.
//
// A DELEGATE NEVER JOINS A RUN AND NOTHING JOINS A DELEGATE'S. A delegated run
// is a run of one task whose worker owns the whole copy for the hour; a second
// task beside it would be a bash worker typing in the tree the program is
// editing, and a delegate added under a live run would be a second program in
// the same tree. Both are refused with what is underway.
func (a *Agent) joinBeltRun(g *TaskGraph, live *beltRun, id uint64, title, brief string, dependencies []plandb.Dependency, stand taskStand, via *delegate.Delegate) error {
	if via != nil || live.delegate != nil {
		return errors.New("work is already underway in a copy of " + live.ground +
			"; " + aloneName(via, live.delegate) + " runs alone, so propose it again when that work has ended")
	}
	if canonicalPath(stand.dir) != live.ground {
		return standsElsewhereError{underway: live.ground, asked: canonicalPath(stand.dir)}
	}
	if _, err := live.store.AddMany([]plandb.TaskSpec{{
		ID: strconv.FormatUint(id, 10), ParentID: live.root, Title: title, Description: brief, Dependencies: dependencies,
	}}); err != nil {
		return err
	}
	a.beltMu.Lock()
	live.joined = append(live.joined, id)
	a.beltMu.Unlock()
	a.publishRunRow(g, TaskNotice{ID: id, Title: title, State: TaskRunning, Parent: live.row, StartedAt: a.taskClockNow()})
	return nil
}

// aloneName is the program a refused join is about: the one asked for, or the
// one already running.
func aloneName(via, running *delegate.Delegate) string {
	if via != nil {
		return via.Name
	}
	if running != nil {
		return running.Name
	}
	return "it"
}

// delegateStartSha is the commit a tree delegate's copy stands on before the
// program has written a byte — the point its commits are squashed back to at
// landing (delegate_door.go). It is read NOW, off the copy itself: whatever the
// ground ladder put under this copy is under this commit, and everything the
// program commits is above it. Empty for every run that is not a tree delegate's.
func delegateStartSha(tree taskTree, via *delegate.Delegate) string {
	if via == nil || !via.LandsTree() {
		return ""
	}
	head, err := git(tree.dir, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(head)
}

// delegateOnPlainFolder says a tree program is about to work in a folder with no
// git history to cut a copy from: a plain folder, or a repository with no
// commit yet. The copy road already answered that by working in the folder
// itself ([prepareTaskTreeOn]); this is the same fact read off the tree it
// answered with, for the program's line and its landing.
//
// IT WAS A RUN THAT DIED ON ITS FIRST LINE. senior-dev keeps its history in
// git unless it is told otherwise, and handed a plain folder it ended at once
// with "workspace is not a git repository", though it has a way of working
// without one. codeaf is the one that read the folder, so codeaf says so.
func delegateOnPlainFolder(tree taskTree, via *delegate.Delegate) bool {
	if via == nil || !via.LandsTree() || tree.merge != mergeInPlace || tree.dir == "" {
		return false
	}
	root, ok := repositoryRoot(tree.dir)
	return !ok || !hasCommit(root)
}

// beltRunSpec is what the engine is handed for a run of this conversation: its
// seats, its bounds and the copy it works in.
//
// IT IS ONE FUNCTION BECAUSE A RUN THAT IS CARRIED ON IS THE SAME RUN. The
// door that picks an interrupted run back up builds no spec of its own
// ([Agent.ContinueRun]); if it did, the two would drift on the day somebody
// changed a seat or a cap on one road, and a continued run would quietly be
// working under different rules from the one it continues.
func (a *Agent) beltRunSpec(run *beltRun, brief string) RunSpec {
	// THE CONVERSATION'S OWN SEATS, read off its role ladder so the engine's
	// crew factory seats the work and plan roles on what this conversation's
	// planner and worker calls already resolve through, rather than asking the
	// profile again for a row the conversation's crew has moved.
	source := roles.Source(a.config.RolesSource)
	workSeat, _ := roles.TierModel(source, roles.TierWorker)
	planSeat, _ := roles.TierModel(source, roles.TierMastermind)

	wallLeft, _ := a.config.Budget.Left()
	if a.config.Budget.Wall > 0 && !a.startedAt.IsZero() {
		wallLeft = a.config.Budget.Wall - time.Since(a.startedAt)
		if wallLeft <= 0 {
			wallLeft = time.Nanosecond
		}
	}
	return RunSpec{
		Store:     run.store,
		Workspace: run.workspace,
		Title:     run.title,
		Brief:     brief,
		Slots:     a.config.TaskParallel,
		CostUSD:   runCostLeft(a.railCap(0), a.Usage().CostUSD),
		Elapsed:   wallLeft,
		// The step cap a node of this session's own tree carries, so a run
		// worker and a node worker stop at the same figure.
		StepsPerTask: taskMaxSteps,
		ProfileDir:   a.config.ProfileDir,
		WorkModel:    workSeat,
		PlanModel:    planSeat,
		CompleterFor: func(string) Completer { return a.beltRunCompleter() },
		Serves:       a.servesModel,
		Delegate:     run.delegate,
		PlainFolder:  run.plain,
	}
}

// openBeltRunStore opens the conversation's store for a run, creating it under
// this run's root or adopting the live one already there. It is [planSeed]'s own
// road stated for the run door: a store whose root has ended is archived beside
// the session folder and a fresh one seeded, because a finished plan is not a
// live one; a store still running is adopted, because it is this conversation's
// run and a second `/task` is more of its work.
func (a *Agent) openBeltRunStore(g *TaskGraph, path, rootID, title, brief string) (*planState, *plandb.Store, error) {
	plan := &planState{path: path, chat: g.planChat()}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		store, err := plandb.Open(path, title, rootID, title, brief, plan.chat)
		return plan, store, err
	} else if err != nil {
		return nil, nil, err
	}
	adopted, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		return nil, nil, err
	}
	if root := adopted.RootID(); adopted.Task(root) != nil && !terminalStoreStatus(adopted.Task(root).Status) {
		return plan, adopted, nil
	}
	// A FINISHED PLAN IS NOT A LIVE ONE. The store is archived beside the
	// session with its own number and a fresh one is seeded under this run.
	_ = adopted.Close()
	archived := fmt.Sprintf("%s.%d", path, len(planArchivePaths(path))+1)
	if err := os.Rename(path, archived); err != nil {
		return nil, nil, err
	}
	store, err := plandb.Open(path, title, rootID, title, brief, plan.chat)
	return plan, store, err
}

// installBeltRun arms the conversation's plan read and records the live run, so
// [Agent.PlanTasks] can read the store and a later `/task` finds the run it
// joins. The plan is set under the graph's plan gate, the same lock every other
// plan reader takes, and the run itself under the Agent's own.
func (a *Agent) installBeltRun(g *TaskGraph, run *beltRun) {
	g.planMu.Lock()
	if g.plan == nil {
		g.plan = run.plan
	}
	g.planMu.Unlock()
	a.beltMu.Lock()
	a.beltRun = run
	a.beltMu.Unlock()
}

// publishRunRow hands one run row to whoever is watching and keeps it for a
// conversation reopened tomorrow, the way a job's row and an adaptive family's
// rows are published: a notice on the standing lane and a row the graph holds.
// The id is the graph's own, minted once, so a row drawn now and the same row
// replayed from the checkpoint are the same row.
// THE COPY IS CARRIED ACROSS HERE AND NOT AT EACH CALLER. Every publish after
// the first REPLACES the row, and only the first one knows where the work is —
// so a later publish that had not thought about it would quietly drop the one
// fact nothing else can recover ([runCopyOf] says why the branch is that fact).
// Carrying it forward in the one function every publisher goes through is what
// keeps that from depending on each of them remembering. A notice that names a
// copy of its own wins, because it is the more recent reading.
func (a *Agent) publishRunRow(g *TaskGraph, notice TaskNotice) {
	if notice.Copy == nil {
		for _, kept := range g.runRows(notice.ID) {
			if kept.ID == notice.ID && kept.Copy != nil {
				notice.Copy = kept.Copy
				break
			}
		}
	}
	a.emitTaskUpdate(notice)
	g.keepRunRows(notice.ID, []TaskNotice{notice})
}

// cutBeltRun ends the live run because the CONVERSATION is ending. It is what
// makes a run's life the conversation's rather than the process's, and it is
// called from exactly one place ([Agent.Close]).
//
// IT IS NOT A PERSON'S STOP AND MUST NOT BE MISTAKEN FOR ONE. A stop writes the
// person's reason on the store's root and settles the row in their words
// (stoprun.go); this writes nothing and says nothing, because nobody asked for
// anything — the room simply closed. What the run did is in its store, which is
// where the next launch reads it from.
func (a *Agent) cutBeltRun() {
	a.beltMu.Lock()
	var cut context.CancelFunc
	if a.beltRun != nil {
		cut = a.beltRun.cut
	}
	a.beltMu.Unlock()
	if cut != nil {
		cut()
	}
}

// driveBeltRun runs one run to its outcome and writes the ending back where the
// conversation reads it: the store's root carries the outcome and the landing,
// the person's conversation is told with the same note a landed task sends, and
// the row the run was published under settles. The store is closed and the run
// cleared once the work is home, so the next `/task` seeds a fresh plan.
func (a *Agent) driveBeltRun(ctx context.Context, engine RunEngine, run *beltRun, spec RunSpec) {
	var foldedUSD float64
	foldSpend := func(total float64) {
		if total <= foldedUSD {
			return
		}
		delta := total - foldedUSD
		a.addFoldedUsage(&ai.Response{Usage: &ai.Usage{Cost: &delta}}, "", 0)
		foldedUSD = total
	}
	spec.OnSpend = foldSpend
	summary := engine.Start(ctx, spec)
	// The final receipt closes any gap between the last live reading and every
	// ending, before the person-stop road and the ordinary landing road split.
	foldSpend(summary.USD)
	if run.cut != nil {
		defer run.cut()
	}
	if stopped, why := a.beltRunStopped(run); stopped {
		// A RUN A PERSON STOPPED IS NOT LANDED. Its work is kept where the stop's
		// own sentence said it would be, and the ending is the stop's (stoprun.go).
		a.settleStoppedBeltRun(run, why, summary.Cut)
		a.beltMu.Lock()
		if a.beltRun == run {
			a.beltRun = nil
		}
		a.beltMu.Unlock()
		_ = run.store.Close()
		return
	}
	var landing RunLanding
	if run.delegate != nil {
		landing = a.landDelegateRun(run, summary)
	} else {
		landing = a.landBeltRun(ctx, engine, run)
	}
	// A LANDING GETS ONE LAST READING before its digest is composed. The call
	// owns the short beltRunSummaryDeadline: refusal, malformed output, or a
	// slow provider leaves the stored reading alone and cannot hold the run
	// beyond that bound. RefreshRunSummary itself declines without a store.
	refreshCtx, cancelRefresh := context.WithTimeout(ctx, beltRunSummaryDeadline)
	a.RefreshRunSummary(refreshCtx, run.root, time.Time{})
	cancelRefresh()
	if _, err := run.store.AddNote(run.root, run.root, beltRunOutcomeNote(run.store, run.root, summary, landing)); err != nil {
		if g := a.graph(); g != nil {
			g.planNote("the run's outcome note failed: " + err.Error())
		}
	}
	a.deliverBeltRunLanding(run, summary, landing)
	a.settleBeltRun(run, summary, landing)

	a.beltMu.Lock()
	if a.beltRun == run {
		a.beltRun = nil
	}
	a.beltMu.Unlock()
	_ = run.store.Close()
}

// landBeltRun brings a finished run's work home, and it does it THE WAY THE
// SHIPPED ROAD BRINGS A TASK'S WORK HOME, through the same function
// ([taskTree.comeHome]): the run's work is committed in the run's own copy, the
// copy's branch is merged into the ground it was cut from, the person's own
// unfinished work is carried across the merge or the branch is kept and the
// files named, and the copy is given back. IT HAPPENS AT THE RUN'S END AND ASKS
// NOBODY, because that is what a task's landing has always done here and a
// finished task whose files are not in the folder is not finished to the person
// who asked for it.
//
// A run that waited in memory for somebody to land it was tried first and had
// three faults: a later hand-off joined a run whose supervisor had stopped and
// never ran, the waiting door was lost when the window closed, and the
// conversation was told the work was done while its folder held none of it.
//
// WHAT THE PERSON IS TOLD IS WHERE THE WORK IS NOW. The engine's own landing
// commits in the copy and names the copy's branch; once the merge is in, the
// branch the conversation's note names is the ground's, and a merge that would
// not go in answers with the sentence that names the kept branch and the files.
func (a *Agent) landBeltRun(ctx context.Context, engine RunEngine, run *beltRun) RunLanding {
	landing, err := engine.Land(ctx, run.store, run.workspace, run.root)
	if err != nil {
		if g := a.graph(); g != nil {
			g.planNote("the run's landing failed: " + err.Error())
		}
		return RunLanding{}
	}
	return a.bringBeltRunHome(run, landing)
}

// bringBeltRunHome is the second half of a run's landing, shared by the engine's
// landing and a delegate's: the copy's branch merged into the ground it was cut
// from, the person's unfinished work carried across or the branch kept and the
// files named, the copy given back, and the homecoming written on the run's page.
func (a *Agent) bringBeltRunHome(run *beltRun, landing RunLanding) RunLanding {
	if run.tree.dir == "" {
		return landing
	}
	merge, said, _, _ := run.tree.comeHome(run.title, nil, a.signsGitWork())
	if landing.Refused != "" {
		// NOTHING TO LAND IS STILL AN ENDING: the copy was given back above, and
		// the sentence the engine answered is the whole account.
		return landing
	}
	if merge != mergeMerged && merge != mergeInPlace {
		// THE WORK DID NOT GO IN, AND THE OUTCOME NOTE SAYS SO IN THE ROAD'S OWN
		// SENTENCE, which names the kept branch and what it clashed with. It is
		// not written twice: the landing carries it and the run's one outcome
		// note is where it is read.
		if said == "" {
			said = "its work is kept on " + landing.Branch + " and did not go into " + run.ground
		}
		landing.Refused, landing.Home = said, merge
		return landing
	}
	landing.Home = merge
	if branch := currentBranch(run.ground); branch != "" {
		landing.Branch = branch
	}
	home := withReport("its work is in "+run.ground+" on "+landing.Branch, said)
	if _, err := run.store.AddNote(run.root, run.root, home); err != nil {
		if g := a.graph(); g != nil {
			g.planNote("the run's homecoming note failed: " + err.Error())
		}
	}
	return landing
}

// deliverBeltRunLanding writes the run's digest into the conversation record.
// A LANDING SPEAKS ONLY WHEN AN ANSWER IS OWED.
func (a *Agent) deliverBeltRunLanding(run *beltRun, summary RunSummary, landing RunLanding) {
	line := beltRunOutcomeNote(run.store, run.root, summary, landing)
	if task := run.store.Task(run.root); landingOwesAnswer(task) {
		document := owedLandingDocument(task, line)
		note := wakeNote(document.text())
		note.landingQuestion, note.landingOutcome = document.landingQuestion, document.landingOutcome
		note.batch = false
		note.settle, note.settleCeiling = true, owedLandingCallCeiling()
		note.settlePrompt = landingAnswerPrompt
		note.settleModel, _ = roles.TierModel(roles.Source(a.config.RolesSource), owedLandingTier())
		a.accept(delivery{origin: fromRuntime, kind: msgResult, note: note})
		return
	}
	note := userText(line)
	note.authored = true
	a.mu.Lock()
	a.recordUserLocked(note)
	a.mu.Unlock()
}

// landingOwesAnswer admits only an owed work root to the one bounded reply turn.
func landingOwesAnswer(task *plandb.Task) bool {
	return task != nil && strings.TrimSpace(task.Question) != "" && task.ParentID == "" && task.Role != plandb.RoleCheck
}

func questionAtTaskHandoff(owed []owedAsk) string {
	for index := len(owed) - 1; index >= 0; index-- {
		if owed[index].from == owedByPerson {
			return strings.TrimSpace(owed[index].text)
		}
	}
	return ""
}

func owedLandingDocument(task *plandb.Task, result string) userMessage {
	question, outcome := strings.TrimSpace(task.Question), strings.TrimSpace(result)
	document := userText(question + "\n\n" + outcome)
	document.landingQuestion, document.landingOutcome = question, outcome
	return document
}

func owedLandingCallCeiling() int { return settleCallCeiling }
func owedLandingTier() roles.Tier { return roles.TierLow }

// settleBeltRun ends the row the run was published under: done when the run
// finished whole, failed on every other ending, with the result and the branch
// a surface draws.
func (a *Agent) settleBeltRun(run *beltRun, summary RunSummary, landing RunLanding) {
	notice := a.beltRunNotice(run, summary, landing)
	notice.EndedAt = a.taskClockNow()
	g := a.graph()
	if g == nil {
		a.emitTaskUpdate(notice)
		return
	}
	// THE ENDING IS KEPT, NOT ONLY SHOWN. The row was saved when the run started
	// and its ending went to the surface alone, so the checkpoint said `running`
	// for ever: a conversation closed and reopened drew a finished run with a
	// spinner and counted it as moving (measured 2026-09-18 on the real binary).
	// What the first row knew and the ending does not (when it started, the row
	// it joined) is carried across.
	for _, kept := range g.runRows(run.row) {
		if kept.ID == run.row {
			notice.StartedAt, notice.Parent = kept.StartedAt, kept.Parent
		}
	}
	a.publishRunRow(g, notice)
	a.settleJoinedRows(g, run, notice.EndedAt, beltRunLimitEnding(summary.Limit), summary.Cut)
}

// settleJoinedRows ends the row of every hand-off that joined the run. A JOINED
// HAND-OFF IS A ROW OF ITS OWN AND ENDS WITH THE RUN IT JOINED: it was published
// running when it joined and nothing ever published its ending, so on the real
// screen it span beside a finished run for as long as the window stayed open.
// Its state is what the store says of that task, and the run's landing is said
// once, on the run's own row.
//
// runEnding is the run's own ending, and cut is the typed record of which
// tasks that ending took down mid-flight ([RunSummary.Cut]). A row in that set
// was ended by the run's ending and not by its own work, so the law draws it
// with that ending and never as a fault: a bound its person set or a stop is
// theirs ([TaskReasonOf]). A row outside it failed on its own and keeps the
// reading it always drew.
func (a *Agent) settleJoinedRows(g *TaskGraph, run *beltRun, ended time.Time, runEnding TaskEnding, cut []string) {
	a.beltMu.Lock()
	joined := append([]uint64(nil), run.joined...)
	a.beltMu.Unlock()
	cutRows := make(map[uint64]bool, len(cut))
	for _, id := range cut {
		if n, err := strconv.ParseUint(id, 10, 64); err == nil {
			cutRows[n] = true
		}
	}
	for _, id := range joined {
		notice := TaskNotice{ID: id, State: TaskFailed, Parent: run.row, EndedAt: ended}
		for _, kept := range g.runRows(id) {
			if kept.ID == id {
				notice.Title, notice.StartedAt = kept.Title, kept.StartedAt
			}
		}
		if task := run.store.Task(strconv.FormatUint(id, 10)); task != nil {
			if task.Status == plandb.StatusDone {
				notice.State = TaskDone
			}
			notice.Result = strings.TrimSpace(task.Result)
			notice.Report = notice.Result
			// THE STORE HOLDS THE ACCOUNT OF WHAT BROKE IN ITS ERROR, and a
			// failed task carries no result: a fault row with nothing to say
			// would draw the bare word, so its first line is the store's own
			// sentence of the break.
			if notice.Report == "" && task.Status != plandb.StatusDone {
				notice.Report = strings.TrimSpace(task.Error)
			}
		}
		if notice.State != TaskDone && runEnding != "" && cutRows[id] {
			notice.Ending = runEnding
		}
		a.publishRunRow(g, notice)
	}
}

// beltRunNotice is the run as a task notice: its row, its ending, the result the
// root reported, and the branch the work landed on. It is the one snapshot both
// the row and the conversation's note are built from, so the two cannot name
// two different endings.
func (a *Agent) beltRunNotice(run *beltRun, summary RunSummary, landing RunLanding) TaskNotice {
	state := TaskDone
	if summary.Outcome != beltRunOutcomeDone {
		state = TaskFailed
	}
	report := strings.TrimSpace(summary.Result)
	if report == "" && summary.Outcome != beltRunOutcomeDone {
		report = strings.TrimSpace(summary.Outcome)
	}
	if line := beltLandingLine(landing); line != "" {
		if report != "" {
			report += "\n"
		}
		report += line
	}
	notice := TaskNotice{
		ID: run.row, Title: run.title, State: state,
		// A LIMIT ITS PERSON SET IS THE ROW'S ENDING, so the reason a surface
		// draws names which limit stopped the work and carries no fault
		// ([TaskReasonOf]): the outcome word alone says only that one of them
		// fired. The ending comes from the summary's own fact and never out of
		// the outcome sentence.
		Ending: beltRunLimitEnding(summary.Limit),
		Report: report, Result: summary.Result,
		Changed: landing.Changed,
	}
	if landing.Branch != "" {
		notice.Branch = landing.Branch
		// WHERE THE WORK IS, AS A FACT. A run whose copy came home says so, and
		// only a branch that is still waiting is `kept`: the card read `branch
		// kept` over work that was already in the person's folder.
		notice.Merge = mergeKept
		if landing.Home != "" {
			notice.Merge = landing.Home
		}
	}
	return notice
}

// beltRunLimitEnding is the run row's ending for a limit its person set, off
// the summary's own fact. Empty, which no reading knows as an ending, is the answer for
// every run that did not end on a bound, which is the reading those runs always
// drew.
func beltRunLimitEnding(limit RunLimit) TaskEnding {
	switch limit {
	case RunLimitTime:
		return TaskEndingTimeLimit
	case RunLimitCost:
		return TaskEndingCostLimit
	}
	return ""
}

// beltRunOutcomeNote is the one line a run's own page carries about how it
// ended: the engine's outcome word and where the work went, or the sentence that
// says why it did not. The last stored run reading supplies its Now sentence;
// without one this remains the landing digest that predates run summaries.
func beltRunOutcomeNote(store *plandb.Store, rootID string, summary RunSummary, landing RunLanding) string {
	parts := []string{summary.Outcome}
	if result := strings.TrimSpace(summary.Result); result != "" {
		parts = append(parts, result)
	}
	if line := beltLandingLine(landing); line != "" {
		parts = append(parts, line)
	}
	if stored, ok := readRunSummary(store, rootID); ok {
		if now := strings.TrimSpace(stored.Summary.Now); now != "" {
			parts = append(parts, now)
		}
	}
	return strings.Join(parts, " · ")
}

// beltLandingLine is what a landing is in one line: where the work went and how
// much of it, or the refusal that says why it did not. It is empty only when
// there is nothing to say — a landing with no branch and no refusal.
func beltLandingLine(landing RunLanding) string {
	if landing.Refused != "" {
		return landing.Refused
	}
	if landing.Branch == "" {
		return ""
	}
	files := "files"
	if len(landing.Changed) == 1 {
		files = "file"
	}
	return fmt.Sprintf("landed on %s: %d %s", landing.Branch, len(landing.Changed), files)
}

func (a *Agent) missingRunDependencies(ids []uint64) []uint64 {
	a.beltMu.Lock()
	live := a.beltRun
	a.beltMu.Unlock()
	if live == nil {
		return ids
	}
	missing := ids[:0]
	for _, id := range ids {
		if live.store.Task(strconv.FormatUint(id, 10)) == nil {
			missing = append(missing, id)
		}
	}
	return missing
}

// beltRunStandsOn reports whether a hand-off may share the live run's copy.
func (a *Agent) beltRunStandsOn(stand taskStand) bool {
	a.beltMu.Lock()
	defer a.beltMu.Unlock()
	return a.beltRun != nil && a.beltRun.ground == canonicalPath(stand.dir)
}

// planArchivePaths names the ended run stores beside path in oldest-run-first
// order. The run door uses the same naming read pages use, so archive creation
// and discovery cannot drift apart.
func planArchivePaths(path string) []string {
	var paths []string
	for suffix := 1; ; suffix++ {
		archived := fmt.Sprintf("%s.%d", path, suffix)
		if _, err := os.Stat(archived); os.IsNotExist(err) {
			break
		} else if err != nil {
			break
		}
		paths = append(paths, archived)
	}
	return paths
}

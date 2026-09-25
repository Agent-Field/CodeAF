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
// store for the new request, archiving the one it finds beside the session
// folder — a finished one as it ended, and one nothing was driving as
// interrupted — so a resumed conversation keeps reading every plan it had, and
// no request ever runs under another run's words ([Agent.openBeltRunStore]).

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/router"
)

// runCostLeft is what a run is handed of the dollar limit the person set: the
// limit less what the conversation has already spent. THE LIMIT IS READ THROUGH
// [Agent.railCap], the one place that decides which of the person's dollar
// limits is the smaller, so a run and an adaptive run cannot come to disagree
// about it. Zero means no limit. A spent or overspent limit becomes the smallest
// positive figure rather than zero because the run engine reads zero as
// unlimited. Both the engine and a program's model API read a limit with
// nothing left as already reached, so such a run makes no paid call at all and
// ends on the person's cost limit.
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

// seniorDevCeilings caps the conversation's remaining allowance at the
// unattended run defaults, so an unset conversation limit is still finite.
func (a *Agent) seniorDevCeilings(spent float64) delegate.Ceilings {
	wallLeft, _ := a.config.Budget.Left()
	if a.config.Budget.Wall > 0 && !a.startedAt.IsZero() {
		wallLeft = a.config.Budget.Wall - time.Since(a.startedAt)
		if wallLeft <= 0 {
			wallLeft = time.Nanosecond
		}
	}
	return (delegate.Ceilings{
		CostUSD: runCostLeft(a.railCap(0), spent),
		Hours:   wallLeft.Hours(),
	}).SeniorDev()
}

// RunSpec is one run as the door hands it to the engine: the store to drive,
// the working copy its workers share, the run's own words, the conversation's
// two limits, and the provider its workers are seated on.
type RunSpec struct {
	// Store is the plan the run drives. The door opened it and keeps it open
	// for the life of the run; the engine reads and writes it like any other
	// writer of the store.
	Store *plandb.Store
	// Workspace is the directory every worker types in and the landing
	// commits: the run's own working copy, or the folder itself for a program
	// that edits files (programfolder.go).
	Workspace string
	// Title and Brief are the run's own words: the title names the root row,
	// and the brief is the assignment the root worker reads.
	Title string
	Brief string
	// Slots is how many workers run at once, and 0 is no limit, which is
	// the word `task.parallel` itself uses. CostUSD is what is left of the
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
	// Admission is the shared machine gate consulted for every worker start.
	Admission RunAdmission
	// OnHold announces the changed set of task ids whose starts are held.
	OnHold func([]string)
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
	// CheckModel is the checker the task's crew was routed to, and empty when
	// the run was not routed — the engine then reads the check seat's own
	// environment rung and the profile's checker row, and never the plan
	// seat's model.
	CheckModel string
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
	// ModelPrice is the catalog price used to reserve each model API call.
	ModelPrice func(model string) (input, output float64, known bool)
	// OnSpend observes the reconciled cumulative run spend while work is live.
	OnSpend func(float64)
	// OnCharge observes each priced call a worker that meters call by call
	// makes — a delegated run's program, through its model API — with the
	// call's own tokens and model, so the conversation folds the call whole
	// rather than as a bare dollar figure ([RunCharge]). Nil for a worker that
	// only reports its running total.
	OnCharge func(RunCharge)
	// Conversation is the id of the conversation that started the run: the
	// journal id its own ledger rows carry as their Session. A delegated
	// run's ledger rows name it as their Root and their Session, beside the
	// task's id, so the conversation's receipt and the spending page can place
	// the money. Empty leaves those rows naming no conversation.
	Conversation string
	// Delegate, when set, is the program this run's root task is handed to
	// instead of a bash worker (delegate_door.go). No key goes with it: the
	// program reaches a model only through the API codeaf serves the run. Nil is
	// every run the conversation's own workers drive.
	Delegate *delegate.Delegate
	// PlainFolder says the delegated run's program works in its folder without
	// git ([ProgramFolder.Plain]), so it is started with its own flags for that
	// (delegate.Delegate.PlainFolder). False for every other run.
	PlainFolder bool
	// ProgramBranch and ProgramIgnoredFile fence the child's eager commits to
	// its own branch and the ignore rules recorded before it started.
	ProgramBranch      string
	ProgramIgnoredFile string
	// Crew is the conversation's crew as a delegated run's program is handed it
	// ([conversationCrew]), so the program works on the models the person
	// chose. Zero for every other run.
	Crew delegate.Crew
}

// ProgramEnding is a delegated run's program's own ending when it did not
// finish, as the engine read it off the program's terminal record: the status
// word, the one sentence the row says, and the program's account.
type ProgramEnding struct {
	// Status is delegate.StatusFail, StatusBudget, StatusCrashed, or a word
	// this build does not know.
	Status string
	// Reason is the sentence: `senior-dev did not finish: …`.
	Reason string
	// Result is the program's account in full.
	Result string
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
	// Program is how a delegated run's program ended when it did not finish,
	// nil otherwise ([ProgramEnding]).
	Program *ProgramEnding
	// ProgramVerdict is a delegated run's program's own word for the work it
	// FINISHED — senior-dev's `pass` or `pass-unverified` — and empty for
	// every other ending and every other run.
	ProgramVerdict string
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
	// Line is a landing that says itself: a program's run, whose folder's
	// ending ([ProgramFolderEnd.Sentence]) is the whole account of where its
	// work is ([beltLandingLine]). Empty for every other run.
	Line string
}

// RunEngine is the run engine as this door reaches it. Start drives one store
// to an outcome and answers what came of it; Land commits the run's working
// copy onto its branch and answers where the work went.
type RunEngine interface {
	Start(ctx context.Context, spec RunSpec) RunSummary
	Land(ctx context.Context, store *plandb.Store, workspace, base, rootID string) (RunLanding, error)
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
	// own ([Agent.landBeltRun]). A program that edits files has no copy: all
	// three name the folder it works in ([ProgramFolder.tree]).
	workspace string
	ground    string
	tree      taskTree
	// asked is the models the person asked this run's program to work with, in
	// place of the crew's working seat; empty for the crew ([Agent.delegateCrew]).
	asked []string
	// joined is every hand-off that joined this run after it started, by the
	// number its row wears. Each was published as a running row of its own, and
	// each is settled with the run ([Agent.settleBeltRun]); it is written and
	// read under [Agent.beltMu].
	joined []uint64
	// machineHeld is the set of starts refused on the latest pass. Readers
	// take beltMu before copying membership onto live plan rows.
	machineHeld map[string]bool
	// cut ends the context the run's workers and every call they have out run
	// under, and stopped and stopReason say a PERSON ended it and in what words
	// (stoprun.go). cut is set once before the run starts; the other two are
	// written and read under [Agent.beltMu].
	cut        context.CancelFunc
	stopped    bool
	stopReason string
	// ending says the engine has answered and the run is only landing,
	// summarising and settling now: its supervisor is gone, so nothing will ever
	// run work added to its store. closing says the CONVERSATION is ending
	// ([Agent.cutBeltRun]), which is not a person's stop and not the run's own
	// ending. Both are written and read under [Agent.beltMu]. over is closed
	// once the run has been cleared off the Agent, which is what a hand-off that
	// arrived while the run was ending waits on before it opens a fresh one.
	ending  bool
	closing bool
	over    chan struct{}
	// born is when this run started, off the conversation's own clock, and it is
	// what the run's row in the work tree ages from ([Agent.beltRunWorkingNow]).
	// It is the same reading the row published to the surface carries, so the
	// tree and the row cannot disagree about when the work began.
	born time.Time
	// ended is the instant the run's engine answered, off the same clock, and
	// spent is what the engine said the run came to: both zero until the run's
	// work is over. They are read under [Agent.beltMu].
	ended                                        time.Time
	spent                                        float64
	costCeiling, timeCeiling                     float64
	conversationCostLimit, conversationTimeLimit bool
	// delegate and folder belong to the program's one run.
	delegate *delegate.Delegate
	folder   *ProgramFolder
	// crew is routed for an ordinary task. A program keeps its requested
	// models and run ceiling instead of receiving a task router's seats.
	crew *taskCrew
}

// startTaskRun is StartTask's second road, taken whenever the bash belt is asked
// for and a run engine is linked. It seeds or reuses the conversation's store,
// adds this brief's work to it, publishes the row a surface draws, and starts
// the engine in a goroutine the moment the run is new. A conversation the run
// road cannot serve at all (no engine linked, no place for a store) gets
// exactly the door it always had; a run road that was there and failed says so
// ([runDidNotStart]) and starts nothing on another engine.
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
		if errors.Is(err, errRunRoadUnavailable) {
			return a.startTaskLegacy(ctx, brief, solo)
		}
		// A RUN ROAD THAT OPENED AND THEN FAILED IS SAID, NEVER HIDDEN. It used
		// to fall through to the older engine's tree here, so a store that would
		// not open or a copy that would not cut turned the person's task into a
		// node of a different engine with nothing on the screen saying so
		// ([runDidNotStart] is the same sentence the proposal door answers).
		if refusal := (standsElsewhereError{}); errors.As(err, &refusal) {
			return 0, "", "", refusal
		}
		return 0, "", "", errors.New(runDidNotStart(id, err))
	}
	return id, title, "", nil
}

// errRunRoadUnavailable is the one failure of the run road that sends a
// hand-off to the older engine: there is no run engine linked or no place for a
// store, so the run road was never there to take ([Agent.startTaskRun]). Every
// other failure happened ON the run road and is said to the person
// ([runDidNotStart]), because falling through to a different engine without a
// word is how a batch of approved hand-offs became old-tree nodes nobody asked
// for.
var errRunRoadUnavailable = errors.New("the run road is unavailable")

// runDidNotStart is what a hand-off whose run did not start answers, on both
// doors: that it did not start, the reason in the store's or the disk's own
// words, and that nothing else was started in its place.
//
// IT MUST NOT READ LIKE SUCCESS. A receipt that said `task N started` over work
// that never started is one output with two meanings, and the person reading it
// cannot tell them apart; so this one opens on the one fact that differs.
//
// IT IS SAID TO THE PERSON, so it carries no instruction meant for the model:
// the proposal door, whose answer the model reads, adds that itself
// ([runDidNotStartToModel]).
func runDidNotStart(id uint64, err error) string {
	reason := strings.TrimSuffix(strings.TrimSpace(err.Error()), ".")
	return fmt.Sprintf("task %d did not start: %s. Nothing is running for it and nothing was started in its place.", id, reason)
}

// runDidNotStartToModel is [runDidNotStart] as the model reads it at the
// proposal door: the same facts, and what it may do next.
func runDidNotStartToModel(id uint64, err error) string {
	return runDidNotStart(id, err) + " Propose it again, or tell the person what stopped it."
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
	_, err := a.startOrJoinTaskRunVia(ctx, id, title, brief, dependsOn, stand, question, nil)
	return err
}

// startKnownTaskRunVia is [Agent.startKnownTaskRun] with the worker named: nil
// is the conversation's own bash worker, and a program is the one the root task
// is handed to (delegate_door.go). One body serves both because a
// delegated run IS a run — the store, the row and the stop road are the same —
// and a second body would be two roads that must stay in step. What differs is
// where it works: a program that edits files works in the folder itself
// (programfolder.go), where every other run gets the copy its ground ladder
// cuts.
//
// asked is the models the person asked the program to work with, resolved;
// none means the conversation's crew ([Agent.delegateCrew]).
func (a *Agent) startKnownTaskRunVia(ctx context.Context, id uint64, title, brief string, dependsOn []uint64, stand taskStand, question string, via *delegate.Delegate, asked ...string) error {
	_, err := a.startOrJoinTaskRunVia(ctx, id, title, brief, dependsOn, stand, question, via, asked...)
	return err
}

// startOrJoinTaskRun is [Agent.startKnownTaskRun] answering, too, whether the
// hand-off JOINED a run already underway rather than starting one, which only
// this door can know: a batch of hand-offs is one run, and which of them opened
// it is decided here, under the start lock, and nowhere before.
func (a *Agent) startOrJoinTaskRun(ctx context.Context, id uint64, title, brief string, dependsOn []uint64, stand taskStand, question string) (bool, error) {
	return a.startOrJoinTaskRunVia(ctx, id, title, brief, dependsOn, stand, question, nil)
}

// startOrJoinTaskRunVia is the one body behind every door above: the start
// lock, the join, the folder or the copy, the store and the first row.
func (a *Agent) startOrJoinTaskRunVia(ctx context.Context, id uint64, title, brief string, dependsOn []uint64, stand taskStand, question string, via *delegate.Delegate, asked ...string) (bool, error) {
	engine := chatRunEngine
	g := a.graph()
	if engine == nil || g == nil || g.planPath() == "" {
		return false, errRunRoadUnavailable
	}
	path := g.planPath()
	storeID := strconv.FormatUint(id, 10)
	dependencies := make([]plandb.Dependency, 0, len(dependsOn))
	for _, dependency := range dependsOn {
		dependencies = append(dependencies, plandb.Dependency{TaskID: strconv.FormatUint(dependency, 10)})
	}

	// A SECOND TASK JOINS THE LIVE RUN. The store holds one root, so the new
	// work is a child of it — normalizeSpec's own law for a task that names no
	// parent — and the supervisor already turning finds it ready on its next
	// pass. Nothing opens a second store.
	//
	// BUT ONLY A RUN THAT IS STILL TURNING CAN BE JOINED. A run whose engine has
	// answered is only landing and settling now, which takes seconds, and work
	// added to its store in that time was never run: its row settled `failed`
	// with no report and no ending. So a hand-off that meets a run on its way
	// out waits for the run to be over and then starts a fresh one of its own
	// ([joinOrWait] is the whole of the decision).
	//
	// AND A PROGRAM NEVER JOINS A RUN AND NOTHING JOINS A PROGRAM'S. A program's
	// run is a run of one task whose worker owns its whole folder for the hour;
	// a second task beside it would be a bash worker typing in the tree the
	// program is editing, and a program added under a live run would be a second
	// worker beside the first. Both are refused with what is underway, and where
	// ([joinOrWait] again).
	//
	// ── ONE RUN PER BATCH ──
	//
	// STARTING A RUN IS ONE CRITICAL SECTION, from "is there a live run" to the
	// run being registered on the Agent, and every other hand-off waits at its
	// door ([Agent.lockBeltStart]). A message that proposes eight tasks, all
	// approved at once, commits eight hand-offs at the same moment, and without
	// this each of them found no live run — the run is registered only after its
	// store is open and its copy is cut, which takes seconds — and each opened or
	// set aside the same store. Measured on the owner's own session: two started
	// runs over one path, one run's worker filed its children into the other's
	// store, and the other six fell through to the older engine. Held here, the
	// first hand-off opens the run and the other seven find it live and join it
	// as children, exactly as a hand-off made a minute later would.
	a.lockBeltStart()
	defer a.beltStartMu.Unlock()
	live, err := a.joinOrWait(ctx, stand, id, title, brief, dependencies, via)
	if err != nil {
		return false, err
	}
	if live != nil {
		a.publishRunRow(g, TaskNotice{
			ID: id, Title: title, State: TaskRunning, Parent: live.row, StartedAt: a.taskClockNow(),
			// AND THE ROW SAYS WHICH STORE TASK IT IS, from its first breath, for
			// the reason the copy is written down in the same breath below: the
			// store is the authority for this work's state and for the page
			// carrying its worker's trajectory, and a row that could not name its
			// task left a surface guessing from the title
			// ([TaskNotice.PlanTask]). IT IS SPELLED THE ONE WAY A STORE ID
			// CROSSES THIS SEAM — [planStoreID], which is what
			// [PlanTaskRow.ID] carries and what [Agent.PlanTaskPage] is asked
			// for — so the id the row names is the id the plan read answers
			// under. The bare stored id is answered under by nothing.
			PlanTask: planStoreID(storeID),
		})
		return true, nil
	}

	crew, folder, err := a.prepareBeltRunStart(ctx, id, title, brief, filepath.Dir(path), stand, via)
	if err != nil {
		return false, err
	}
	plan, store, err := a.openBeltRunStore(g, path, storeID, title, brief, false)
	if err != nil {
		folder.abandon()
		return false, err
	}
	if question = strings.TrimSpace(question); question != "" {
		if _, err := store.Revise(store.RootID(), plandb.TaskPatch{Question: &question}); err != nil {
			discardUnstartedRunStore(store)
			folder.abandon()
			return false, err
		}
	}
	tree, ground := folder.tree(), canonicalPath(stand.dir)
	if folder != nil {
		// A PROGRAM'S GROUND IS THE FOLDER IT WORKS IN, which is the
		// repository's root when it was handed a folder inside one.
		ground = canonicalPath(folder.Dir)
	} else {
		tree, err = beltRunPrepare(ctx, a.config.Place, a.config.Workspace, a.journalID(), id, title, stand)
		if err != nil {
			discardUnstartedRunStore(store)
			return false, err
		}
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
		workspace: tree.dir, ground: ground, tree: tree, cut: cut,
		born: born, over: make(chan struct{}),
		delegate: via, folder: folder, asked: asked, crew: crew,
	}
	a.installBeltRun(g, run)
	// THE COPY IS WRITTEN DOWN IN THE SAME BREATH THE RUN IS PUBLISHED, because
	// the branch it names exists only in this variable until it is: the road that
	// cut it minted the name at random and wrote it nowhere ([runCopyOf] says the
	// whole of why). A run published without it is a run nobody can carry on.
	//
	// AND THE ROW SAYS WHICH PROGRAM HAS THE WORK from this first publish on, which
	// is the one place both doors meet — a typed `/<name>` and an approved
	// `via` — so every later publish carries it forward from here
	// ([TaskNotice.Program], [Agent.publishRunRow]).
	a.publishRunRow(g, TaskNotice{
		ID: id, Title: title, State: TaskRunning, StartedAt: born,
		Copy: runCopyOf(tree), Program: programName(via),
		// THE ROOT'S ROW NAMES THE STORE'S ROOT, which is this same number: the
		// store was seeded under `storeID` a few lines up, so the row the person
		// was answered with and the task the store drives are one identity said
		// twice rather than two pieces of work ([TaskNotice.PlanTask]). In
		// [planStoreID]'s spelling, which is the one the plan read answers under.
		PlanTask: planStoreID(storeID),
		Crew:     run.crewDecision(),
		Model:    run.crewWorker(),
	})

	spec := a.beltRunSpec(run, brief)
	if programName(via) == "senior-dev" {
		run.costCeiling, run.timeCeiling = spec.CostUSD, spec.Elapsed.Hours()
		run.conversationCostLimit = a.railCap(0) > 0 && runCostLeft(a.railCap(0), a.Usage().CostUSD) <= delegate.DefaultSeniorDevCostUSD
		run.conversationTimeLimit = a.seniorDevConversationTimeLimit()
	}
	go a.driveBeltRun(runCtx, engine, run, spec)
	return false, nil
}

// prepareBeltRunStart settles admission before the store is seeded. An
// ordinary task gets its per-task crew before its copy opens; a program keeps
// its requested models and separate ceiling, and its folder is held before
// any run records are written. Both refusals therefore leave no run behind.
func (a *Agent) prepareBeltRunStart(ctx context.Context, id uint64, title, brief, sessionDir string, stand taskStand, via *delegate.Delegate) (*taskCrew, *ProgramFolder, error) {
	var crew *taskCrew
	var err error
	if via == nil {
		crew, err = a.routeTaskCrew(ctx, id, title, brief)
		if err != nil {
			return nil, nil, err
		}
	}
	folder, err := a.readyRunFolder(id, title, sessionDir, stand, via)
	if err != nil {
		return nil, nil, err
	}
	return crew, folder, nil
}

// seniorDevConversationTimeLimit reports whether the person's remaining wall
// limit, rather than the unattended default, is the one that will stop the run.
func (a *Agent) seniorDevConversationTimeLimit() bool {
	if a.config.Budget.Wall <= 0 {
		return false
	}
	remaining := a.config.Budget.Wall
	if !a.startedAt.IsZero() {
		remaining -= time.Since(a.startedAt)
	}
	return remaining <= time.Duration(delegate.DefaultSeniorDevHours*float64(time.Hour))
}

// lockBeltStart takes the conversation's start lock, the one door every road
// that may open a run's store passes ([Agent.startOrJoinTaskRun] and
// [Agent.ContinueRun]). It is held from the look for a live run until the run
// is registered, so two hand-offs can never both decide there is no run and
// both open one.
func (a *Agent) lockBeltStart() {
	if a.beltStartMu.TryLock() {
		return
	}
	if beltStartWaits != nil {
		beltStartWaits()
	}
	a.beltStartMu.Lock()
}

// discardUnstartedRunStore takes back a store this door seeded for a run that
// then did not start. NOTHING WAS EVER RUN ON IT, so it is removed rather than
// left for the plan to read: a root nobody drives drew as work in flight, and
// the hand-off it was seeded for has already said it did not start
// ([runDidNotStart]). The path is free again for the next request.
func discardUnstartedRunStore(store *plandb.Store) {
	path := store.Path()
	_ = store.Close()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_ = os.Remove(path + suffix)
	}
}

// beltRunPrepare cuts a run's working copy. It is [prepareTaskTreeOn] in the
// product, and a variable only so a test can make the cut slow or make it fail:
// the slow cut is the window a batch of simultaneous hand-offs used to race
// through, and a failed cut is a run road that did not open.
var beltRunPrepare = prepareTaskTreeOn

// beltStartWaits is a test's observation point: it is called when a hand-off
// finds another hand-off in the middle of starting the conversation's run and
// is about to wait for it. Nil outside tests, and nothing in the product reads
// it.
var beltStartWaits func()

// beltJoinWaits is a test's observation point: it is called when a hand-off has
// met a run on its way out and is about to wait for it to be over. Nil outside
// tests, and nothing in the product reads it.
var beltJoinWaits func()

// joinOrWait adds a hand-off's work to the live run when there is one that is
// still turning, and answers that run; it answers nil when there is no run to
// join, and by then any run that was on its way out has been cleared.
//
// THE DECISION IS TAKEN UNDER THE BELT'S OWN LOCK, and the store write with it,
// because the flag it reads ([beltRun.ending]) is set under that lock the moment
// the engine answers. Read first and written after, a hand-off could still slip
// its work into a store whose supervisor had already gone home.
//
// AND THE STORE HAS THE LAST WORD. A run whose engine has not answered yet may
// already have written its root's ending ([plandb.Store.CompleteRoot], or the
// ending of a limit), and the store refuses a child under an ended task in the
// same transaction that would have added it. That refusal is read as the run
// being on its way out, never as the hand-off failing.
//
// A PROGRAM NEVER JOINS A RUN AND NOTHING JOINS A PROGRAM'S, while that run is
// still turning: both are refused with what is underway, and where. A run on
// its way out is waited for like any other.
func (a *Agent) joinOrWait(ctx context.Context, stand taskStand, id uint64, title, brief string, dependencies []plandb.Dependency, via *delegate.Delegate) (*beltRun, error) {
	storeID := strconv.FormatUint(id, 10)
	for {
		a.beltMu.Lock()
		live := a.beltRun
		if live == nil {
			a.beltMu.Unlock()
			return nil, nil
		}
		over := live.over
		if !live.ending && !live.closing && !live.stopped {
			if refusal := programJoinRefusal(via, live); refusal != nil {
				a.beltMu.Unlock()
				return nil, refusal
			}
			if canonicalPath(stand.dir) != live.ground {
				a.beltMu.Unlock()
				return nil, standsElsewhereError{underway: live.ground, asked: canonicalPath(stand.dir)}
			}
			_, err := live.store.AddMany([]plandb.TaskSpec{{
				ID: storeID, ParentID: live.root, Title: title, Description: brief, Dependencies: dependencies,
			}})
			if err == nil {
				live.joined = append(live.joined, id)
				a.beltMu.Unlock()
				return live, nil
			}
			if root := live.store.Task(live.root); root != nil && !terminalStoreStatus(root.Status) {
				a.beltMu.Unlock()
				return nil, err
			}
		}
		a.beltMu.Unlock()
		// THE RUN IS ON ITS WAY OUT: wait for it to be over, and look again. A run
		// installed by nobody else is the common answer, and a fresh run is then
		// this hand-off's own.
		if over == nil {
			return nil, errors.New("the run already underway is ending")
		}
		if beltJoinWaits != nil {
			beltJoinWaits()
		}
		select {
		case <-over:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// readyRunFolder answers the folder a new run that edits files by a program's
// hand works in, readied, and what refuses a run its folder. It is the first
// thing a run does, so a folder that refuses refuses before a store is seeded
// or a row is published. sessionDir is the folder the run's store is in.
//
//   - A PROGRAM THAT EDITS FILES WORKS IN THE FOLDER ITSELF (programfolder.go),
//     readied here: refused over changes that are not committed or another
//     program's run in or around it, and otherwise held for the run.
//   - AN ORDINARY RUN IS REFUSED A FOLDER A PROGRAM'S RUN HOLDS
//     (programhold.go), before a copy is cut from it.
//
// A program that only answers reads the folder where it is and changes
// nothing, so it is neither readied nor refused, and nil is its folder.
func (a *Agent) readyRunFolder(id uint64, title, sessionDir string, stand taskStand, via *delegate.Delegate) (*ProgramFolder, error) {
	if via == nil {
		if refusal := standHeldRefusal(stand, a.config.Workspace); refusal != "" {
			return nil, errors.New(refusal)
		}
		return nil, nil
	}
	if !via.LandsTree() {
		return nil, nil
	}
	return PrepareProgramFolder(ProgramFolderOrder{
		Program: *via, Dir: stand.dir, Title: title, Holder: taskStopName(id, title),
		Keep: plandb.TaskDir(sessionDir, strconv.FormatUint(id, 10)), Instead: "say which folder the work is in, as ground",
		Place: a.config.Place, SignModel: a.signsGitWork().namedModel(),
	})
}

// programJoinRefusal is why a hand-off may not join the live run because a
// program is on one side of it, and nil when neither is a program's: a program
// never joins a run, and nothing joins a program's ([Agent.joinOrWait]).
func programJoinRefusal(via *delegate.Delegate, live *beltRun) error {
	if via == nil && live.delegate == nil {
		return nil
	}
	where := "in a copy of " + live.ground
	if live.folder != nil {
		where = "in " + live.ground
	}
	return errors.New("work is already underway " + where +
		"; " + aloneName(via, live.delegate) + " runs alone, so propose it again when that work has ended")
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

// programName is the name a run's rows carry for the program its worker is
// ([TaskNotice.Program]), and "" for the conversation's own bash worker, which
// is no program at all.
func programName(via *delegate.Delegate) string {
	if via == nil {
		return ""
	}
	return strings.TrimSpace(via.Name)
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
	programCrew := a.delegateCrew(run)
	if run.delegate != nil && workSeat == "" {
		workSeat = programCrew.Hands
	}
	checkSeat := ""
	// A ROUTED RUN IS SEATED ON ITS OWN CREW, all three seats, and the check
	// seat is the checker the router picked for this task — never the plan
	// seat's model by inheritance (taskcrew.go).
	if d := run.crewDecision(); d != nil {
		workSeat = d.Seat(crewroute.Worker).Send
		planSeat = d.Seat(crewroute.Planner).Send
		checkSeat = d.Seat(crewroute.Checker).Send
	}

	wallLeft, _ := a.config.Budget.Left()
	if a.config.Budget.Wall > 0 && !a.startedAt.IsZero() {
		wallLeft = a.config.Budget.Wall - time.Since(a.startedAt)
		if wallLeft <= 0 {
			wallLeft = time.Nanosecond
		}
	}
	cost := runCostLeft(a.railCap(0), a.Usage().CostUSD)
	if programName(run.delegate) == "senior-dev" {
		ceilings := a.seniorDevCeilings(a.Usage().CostUSD)
		cost, wallLeft = ceilings.CostUSD, ceilings.Elapsed()
	}
	return RunSpec{
		Store:     run.store,
		Workspace: run.workspace,
		Title:     run.title,
		Brief:     brief,
		Slots:     a.config.TaskParallel,
		CostUSD:   cost,
		Elapsed:   wallLeft,
		// The step cap a node of this session's own tree carries, so a run
		// worker and a node worker stop at the same figure.
		StepsPerTask: taskMaxSteps,
		// The graph already owns the fallback account when the door handed
		// none. Run and node workers must charge that same conversation.
		Admission:    NewRunAdmission(a.config.TaskMaxLoad, a.config.TaskMinFreeMB, a.graph().lanes),
		OnHold:       func(ids []string) { a.setBeltRunMachineHold(run, ids) },
		ProfileDir:   a.config.ProfileDir,
		WorkModel:    workSeat,
		PlanModel:    planSeat,
		CheckModel:   checkSeat,
		CompleterFor: func(string) Completer { return a.crewRunCompleter(run) },
		Serves:       a.servesModel,
		ModelPrice:   a.config.ModelPrice,
		Conversation: a.runConversation(),
		Delegate:     run.delegate,
		PlainFolder:  run.folder != nil && run.folder.Plain(),
		ProgramBranch: func() string {
			if run.folder == nil {
				return ""
			}
			return run.folder.Branch
		}(),
		ProgramIgnoredFile: run.folder.IgnoredFile(),
		Crew:               programCrew,
	}
}

// delegateCrew is the conversation's crew as a delegated run hands it to its
// program: the planning seat, the working seat and the light seat, read off the
// same role ladder this conversation's own planner and workers resolve through,
// with each seat's effort taken off, because a program's pool is a list of
// models and an effort is a knob of the request. Zero for a run no program works.
//
// THE PERSON'S CREW IS THE DEFAULT. A program handed an hour of work used to
// route on a list of its own the person never chose, while the crew they set
// sat unread beside it.
func (a *Agent) delegateCrew(run *beltRun) delegate.Crew {
	if run.delegate == nil {
		return delegate.Crew{}
	}
	source := roles.Source(a.config.RolesSource)
	seat := func(tier roles.Tier) string {
		value, _ := roles.TierModel(source, tier)
		model, _ := roles.SplitEffort(strings.TrimSpace(value))
		return strings.TrimSpace(model)
	}
	crew := delegate.Crew{
		Brain: seat(roles.TierMastermind), Hands: seat(roles.TierWorker), Light: seat(roles.TierLow),
		Asked: append([]string(nil), run.asked...),
	}
	// The routed crew has no permanent worker row. A program keeps a single
	// worker recommendation from the profile when nobody pinned that row; its
	// explicit model list still takes precedence inside the program. This does
	// not route the program by its brief or put it under an ordinary task cap.
	if crew.Hands == "" && a.config.RouteCrew != nil {
		if decision, err := a.config.RouteCrew(config.CrewAsk{ChatModel: a.Model()}); err == nil {
			crew.Hands = decision.Seat(crewroute.Worker).Send
		}
	}
	return crew
}

// programClosedSentence is the ending written on a program's run that codeaf
// closed under: the conversation or the engine ended while the program worked.
// It says what happened in plain words, because nobody decided anything and the
// work did not fail on its own.
func programClosedSentence(name string) string {
	return "codeaf closed while " + name + " was running"
}

// programEndedSentence is the ending written on a program's run that codeaf
// closed under AFTER the program had exited: its worker was still settling
// owed receipts, or the run was about to end. The program was not running, so
// the sentence does not say it was; what was not done is the run's ending in
// its folder, which the next codeaf to find the run settles without writing
// to git ([settleOwedProgramFolder]) and says under this line.
func programEndedSentence(name string) string {
	return name + " had ended; codeaf closed before it could say where its work is"
}

// runLimitSentence is the run engine's outcome word for a run a limit its
// person set ended (internal/run's OutcomeLimit, spelled here because this
// package may not reach that one). A program's run that ended on a limit
// carries it as its store's ending when no sentence of the program's own came
// back ([runEndingWords]), and a reopen reads the limit back out of it.
const runLimitSentence = "a limit you set stopped it"

// endOrphanedProgramRun ends a program's run whose store was left open by a
// process that went away, at the run's last evidence of life, and settles the
// folder it worked in when that process went away before it could finish it
// ([settleOwedProgramFolder]), answering how it left the folder. It ends
// nothing in a store whose run has ended, or whose run no program worked (the
// task's record folder holds no program record, [delegate.ProgramFile]).
//
// THE ENDING IS WRITTEN WHEN THE RUN WAS LAST SEEN, NOT NOW. The process that
// finds the store can be hours later than the one that lost it, and the page
// counts a run's time to its ending ([plandb.Store.FailRootAt] says why).
//
// THE FOLDER IS SETTLED WHATEVER THE STORE SAYS. A run a person stopped, or
// one codeaf closed under, has its store's ending written before its folder is
// finished, so a process that went away in between leaves an ended store over
// a folder still on the program's branch with its last changes uncommitted —
// and those are left uncommitted, because nobody saw the run end
// ([ProgramFolder.settleGone]).
func endOrphanedProgramRun(store *plandb.Store) (ProgramFolderEnd, bool) {
	rootID := store.RootID()
	root := store.Task(rootID)
	if root == nil {
		return ProgramFolderEnd{}, false
	}
	taskDir := plandb.TaskDir(filepath.Dir(store.Path()), rootID)
	if record, ok := delegate.ReadProgram(taskDir); ok && !terminalStoreStatus(root.Status) {
		endProgramRunClosed(store, record, lastEvidenceOfLife(store, root, taskDir, record))
	}
	end, settled := settleOwedProgramFolder(taskDir)
	if settled {
		_, _ = store.AddNote(rootID, rootID, end.Sentence())
	}
	return end, settled
}

// endProgramRunClosed writes a program's run's ending when codeaf closed under
// it: the run's task failed with the plain sentence at the instant named (zero
// is now), and the same sentence as the task's newest note, which is the line
// its page carries — the store's error is a field no page draws, and a page
// that read `incomplete` with nothing beside it would send a person looking
// for a fault in the work.
//
// A PROGRAM WHOSE RECORD CARRIES ITS EXIT WAS NOT RUNNING. Its worker writes
// the exit before it settles the program's owed receipts, and the run lands
// only after that, so codeaf can close over a program that has already gone:
// that run is ended at the program's exit ([runClockEnd]'s instant), in the
// sentence that says so ([programEndedSentence]), and never in the one that
// claims codeaf closed under a program at work.
func endProgramRunClosed(store *plandb.Store, record delegate.ProgramRecord, at time.Time) {
	sentence := programClosedSentence(record.Name)
	if !record.EndedAt.IsZero() {
		sentence, at = programEndedSentence(record.Name), record.EndedAt
	}
	if err := store.FailRootAt(sentence, at); err != nil {
		return
	}
	if root := store.Task(store.RootID()); root == nil || root.Error != sentence {
		// A run that had already ended keeps its own ending and its own words.
		return
	}
	_, _ = store.AddNote(store.RootID(), store.RootID(), sentence)
}

// lastEvidenceOfLife is the latest instant a program's run is known to have
// been working: its program's recorded exit when it has one, the end of its
// last model call (or the start of one that never came back), its last charge,
// and the store's own last write to its task. The zero time means none of them
// is known, which the ending reads as now.
func lastEvidenceOfLife(store *plandb.Store, root *plandb.Task, taskDir string, record delegate.ProgramRecord) time.Time {
	latest := root.UpdatedAt
	later := func(at time.Time) {
		if at.After(latest) {
			latest = at
		}
	}
	later(record.StartedAt)
	later(record.EndedAt)
	later(store.LastSpendAt())
	if turns, err := delegate.ReadTurns(taskDir, 0); err == nil {
		for _, turn := range turns {
			later(turn.Started)
			later(turn.Ended)
		}
	}
	return latest
}

// endInterruptedProgramRun is the restore's half of the same ending: a
// conversation read back from disk whose run row comes back interrupted over a
// program's store that is still open has its run ended there, at the run's
// last evidence of life. It runs once, as the conversation is opened, when the
// process opening it is the only one that holds it (the session file's lock),
// so no run of this conversation can be live anywhere.
//
// WITHOUT IT THE PAGE READ `running` UNTIL THE NEXT HAND-OFF. codeaf closing
// under a program's run left the store open, and a reopened conversation drew
// that run's page as running, offered to stop it, and counted its clock up from
// when it started for as long as the page stayed open.
func (a *Agent) endInterruptedProgramRun() {
	if a.config.InTask {
		return
	}
	// READ, NEVER BUILT: an interrupted row exists only where [Agent.recoverTasks]
	// read a checkpoint back, and that already built the graph. A conversation
	// with none is not given one by being opened.
	g := a.tasker()
	if g == nil || !g.holdsInterruptedRun() {
		return
	}
	path := g.planPath()
	if path == "" {
		return
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return
	}
	store, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		return
	}
	defer store.Close()
	row, err := strconv.ParseUint(store.RootID(), 10, 64)
	if err != nil {
		return
	}
	kept, found := runRowOf(g, row)
	if !found || kept.State != TaskInterrupted {
		return
	}
	end, settled := endOrphanedProgramRun(store)
	if !settled {
		// A FOLDER ENDED BY A PROCESS THAT DID NOT LIVE TO SETTLE THE ROW is read
		// back from the run's record folder ([keptProgramFolderEnd]), and its
		// page is told once where the work is.
		taskDir := plandb.TaskDir(filepath.Dir(store.Path()), store.RootID())
		if end, settled = keptProgramFolderEnd(taskDir); settled && !storeSays(store, end.Sentence()) {
			_, _ = store.AddNote(store.RootID(), store.RootID(), end.Sentence())
		}
	}
	a.settleInterruptedProgramRow(g, store, kept, end, settled)
}

// storeSays is whether a note on the store's root already says sentence.
func storeSays(store *plandb.Store, sentence string) bool {
	for _, note := range store.Notes(store.RootID(), 0) {
		if strings.Contains(note.Body, sentence) {
			return true
		}
	}
	return false
}

// settleInterruptedProgramRow settles the row a reopen restored as interrupted
// once its program's run has ended in its store, whether this reopen ended it
// or the closing did first ([Agent.cutBeltRun]).
//
// A PROGRAM'S RUN IS ONE NOTHING CAN CARRY ON, so a row left interrupted — which
// the side list draws as waiting on a person — says something the page does
// not: the page reads it ended, in codeaf's sentence, with its time stopped.
// The row now says the same, not as a fault, ending where the store ended it.
//
// A RUN WHOSE TASK THE STORE CALLS DONE SETTLES DONE. Its program finished and
// codeaf closed before the row was published; the row used to be left
// interrupted, on the reading that the work was never landed and that was a
// person's call — but a program's work is never landed by anybody, it is left
// on its branch, and this reopen settles the folder too. So the row reads done,
// with the program's result and where the work is.
//
// THE ROW ENDS AT THE PROGRAM'S RECORDED EXIT when the record carries one, and
// at the store's ending otherwise ([runClockEnd]) — the pair every live settle
// reads ([Agent.beltRunEndedAt]). The store's ending can come after the exit
// by the whole wait for owed receipts, and that wait is not the run's time.
//
// AND IT SAYS WHERE THE WORK IS when this reopen settled the run's folder
// (settled): the folder's sentence under the ending, and the program's branch
// when it holds the work.
func (a *Agent) settleInterruptedProgramRow(g *TaskGraph, store *plandb.Store, kept TaskNotice, end ProgramFolderEnd, settled bool) {
	root := store.Task(store.RootID())
	if root == nil || (root.Status != plandb.StatusFailed && root.Status != plandb.StatusCancelled && root.Status != plandb.StatusDone) {
		return
	}
	record, ok := delegate.ReadProgram(plandb.TaskDir(filepath.Dir(store.Path()), store.RootID()))
	if !ok {
		return
	}
	row := kept
	// AND WHAT IT CAME TO, read off the store's spend rows: the process that
	// knew the run's total is gone ([Agent.publishRunRow] carries it live).
	if row.CostUSD == 0 {
		row.CostUSD = storeSpent(store)
	}
	if root.Status == plandb.StatusDone {
		row.State, row.Ending, row.Stopped = TaskDone, "", false
		row.Result = strings.TrimSpace(root.Result)
		row.Report = row.Result
	} else {
		row.State = TaskFailed
		row.Report, row.Ending, row.Stopped = interruptedProgramEnding(store, root, record)
	}
	row.EndedAt = runClockEnd(kept.StartedAt, record, root.CompletedAt)
	row.Elapsed = 0
	if settled {
		row.Report = strings.TrimSpace(row.Report + "\n" + end.Sentence())
		row.Changed = end.Changed
		if end.Kept {
			row.Branch, row.Merge = end.Folder.Branch, mergeKept
		}
	}
	a.publishRunRow(g, row)
}

// interruptedProgramEnding is how a program's run that a reopen settles ended,
// read off what its store and its record kept: the sentence the row carries,
// its ending, and whether a person stopped it.
//
// EACH ENDING IS THE ONE THE LIVE SETTLE WOULD HAVE DRAWN, because the row is
// the same row whichever process settles it. A CANCELLED ROOT IS A PERSON'S
// STOP (the stop road writes it before the program is ended, and a person who
// quits during that wait has still stopped it). THE LIMIT SENTENCE IS THE
// LIMIT, and which one is a fact of the run: the dollar ceiling it handed its
// program was reached, or else its time ran out. codeaf's own sentences, and
// every sentence of the program's, are read as the program's ending, whose
// reason is the sentence itself — so the side list reads `codeaf closed while
// senior-dev was running`, not the fixed words of a cut it cannot explain.
func interruptedProgramEnding(store *plandb.Store, root *plandb.Task, record delegate.ProgramRecord) (string, TaskEnding, bool) {
	report := strings.TrimSpace(root.Error)
	switch {
	case root.Status == plandb.StatusCancelled:
		return report, TaskEndingStopped, true
	case report == runLimitSentence:
		return report, interruptedLimitEnding(store, record), false
	case report == "":
		return programClosedSentence(record.Name), TaskEndingProgram, false
	}
	return report, TaskEndingProgram, false
}

// storeSpent is every dollar a run's store holds spend rows for.
func storeSpent(store *plandb.Store) float64 {
	spent := 0.0
	for _, total := range store.SpendSummary().ByRole {
		spent += total.USD
	}
	return spent
}

// interruptedLimitEnding is which limit ended a program's run, off the run's
// own facts: its spend against the dollar ceiling the run handed its program
// (the run's own ceiling, [delegate.ProgramRecord.CeilingUSD]) says the
// dollars ran out, and any other limit ending is the run's time.
func interruptedLimitEnding(store *plandb.Store, record delegate.ProgramRecord) TaskEnding {
	if spent := storeSpent(store); record.CeilingUSD > 0 && spent >= record.CeilingUSD {
		return TaskEndingCostLimit
	}
	return TaskEndingTimeLimit
}

// holdsInterruptedRun says whether any run row this graph holds came back
// interrupted, so a conversation with none never opens its store to ask.
func (g *TaskGraph) holdsInterruptedRun() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, rows := range g.runs {
		for _, row := range rows {
			if row.State == TaskInterrupted {
				return true
			}
		}
	}
	return false
}

// setBeltRunMachineHold keeps the rail's word tied to actual refused starts.
// The engine sends an empty set after cancellation or completion so no old
// refusal can remain attached to the run's row.
func (a *Agent) setBeltRunMachineHold(run *beltRun, ids []string) {
	a.beltMu.Lock()
	if a.beltRun != run {
		a.beltMu.Unlock()
		return
	}
	held := make(map[string]bool, len(ids))
	for _, id := range ids {
		held[planStoreID(id)] = true
	}
	if len(run.machineHeld) == len(held) {
		same := true
		for id := range held {
			if !run.machineHeld[id] {
				same = false
				break
			}
		}
		if same {
			a.beltMu.Unlock()
			return
		}
	}
	run.machineHeld = held
	a.beltMu.Unlock()
	if g := a.graph(); g != nil {
		waiting := ""
		if len(held) != 0 {
			waiting = waitingMachineBusy
		}
		a.publishRunRow(g, TaskNotice{
			ID: run.row, Title: run.title, State: TaskRunning,
			StartedAt: run.born, PlanTask: planStoreID(run.root), Waiting: waiting,
		})
	}
}

// crewRunCompleter is the completer a run's seats call through: the
// conversation's account-aware view, with the routed crew's seat fallback in
// front of it when the run was routed (taskcrew.go).
func (a *Agent) crewRunCompleter(run *beltRun) Completer {
	if run == nil || run.crew == nil {
		return a.beltRunCompleter()
	}
	return crewSeatCompleter{agent: a, run: run}
}

// openBeltRunStore opens the conversation's store for a run. A NEW REQUEST GETS
// A STORE OF ITS OWN, seeded under its own root with its own words; whatever
// store it finds there is set aside first ([setAsideRunStore]). Only carryOn —
// the door that picks a named run back up ([Agent.ContinueRun]) — adopts what is
// there, and only when the store's own root is the run it was asked to carry on.
//
// A NEW REQUEST NEVER ADOPTS A RUN IT DID NOT START. This door used to adopt any
// store whose root was still open, on the reading that an open root was this
// conversation's live run and the new `/task` more of its work. But a live run
// is joined before this door is reached ([Agent.joinOrWait]), so an open root
// here is one NOTHING is driving: a run whose conversation closed, whose process
// died, or that ended on something that wrote no ending. Adopting it ran that
// run's brief under the new request's number and dropped the new words, and
// nobody was asked.
func (a *Agent) openBeltRunStore(g *TaskGraph, path, rootID, title, brief string, carryOn bool) (*planState, *plandb.Store, error) {
	// TWO RUNS NEVER SHARE A STORE PATH. Every caller holds the start lock and
	// has seen no live run ([Agent.lockBeltStart]); this is the same fact asked
	// once more where it would do the damage, because setting aside the store
	// of a run that is still driving it is what split one batch into two runs
	// writing through one path.
	a.beltMu.Lock()
	live := a.beltRun != nil
	a.beltMu.Unlock()
	if live {
		return nil, nil, errors.New("a run is already live on this conversation's plan, so a second one may not open it")
	}
	plan := &planState{path: path, chat: g.planChat()}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// NO STORE AT ALL IS NOBODY ELSE'S RUN, on either road: the run is seeded
		// under the root it was asked for, and a carried-on run reads its work
		// from the copy it was written down as working in.
		store, err := plandb.Open(path, title, rootID, title, brief, plan.chat)
		return plan, store, err
	} else if err != nil {
		return nil, nil, err
	}
	if carryOn {
		adopted, err := plandb.Open(path, "", "", "", "")
		if err != nil {
			return nil, nil, err
		}
		root := adopted.Task(adopted.RootID())
		if adopted.RootID() != rootID || root == nil || terminalStoreStatus(root.Status) {
			_ = adopted.Close()
			return nil, nil, errRunStoreGone
		}
		return plan, adopted, nil
	}
	if err := setAsideRunStore(path); err != nil {
		return nil, nil, err
	}
	store, err := plandb.Open(path, title, rootID, title, brief, plan.chat)
	return plan, store, err
}

// errRunStoreGone is the carry-on door's refusal for a run whose store is no
// longer the conversation's live one: a later request set it aside and another
// run's store is at the path now, or the run's own task has ended.
var errRunStoreGone = errors.New("this run's plan is no longer the conversation's live one, so there is nothing to carry on")

// setAsideRunStore moves the store at path beside itself under the next archive
// number, so the path is free for a fresh run and the old run stays readable
// ([planArchivePaths] is how the reading verbs find it again).
//
// A RUN NOTHING WAS DRIVING IS ARCHIVED AS INTERRUPTED, NOT AS RUNNING. Its root
// and everything still open under it are ended with the word `interrupted`
// ([plandb.Store.EndRoot]) before it is moved, because an archived store is read
// as it stands for good, and one whose rows still said running would draw work
// in flight that nothing will ever move. The word is the one its row already
// wears ([TaskInterrupted]): nothing decided anything about the work, and every
// step it took is kept. A store whose run had ended is moved as it ended.
//
// A PROGRAM'S RUN LEFT OPEN IS ENDED IN ITS OWN WORDS FIRST, at its last evidence
// of life ([endOrphanedProgramRun]): `codeaf closed while senior-dev was
// running`, never the bare word, because the program's page reads its ending
// and a program's run is never carried on.
func setAsideRunStore(path string) error {
	existing, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		return err
	}
	_, _ = endOrphanedProgramRun(existing)
	if root := existing.Task(existing.RootID()); root != nil && !terminalStoreStatus(root.Status) {
		if err := existing.EndRoot(taskWordInterrupted); err != nil {
			_ = existing.Close()
			return err
		}
	}
	if err := existing.Close(); err != nil {
		return err
	}
	return os.Rename(path, fmt.Sprintf("%s.%d", path, len(planArchivePaths(path))+1))
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
//
// THE PROGRAM IS CARRIED ACROSS HERE TOO, for the same reason and on its own
// test: only the first publish knows which program has the work
// ([TaskNotice.Program]), and a stop, a landing or a carry-on that published
// without it would take the program's badge off its row halfway through its
// life. A row that never had one — the conversation's own worker's — has
// nothing to carry.
//
// AND A ROW THAT HAS ENDED CARRIES HOW LONG IT RAN, worked out here from the
// one pair it carries ([runSpan]) so that no publisher can put a different
// figure beside the same two instants: the rail's clock, the card's span and
// the checkpoint's elapsed_ms all read it. Every row also reaches the project's
// index and, while it runs, this conversation's presence ([Agent.indexRunRow]),
// which is how the `@` list, another window and another conversation's tasks
// tool know the run is there at all.
//
// AND THE STORE TASK IS CARRIED THE SAME WAY, for the same reason: which task
// of the plan this row IS was settled when the row was minted and is true for
// its whole life, so a settle or a stop that publishes a fresh notice must not
// be able to drop it ([TaskNotice.PlanTask]). A row that lost its identity
// halfway through would send the place back to guessing by title exactly when
// the work ended, which is the moment a person goes looking for its page.
func (a *Agent) publishRunRow(g *TaskGraph, notice TaskNotice) {
	if notice.Copy == nil || notice.PlanTask == "" || notice.Crew == nil {
		for _, kept := range g.runRows(notice.ID) {
			if kept.ID != notice.ID {
				continue
			}
			if notice.Copy == nil && kept.Copy != nil {
				notice.Copy = kept.Copy
			}
			if notice.PlanTask == "" && kept.PlanTask != "" {
				notice.PlanTask = kept.PlanTask
			}
			if notice.Crew == nil && kept.Crew != nil {
				notice.Crew = kept.Crew
			}
			break
		}
	}
	notice.Program = keptRunProgram(g, notice)
	if notice.Elapsed == 0 {
		notice.Elapsed = runSpan(notice.StartedAt, notice.EndedAt)
	}
	// A SETTLED RUN'S OWN ROW SAYS WHAT IT CAME TO, the figure its index row
	// carries ([Agent.beltRunSpent]), so the landed card and every page drawn
	// from the row show the price. No book is summed from rows: the conversation's
	// total comes from the calls themselves (task_run_money.go), so this is a
	// label and never a second charge.
	if notice.State.settled() && notice.CostUSD == 0 {
		notice.CostUSD = a.beltRunSpent(notice.ID)
	}
	a.emitTaskUpdate(notice)
	g.keepRunRows(notice.ID, []TaskNotice{notice})
	a.indexRunRow(notice)
}

// keptRunProgram is the program a run row names: its own when it names one,
// and otherwise the one the row it replaces was published with
// ([Agent.publishRunRow] says why it is carried).
func keptRunProgram(g *TaskGraph, notice TaskNotice) string {
	if notice.Program != "" {
		return notice.Program
	}
	for _, kept := range g.runRows(notice.ID) {
		if kept.ID == notice.ID && kept.Program != "" {
			return kept.Program
		}
	}
	return ""
}

// cutBeltRun ends the live run because the CONVERSATION is ending. It is what
// makes a run's life the conversation's rather than the process's, and it is
// called from exactly one place ([Agent.Close]).
//
// IT IS NOT A PERSON'S STOP AND MUST NOT BE MISTAKEN FOR ONE. A stop writes the
// person's reason on the store's root and settles the row in their words
// (stoprun.go); this says nothing in the conversation, because nobody asked for
// anything — the room simply closed. What an ordinary run did is in its store,
// which is where the next launch reads it from, and its root is left open.
//
// AND THE RUN'S DRIVER IS TOLD SO BEFORE THE CONTEXT IS CUT ([beltRun.closing]),
// because what a cut context means is otherwise ambiguous to it: the engine
// answers the same unfinished word for a closed room as for any other road
// that cut it short, and the driver used to go on to land the work and settle
// the row `failed` after the conversation had gone. The record then disagreed
// with itself: the row the surface was sent said failed, and the row read back
// tomorrow said interrupted.
//
// A PROGRAM'S RUN IS ENDED IN ITS STORE FIRST, THEN CUT, the order a stop takes.
// Nothing can carry a program's run on, and the ending the run writes for itself
// comes only after the engine has answered, which on an engine being shut down
// (a signal, `codeaf engine --stop`) is after the process has gone: the store
// then said `running` for ever, and the next hand-off ran inside it. Written
// here, the ending is on disk before anything is cut. WAITING instead — holding
// Close until the run had written its own ending — was the other road, and it
// is the weaker one: it holds a person's quit for the program's grace and the
// landing behind it, and a process killed during that wait writes nothing at
// all. A crash writes nothing either way; that store is ended by the next
// process to find it ([endOrphanedProgramRun]).
func (a *Agent) cutBeltRun() {
	a.beltMu.Lock()
	run := a.beltRun
	var cut context.CancelFunc
	stopped := false
	if run != nil {
		run.closing = true
		cut, stopped = run.cut, run.stopped
	}
	a.beltMu.Unlock()
	if run != nil && run.delegate != nil && !stopped {
		// A run a person already stopped keeps the stop's ending and its words.
		// A program that has not written its record yet is named by the run.
		record := beltRunProgram(run)
		if record.Name == "" {
			record.Name = run.delegate.Name
		}
		endProgramRunClosed(run.store, record, time.Time{})
	}
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
	// THE RUN'S MONEY REACHES THE CONVERSATION'S BOOKS THROUGH ONE FOLD
	// (task_run_money.go): each call whole as a program's model API meters it,
	// and whatever the run's running total holds beyond those — a bash
	// worker's spend, which arrives only as a total.
	fold := &beltFold{agent: a}
	spec.OnSpend = fold.total
	spec.OnCharge = fold.charge
	summary := engine.Start(ctx, spec)
	// THE RUN'S WORK IS OVER THE MOMENT THE ENGINE ANSWERS, and that instant is
	// taken now, before the landing, the summary refresh and the note — which
	// can take a quarter of a minute between them and are not the work.
	//
	// AND THE RUN IS ON ITS WAY OUT FROM THAT MOMENT. Nothing will run work added
	// to its store after this line, so a hand-off arriving now waits for the run
	// to be over instead of joining it ([Agent.joinOrWait]). The run is cleared
	// off the Agent and its waiters released on every road out of here, which is
	// what the deferred release says once.
	a.beltMu.Lock()
	run.ended, run.spent = a.taskClockNow(), summary.USD
	run.ending = true
	closing := run.closing
	a.beltMu.Unlock()
	defer a.releaseBeltRun(run)
	// The final receipt closes any gap between the last live reading and every
	// ending, before the person-stop road and the ordinary landing road split.
	fold.total(summary.USD)
	// AND meta.json IS TOLD NOW, not at the next turn's seal. Home reads this
	// conversation's bill as the larger of its stamped books and its index rows
	// (tui3's homeFacts), which is exact only while the books on disk already
	// hold every run the index names. A stamp that waited for the next turn left
	// the card reading the run alone, with the conversation's own talking missing.
	a.stampSpend()
	if run.cut != nil {
		defer run.cut()
	}
	if stopped, why := a.beltRunStopped(run); stopped {
		// A RUN A PERSON STOPPED IS NOT LANDED. Its work is kept where the stop's
		// own sentence said it would be, and the ending is the stop's (stoprun.go).
		a.settleStoppedBeltRun(run, why, summary.Cut)
		a.settleTaskCrew(run.row, router.CrewNotKept, summary.USD)
		return
	}
	if closing && run.delegate == nil && summary.Outcome != beltRunOutcomeDone {
		// THE CONVERSATION CLOSED UNDER THE RUN, AND THAT IS NOBODY'S ENDING. The
		// run is not landed, its row is not settled and nothing is written on its
		// record: it is work nothing is driving any more, every step of it is in
		// its store, and the row read back tomorrow says so in the one word for
		// it ([TaskInterrupted]). Landing it here put the work into the folder of
		// a person who had closed the window on it, and settling the row said
		// `failed` about work that had not failed.
		//
		// A PROGRAM'S RUN IS THE EXCEPTION: [Agent.cutBeltRun] already wrote its
		// ending, nothing can carry it on, and its folder is finished on its own
		// branch below — never merged into the person's.
		return
	}
	var landing RunLanding
	if run.delegate != nil {
		// A PROGRAM'S RUN IS OVER WHEN ITS PROGRAM IS, however it ended: it is
		// a run of one task that nothing continues, so a store the engine left
		// open — a program ended at a limit leaves it so — is closed here, or its
		// page would read `running` and offer `stop it` for ever. A run that
		// already ended is left as it ended.
		//
		// IT IS CLOSED BEFORE THE LANDING, NOT AFTER IT. The folder's last
		// commit takes its time, and a page that went on reading `running` over
		// a program that had already exited was a page claiming a present that
		// was over — for the two limit endings alone, because every other ending
		// is written by the engine at the program's exit.
		if summary.Outcome != beltRunOutcomeDone {
			words, _ := runEndingWords(summary)
			_ = run.store.FailRoot(words)
		}
		landing = a.landDelegateRun(run, summary)
	} else {
		// EVERY OTHER ENDING IS WRITTEN ON THE RUN'S OWN TASK. The engine writes
		// the ending of a limit or a failed root worker itself; this is the same
		// write made again from the door, which the store takes once and ignores
		// after, so no engine can leave a run the next hand-off would find still
		// open.
		if summary.Outcome != beltRunOutcomeDone {
			_ = run.store.EndRoot(summary.Outcome)
		}
		landing = a.landBeltRun(ctx, engine, run)
	}
	// A LANDING GETS ONE LAST READING before its digest is composed. The call
	// owns the short beltRunSummaryDeadline: refusal, malformed output, or a
	// slow provider leaves the stored reading alone and cannot hold the run
	// beyond that bound. RefreshRunSummary itself declines without a store.
	refreshCtx, cancelRefresh := context.WithTimeout(withCrewTask(ctx, run.crew), beltRunSummaryDeadline)
	a.RefreshRunSummary(refreshCtx, run.root, time.Time{})
	cancelRefresh()
	note := beltRunOutcomeNote(run.store, run.root, summary, landing, a.beltRunSpan(run))
	if run.delegate != nil {
		note = programPageNote(programName(run.delegate), landing)
	}
	if _, err := run.store.AddNote(run.root, run.root, note); err != nil {
		if g := a.graph(); g != nil {
			g.planNote("the run's outcome note failed: " + err.Error())
		}
	}
	a.deliverBeltRunLanding(run, summary, landing)
	a.settleBeltRun(run, summary, landing)
	// THE CREW'S OUTCOME is the TASK's — was the result kept — and never the
	// route's, which the seats' first calls wrote already (taskcrew.go). A run
	// that finished and whose landing nobody refused is accepted: merged, in
	// place, KEPT on its branch on purpose (a protected branch, a clean repo
	// on main), or nothing to land because the work was an answer. Only a
	// refused, conflicted or abandoned landing is not kept. A later
	// `/redo stronger` overwrites it.
	outcome := router.CrewNotKept
	if crewKept(summary.Outcome, landing) {
		outcome = router.CrewAccepted
	}
	a.settleTaskCrew(run.row, outcome, run.crew.taskSpent(summary.USD))
}

// crewKept is whether a run's ending counts as its crew's work kept: it
// finished, and its landing was neither refused, conflicted nor abandoned.
//
// HOW THE WORK CAME HOME IS READ FIRST. A run whose work was kept on its
// branch on purpose carries the sentence naming that branch in Refused too
// ([Agent.landBeltRun] writes the road's own words there, for the outcome
// note), and that sentence is an account of a keep, not a refusal. Only a
// landing with no homecoming at all is judged by Refused: the engine's own
// refusal to land.
func crewKept(outcome string, landing RunLanding) bool {
	if outcome != beltRunOutcomeDone {
		return false
	}
	switch landing.Home {
	case mergeConflicted, mergeAborted:
		return false
	case "":
		return landing.Refused == ""
	}
	return true
}

// releaseBeltRun is the last thing every run does: it is cleared off the Agent,
// its store is closed, and every hand-off that was waiting for it to be over is
// let go to start a run of its own. The clearing comes first, so a waiter that
// wakes finds no run on the Agent and opens a fresh one rather than meeting this
// one again.
func (a *Agent) releaseBeltRun(run *beltRun) {
	a.beltMu.Lock()
	if a.beltRun == run {
		a.beltRun = nil
	}
	a.beltMu.Unlock()
	_ = run.store.Close()
	if run.over != nil {
		close(run.over)
	}
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
	landing, err := engine.Land(ctx, run.store, run.workspace, run.tree.checkBase, run.root)
	if err != nil {
		if g := a.graph(); g != nil {
			g.planNote("the run's landing failed: " + err.Error())
		}
		return RunLanding{}
	}
	return a.bringBeltRunHome(run, landing)
}

// bringBeltRunHome is the second half of a run's landing: the copy's branch
// merged into the ground it was cut from, the person's unfinished work carried
// across or the branch kept and the files named, the copy given back, and the
// homecoming written on the run's page.
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
// A LANDING SPEAKS ONLY WHEN AN ANSWER IS OWED — or when a program ended it,
// whose ending is always the conversation's to act on (program_outcome.go).
func (a *Agent) deliverBeltRunLanding(run *beltRun, summary RunSummary, landing RunLanding) {
	line := beltRunOutcomeNote(run.store, run.root, summary, landing, a.beltRunSpan(run))
	if run.delegate != nil {
		if limit := programLimitLine(run, summary, landing); limit != "" {
			a.recordProgramLimit(limit)
		}
		a.accept(delivery{origin: fromRuntime, kind: msgResult, note: a.programLandingNote(run, summary, line)})
		return
	}
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

// recordProgramLimit gives the open surface the same authored line the
// conversation keeps. The model wake may be refused by this very limit, so
// the standing lane must paint it without waiting for another turn.
func (a *Agent) recordProgramLimit(line string) {
	note := userText(line)
	note.authored = true
	a.mu.Lock()
	a.recordUserLocked(note)
	watchers := append([]*eventStream(nil), a.taskWatchers...)
	a.mu.Unlock()
	event := Event{Kind: EventNotice, Text: line}
	for _, watcher := range watchers {
		watcher.send(event)
	}
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
	notice.EndedAt = a.beltRunEndedAt(run)
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
		if notice.State != TaskDone && runEnding != "" && (cutRows[id] || cancelledByRunEnding(run.store, id)) {
			notice.Ending = runEnding
		}
		a.publishRunRow(g, notice)
	}
}

// cancelledByRunEnding answers whether a joined row's task was cancelled by the
// run's own ending rather than by a person.
//
// A JOINED ROW THE RUN'S OWN ENDING CANCELLED BEFORE IT STARTED IS THE RUN'S
// ENDING TOO, not a fault. The store ends every task still open under the
// ending's own reason ([plandb.Store.EndRoot]), so work that was waiting for a
// slot when a limit fired carried that limit's sentence with no ending to read
// it by, and drew `a fault` over a bound its person set.
func cancelledByRunEnding(store *plandb.Store, id uint64) bool {
	task := store.Task(strconv.FormatUint(id, 10))
	return task != nil && task.Status == plandb.StatusCancelled && !planStopReason(task.Error)
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
	outcome, result := runEndingWords(summary)
	report := result
	if summary.Outcome != beltRunOutcomeDone {
		if summary.Program != nil {
			// THE PROGRAM'S OWN SENTENCE LEADS, and its account follows: the
			// reason line a surface draws is the report's first line.
			report = strings.TrimSpace(outcome + "\n" + result)
		} else if report == "" {
			report = outcome
		}
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
		Ending: beltRunEnding(summary),
		Report: report, Result: summary.Result,
		Changed: landing.Changed,
		// THE CREW THAT DID IT AND WHAT IT COST, beside the estimate it was
		// picked under, for the card's crew line.
		Crew: run.crewDecision(), Model: run.crewWorker(), CostUSD: run.crew.taskSpent(summary.USD),
	}
	if state == TaskFailed && run.crew != nil {
		decision := run.crew.stoppedIfCutOff()
		notice.Crew = &decision
	}
	if run.crew != nil && !run.crew.anyStarted() {
		// A CREW NO SEAT OF WHICH EVER ANSWERED WROTE NOTHING: whatever the
		// landing counted is the run's own scaffolding, and "1 file" under a
		// run that never started claims work nobody did.
		notice.Changed = nil
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

// beltRunEnding is the run row's ending: a limit its person set, or how the
// program a delegated run was handed to ended it — a crash is the fault it is,
// and every other ending of the program's own is [TaskEndingProgram], whose
// reason is the program's sentence. Empty for every other run.
func beltRunEnding(summary RunSummary) TaskEnding {
	if ending := beltRunLimitEnding(summary.Limit); ending != "" {
		return ending
	}
	if ended := summary.Program; ended != nil && summary.Outcome != beltRunOutcomeDone {
		if ended.Status == delegate.StatusCrashed {
			return TaskEndingError
		}
		return TaskEndingProgram
	}
	return ""
}

// runEndingWords is a run's ending in the two parts every drawing of it reads:
// the one sentence, and the account under it. A program that ended its run
// unfinished speaks for itself; every other run answers the engine's outcome
// word and the root's result.
func runEndingWords(summary RunSummary) (string, string) {
	if ended := summary.Program; ended != nil && summary.Outcome != beltRunOutcomeDone {
		return strings.TrimSpace(ended.Reason), strings.TrimSpace(ended.Result)
	}
	return summary.Outcome, strings.TrimSpace(summary.Result)
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
// ended: the engine's outcome word, how long the run took, and where the work
// went, or the sentence that says why it did not. The last stored run reading
// supplies its Now sentence; without one this remains the landing digest that
// predates run summaries.
//
// THE TIME IS THE RUN'S ONE PAIR ([Agent.beltRunSpan]), said as `ran 22m 51s`
// in the page's own spelling ([runSpanWord]) and said not at all under a
// second. The same line is what the conversation is handed when the run lands,
// and a conversation told only that a run was done could not say how long it
// had taken when it was asked.
func beltRunOutcomeNote(store *plandb.Store, rootID string, summary RunSummary, landing RunLanding, span time.Duration) string {
	outcome, result := runEndingWords(summary)
	parts := []string{outcome}
	if ran := runSpanWord(span); ran != "" {
		parts = append(parts, "ran "+ran)
	}
	if result != "" {
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
//
// A PROGRAM'S LANDING SAYS ITSELF ([RunLanding.Line]): where its work is, that
// its branch is checked out in the person's folder, and the two commands that
// go back to their own branch and bring the work in. This line is the one
// account of a landing the conversation's model is given, and a model told
// only `landed on task/x: 2 files` would tell the person a thing about their
// folder that nobody checked.
func beltLandingLine(landing RunLanding) string {
	if landing.Line != "" {
		return landing.Line
	}
	if landing.Refused != "" {
		return landing.Refused
	}
	if landing.Branch == "" {
		return ""
	}
	return fmt.Sprintf("landed on %s: %s", landing.Branch, fileCount(len(landing.Changed)))
}

// fileCount is a count of files in words, `1 file` and `2 files`, so every
// landing line that counts them counts them the same way.
func fileCount(n int) string {
	if n == 1 {
		return "1 file"
	}
	return strconv.Itoa(n) + " files"
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

// crewDecision is the run's routed crew, nil when it was not routed.
func (run *beltRun) crewDecision() *crewroute.Decision {
	if run == nil || run.crew == nil {
		return nil
	}
	decision := run.crew.current()
	return &decision
}

// crewWorker is the routed worker's id, the model the run's row names; empty
// when the run was not routed.
func (run *beltRun) crewWorker() string {
	if d := run.crewDecision(); d != nil {
		return d.Seat(crewroute.Worker).Model
	}
	return ""
}

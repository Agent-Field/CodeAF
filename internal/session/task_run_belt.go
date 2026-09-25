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
// supervisor already turning. A conversation that has no live run seeds a
// fresh store under the new hand-off's own number, and the store already
// there is archived beside the session folder whatever its root says
// ([Agent.seedBeltRunStore]): a store no run in this process holds is a record,
// never more work, so a resumed conversation still reads its old plan and a
// new hand-off never runs inside it.

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
	// ended is the instant the run's engine answered, off the same clock, and
	// spent is what the engine said the run came to: both zero until the run's
	// work is over. ended is what a run with no program's clock settles at
	// ([Agent.beltRunEndedAt]), which is never the later instant its landing
	// and its summary have finished at. Both are written and read under
	// [Agent.beltMu], because a hand-off joining the run publishes from another
	// goroutine while the run is ending.
	ended time.Time
	spent float64
	// delegate is the program this run's root is handed to, nil for a run the
	// conversation's own workers drive; folder is the folder a program that
	// edits files works in, held for the run and finished when it ends
	// ([PrepareProgramFolder]), nil for every other run. It is set once, before
	// the run starts, and never written again.
	delegate *delegate.Delegate
	folder   *ProgramFolder
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
// delegated run IS a run — the store, the row and the stop road are the same —
// and a second body would be two roads that must stay in step. What differs is
// where it works: a program that edits files works in the folder itself
// (programfolder.go), where every other run gets the copy its ground ladder
// cuts.
//
// asked is the models the person asked the program to work with, resolved;
// none means the conversation's crew ([Agent.delegateCrew]).
func (a *Agent) startKnownTaskRunVia(ctx context.Context, id uint64, title, brief string, dependsOn []uint64, stand taskStand, question string, via *delegate.Delegate, asked ...string) error {
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

	folder, err := a.readyRunFolder(id, title, filepath.Dir(path), stand, via)
	if err != nil {
		return err
	}
	plan, store, err := a.seedBeltRunStore(g, path, storeID, title, brief)
	if err != nil {
		folder.abandon()
		return err
	}
	if question = strings.TrimSpace(question); question != "" {
		if _, err := store.Revise(store.RootID(), plandb.TaskPatch{Question: &question}); err != nil {
			_ = store.Close()
			folder.abandon()
			return err
		}
	}
	tree, ground := folder.tree(), canonicalPath(stand.dir)
	if folder != nil {
		// A PROGRAM'S GROUND IS THE FOLDER IT WORKS IN, which is the
		// repository's root when it was handed a folder inside one.
		ground = canonicalPath(folder.Dir)
	} else {
		tree, err = prepareTaskTreeOn(ctx, a.config.Place, a.config.Workspace, a.journalID(), id, title, stand)
		if err != nil {
			_ = store.Close()
			return err
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
		born: born, delegate: via, folder: folder, asked: asked,
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
	})

	go a.driveBeltRun(runCtx, engine, run, a.beltRunSpec(run, brief))
	return nil
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
		Place: a.config.Place, Sign: a.signsGitWork(),
	})
}

// joinBeltRun is the second task of a live run. The store holds one root, so the
// new work is a child of it — normalizeSpec's own law for a task that names no
// parent — and the supervisor already turning finds it ready on its next pass.
// Nothing opens a second store.
//
// A DELEGATE NEVER JOINS A RUN AND NOTHING JOINS A DELEGATE'S. A delegated run
// is a run of one task whose worker owns its whole folder for the hour; a
// second task beside it would be a bash worker typing in the tree the program
// is editing, and a delegate added under a live run would be a second program
// beside the first. Both are refused with what is underway, and where.
func (a *Agent) joinBeltRun(g *TaskGraph, live *beltRun, id uint64, title, brief string, dependencies []plandb.Dependency, stand taskStand, via *delegate.Delegate) error {
	if via != nil || live.delegate != nil {
		where := "in a copy of " + live.ground
		if live.folder != nil {
			where = "in " + live.ground
		}
		return errors.New("work is already underway " + where +
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
		Conversation: a.runConversation(),
		Delegate:     run.delegate,
		PlainFolder:  run.folder != nil && run.folder.Plain(),
		Crew:         a.delegateCrew(run),
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
	return delegate.Crew{
		Brain: seat(roles.TierMastermind), Hands: seat(roles.TierWorker), Light: seat(roles.TierLow),
		Asked: append([]string(nil), run.asked...),
	}
}

// seedBeltRunStore opens a fresh store for a NEW hand-off, under the hand-off's
// own number. A store already at the path is archived beside the session folder
// the way a finished one always was, and never adopted, whatever its root says.
//
// A NEW HAND-OFF ONCE ADOPTED ANY STORE WHOSE ROOT HAD NOT ENDED, and a root
// stays open whenever its process went away mid-run: codeaf quit, crashed, or
// was stopped by signal while a program worked, or an ordinary run ended on a
// limit its person set. The next `/senior-dev` in that conversation then ran
// inside the dead run's store: measured on 2026-09-24, a CSSTree run was handed
// the earlier happy-dom run's brief, its calls, spend, ceiling and ending were
// written into the happy-dom task's record folder, the happy-dom page came to
// read `stopped · $2.38 of $1.24 · 277 calls · 1h 7m` over a run that had
// failed after 29 minutes, and the CSSTree task had no page at all. This door
// is only ever reached with no run live in this process ([Agent.beltRun] is
// asked first), so any store it finds is a record, and a record is archived.
//
// A PROGRAM'S RUN LEFT OPEN IS ENDED BEFORE IT IS ARCHIVED, at its last evidence
// of life ([endOrphanedProgramRun]), because nothing can ever carry a program's
// run on and its page would read `running` for ever. An ordinary run left open
// is archived INTACT: its store is the record of what it did, and ending it
// here would write a fault over work a limit its person set had paused.
func (a *Agent) seedBeltRunStore(g *TaskGraph, path, rootID, title, brief string) (*planState, *plandb.Store, error) {
	plan := &planState{path: path, chat: g.planChat()}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		store, err := plandb.Open(path, title, rootID, title, brief, plan.chat)
		return plan, store, err
	} else if err != nil {
		return nil, nil, err
	}
	old, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		return nil, nil, err
	}
	_, _ = endOrphanedProgramRun(old)
	_ = old.Close()
	archived := fmt.Sprintf("%s.%d", path, len(planArchivePaths(path))+1)
	if err := os.Rename(path, archived); err != nil {
		return nil, nil, err
	}
	store, err := plandb.Open(path, title, rootID, title, brief, plan.chat)
	return plan, store, err
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

// openBeltRunStore opens the conversation's store for a run that is CARRIED ON
// ([Agent.ContinueRun]), adopting the store already there. It is [planSeed]'s own
// road stated for that door: a store whose root has ended is archived beside
// the session folder and a fresh one seeded, because a finished plan is not a
// live one; a store still running is adopted, because the run being carried on
// is the one it holds. A new hand-off never comes here ([Agent.seedBeltRunStore]).
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
func (a *Agent) publishRunRow(g *TaskGraph, notice TaskNotice) {
	if notice.Copy == nil {
		for _, kept := range g.runRows(notice.ID) {
			if kept.ID == notice.ID && kept.Copy != nil {
				notice.Copy = kept.Copy
				break
			}
		}
	}
	if notice.Program == "" {
		for _, kept := range g.runRows(notice.ID) {
			if kept.ID == notice.ID && kept.Program != "" {
				notice.Program = kept.Program
				break
			}
		}
	}
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
	a.beltMu.Lock()
	run.ended, run.spent = a.taskClockNow(), summary.USD
	a.beltMu.Unlock()
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
		landing = a.landBeltRun(ctx, engine, run)
	}
	// A LANDING GETS ONE LAST READING before its digest is composed. The call
	// owns the short beltRunSummaryDeadline: refusal, malformed output, or a
	// slow provider leaves the stored reading alone and cannot hold the run
	// beyond that bound. RefreshRunSummary itself declines without a store.
	refreshCtx, cancelRefresh := context.WithTimeout(ctx, beltRunSummaryDeadline)
	a.RefreshRunSummary(refreshCtx, run.root, time.Time{})
	cancelRefresh()
	if _, err := run.store.AddNote(run.root, run.root, beltRunOutcomeNote(run.store, run.root, summary, landing, a.beltRunSpan(run))); err != nil {
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
// A LANDING SPEAKS ONLY WHEN AN ANSWER IS OWED.
func (a *Agent) deliverBeltRunLanding(run *beltRun, summary RunSummary, landing RunLanding) {
	line := beltRunOutcomeNote(run.store, run.root, summary, landing, a.beltRunSpan(run))
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

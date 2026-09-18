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
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/roles"
)

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
	// Slots is how many workers run at once, and CostUSD is what the whole run
	// may spend. Both are the conversation's own numbers (TaskParallel and
	// SpendRailUSD), so a run costs what the conversation costs and runs as
	// wide as the conversation may.
	Slots   int
	CostUSD float64
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
}

// RunSummary is what a run came to, folded onto the words this package reads:
// the engine's outcome word, the root's result, and the run's size.
type RunSummary struct {
	Outcome string
	Result  string
	Nodes   int
	Steps   int
	USD     float64
}

// RunLanding is what the run's landing answered: the branch the working copy's
// work was committed on, the paths that commit carried, and the sentence saying
// why it refused. A landing names a branch or says what stopped it.
type RunLanding struct {
	Branch  string
	Changed []string
	Refused string
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
}

// startTaskRun is StartTask's second road, taken whenever the bash belt is asked
// for and a run engine is linked. It seeds or reuses the conversation's store,
// adds this brief's work to it, publishes the row a surface draws, and starts
// the engine in a goroutine the moment the run is new. Every refusal falls back
// to the legacy road rather than inventing a sentence of its own, so a
// conversation the run road cannot serve gets exactly the door it always had.
func (a *Agent) startTaskRun(ctx context.Context, brief string, solo bool) (uint64, string, string, error) {
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
	storeID := strconv.FormatUint(id, 10)

	a.beltMu.Lock()
	live := a.beltRun
	a.beltMu.Unlock()

	// A SECOND TASK JOINS THE LIVE RUN. The store holds one root, so the new
	// work is a child of it — normalizeSpec's own law for a task that names no
	// parent — and the supervisor already turning finds it ready on its next
	// pass. Nothing opens a second store.
	if live != nil {
		if _, err := live.store.AddMany([]plandb.TaskSpec{{
			ID: storeID, ParentID: live.root, Title: title, Description: brief,
		}}); err != nil {
			return a.startTaskLegacy(ctx, brief, solo)
		}
		a.publishRunRow(g, TaskNotice{ID: id, Title: title, State: TaskRunning, Parent: live.row, StartedAt: time.Now()})
		return id, title, "", nil
	}

	plan, store, err := a.openBeltRunStore(g, path, storeID, title, brief)
	if err != nil {
		return a.startTaskLegacy(ctx, brief, solo)
	}
	run := &beltRun{plan: plan, store: store, root: store.RootID(), row: id, title: title}
	a.installBeltRun(g, run)
	a.publishRunRow(g, TaskNotice{ID: id, Title: title, State: TaskRunning, StartedAt: time.Now()})

	// THE CONVERSATION'S OWN SEATS, read off its role ladder so the engine's
	// crew factory seats the work and plan roles on what this conversation's
	// planner and worker calls already resolve through, rather than asking the
	// profile again for a row the conversation's crew has moved.
	source := roles.Source(a.config.RolesSource)
	workSeat, _ := roles.TierModel(source, roles.TierWorker)
	planSeat, _ := roles.TierModel(source, roles.TierMastermind)

	spec := RunSpec{
		Store:     store,
		Workspace: a.config.Workspace,
		Title:     title,
		Brief:     brief,
		Slots:     a.config.TaskParallel,
		CostUSD:   a.config.SpendRailUSD,
		// The step cap a node of this session's own tree carries, so a run
		// worker and a node worker stop at the same figure.
		StepsPerTask: taskMaxSteps,
		ProfileDir:   a.config.ProfileDir,
		WorkModel:    workSeat,
		PlanModel:    planSeat,
		CompleterFor: func(string) Completer { return a.beltRunCompleter() },
	}
	go a.driveBeltRun(ctx, engine, run, spec)
	return id, title, "", nil
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
	for suffix := 1; ; suffix++ {
		archived := fmt.Sprintf("%s.%d", path, suffix)
		if _, err := os.Stat(archived); os.IsNotExist(err) {
			if err := os.Rename(path, archived); err != nil {
				return nil, nil, err
			}
			break
		}
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
func (a *Agent) publishRunRow(g *TaskGraph, notice TaskNotice) {
	a.emitTaskUpdate(notice)
	g.keepRunRows(notice.ID, []TaskNotice{notice})
}

// driveBeltRun runs one run to its outcome and writes the ending back where the
// conversation reads it: the store's root carries the outcome and the landing,
// the person's conversation is told with the same note a landed task sends, and
// the row the run was published under settles. The store is closed and the run
// cleared once the work is home, so the next `/task` seeds a fresh plan.
func (a *Agent) driveBeltRun(ctx context.Context, engine RunEngine, run *beltRun, spec RunSpec) {
	summary := engine.Start(ctx, spec)
	landing, err := engine.Land(ctx, run.store, spec.Workspace, run.root)
	if err != nil {
		if g := a.graph(); g != nil {
			g.planNote("the run's landing failed: " + err.Error())
		}
		landing = RunLanding{}
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

// deliverBeltRunLanding wakes the conversation with the note a landed task
// sends, composed for a run: the outcome word in place of a tier word, the
// result the root reported, and where the work went. It is the run's own voice
// ([fromRuntime]), so a person reads it as the session's news and not as
// something they typed.
func (a *Agent) deliverBeltRunLanding(run *beltRun, summary RunSummary, landing RunLanding) {
	notice := a.beltRunNotice(run, summary, landing)
	line := landingNoteLead(notice) + taskNote(notice, "", a.settlePolicy(), a.addressLanding(notice))
	a.accept(delivery{origin: fromRuntime, kind: msgResult, note: wakeNote(line)})
}

// settleBeltRun ends the row the run was published under: done when the run
// finished whole, failed on every other ending, with the result and the branch
// a surface draws.
func (a *Agent) settleBeltRun(run *beltRun, summary RunSummary, landing RunLanding) {
	notice := a.beltRunNotice(run, summary, landing)
	notice.EndedAt = time.Now()
	a.emitTaskUpdate(notice)
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
	if line := beltLandingLine(landing); line != "" {
		if report != "" {
			report += "\n"
		}
		report += line
	}
	notice := TaskNotice{
		ID: run.row, Title: run.title, State: state,
		Report: report, Result: summary.Result,
		Changed: landing.Changed,
	}
	if landing.Branch != "" {
		notice.Branch = landing.Branch
		notice.Merge = mergeKept
	}
	return notice
}

// beltRunOutcomeNote is the one line a run's own page carries about how it
// ended: the engine's outcome word and where the work went, or the sentence that
// says why it did not. The last stored run reading supplies its Now sentence;
// without one this remains the landing digest that predates run summaries.
func beltRunOutcomeNote(store *plandb.Store, rootID string, summary RunSummary, landing RunLanding) string {
	line := beltLandingLine(landing)
	if line == "" {
		line = summary.Outcome
	} else {
		line = summary.Outcome + " · " + line
	}
	if stored, ok := readRunSummary(store, rootID); ok {
		if now := strings.TrimSpace(stored.Summary.Now); now != "" {
			line += " · " + now
		}
	}
	return line
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

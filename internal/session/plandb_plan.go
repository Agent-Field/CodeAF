package session

// The plan side of the bash belt (docs/design/plandb-cli/DESIGN.md, the
// wiring section): the store the worker's `plandb` calls write, and the two
// pulse points that make the graph and the store one thing. THE STORE IS THE
// WORKER'S CLI's STORE and this file's store at once — one SQLite database in
// the session folder, found by both roads the same way (the runtime by path, the
// CLI by walking up from its own working directory) — so a task the model
// adds through bash is a task the runtime dispatches, and a node that lands
// is a task the plan says is done.
//
// LOCK ORDER, stated once because everything here depends on it: a pulse may
// hold the plan gate while taking the graph's mu (claimChild and admit take
// it internally), and nothing may hold the graph's mu while asking for the
// plan gate. Every pulse point — after a worker's bash call, at the end of its
// turn, when a fan slot goes back, and after a node lands — is called from
// code that holds neither, and the seed takes the plan gate BEFORE admit takes
// the graph's mu, never inside it.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// planState is one run's plan: where the store lives, and whether the bin
// shim has been armed. Nothing else is kept here — the node-to-task mapping
// lives on the nodes themselves (taskSpec.planID, checkpointed), because a
// map on the side that disagreed with its nodes would be a second truth.
type planState struct {
	mu   sync.Mutex
	path string
	// chat is the conversation's tag: the session folder's own name, stamped on
	// every row the seed makes so the plan can be read back as this chat's
	// (PlanTasks). It is settled with the path at the seed and never moves.
	chat string
	// root is the run a worker's plan belongs to, set only on a run worker's
	// own plan ([NewBeltWorker]) from the run's open handle. It is what the
	// worker's `plandb` checks the store at path against ([plandb.RunEnv]), so
	// a later store at the same path cannot take the worker's writes.
	root    string
	shimmed bool
	// archives holds read handles for ended stores. Ended stores are immutable,
	// so each is opened at most once for the life of this conversation.
	archives map[string]*plandb.Store
}

// planStoreFilename is the file name every road agrees on: the runtime's
// path helper, the CLI's walk-up, and the store's own creation all spell it
// the same way.
const (
	planStoreFilename  = "plandb.db"
	planShimFilename   = "plandb"
	codeafShimFilename = "codeaf"
)

// planRootID is the store's root task. The reference loop's supervisor seeds
// a root named t-root; the store trims the prefix, so the stored id is the
// bare word and the CLI prints t-root.
const planRootID = "root"

// planNodeSnapshot is what one pass reads about a plan-born node before any
// store work: the plan id it carries, where it stands, and the words it ended
// with. Read in one short hold of the graph's lock, so the pass never holds
// that lock across a rename.
type planNodeSnapshot struct {
	nodeID uint64
	planID string
	state  TaskState
	report string
	ended  TaskEnding
	depth  int
	spec   taskSpec
}

// planIfArmed answers the run's plan, or nil when this session is not
// running the bash-belt experiment or the store is not there. Nil-safe by
// design: every pulse call site is on a road any node may take, plan or not.
func (g *TaskGraph) planIfArmed() *planState {
	if !bashBeltAsked() {
		return nil
	}
	g.planMu.Lock()
	defer g.planMu.Unlock()
	if g.plan == nil {
		// A REOPENED CONVERSATION FINDS THE RUN IT LEFT. The plan was armed only
		// by the `/task` that seeded it, in the process that seeded it, so a
		// conversation closed and reopened drew no run while its store sat in the
		// session folder holding every row. The store is the memory: where the
		// folder already holds one, this conversation's plan is that file. No
		// store is ever MADE here — a conversation that never ran a task still
		// answers nil — and the shim is armed by the road that next runs a
		// worker, which is the only reader of it.
		if path := g.planPath(); path != "" {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				g.plan = &planState{path: path, chat: g.planChat()}
			}
		}
	}
	return g.plan
}

// planPath resolves where this run's store lives: the session folder, or —
// for the legacy flat layout, whose Place is zero — the workspace's .codeaf
// folder. The CLI finds the same file by walking up from the worker's own
// working directory, which is inside this session's tree folder, so the two
// roads cannot disagree about which plan a worker is driving.
func (g *TaskGraph) planPath() string {
	if g.home == nil {
		return ""
	}
	place := g.home.config.Place
	if place.Dir != "" {
		return filepath.Join(place.Dir, planStoreFilename)
	}
	if g.home.config.Workspace != "" {
		return PlanStorePath(g.home.config.Workspace)
	}
	return ""
}

// PlanStorePath is the flat-layout fallback for a session with no Place:
// <dir>/.codeaf/plandb.db. A new headless run uses [OpenRunPlanAt] in a
// private folder instead; this path remains for the session fallback.
func PlanStorePath(dir string) string {
	return filepath.Join(dir, ".codeaf", planStoreFilename)
}

// OpenRunPlan opens the older flat-layout plan path, seeded with the run's words.
// The headless do door now uses [OpenRunPlanAt] in its private folder. A store
// already at that path is SET ASIDE beside it first ([setAsideRunStore]) — a
// finished one as it ended, and one nothing is driving as interrupted — and a
// fresh one is seeded, so a second errand in one project is a second run rather
// than a reader of the first one's ending. The store is the caller's to close.
//
// A NEW ERRAND NEVER ADOPTS A RUN IT DID NOT START. This door used to adopt a
// store whose root was still open, on the reading that an open root was a live
// run to resume. Nothing resumes through this door: every call carries a new
// request's words, and a store left open by a run that was interrupted — a
// timeout, an interrupt, a process that died — was run again under its old
// title and brief while the new request was dropped. Measured on this door: a
// directory holding a left-open store answered `status=running title="rename
// the logger"` for a request about something else entirely.
func OpenRunPlan(dir, title, brief string) (*plandb.Store, error) {
	path := PlanStorePath(dir)
	if _, err := os.Stat(path); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		return plandb.Open(path, title, planRootID, title, brief)
	}
	if err := setAsideRunStore(path); err != nil {
		return nil, err
	}
	return plandb.Open(path, title, planRootID, title, brief)
}

// OpenRunPlanAt seeds one run at an explicit store path. A headless run puts
// this path in its private home, leaving the working copy for the work alone.
func OpenRunPlanAt(path, title, brief string) (*plandb.Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return plandb.Open(path, title, planRootID, title, brief)
}

// planChat is the conversation's tag — the id every row the seed makes carries,
// and the one the reading verbs narrow the plan by (planTaskRow). It is the
// session folder's own name, read off the same Place planPath reads and with no
// lock, the way every other config read in the seed is; a session with no
// folder tags nothing, and its plan is read whole.
func (g *TaskGraph) planChat() string {
	if g.home == nil {
		return ""
	}
	return g.home.config.Place.ID()
}

// planSeed is the wiring point the design names: the one door every task
// passes, before the node is built, so the work order the worker eventually
// reads was composed FROM the store rather than pasted beside it. Under the
// switch, the first ordinary task seeds the store with itself as the root;
// every later ordinary task becomes a plan child of that root. Quick, design
// and run nodes are not store-driven — their middles are not the loop's.
//
// It takes the plan gate and nothing else, so admit's own locking underneath
// is untouched.
func (g *TaskGraph) planSeed(spec *taskSpec) {
	if !bashBeltAsked() || spec.kind() != "" {
		return
	}
	g.planMu.Lock()
	defer g.planMu.Unlock()
	if g.plan == nil {
		path := g.planPath()
		if path == "" {
			return
		}
		created := false
		var store *plandb.Store
		for {
			if _, err := os.Stat(path); err != nil {
				if !os.IsNotExist(err) {
					return
				}
				store, err = plandb.Open(path, spec.title, planRootID, spec.title, spec.brief, g.planChat())
				if err != nil {
					// A store that will not open is a run without a plan, and a
					// run without a plan is the belt it was before this
					// experiment: the node runs on its brief alone. The reason
					// goes in the session's own log rather than quietly.
					g.planNote("plan store unavailable, tasks run without one: " + err.Error())
					return
				}
				created = true
				break
			}
			// ADOPT: the store is the run's because the session folder is the
			// run's — a resumed run, or a second task under a plan the first
			// one seeded. The run's project is the first task's title, and a
			// later task has its own, so the adopt demands the root id and
			// nothing else; demanding the new node's title as the project would
			// refuse every differently-named task a run legitimately holds.
			adopted, err := plandb.Open(path, "", planRootID, "", "")
			if err != nil {
				g.planNote("plan store unavailable, tasks run without one: " + err.Error())
				return
			}
			if root := adopted.Task(planRootID); root != nil && !terminalStoreStatus(root.Status) {
				store = adopted
				break
			}
			// A FINISHED PLAN IS NOT A LIVE ONE. A run whose root has completed
			// is over — the reference loop is one store per run — and a new
			// task in the same conversation is a new run, not a child of a
			// done root. The finished plan is archived beside the session with
			// its own number and a fresh store is seeded; the walk-up and the
			// runtime both keep finding the live file at the one name.
			for suffix := 1; ; suffix++ {
				archived := fmt.Sprintf("%s.%d", path, suffix)
				if _, err := os.Stat(archived); os.IsNotExist(err) {
					if err := os.Rename(path, archived); err != nil {
						g.planNote("plan store archive failed, tasks run without one: " + err.Error())
						return
					}
					break
				}
			}
		}
		// The seed's own handle is done once the brief is composed from it; a
		// store opened per pass must be closed, or every pass would leave a
		// database connection behind.
		defer store.Close()
		g.plan = &planState{path: path, chat: g.planChat()}
		if err := g.plan.armShim(); err != nil {
			// A plan whose shim never landed is still the run's plan — the
			// store is seeded and the runtime dispatches from it — but every
			// plandb call the worker makes will miss its binary, and the
			// worker reads that failure. The reason goes in the session's own
			// log rather than being swallowed, which is the one shape this
			// line must never take.
			g.planNote("plandb shim not armed: " + err.Error())
		}
		if created {
			// THE SEED: the contract went INTO the store as the root task's
			// description, and the brief the worker will read is composed
			// back FROM that store read — which is what makes the store the
			// source and not a copy.
			spec.planID = planRootID
			spec.brief = planBrief(store.Task(planRootID), planRootID, planIsRoot)
			return
		}
	}
	store := g.plan.open()
	if store == nil {
		return
	}
	defer store.Close()
	// A NODE THE CHECKPOINT ALREADY NAMED: a resumed run's node knows its
	// plan task, and its brief is re-composed from the store read rather than
	// added again — adding would mint a second task for work one task already
	// describes.
	if spec.planID != "" {
		if task := store.Task(spec.planID); task != nil {
			role := planIsTask
			if spec.planID == planRootID {
				role = planIsRoot
			}
			spec.brief = planBrief(task, spec.planID, role)
		}
		return
	}
	// A LATER ORDINARY TASK: a plan child of the root, claimed under its own
	// id the way every dispatched task is, its brief composed from the store
	// read the same way.
	created, err := store.AddMany([]plandb.TaskSpec{{
		ID: store.NextID(), Title: spec.title, Description: spec.brief, ParentID: planRootID,
	}})
	if err != nil || len(created) == 0 {
		return
	}
	if _, err := store.Claim(created[0].ID, created[0].ID); err != nil {
		return
	}
	spec.planID = created[0].ID
	spec.brief = planBrief(created[0], created[0].ID, planIsTask)
}

// open re-opens the store from disk. THE RE-OPEN IS THE POINT: a store
// handle's memory is only as fresh as its last transaction, and the worker's
// CLI is a separate process that has been writing since — every pass reads
// the store, never a cached copy. A nil answer is a pass with no plan; when
// the answer is a store the caller closes it, because every pass opens one.
func (p *planState) open() *plandb.Store {
	// THE STORE IS ADOPTED BY ITS PATH AND NOT BY ITS ROOT, because a run seeded
	// through the chat's task door names its root with the id the door answered
	// the person (task_run_belt.go), while the legacy seed's root is [planRootID]
	// — and this reading must serve both. Demanding the legacy root here would
	// make [Agent.PlanTasks] blind to a run's own store.
	store, err := plandb.Open(p.path, "", "", "", "")
	if err != nil {
		return nil
	}
	return store
}

// planNote leaves one engine-side line in the session's own log directory,
// where the transcript already lives. There is no other honest channel for a
// note nobody asked a question about.
func (g *TaskGraph) planNote(line string) {
	dir := g.home.logDirectory()
	if dir == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "plandb.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line + "\n")
}

// planPulse is one pass: write settled nodes back to the store, dispatch
// what became ready, and end the run's root when the whole tree has. It is
// called from the four moments the plan can have moved and from nowhere else
// — after a bash-belt worker's bash call, at the end of such a worker's turn,
// when a fan slot goes back, and on every road a node lands by; there is no
// timer and no polling, because a pass that reads a plan nobody has touched is
// work the harness does for nothing.
//
// THE THREE MOMENTS BESIDE THE LANDING ARE THE THREE WAYS A TASK CAN BECOME
// DELIVERABLE WITH NO BASH CALL IN FRONT OF IT: the store grew while the
// worker that owns the parent was between calls, a worker's turn ended and the
// pass after its last call had already run, and a proposal that came to
// nothing handed a fan slot back. A ready task that waits for a pass that
// never comes is not a delay: the run can end first, and its ending cancels
// what nothing delivered.
func (g *TaskGraph) planPulse() {
	plan := g.planIfArmed()
	if plan == nil {
		return
	}
	plan.mu.Lock()
	defer plan.mu.Unlock()
	store := plan.open()
	if store == nil {
		return
	}
	defer store.Close()
	// THE SNAPSHOT: one short hold of the graph's lock to read what the
	// nodes say, released before any store work.
	snapshot := make(map[string]*planNodeSnapshot, len(g.nodes))
	g.mu.Lock()
	for _, node := range g.nodes {
		if node.spec.planID == "" {
			continue
		}
		snapshot[node.spec.planID] = &planNodeSnapshot{
			nodeID: node.id, planID: node.spec.planID, state: node.state,
			report: node.report, ended: node.endingLocked(),
			depth: node.depth, spec: node.spec,
		}
	}
	g.mu.Unlock()
	seed := snapshot[planRootID]
	if seed == nil {
		return
	}

	// WRITEBACK: a settled node's ending becomes the store task's ending,
	// completed AS THE TASK'S OWN claimed_by — the agent the dispatch claimed
	// it under — so a worker that finished its own task first is never written
	// over, and a node the person stopped reads as a cancelled task. THE ROOT
	// IS WRITTEN BY COMPLETION, not by writeback: the run's own task stays
	// running in the store until the whole tree has settled, because a root
	// marked done while its children still work would be a plan that says the
	// run is over while it is not.
	for planID, node := range snapshot {
		if node.planID == planRootID || node.state == TaskQueued || node.state == TaskRunning {
			continue
		}
		task := store.Task(planID)
		if task == nil {
			continue
		}
		// AN AUTO-COMPLETED PLACEHOLDER IS NOT AN ENDING. A composite parent
		// whose children all finished is auto-completed by the store with an
		// empty result while its own node may still be working; when that node
		// lands, its report is the truth and the placeholder is overwritable.
		// A task with words in it — the worker's own done, or a real ending —
		// is never written over.
		if terminalStoreStatus(task.Status) && planStoreTaskSaysSomething(task) {
			continue
		}
		planSettleStoreTask(store, task, node)
	}
	// A PARENT THAT LANDED closes its depth-floor children: a store child of
	// a node already at the tree's depth limit can never be dispatched, and
	// leaving it ready would be a plan that lies about work it will never
	// hand out.
	for planID, node := range snapshot {
		if node.state == TaskQueued || node.state == TaskRunning || node.depth < taskDepthLimit {
			continue
		}
		for _, child := range store.Tasks() {
			if child.ParentID == planID && child.ID != store.RootID() && !terminalStoreStatus(child.Status) && snapshot[child.ID] == nil {
				_, _ = store.Cancel(child.ID, "its parent "+planStoreID(planID)+" landed with the tree at its depth limit")
			}
		}
	}

	// DISPATCH: every ready store task with no node becomes one, through the
	// graph's own door, with the store task claimed under its own id — the
	// reference supervisor's trick, and what makes the worker's
	// `plandb done --agent <id>` the ownership check. A fan-cap refusal or a
	// missing parent leaves the task in the store for the next pass. Whether
	// this pass handed ANY out is kept, because the root-completion step below
	// reads the same pass and must not end a run a node was just admitted to.
	handedOut := false
	for _, task := range store.Tasks() {
		if task.Composite || snapshot[task.ID] != nil {
			continue
		}
		switch task.Status {
		case plandb.StatusReady:
		case plandb.StatusClaimed, plandb.StatusRunning:
			// A task claimed through the CLI's own `go` is work somebody
			// asked for by hand: take it back under the task's own id, which
			// is the runtime's claim, and hand it to a node like any other.
			if task.ClaimedBy == task.ID {
				continue
			}
			if _, err := store.Release(task.ID, task.ClaimedBy); err != nil {
				continue
			}
			if _, err := store.Claim(task.ID, task.ID); err != nil {
				continue
			}
		default:
			continue
		}
		parent := snapshot[task.ParentID]
		if parent == nil || parent.state == TaskQueued || parent.depth+1 > taskDepthLimit {
			continue
		}
		if refused := g.claimChild(parent.nodeID); refused != "" {
			continue
		}
		spec := taskSpec{
			title:  task.Title,
			named:  true,
			brief:  planBrief(store.Task(task.ID), task.ID, planIsTask),
			parent: parent.nodeID,
			depth:  parent.depth + 1,
			// The person's own words travel down the family the way they do
			// for codeaf's own sub-tasks; the ground and mode are the parent's
			// work, and the model is whatever the parent runs.
			request: parent.spec.request,
			origin:  parent.spec.origin,
			ground:  parent.spec.ground,
			mode:    parent.spec.mode,
			owner:   parent.spec.owner,
			planID:  task.ID,
		}
		g.admit(g.reserve(), spec)
		// THE CLAIM IS THE DISPATCH'S OWN HALF. The node exists; the store task
		// is claimed under its own id from here on — the reference supervisor's
		// trick, and what makes the worker's `plandb done --agent <id>` the
		// ownership check that passes. Without it a dispatched task stood ready
		// and unclaimed, and the taught finish was refused until the landing.
		if _, err := store.Claim(task.ID, task.ID); err != nil {
			g.planNote("dispatch claim failed for " + planStoreID(task.ID) + ": " + err.Error())
		}
		handedOut = true
	}

	// ROOT COMPLETION: the run is over when the seeding node has landed and
	// no plan-born node is open. The root's own ending is the run's word, and
	// whatever was left undelivered is cancelled with a plain reason rather
	// than left pending forever.
	if seed.state == TaskQueued || seed.state == TaskRunning {
		return
	}
	for _, node := range snapshot {
		if node.planID != planRootID && (node.state == TaskQueued || node.state == TaskRunning) {
			return
		}
	}
	// AND A PASS THAT HANDED WORK OUT IS NOT THE RUN'S END. Every reader below
	// reads the snapshot taken at the top of this pass, so the nodes the
	// dispatch above just admitted are invisible to it: left to the snapshot,
	// this pass would cancel the ready tasks whose one remaining deliverer is
	// the worker it just made — a leaf whose parent has no node YET is exactly
	// what a fresh node is about to become — and complete a root whose tree is
	// still growing. The pass's own two facts answer the question instead:
	// nothing open in the snapshot, and nothing handed out here. The next pass
	// is guaranteed, because a node this pass admitted lands, and every landing
	// is a pass.
	if handedOut {
		return
	}
	for _, task := range store.Tasks() {
		if task.ID == store.RootID() || terminalStoreStatus(task.Status) || snapshot[task.ID] != nil {
			continue
		}
		_, _ = store.Cancel(task.ID, "the run has ended and nothing will deliver this task")
	}
	word := seed.report
	if word == "" && seed.ended != "" {
		word = string(seed.ended)
	}
	_ = store.CompleteRoot(word)
}

// planSettleStoreTask writes one settled node's ending into its store task,
// under the task's own claimed agent. Done, failed and cancelled are the
// store's three endings; a node the auditor could not judge is written as
// failed with that fact as the reason, because the store has no fourth word
// and leaving the task running would be a plan that waits forever on a run
// that is over.
func planSettleStoreTask(store *plandb.Store, task *plandb.Task, node *planNodeSnapshot) {
	agent := task.ClaimedBy
	if agent == "" {
		if _, err := store.Claim(task.ID, task.ID); err != nil {
			return
		}
		agent = task.ID
	}
	switch node.state {
	case TaskDone:
		_, _ = store.Done(task.ID, agent, node.report, nil, nil)
	case TaskFailed, TaskUnverified:
		word := node.report
		if word == "" {
			word = string(node.ended)
		}
		_, _ = store.Fail(task.ID, agent, word)
	default:
		word := string(node.ended)
		if word == "" {
			word = node.report
		}
		_, _ = store.Cancel(task.ID, word)
	}
}

// planStopLandedChildren is the landing road's stop for a node that seeded or
// drove a plan. A PLAN-BORN CHILD IS THE PLAN'S TO END, not the landing node's:
// the pulse completes the run's root only once no plan-born node is open, so a
// child cut here would be a child the run still expects — the orphan the
// landing pulse was placed after stopChildren to avoid. Everything else stops
// exactly as it did.
func (g *TaskGraph) planStopLandedChildren(parent uint64) {
	for _, kid := range g.children(parent) {
		if kid.spec.planID != "" || kid.stateNow().settled() {
			continue
		}
		_, _ = g.stop(kid.id)
	}
}

func terminalStoreStatus(status plandb.Status) bool {
	return status == plandb.StatusDone || status == plandb.StatusFailed || status == plandb.StatusCancelled
}

// planStoreTaskSaysSomething answers whether a terminal task carries a real
// ending rather than the store's own empty auto-completion placeholder. A
// composite parent auto-completed with an empty result is bookkeeping, not a
// report, and its node's landing may still fill it.
func planStoreTaskSaysSomething(task *plandb.Task) bool {
	switch task.Status {
	case plandb.StatusDone:
		return strings.TrimSpace(task.Result) != ""
	case plandb.StatusFailed, plandb.StatusCancelled:
		return strings.TrimSpace(task.Error) != ""
	default:
		return false
	}
}

// planStoreID spells a plan task the way the CLI prints it, for sentences a
// model or a person reads.
func planStoreID(id string) string {
	return "t-" + id
}

type planRole int

const (
	planIsRoot planRole = iota
	planIsTask
)

// planBrief composes the plan section of a node's work order FROM the store
// read: the task's description first — the work order the doctrine says to
// write — then the per-node facts the finish command needs. The CLI's name
// is `plandb` because the session's bin shim is written before any worker
// runs, and nothing here names a binary that is not there.
func planBrief(task *plandb.Task, agent string, role planRole) string {
	if task == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(task.Description))
	b.WriteString("\n\nYOUR TASK IN THE PLAN IS ")
	b.WriteString(planStoreID(task.ID))
	b.WriteString(", claimed by agent ")
	b.WriteString(agent)
	b.WriteString(".\n")
	if role == planIsRoot {
		b.WriteString("- Finish the run with: plandb done ")
		b.WriteString(planStoreID(task.ID))
		b.WriteString(" --agent ")
		b.WriteString(agent)
		b.WriteString(" --result 'what the run did and what it changed' — the root's own worker writes it, after every child has landed and the work holds. A reply that runs nothing does not end it.\n")
	} else {
		b.WriteString("- Finish it with: plandb done --agent ")
		b.WriteString(agent)
		b.WriteString(" --result 'what you did and what it changed' — after the work holds, and never before.\n")
	}
	b.WriteString("- If you are blocked on another task, park with: plandb wait --agent ")
	b.WriteString(agent)
	b.WriteString(" — the runtime runs you again, with what changed, once a dependency or a child moves.\n")
	b.WriteString("- Coordinate through the plan CLI: plandb add, plandb split, plandb task note, plandb task overview (the page lists them all).\n")
	b.WriteString("- Dispatch is automatic: every ready task you create gets a worker. Never run the lifecycle verbs (claim, start, go, fail, pause, approve) — the runtime owns them.\n")
	return b.String()
}

// armShim writes the session's `plandb` shim, once. THE SHIM IS WHY THE PAGE
// CAN SAY `plandb` PLAINLY: the plandb that lives on this machine's PATH is a
// different store, and a worker that reached it would write a plan the runtime
// could not read. The shim execs the CLI the resolver reached
// (resolvePlanCLI), so the page's one word always names the CLI this run
// shares. It does NOT touch the process environment: the shim's directory
// travels to the commands that need it as a prefix on each command string
// ([planBashPrefix]), because a process PATH is shared by every session in
// this process — one that grew by a directory per session would never shrink,
// would leak into conversations that never asked for a plan, and would keep
// pointing at a directory a sweep could take away.
//
// IT ANSWERS WHY IT COULD NOT, and the seed records that in the session's own
// log (planNote): a plan whose shim never landed is a plan whose workers miss
// every plandb call, and the one thing worse than that is missing it silently
// — which is what the first shape did with every error it met.
func (p *planState) armShim() error {
	if p.shimmed {
		return nil
	}
	dir := filepath.Dir(p.path)
	argv := resolvePlanCLI(dir)
	if argv == nil {
		return errors.New("no plandb CLI found: " + planCLIBinEnv + " unset, and neither the running binary nor a codeaf on PATH answered `plandb status`")
	}
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		return err
	}
	words := make([]string, 0, len(argv)+1)
	for _, word := range argv {
		words = append(words, quoteShWord(word))
	}
	if err := writeShim(filepath.Join(bin, planShimFilename), "#!/bin/sh\nexec "+strings.Join(words, " ")+" \"$@\"\n"); err != nil {
		return err
	}
	// AND `codeaf` BESIDE IT, for the same reason and on the same PATH entry. The
	// worker is taught to edit with `codeaf patch` (prompts/bashworker.md), and a
	// `codeaf` resolved on the machine's PATH is a coin toss: nothing at all on an
	// install whose file is devaf or stageaf, and on the fresh-install check of
	// 2026-09-25 an older codeaf that answered `there is no \`codeaf patch\``.
	// The shim makes the one word the page teaches mean the codeaf that is
	// running, whatever its file is called ([codeafShimScript]).
	if err := writeShim(filepath.Join(bin, codeafShimFilename), codeafShimScript()); err != nil {
		return err
	}
	p.shimmed = true
	return nil
}

// writeShim writes one shim executable, and leaves a file already holding the
// same script alone so a run's many workers do not rewrite it under each other.
func writeShim(path, script string) error {
	if existing, err := os.ReadFile(path); err == nil && string(existing) == script {
		return nil
	}
	return os.WriteFile(path, []byte(script), 0o755)
}

// shimDir is the directory the armed shim lives in — beside the store, the
// one place both the walk-up and the belt's prefix agree on. Empty while the
// shim never armed: a prefix into a directory that does not exist would
// silently lose every plandb call instead of the one the arming already
// recorded.
func (p *planState) shimDir() string {
	if !p.shimmed {
		return ""
	}
	return filepath.Join(filepath.Dir(p.path), "bin")
}

// planBashPrefix is the assignment that puts the shim's directory FIRST on the
// PATH of ONE command and binds that same command to the run's store — the
// prefix a bash-belt worker's command carries, and the whole of the mechanism.
// THE LAW IT CARRIES: THE ONLY `plandb` A WORKER CAN REACH IS THE RUN'S OWN, AND
// THE ONLY `codeaf` IS THE ONE RUNNING.
//
// IT IS AN `export`, NOT A BARE COMMAND-PREFIX ASSIGNMENT, and that is the fix,
// not decoration: `PATH=x:$PATH cmd` binds only the FIRST simple command of the
// line, so `cd elsewhere && plandb add` — or any step whose plandb call is not
// the first word — left `plandb` to resolve on the host's PATH and write a store
// this run never reads, the exact fault this exists to stop. An `export` reaches
// the whole command line in the one shell process the call runs. It still moves
// nothing in the process environment, which every session in this process shares
// and none may grow.
//
// THE STORE IS BOUND WITH IT. The shim's directory makes the run's CLI the one
// on PATH, and PLANDB_DB lets that CLI read this run's plan from ANY working
// directory, so a worker that cd's outside the run's tree still writes the run's
// store rather than failing to find one. The CLI honours PLANDB_DB ahead of its
// walk-up (internal/plandb's cliStore), so no `--db` is ever the worker's to
// pass. The prefix is empty when this run has no armed plan, which is every
// worker outside the experiment.
func (g *TaskGraph) planBashPrefix() string {
	plan := g.planIfArmed()
	if plan == nil {
		return ""
	}
	plan.mu.Lock()
	defer plan.mu.Unlock()
	bin := plan.shimDir()
	if bin == "" {
		return ""
	}
	prefix := "export PATH=" + quoteShWord(bin) + ":$PATH PLANDB_DB=" + quoteShWord(plan.path)
	// AND THE RUN IS BOUND, NOT ONLY THE PATH. A path says where the run's store
	// was when the run opened it; a later request can set that store aside and
	// seed another at the same path, and a worker bound by the path alone then
	// wrote its children and its `done`s into a run that was not its own
	// (measured on the owner's session: four children and ten `done`s). The
	// root names which run this is, and the CLI refuses a store at the path
	// whose root is another's ([plandb.RunEnv]).
	if plan.root != "" {
		prefix += " " + plandb.RunEnv + "=" + quoteShWord(plan.root)
	}
	return prefix + "; "
}

// runningCLI is the running codeaf binary, as the codeaf command registered it
// at its own start ([SetRunningCLI]), and empty in every process that is not
// the codeaf command: a bench driver that answers only `plandb`, a go test
// binary. It is what a worker's `codeaf` shim execs.
//
// IT IS REGISTERED, NOT PROBED, because the one thing this shim must never do is
// run a binary that is not codeaf with codeaf's words. The plan CLI's resolver
// may probe (resolvePlanCLI), since `<bin> plandb status` is a read; there is no
// such harmless read for every verb a worker might type, and a bench driver
// handed `patch` would parse it as its own flags and start its grid. So the
// codeaf command says what it is, and anything that has not said is refused.
var runningCLI string

// SetRunningCLI registers the path of the running codeaf binary — the one the
// person started, under whatever file name it was installed (codeaf, devaf,
// stageaf, a `--name` word). cmd/codeaf calls it once, at start.
func SetRunningCLI(path string) { runningCLI = strings.TrimSpace(path) }

// codeafShimRefusal is the one line a worker's `codeaf` says when this process
// never registered a running codeaf. It refuses rather than falling through to
// the machine's PATH, where a different codeaf answers with the wrong verbs.
const codeafShimRefusal = "codeaf is not reachable from this shell in this run; edit with sed -i or a heredoc instead"

// codeafShimScript is the `codeaf` shim's whole text: an exec of the running
// codeaf, or the refusal and exit 127 — the shell's own "not found" status —
// when there is none to exec.
func codeafShimScript() string {
	if runningCLI == "" {
		return "#!/bin/sh\necho " + quoteShWord(codeafShimRefusal) + " >&2\nexit 127\n"
	}
	return "#!/bin/sh\nexec " + quoteShWord(runningCLI) + " \"$@\"\n"
}

// planCLIBinEnv is the resolver's one override: it names a binary that
// answers `<bin> plandb …` — the codeaf-shaped door — and it wins unprobed,
// because an override that needs a probe is a suggestion.
const planCLIBinEnv = "CODEAF_PLANDB_BIN"

// resolvePlanCLI answers the argv the shim execs — the word or words that
// reach the ported CLI (internal/plandb's Main) — or nil when no candidate
// works, which is the answer the arming records rather than guesses past.
//
// THE RESOLUTION IS A PROBE, NOT A GUESS, because the one thing it must never
// do is what it did first: exec os.Executable() blind. The engine is not
// always the codeaf binary — a bench drives the task door in-process
// (bench/bashloop), where os.Executable() is the DRIVER, so every plandb call
// a worker made started the bench's grid instead of answering the store. The
// probe is the CLI's own cheapest read, `<candidate> plandb status`, run
// beside the run's store: exit 0 is the one proof the candidate answers as
// the CLI, and the timeout is the defence against a binary that starts
// instead of answering.
//
// THE ORDER IS HOW WELL EACH CANDIDATE KNOWS ITSELF: the explicit override
// first; the running binary, only when it passes the probe (a driver that
// routes the door passes — the bench's own binary is how the in-process arm
// gets a CLI at all); the sibling `plandb` beside the executable, whose own
// name is the whole contract — cmd/plandb builds it beside bin/codeaf — so
// its existence is the proof and it takes no `plandb` argument; and a codeaf
// found on PATH, probed again. A go test binary is never a candidate: handed
// `plandb status` it would run its whole suite, and every arming test in it
// would probe again — the .test suffix refuses it before that recursion can
// start.
func resolvePlanCLI(storeDir string) []string {
	if override := strings.TrimSpace(env.Get(planCLIBinEnv)); override != "" {
		return []string{override, "plandb"}
	}
	if self, err := os.Executable(); err == nil && !looksLikeTestBinary(self) {
		if planCLIProbes(self, storeDir) {
			return []string{self, "plandb"}
		}
		if sibling := filepath.Join(filepath.Dir(self), "plandb"); fileExecutable(sibling) {
			return []string{sibling}
		}
	}
	if onPath, err := exec.LookPath("codeaf"); err == nil && planCLIProbes(onPath, storeDir) {
		return []string{onPath, "plandb"}
	}
	return nil
}

// planCLIProbes runs the CLI's cheapest read beside the store and answers
// whether the candidate answered as the CLI. Ten seconds is the whole defence
// against a candidate that is a door into something else: a binary that
// starts instead of answering is killed and counted as failed.
func planCLIProbes(bin, storeDir string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	probe := exec.CommandContext(ctx, bin, "plandb", "status")
	probe.Dir = storeDir
	return probe.Run() == nil
}

// looksLikeTestBinary names the one executable shape the probe must never
// run: a go test binary answers an unknown argument by running its suite.
func looksLikeTestBinary(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".test.exe")
}

// fileExecutable answers whether the path is a regular file with any execute
// bit — the whole contract a sibling `plandb` has to meet, since its name is
// the contract's other half.
func fileExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

// quoteShWord makes one word safe as a shell argument in the shim's exec
// line. A path with a space in it is ordinary on this machine and the shim
// is read by /bin/sh, which splits on spaces without this.
func quoteShWord(word string) string {
	if !strings.ContainsAny(word, " \t\"'$`\\") {
		return word
	}
	return "'" + strings.ReplaceAll(word, "'", "'\\''") + "'"
}

// planReviseThrough writes a person's revision of a plan-born node's brief
// through to the store task, so the store stays the record of what the work
// is. It is the revise_assignment door's plan half, and it is best-effort: a
// store that will not take the revision leaves the node's own brief as the
// worker reads it, which is what the worker acts on.
func (g *TaskGraph) planReviseThrough(planID, brief string) {
	plan := g.planIfArmed()
	if plan == nil || planID == "" || planID == planRootID {
		return
	}
	plan.mu.Lock()
	defer plan.mu.Unlock()
	store := plan.open()
	if store == nil {
		return
	}
	defer store.Close()
	if store.Task(planID) == nil {
		return
	}
	_, _ = store.Revise(planID, plandb.TaskPatch{Description: &brief})
}

var errPlanNoStore = errors.New("plan store is not open")

// planRecordSpend writes one spend row into the run's store: the model call a
// plan-driven worker just made, charged to the plan task it carries. It is
// called on its own goroutine (recordPlanSpend) and is best-effort whole — a
// store that will not open, a row the store refuses, is silence, because the
// money is already in the session's ledger row and a plan that misses one row
// under-reports by less than a turn held up for the write would.
func (g *TaskGraph) planRecordSpend(planID, model, role string, usd float64, inTokens, outTokens int) {
	plan := g.planIfArmed()
	if plan == nil {
		return
	}
	plan.mu.Lock()
	defer plan.mu.Unlock()
	store := plan.open()
	if store == nil {
		return
	}
	defer store.Close()
	_ = store.AddSpend(planID, model, role, usd, inTokens, outTokens)
}

// recordPlanSpend charges one banked model call to the worker's plan task —
// the write beside the usage ledger row (recordUsageLine, its only caller).
// The figures are the call's own; the task is the node's plan task; the role
// is what the node was doing at that instant: 'plan' while it has plan
// children of its own, 'work' while it is a leaf.
//
// THE GATE IS TWOFOLD and cheap: the worker is on the bash belt
// (Config.mayBashBelt, checked by the caller) and its node carries a plan id —
// a worker without a plan task has nothing to charge, and every agent outside
// the experiment stops here. The graph's lock is taken once, briefly, to read
// both facts; nothing else is taken while it is held, so the caller — bank,
// already outside a.mu — waits on nothing.
func (a *Agent) recordPlanSpend(used Usage, model string) {
	taskID := a.config.taskID
	if taskID == 0 {
		return
	}
	g := a.graph()
	if g == nil {
		return
	}
	g.mu.Lock()
	node := g.nodes[taskID]
	if node == nil {
		g.mu.Unlock()
		return
	}
	planID := node.spec.planID
	children := false
	for _, kid := range g.order {
		child := g.nodes[kid]
		if child != nil && child.parent == taskID && child.spec.planID != "" {
			children = true
			break
		}
	}
	g.mu.Unlock()
	if planID == "" {
		return
	}
	role := "work"
	if children {
		role = "plan"
	}
	go g.planRecordSpend(planID, model, role, used.CostUSD, used.Input, used.Output)
}

// planRunSpend answers the dollars the run's ledger holds in the store — the
// per-project rollup under the root task's tag, which is every task's tag, so
// the number is the run's whole bill and not one worker's share of it. Zero
// says the run has never been charged: no plan, a store that will not open, or
// no row joined to the project — the caller renders nothing rather than a zero
// somebody reads as a figure.
//
// The read takes the plan's gate and nothing else, the same order every pulse
// takes, and opens the store fresh the way every pass does — the worker's CLI
// has been writing since any cached copy was made.
func (g *TaskGraph) planRunSpend() float64 {
	plan := g.planIfArmed()
	if plan == nil {
		return 0
	}
	plan.mu.Lock()
	defer plan.mu.Unlock()
	store := plan.open()
	if store == nil {
		return 0
	}
	defer store.Close()
	root := store.Task(planRootID)
	if root == nil {
		return 0
	}
	return store.Summary().ProjectSpend[root.Project].USD
}

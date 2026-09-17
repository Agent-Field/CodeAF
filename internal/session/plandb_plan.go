package session

// The plan side of the bash belt (docs/design/plandb-cli/DESIGN.md, the
// wiring section): the store the worker's `plandb` calls write, and the two
// pulse points that make the graph and the store one thing. THE STORE IS THE
// WORKER'S CLI's STORE and this file's store at once — one JSON file in the
// session folder, found by both roads the same way (the runtime by path, the
// CLI by walking up from its own working directory) — so a task the model
// adds through bash is a task the runtime dispatches, and a node that lands
// is a task the plan says is done.
//
// LOCK ORDER, stated once because everything here depends on it: a pulse may
// hold the plan gate while taking the graph's mu (claimChild and admit take
// it internally), and nothing may hold the graph's mu while asking for the
// plan gate. The two pulse points — after a worker's bash call, after a node
// lands — are called from code that holds neither, and the seed takes the
// plan gate BEFORE admit takes the graph's mu, never inside it.

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

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// planState is one run's plan: where the store lives, and whether the bin
// shim has been armed. Nothing else is kept here — the node-to-task mapping
// lives on the nodes themselves (taskSpec.planID, checkpointed), because a
// map on the side that disagreed with its nodes would be a second truth.
type planState struct {
	mu      sync.Mutex
	path    string
	shimmed bool
}

// planStoreFilename is the file name every road agrees on: the runtime's
// path helper, the CLI's walk-up, and the store's own creation all spell it
// the same way.
const planStoreFilename = "plandb.json"

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
		return filepath.Join(g.home.config.Workspace, ".codeaf", planStoreFilename)
	}
	return ""
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
				store, err = plandb.Open(path, spec.title, planRootID, spec.title, spec.brief)
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
		g.plan = &planState{path: path}
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
// the file, never a cached copy.
func (p *planState) open() *plandb.Store {
	store, err := plandb.Open(p.path, "", planRootID, "", "")
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
// called from exactly two places — after a bash-belt worker's bash call, and
// on every road a node lands by — and from nowhere else; there is no timer
// and no polling, because a pass that reads a plan nobody has touched is
// work the harness does for nothing.
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
	// missing parent leaves the task in the store for the next pass. The ids
	// dispatched THIS pass are remembered, because the root-completion step
	// below reads the same pass and must not cancel work it just handed out.
	dispatched := map[string]bool{}
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
		dispatched[task.ID] = true
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
	for _, task := range store.Tasks() {
		if task.ID == store.RootID() || terminalStoreStatus(task.Status) || snapshot[task.ID] != nil || dispatched[task.ID] {
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
		b.WriteString("- The runtime completes the run itself; finish your work and end your turn.\n")
	} else {
		b.WriteString("- Finish it with: plandb done --agent ")
		b.WriteString(agent)
		b.WriteString(" --result 'what you did and what it changed' — after the work holds, and never before.\n")
	}
	b.WriteString("- Coordinate through the plan CLI: plandb add, plandb split, plandb task note, plandb task overview (the page lists them all).\n")
	b.WriteString("- Dispatch is automatic: every ready task you create gets a worker. Never run the lifecycle verbs (claim, start, go, fail, pause, approve) — the runtime owns them.\n")
	return b.String()
}

// armShim writes the session's `plandb` shim and puts its directory first on
// the PATH, once. THE SHIM IS WHY THE PAGE CAN SAY `plandb` PLAINLY: the
// plandb that lives on this machine's PATH is a different store, and a worker
// that reached it would write a plan the runtime could not read. The shim
// execs the CLI the resolver reached (resolvePlanCLI), so the page's one word
// always names the CLI this run shares. Prepending the PATH is safe where
// pointing an environment variable at one store would not be: every shim is
// the same CLI, and the store a call binds to is found by walking up from the
// caller's own working directory.
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
	shim := filepath.Join(bin, "plandb")
	words := make([]string, 0, len(argv)+1)
	for _, word := range argv {
		words = append(words, quoteShWord(word))
	}
	script := "#!/bin/sh\nexec " + strings.Join(words, " ") + " \"$@\"\n"
	if existing, err := os.ReadFile(shim); err != nil || string(existing) != script {
		if err := os.WriteFile(shim, []byte(script), 0o755); err != nil {
			return err
		}
	}
	p.shimmed = true
	path := os.Getenv("PATH")
	for _, part := range strings.Split(path, string(os.PathListSeparator)) {
		if part == bin {
			return nil
		}
	}
	_ = os.Setenv("PATH", bin+string(os.PathListSeparator)+path)
	return nil
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
	if override := strings.TrimSpace(os.Getenv(planCLIBinEnv)); override != "" {
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
	if store.Task(planID) == nil {
		return
	}
	_, _ = store.Revise(planID, plandb.TaskPatch{Description: &brief})
}

var errPlanNoStore = errors.New("plan store is not open")

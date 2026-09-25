package session

// The plandb CLI's end-to-end proof (docs/design/plandb-cli/DESIGN.md, wave
// 2): the acceptance the whole wiring wave is judged by. A bash-belt worker
// coordinates through the REAL `plandb` binary — add, add+dep, note, the
// reading set between parts, and the taught finish — and the runtime's pulse
// turns those store writes into the same node tree codeaf's own verbs grow,
// writes every landing back as the store task's own claimed agent, and
// completes the run's root when the tree has settled. The flag-off arm rides
// beside it: the switch unset, and not one byte of the ordinary flow has
// moved.
//
// THE CLI THE WORKERS DRIVE IS THE ONE cmd/plandb BUILDS. Under `go test` the
// running binary is the TEST binary, which has no `plandb` verb — so the PATH
// shim the session arms (plandb_plan.go's armShim, which execs the running
// binary's own subcommand) is deliberately KEPT OFF the front of the PATH here:
// the test builds the real CLI once into a directory of its own and prepends
// THAT, and then also names the session's shim directory in the PATH it sets —
// because armShim only prepends its bin when the bin is not already on the
// PATH, so naming it second is what stops the shim from shadowing the real
// binary. The CLI needs no --db: it finds the run's store by walking up from
// the worker's own working directory, which is the session's tree folder, and
// the store it finds there is the one the runtime seeded — asserted below, so
// a wrong PATH fails loudly instead of quietly writing some other machine's
// store.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// ── the real CLI, built once for the test that needs it ──────────────────────

// planE2ECLIPackage is the CLI's import path, spelled by module path rather
// than ./cmd/plandb because the test's working directory is this package's
// folder and the pattern would not resolve from there.
const planE2ECLIPackage = "github.com/Agent-Field/codeaf/cmd/plandb"

// planE2EArmCLI builds the real CLI binary into a directory of its own and
// puts that directory FIRST on the PATH, ahead of both the machine's own
// plandb (a different program, on a different store) and the session's bin
// shim. It must run before the agent is built: armShim reads the PATH at seed
// time, and the whole point of the ordering is what it decides there.
func planE2EArmCLI(t *testing.T, place Place) {
	t.Helper()
	bin := t.TempDir()
	out := filepath.Join(bin, "plandb")
	build := exec.Command("go", "build", "-o", out, planE2ECLIPackage)
	if output, err := build.CombinedOutput(); err != nil {
		t.Skipf("cannot build the real plandb CLI (go build %s: %v)\n%s", planE2ECLIPackage, err, output)
	}
	// THE ORDER IS THE TEST: the built binary first, the session's shim
	// directory second. Second is not decoration — armShim prepends its bin to
	// the PATH unless the bin is already ON it, so naming it here is what
	// keeps the shim (which under go test would exec the test binary) from
	// ever being the `plandb` a worker finds.
	t.Setenv("PATH", bin+string(os.PathListSeparator)+
		filepath.Join(place.Dir, "bin")+string(os.PathListSeparator)+
		os.Getenv("PATH"))
}

// ── the four-lane completer ─────────────────────────────────────────────────

// planE2ECompleter scripts the conversation, the run's seeding worker, each
// dispatched plan worker, and the auditor, against one provider. The lanes
// are told apart by the one line the runtime composes into every plan-born
// brief — "YOUR TASK IN THE PLAN IS t-<id>" — because that line rides in the
// brief and nothing the conversation sends can contain it; the flag-off arm
// has no plan lines and routes its worker the classic way, on the brief mark.
// A lane out of steps answers prose, which ends a worker's turn — that is how
// the dispatched parts are scripted to finish immediately.
type planE2ECompleter struct {
	mu    sync.Mutex
	lanes map[string][]step
	seen  map[string]int
	asked map[string][][]ai.Message
}

func newPlanE2ECompleter(lanes map[string][]step) *planE2ECompleter {
	return &planE2ECompleter{
		lanes: lanes,
		seen:  map[string]int{},
		asked: map[string][][]ai.Message{},
	}
}

// planE2ELaneIDs are the store tasks this file's scripts create, in the
// spelling the composed briefs carry them in.
var planE2ELaneIDs = []string{"t-a", "t-b", "t-c", "t-root"}

func (c *planE2ECompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	// THE ERRANDS BESIDE THE WORK are answered before any lane is touched:
	// a namer arms itself with the node's own brief and would otherwise take
	// a step scripted for a worker (task_test.go's routedCompleter says why).
	if isNameCall(messages) || isCaptionCall(messages) || isTitleCall(messages) {
		return textResponse(""), nil
	}
	lane := "chat"
	if len(messages) > 0 && messages[0].Role == "system" &&
		strings.Contains(messageText(messages[0]), "You are an AUDITOR") {
		lane = "audit"
	} else {
		for _, id := range planE2ELaneIDs {
			if planE2EUserCarries(messages, "YOUR TASK IN THE PLAN IS "+id) {
				lane = id
				break
			}
		}
		if lane == "chat" && planE2EUserCarries(messages, taskBriefMark) {
			lane = "child"
		}
	}
	c.mu.Lock()
	c.asked[lane] = append(c.asked[lane], append([]ai.Message(nil), messages...))
	index := c.seen[lane]
	c.seen[lane] = index + 1
	c.mu.Unlock()
	if index < len(c.lanes[lane]) {
		return c.lanes[lane][index](ctx, messages)
	}
	return textResponse("(unscripted)"), nil
}

// planE2ELaneText is one lane's opening request as text — the whole request,
// system page and brief both, because the composed work order is spread
// across the messages and this is the only place it can be read from the
// outside.
func planE2ELaneText(c *planE2ECompleter, lane string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	asks := c.asked[lane]
	if len(asks) == 0 {
		return ""
	}
	var all []string
	for _, message := range asks[0] {
		all = append(all, messageText(message))
	}
	return strings.Join(all, "\n")
}

// planE2EFirstAsk is one lane's opening request, messages and all — for the
// asserts that read one message of it rather than the whole text.
func (c *planE2ECompleter) planE2EFirstAsk(lane string) []ai.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	if asks := c.asked[lane]; len(asks) > 0 {
		return asks[0]
	}
	return nil
}

// planE2EUserCarries says whether any user-role message carries the line.
// The conversation's own propose_task call is an assistant message and the
// proposal notice quotes no brief, so the lanes cannot be stolen by the very
// words they route on.
func planE2EUserCarries(messages []ai.Message, line string) bool {
	for _, message := range messages {
		if message.Role != "user" {
			continue
		}
		if strings.Contains(messageText(message), line) {
			return true
		}
	}
	return false
}

// ── steps, waits and readings ────────────────────────────────────────────────

// planE2EProposeStep is the conversation handing the run its root task. The
// brief becomes the root store task's description — the seeding door composes
// the worker's whole work order back out of that store read — so the words
// here are the words the root worker's brief must prove it received.
func planE2EProposeStep(title, brief string) step {
	arguments, _ := json.Marshal(taskArguments{
		Title:       title,
		Summary:     "the plan CLI acceptance run",
		Brief:       brief,
		Deliverable: "the plan store's own record of the run",
		Acceptance:  "every part of the plan reads done in the store the runtime seeded.",
		MaxSteps:    200,
		NoProgress:  6,
	})
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse("plan-e2e-task", "propose_task", string(arguments)), nil
	}
}

// planE2EBashStep is one bash call on the belt — the only hand a plan worker
// coordinates with, and the pulse's first firing point.
func planE2EBashStep(id, command string) step {
	arguments, _ := json.Marshal(map[string]string{"command": command})
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse(id, "bash", string(arguments)), nil
	}
}

// planE2EWaitSettled waits one node out with more room than the shared
// helper: this flow cuts three working copies and spawns a CLI process per
// command, and a deadline bought at the flow's real cost is cheaper than a
// flake at the shared one's.
func planE2EWaitSettled(t *testing.T, node *TaskNode) {
	t.Helper()
	if node == nil {
		t.Fatal("no such node to wait on")
	}
	select {
	case <-node.done:
	case <-time.After(60 * time.Second):
		t.Fatalf("plan node %d never settled", node.id)
	}
}

// planE2EShape is one node as the assertions read it — the fields the tree
// proof needs, taken under the graph's lock the way the pulse itself takes
// them.
type planE2EShape struct {
	id     uint64
	parent uint64
	depth  int
	planID string
	state  TaskState
	report string
}

func planE2EShapes(t *testing.T, g *TaskGraph) []planE2EShape {
	t.Helper()
	g.mu.Lock()
	shapes := make([]planE2EShape, 0, len(g.nodes))
	for _, node := range g.nodes {
		shapes = append(shapes, planE2EShape{
			id: node.id, parent: node.parent, depth: node.depth,
			planID: node.spec.planID, state: node.state, report: node.report,
		})
	}
	g.mu.Unlock()
	sort.Slice(shapes, func(i, j int) bool { return shapes[i].id < shapes[j].id })
	return shapes
}

// planE2ENodeByPlan finds the node a store task became, by the plan id on its
// spec — the one stable name for a dispatched part, since node ids are the
// graph's own arithmetic.
func planE2ENodeByPlan(g *TaskGraph, planID string) *TaskNode {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, node := range g.nodes {
		if node.spec.planID == planID {
			return node
		}
	}
	return nil
}

// planE2EOpenStore opens the run's store from the RUNTIME's own path — not by
// walking up, which is the CLI's road — so the assertions read the very file
// the seed wrote and the CLI had to find on its own.
func planE2EOpenStore(t *testing.T, place Place) *plandb.Store {
	t.Helper()
	store, err := plandb.Open(filepath.Join(place.Dir, planStoreFilename), "", planRootID, "", "")
	if err != nil {
		t.Fatalf("open the run's plan store: %v", err)
	}
	return store
}

// planE2EWaitStoreRoot waits, bounded, for the run's root task to read done
// in the store — the word CompleteRoot writes at the last landing's pulse.
// It fails with the task's actual status when the wait runs out, so a
// missing completion is a named failure, not a timeout's shrug.
func planE2EWaitStoreRoot(t *testing.T, place Place) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		store := planE2EOpenStore(t, place)
		root := store.Task(planRootID)
		// A store handle holds the database open, so every poll closes the one
		// it read through rather than leaving a connection behind per tick.
		_ = store.Close()
		if root != nil && root.Status == plandb.StatusDone {
			return
		}
		if time.Now().After(deadline) {
			status := "absent"
			if root != nil {
				status = string(root.Status)
			}
			t.Fatalf("the store's root task never completed; it reads %s", status)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// planE2EStoresUnder lists every plandb.db under a directory, which is the
// loud half of the no-foreign-store proof: a worker whose PATH reached some
// other plandb would have left that program's store somewhere, and a store
// beside the one the runtime seeded is the failure this finds.
func planE2EStoresUnder(t *testing.T, dir string) []string {
	t.Helper()
	var found []string
	_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && entry.Name() == planStoreFilename {
			found = append(found, path)
		}
		return nil
	})
	return found
}

// ── the acceptance: the loop through bash ─────────────────────────────────────

// TestPlandbCliLoopThroughBash is the wave's acceptance line. One bash-belt
// worker runs the doctrine's own commands against the store the runtime
// seeded — add A, add B with a hard dep on A, note A — then waits on its
// parts the way the loop's frame says (reading the plan between landings,
// each read a pulse), and ends its turn without a done verb, because its work
// order says the runtime completes the run. What must come back: A dispatched
// as a real node the moment its add lands, B only after A has landed and been
// written back (B's store dep on A), the same tree propose_task would grow,
// and the store holding the run's own words for every ending — the parts'
// landing reports, the note under t-a, and the root completed by the runtime
// alone.
func TestPlandbCliLoopThroughBash(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	place := Place{Dir: t.TempDir()}
	repo := newTestRepo(t)
	planE2EArmCLI(t, place)
	t.Setenv("HOME", t.TempDir())

	completer := newPlanE2ECompleter(map[string][]step{
		"chat": {
			planE2EProposeStep("Two-part plan",
				"Run the two-part plan through the CLI only: add part A, add part B on A, leave A a note, wait for the parts, then end the turn."),
			finalText("handed off"), finalText("handed off"), finalText("handed off"),
		},
		"t-root": {
			// THE DOCTRINE'S OWN COMMANDS, run against the store the seed
			// wrote — the parent's coordination is the whole of this
			// worker's first turn, and each call is the pulse's firing
			// point.
			planE2EBashStep("plan-e2e-add-a",
				`plandb add "Part A" --description 'build the index the two-part run stands on' --parent t-root --as a`),
			planE2EBashStep("plan-e2e-add-b",
				`plandb add "Part B" --description 'consume the index part A built' --parent t-root --dep t-a --as b`),
			planE2EBashStep("plan-e2e-note",
				`plandb task note t-a 'remember the index'`),
			// The root does NOT run done — its work order says the runtime
			// completes the run, and the store's root task may only be
			// written by that completion.
			finalText("both parts are in the plan and dispatch is automatic; I wait."),
			// THE FRAME'S WAIT-AND-INTEGRATE HALF: each wake reads the plan
			// through the CLI — the doctrine's reading set — and each read
			// is a pulse, which is what carries A's write-back and B's
			// dispatch while the root still sits in front of the plan.
			planE2EBashStep("plan-e2e-see-a", `plandb task overview`),
			finalText("part A has landed; part B runs."),
			planE2EBashStep("plan-e2e-see-b", `plandb task overview`),
			finalText("both parts have landed; the run is the runtime's to complete."),
			finalText("handed off"), finalText("handed off"), finalText("handed off"),
		},
		// The dispatched parts end immediately: a lane out of steps answers
		// prose and the worker's turn ends on it.
		"t-a": {},
		"t-b": {},
	})
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Place = place
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	collect(t, mustSubmit(t, agent, "run the two-part plan through the plan CLI"))

	g := agent.graph()
	planE2EWaitSettled(t, g.node(1))
	planE2EWaitSettled(t, planE2ENodeByPlan(g, "a"))
	planE2EWaitSettled(t, planE2ENodeByPlan(g, "b"))
	// AND THE STORE'S END STATE, which is written by the LAST landing's pulse
	// — root completion is that pass's work, and the pass runs in the last
	// child's own goroutine a breath after its state settles. A bounded wait
	// on the word completion writes is the honest wait; the pulse is not a
	// goroutine of its own and there is nothing to poll but this.
	planE2EWaitStoreRoot(t, place)

	// THE TREE: exactly the run and its two parts, the same shape codeaf's
	// own verbs grow — children of the seeding node, one below it, both
	// landed. Node ids are the graph's arithmetic, so the parts are found by
	// their plan ids and the shape is asserted, not the numbering.
	shapes := planE2EShapes(t, g)
	if len(shapes) != 3 {
		t.Fatalf("the graph holds %d nodes, want exactly the run and its two parts: %+v", len(shapes), shapes)
	}
	byPlan := map[string]planE2EShape{}
	for _, shape := range shapes {
		byPlan[shape.planID] = shape
	}
	root, a, b := byPlan["root"], byPlan["a"], byPlan["b"]
	if root.id != 1 || root.parent != 0 || root.depth != 1 {
		t.Errorf("the seeding node is %+v, want the conversation's first task at depth 1", root)
	}
	for _, part := range []planE2EShape{a, b} {
		if part.parent != root.id {
			t.Errorf("part %s has parent %d, want the seeding node %d", part.planID, part.parent, root.id)
		}
		if part.depth != root.depth+1 {
			t.Errorf("part %s sits at depth %d, want %d", part.planID, part.depth, root.depth+1)
		}
		if part.state != TaskDone {
			t.Errorf("part %s landed %q, want done", part.planID, part.state)
		}
	}
	if kids := g.children(root.id); len(kids) != 2 {
		t.Errorf("the seeding node has %d children, want its two parts", len(kids))
	}

	// THE WORK ORDERS CAME FROM THE STORE: the root's brief carries its own
	// plan line and the runtime-completes clause, and part A's brief carries
	// the description the root's add wrote plus the finish command under its
	// own agent — the composed facts nothing but a store read would know.
	rootBrief := planE2ELaneText(completer, "t-root")
	if !strings.Contains(rootBrief, "YOUR TASK IN THE PLAN IS t-root, claimed by agent root") {
		t.Error("the root worker's brief does not carry its own plan line")
	}
	if !strings.Contains(rootBrief, "plandb done t-root --agent root") {
		t.Error("the root worker's brief does not teach the finish under the root's own agent")
	}
	aBrief := planE2ELaneText(completer, "t-a")
	if !strings.Contains(aBrief, "YOUR TASK IN THE PLAN IS t-a, claimed by agent a") {
		t.Error("part A's brief does not carry its plan line under its own agent")
	}
	if !strings.Contains(aBrief, "plandb done --agent a") {
		t.Error("part A's brief does not teach the finish under its own agent")
	}
	if !strings.Contains(aBrief, "build the index the two-part run stands on") {
		t.Error("part A's brief does not carry the description its add wrote")
	}

	// THE STORE, read from the runtime's own path. Three tasks is the
	// no-foreign-store proof in one number: the file the seed wrote is the
	// one the worker's CLI grew, from one root to the run's whole plan.
	store := planE2EOpenStore(t, place)
	tasks := store.Tasks()
	if len(tasks) != 3 {
		t.Fatalf("the runtime's store holds %d tasks, want the root and its two parts — the CLI the workers ran wrote somewhere else", len(tasks))
	}
	if places := planE2EStoresUnder(t, place.Dir); len(places) != 1 || places[0] != filepath.Join(place.Dir, planStoreFilename) {
		t.Errorf("the session folder holds stores at %v, want exactly the one the runtime seeded", places)
	}
	if leaked := planE2EStoresUnder(t, repo); len(leaked) != 0 {
		t.Errorf("a second store appeared in the repository at %v", leaked)
	}
	byID := map[string]*plandb.Task{}
	for _, task := range tasks {
		byID[task.ID] = task
	}
	// B AFTER A: B's store dep on A held it back until A landed and was
	// written back, so the store's own record answers the ordering — the
	// parts in admission order, and B's completion not before A's.
	seen := 0
	for _, task := range tasks {
		switch task.ID {
		case "root":
			if seen != 0 {
				t.Errorf("the root task is not first in the store's admission order")
			}
		case "a":
			if seen != 1 {
				t.Errorf("part A is not second in the store's admission order")
			}
		case "b":
			if seen != 2 {
				t.Errorf("part B is not third in the store's admission order")
			}
		}
		seen++
	}
	for _, id := range []string{"root", "a", "b"} {
		if byID[id] == nil {
			t.Fatalf("the store has no task %s", id)
		}
	}
	aTask, bTask, rootTask := byID["a"], byID["b"], byID["root"]
	if aTask.Status != plandb.StatusDone || bTask.Status != plandb.StatusDone {
		t.Errorf("the parts read %q and %q, want both done", aTask.Status, bTask.Status)
	}
	if rootTask.Status != plandb.StatusDone {
		t.Errorf("the root task reads %q, want the run completed by the runtime", rootTask.Status)
	}
	if strings.TrimSpace(rootTask.Result) == "" {
		t.Error("the root task carries no result — CompleteRoot never wrote the run's word")
	}
	// THE WRITEBACK'S OWNERSHIP: each part was completed as the task's own
	// claimed agent, and the result is the landing's report — the node's
	// words, not a second author's.
	if aTask.ClaimedBy != "a" || bTask.ClaimedBy != "b" {
		t.Errorf("the parts are claimed by %q and %q, want their own ids", aTask.ClaimedBy, bTask.ClaimedBy)
	}
	if aTask.Result != a.report {
		t.Errorf("part A's result is %q, want its landing's report %q", aTask.Result, a.report)
	}
	if bTask.Result != b.report {
		t.Errorf("part B's result is %q, want its landing's report %q", bTask.Result, b.report)
	}
	if !bTask.CompletedAt.After(aTask.CompletedAt) && !bTask.CompletedAt.Equal(aTask.CompletedAt) {
		t.Errorf("part B completed at %v, not after part A at %v — the dep the root wrote did not order the plan", bTask.CompletedAt, aTask.CompletedAt)
	}
	// THE NOTE the root left rides under A, where the doctrine says the next
	// worker reads it.
	notes := store.Notes("a", 10)
	if len(notes) != 1 || notes[0].Body != "remember the index" {
		t.Errorf("task a holds notes %+v, want the one the root left", notes)
	}
}

// ── the worker's own finish ───────────────────────────────────────────────────

// TestPlandbCliWorkerOwnDone proves the taught finish is real: a plan task
// one node deeper, whose worker ends it ITSELF through the CLI — done under
// the agent the runtime claimed it as — lands its own words in the store, and
// the landing's write-back passes it by rather than writing over it. The
// sibling task that did not finish itself is the contrast: its result is the
// landing's report.
func TestPlandbCliWorkerOwnDone(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	place := Place{Dir: t.TempDir()}
	repo := newTestRepo(t)
	planE2EArmCLI(t, place)
	t.Setenv("HOME", t.TempDir())

	completer := newPlanE2ECompleter(map[string][]step{
		"chat": {
			planE2EProposeStep("Deeper plan",
				"Add part A; A's own worker adds one child of A and waits; the child finishes itself through the CLI."),
			finalText("handed off"), finalText("handed off"), finalText("handed off"),
		},
		"t-root": {
			planE2EBashStep("plan-e2e-deep-a",
				`plandb add "Part A" --description 'hold one deeper child and let it finish itself' --parent t-root --as a`),
			finalText("part A is in the plan; I wait."),
			planE2EBashStep("plan-e2e-deep-see", `plandb task overview`),
			finalText("part A and its child have landed; the run is the runtime's to complete."),
			finalText("handed off"), finalText("handed off"), finalText("handed off"),
		},
		"t-a": {
			planE2EBashStep("plan-e2e-deep-c",
				`plandb add "Deeper work" --description 'one child of A, finished by its own worker through the CLI' --parent t-a --as c`),
			finalText("the deeper child is in the plan; I wait."),
			planE2EBashStep("plan-e2e-deep-see-c", `plandb task overview`),
			finalText("the child has landed; nothing else is mine."),
			finalText("handed off"), finalText("handed off"), finalText("handed off"),
		},
		"t-c": {
			// THE TAUGHT FINISH: done under the task's own agent, which is
			// the ownership check — the runtime claimed this task as "c", and
			// only c's worker may complete it.
			planE2EBashStep("plan-e2e-own-done",
				`plandb done --agent c --result 'did it myself'`),
			finalText("I finished my own task through the done verb; nothing else is mine."),
			finalText("handed off"), finalText("handed off"),
		},
	})
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Place = place
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	collect(t, mustSubmit(t, agent, "run the deeper plan through the plan CLI"))

	g := agent.graph()
	planE2EWaitSettled(t, g.node(1))
	planE2EWaitSettled(t, planE2ENodeByPlan(g, "a"))
	planE2EWaitSettled(t, planE2ENodeByPlan(g, "c"))

	// THE TREE: the run, its part, and the part's own child — one node
	// deeper than the acceptance run grows, and still inside the depth the
	// graph allows.
	shapes := planE2EShapes(t, g)
	if len(shapes) != 3 {
		t.Fatalf("the graph holds %d nodes, want the run, its part and the part's child: %+v", len(shapes), shapes)
	}
	byPlan := map[string]planE2EShape{}
	for _, shape := range shapes {
		byPlan[shape.planID] = shape
	}
	root, a, c := byPlan["root"], byPlan["a"], byPlan["c"]
	if a.parent != root.id || a.depth != root.depth+1 {
		t.Errorf("part A is %+v, want a child of the seeding node", a)
	}
	if c.parent != a.id || c.depth != a.depth+1 {
		t.Errorf("the deeper part is %+v, want a child of A one below it", c)
	}
	for _, shape := range []planE2EShape{root, a, c} {
		if shape.state != TaskDone {
			t.Errorf("node %s landed %q, want done", shape.planID, shape.state)
		}
	}
	// THE RUN'S WORD, written by the last landing's pulse a breath after the
	// state settles — bounded wait, named failure (see the loop test).
	planE2EWaitStoreRoot(t, place)

	store := planE2EOpenStore(t, place)
	tasks := store.Tasks()
	if len(tasks) != 3 {
		t.Fatalf("the runtime's store holds %d tasks, want the root, part A and its child", len(tasks))
	}
	byID := map[string]*plandb.Task{}
	for _, task := range tasks {
		byID[task.ID] = task
	}
	cTask, aTask, rootTask := byID["c"], byID["a"], byID["root"]
	// THE WORKER'S OWN WORDS: the store's result for the child is the words
	// its worker passed the done verb, and not the landing's report — the
	// write-back passed a task the worker had already finished, which is the
	// whole promise the taught finish makes.
	if cTask.Status != plandb.StatusDone {
		t.Errorf("the deeper task reads %q, want done", cTask.Status)
	}
	if cTask.Result != "did it myself" {
		t.Errorf("the deeper task's result is %q, want the worker's own words", cTask.Result)
	}
	if cTask.Result == c.report {
		t.Errorf("the deeper task's result equals the landing's report — the write-back wrote over the worker's own finish")
	}
	if cTask.ClaimedBy != "c" {
		t.Errorf("the deeper task is claimed by %q, want its own id — the done verb's ownership check ran against the runtime's claim", cTask.ClaimedBy)
	}
	// THE CONTRAST: the part above finished itself and the store carries its
	// worker's words; this part's store record is nobody's worker words at all
	// — the plan reads it done, and 'did it myself' is the one result in the
	// store that the worker, not the runtime, wrote.
	if aTask.Status != plandb.StatusDone {
		t.Errorf("part A reads %q, want done", aTask.Status)
	}
	if aTask.Result == "did it myself" || cTask.Result != "did it myself" {
		t.Errorf("the store's one worker-written result is misplaced: a=%q c=%q", aTask.Result, cTask.Result)
	}
	if rootTask.Status != plandb.StatusDone {
		t.Errorf("the root task reads %q, want the run completed by the runtime", rootTask.Status)
	}
}

// ── the flag-off arm ─────────────────────────────────────────────────────────

// TestPlandbCliFlagOffTouchesNothing is the experiment's other arm: the same
// propose-and-run flow with the switch unset leaves no store anywhere in the
// session folder, arms no shim, and hands the worker the ordinary belt with
// the graph's own verbs on it — every byte where it was.
func TestPlandbCliFlagOffTouchesNothing(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "node")
	place := Place{Dir: t.TempDir()}
	repo := newTestRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := newPlanE2ECompleter(map[string][]step{
		"chat": {
			planE2EProposeStep("Plain run",
				"Sit in the copy; nothing needs doing.\n"+taskBriefMark),
			finalText("handed off"), finalText("handed off"), finalText("handed off"),
		},
		"child": {
			finalText("nothing needed doing; the copy holds."),
			finalText("nothing needed doing; the copy holds."),
		},
		"audit": {finalText("VERIFIED — the copy holds and nothing was needed")},
	})
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Place = place
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	collect(t, mustSubmit(t, agent, "run the plain task"))
	node := agent.graph().node(1)
	planE2EWaitSettled(t, node)
	if state := node.notice().State; state != TaskDone {
		t.Fatalf("the plain task landed %q, want done — the flag-off flow itself must be whole", state)
	}

	// NO STORE ANYWHERE: the seed never ran, so the session folder holds no
	// plandb.db — and the bin directory the shim would have armed is not
	// there either, which is the flag-off path leaving the PATH alone too.
	if stores := planE2EStoresUnder(t, place.Dir); len(stores) != 0 {
		t.Errorf("the flag-off run left a plan store at %v, want none", stores)
	}
	if _, err := os.Stat(filepath.Join(place.Dir, "bin")); !os.IsNotExist(err) {
		t.Error("the flag-off run armed the bin shim, want the PATH untouched")
	}
	// THE CHILD'S BELT IS THE ORDINARY ONE: a worker built on this shape —
	// in a task, at the depth, on the flag-off belt — carries propose_task,
	// the graph's own verbs, exactly as the pre-experiment composition put
	// them.
	worker, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.InTask = true
		config.tasker = graphForShape(t)
		config.taskID = 2
		config.taskDepth = 1
	})
	belt := worker.beltTools()
	carried := map[string]bool{}
	for _, tool := range belt {
		carried[tool.Name] = true
	}
	if !carried["propose_task"] {
		t.Error("the flag-off task worker lost propose_task — the ordinary belt must be where it was")
	}
	// AND THE CHILD RAN THE ORDINARY PAGE: the file-tool guidance where the
	// bash belt's doctrine would be, read out of the very request the worker
	// was handed.
	page := messageContentText(completer.planE2EFirstAsk("child")[0])
	if !strings.Contains(page, "## Specialized Tools") {
		t.Error("the flag-off worker did not read the ordinary file-tool page")
	}
	if strings.Contains(page, "ONE ACTION PER RESPONSE") {
		t.Error("the flag-off worker was handed the bash belt's doctrine page")
	}
}

// ── the belt's landing: what a shell writes reaches the branch ──────────────────

// TestPlandbBashWritesReachTheBranch is the belt's own landing proof. A
// bash-belt worker makes its one edit through bash — the only hand the belt
// gives it — and ends its turn, so its ledger stays empty and the tree's own
// git status is the only account of what it wrote. The landing stages from
// that status ([stageTaskWork]'s belt arm), and what must come back: the
// change is ON the branch the landing merges home, and the report carries no
// left-behind sentence — before the arm, a shell worker's file sat unstaged
// in its task folder and was reported exactly as that.
func TestPlandbBashWritesReachTheBranch(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	place := Place{Dir: t.TempDir()}
	repo := newTestRepo(t)
	planE2EArmCLI(t, place)
	t.Setenv("HOME", t.TempDir())

	completer := newPlanE2ECompleter(map[string][]step{
		"chat": {
			planE2EProposeStep("The whole run",
				"Write one file with bash — the belt's own hand — and end the turn."),
			finalText("handed off"), finalText("handed off"), finalText("handed off"),
		},
		"t-root": {
			planE2EBashStep("belt-write", `printf 'written through bash\n' > note.txt`),
			// THE TURN ENDS WITHOUT A DONE VERB: the run's own task is the
			// runtime's to complete, and the landing is what carries the file.
			finalText("the file is written; the run is the runtime's to complete."),
			finalText("handed off"), finalText("handed off"), finalText("handed off"),
		},
	})
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Place = place
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	collect(t, mustSubmit(t, agent, "write the file through bash"))

	g := agent.graph()
	root := g.node(1)
	planE2EWaitSettled(t, root)

	// THE LANDED BRANCH CARRIES THE CHANGE. The merge home is the proof:
	// note.txt was written by a shell command in the worktree, named by no
	// ledger, and it reads in the person's own checkout only if the landing
	// staged the tree's status.
	written, err := os.ReadFile(filepath.Join(repo, "note.txt"))
	if err != nil {
		t.Fatalf("the branch does not carry the file the shell wrote: %v", err)
	}
	if !strings.Contains(string(written), "written through bash") {
		t.Fatalf("the landed file reads %q, want the shell's own line", written)
	}
	// AND NOTHING IS REPORTED LEFT BEHIND. The report a person reads is the
	// landing's own word: the shell's file went onto the branch, so the
	// sentence about files it did not write is empty.
	g.mu.Lock()
	report := root.report
	g.mu.Unlock()
	if strings.Contains(report, "left files it did not write") {
		t.Fatalf("the landing reported work as left behind:\n%s", report)
	}
}

// ── the dispatch stall: a turn that ends with parts nobody handed out ─────────

// TestPlandbCliASplitEndingItsTurnStillDispatches is the dispatch proof for a
// coordinator whose LAST bash call is the split itself. The pulse that runs
// after a bash call is behind it by then, and the turn that follows ends
// without another one — so the parts are handed out by the pass the turn's end
// takes, or they are handed out never: the coordinator sees no child of its own
// to wait on, lands, and the root completion that follows cancels the three
// ready tasks as work nothing will deliver. What must come back instead: all
// three parts are nodes under the coordinator, the coordinator stays open and
// parked on them while they work, and it lands only after they have — with the
// store reading every part done and none of them cancelled.
func TestPlandbCliASplitEndingItsTurnStillDispatches(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	place := Place{Dir: t.TempDir()}
	repo := newTestRepo(t)
	planE2EArmCLI(t, place)
	t.Setenv("HOME", t.TempDir())

	// THE THREE PARTS ARE HELD OPEN by their own lane's step, so the ordering
	// below is caused rather than hoped for: no part lands until the test says
	// so, and a coordinator that landed on top of them is a failure this can
	// see rather than a race it sometimes wins.
	var once sync.Once
	hold := make(chan struct{})
	free := func() { once.Do(func() { close(hold) }) }
	defer free()
	t.Cleanup(free)
	held := func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		select {
		case <-hold:
		case <-ctx.Done():
		}
		return textResponse("the part is finished"), nil
	}
	// The parts' ids are the store's own minting, which no script can know in
	// advance, so the three share the lane the mark routes them to — the same
	// mark the flag-off arm uses, carried in the description the split writes
	// and therefore in the brief the runtime composes back from that store read.
	share := `the share of the work this part owns ` + taskBriefMark

	completer := newPlanE2ECompleter(map[string][]step{
		"chat": {
			planE2EProposeStep("Three-part plan",
				"Split the work three ways through the CLI, then end the turn and let the parts run."),
			finalText("handed off"), finalText("handed off"), finalText("handed off"),
		},
		"t-root": {
			// ONE CALL, THREE PARTS, AND NO CALL AFTER IT that could carry a
			// pulse: the turn ends on the prose below.
			planE2EBashStep("plan-e2e-split",
				`plandb split t-root --into '[{"title":"Part A","description":"`+share+`"},`+
					`{"title":"Part B","description":"`+share+`"},`+
					`{"title":"Part C","description":"`+share+`"}]'`),
			finalText("the three parts are in the plan; I wait for them."),
			finalText("handed off"), finalText("handed off"), finalText("handed off"),
		},
		"child": {held, held, held},
	})
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Place = place
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	collect(t, mustSubmit(t, agent, "split the work three ways through the plan CLI"))

	g := agent.graph()
	// ALL THREE PARTS ARE NODES, AND THE COORDINATOR IS PARKED ON THEM. Parked
	// is the observable that its turn has ENDED and that it is waiting on work
	// it created ([TaskNode.waitingOnItsPieces]) — the two halves of the
	// scenario this test is about, read as one condition.
	var root *TaskNode
	waitFor(t, "the coordinator to end its turn parked on the three parts it split into", func() bool {
		root = g.node(1)
		return root != nil && len(g.children(root.id)) == 3 && root.waitingOnItsPieces()
	})
	if state := root.stateNow(); state != TaskRunning {
		t.Fatalf("the coordinator stands %q while its three parts are still held open, want it running and waiting on them", state)
	}

	// AND NOTHING WAS CANCELLED WHILE IT WAITED. A part the dispatch refused is
	// a part no worker will ever report on, and the run's ending used to read
	// exactly that as work nothing would deliver.
	for _, part := range planE2EParts(t, place) {
		if part.Status == plandb.StatusCancelled {
			t.Fatalf("part %s was cancelled while its coordinator still waited on it: %s", part.ID, part.Error)
		}
	}

	// THE PARTS LAND, AND ONLY THEN DOES THE COORDINATOR.
	free()
	for _, part := range g.children(root.id) {
		planE2EWaitSettled(t, part)
	}
	planE2EWaitSettled(t, root)
	planE2EWaitStoreRoot(t, place)

	// THE STORE'S END STATE: three parts done under their own claims, the root
	// completed by the runtime, and not one task left cancelled — the sentence
	// the stall used to end the run on.
	store := planE2EOpenStore(t, place)
	parts := planE2EParts(t, place)
	if len(parts) != 3 {
		t.Fatalf("the store holds %d parts, want the three the split wrote", len(parts))
	}
	for _, part := range parts {
		if part.Status != plandb.StatusDone {
			t.Errorf("part %s reads %q (%s), want done", part.ID, part.Status, part.Error)
		}
		if part.ClaimedBy != part.ID {
			t.Errorf("part %s is claimed by %q, want its own id", part.ID, part.ClaimedBy)
		}
	}
	for _, task := range store.Tasks() {
		if task.Status == plandb.StatusCancelled {
			t.Errorf("task %s was cancelled: %s", task.ID, task.Error)
		}
	}
	// THE TREE IT GREW: the coordinator and its three parts, every one of them
	// landed, and no fourth node — a part dispatched twice would be a store
	// task with two workers on it.
	shapes := planE2EShapes(t, g)
	if len(shapes) != 4 {
		t.Fatalf("the graph holds %d nodes, want the coordinator and its three parts: %+v", len(shapes), shapes)
	}
	for _, shape := range shapes {
		if shape.state != TaskDone {
			t.Errorf("node %s landed %q, want done", shape.planID, shape.state)
		}
	}
}

// planE2EParts is every task in the run's store below the root, in the store's
// own order — the parts a coordinator created, whatever ids the store minted
// for them.
func planE2EParts(t *testing.T, place Place) []*plandb.Task {
	t.Helper()
	var parts []*plandb.Task
	for _, task := range planE2EOpenStore(t, place).Tasks() {
		if task.ID != planRootID {
			parts = append(parts, task)
		}
	}
	return parts
}

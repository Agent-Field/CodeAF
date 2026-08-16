package session

// The task slice, as tests: the proposal's four endings, the belt a node
// carries, the frontier's scheduling law, and the branch coming home.
//
// The graph is exercised in two ways on purpose. [TestTaskFrontier…] drives
// [TaskGraph.runFrontier] with a scripted runner — no provider, no git, no
// clock — because readiness, the cap and brief assembly are the scheduler's own
// law and should be readable without a repository on the other end. The git
// tests drive the real executor end to end, because "the work came home" is not
// a claim a stub can make.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── harness ─────────────────────────────────────────────────────────────────

// taskBriefMark is what tells a child's request from the conversation's: it
// rides in the brief, so every message the node's agent sends carries it and no
// message the conversation sends does.
const taskBriefMark = "BRIEF-MARK"

// routedCompleter scripts THREE agents against one provider: the conversation,
// the node it proposes, and the auditor that decides whether the node's work is
// real. They run concurrently and would otherwise race for the next entry of a
// single list.
//
// The lanes are told apart by what only that agent's context can contain: the
// auditor by its system prompt, which is the audit contract and nothing else,
// and the node by a mark riding in its brief. The auditor is checked FIRST
// because its question quotes the node's own words back at it, and a claim
// containing the mark would otherwise route an audit into the node's script.
type routedCompleter struct {
	mu     sync.Mutex
	parent []step
	child  []step
	audit  []step
	seen   struct{ parent, child, audit int }
	// childRequests keeps what the node was actually asked, which is the only
	// place the assembled brief can be observed from the outside.
	childRequests [][]ai.Message
	// auditRequests is the same for the auditor: the only place to see what a
	// verdict was actually reached against.
	auditRequests [][]ai.Message
}

func (c *routedCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	lane := "parent"
	if len(messages) > 0 && messages[0].Role == "system" &&
		strings.Contains(messageText(messages[0]), "You are an AUDITOR") {
		lane = "audit"
	} else {
		for _, message := range messages {
			if message.Role == "user" && strings.Contains(messageText(message), taskBriefMark) {
				lane = "child"
				break
			}
		}
	}

	c.mu.Lock()
	var next step
	snapshot := make([]ai.Message, len(messages))
	copy(snapshot, messages)
	switch lane {
	case "audit":
		c.auditRequests = append(c.auditRequests, snapshot)
		if c.seen.audit < len(c.audit) {
			next = c.audit[c.seen.audit]
		}
		c.seen.audit++
	case "child":
		c.childRequests = append(c.childRequests, snapshot)
		if c.seen.child < len(c.child) {
			next = c.child[c.seen.child]
		}
		c.seen.child++
	default:
		if c.seen.parent < len(c.parent) {
			next = c.parent[c.seen.parent]
		}
		c.seen.parent++
	}
	c.mu.Unlock()

	if next == nil {
		return textResponse("(unscripted)"), nil
	}
	return next(ctx, messages)
}

func (c *routedCompleter) childAsked() []ai.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.childRequests) == 0 {
		return nil
	}
	return c.childRequests[0]
}

func (c *routedCompleter) auditAsked() []ai.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.auditRequests) == 0 {
		return nil
	}
	return c.auditRequests[0]
}

// verdict is the auditor's answer, scripted.
func verdict(text string) step { return finalText(text) }

// bashCall is one call to the auditor's (or a node's) bash.
func bashCall(id, command string) step {
	arguments, _ := json.Marshal(struct {
		Command string `json:"command"`
	}{Command: command})
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse(id, "bash", string(arguments)), nil
	}
}

// verdictFromEvidence is the auditor doing its actual job: it reads what its
// own last tool call returned and answers on THAT, so a test that asserts
// VERIFIED is asserting the verification really passed rather than asserting a
// scripted string.
func verdictFromEvidence(marker, verified, refuted string) step {
	return func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		for index := len(messages) - 1; index >= 0; index-- {
			if messages[index].Role != "tool" {
				continue
			}
			if strings.Contains(messageText(messages[index]), marker) {
				return textResponse(verified), nil
			}
			return textResponse(refuted), nil
		}
		return textResponse(refuted), nil
	}
}

// proposeCall is the model asking for one task, with the mark in the brief.
func proposeCall(title, brief string) step {
	arguments, _ := json.Marshal(taskArguments{
		Title:      title,
		Summary:    "two lines the person reads",
		Brief:      brief + "\n" + taskBriefMark,
		Acceptance: "the file is there",
	})
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse("call-task", "propose_task", string(arguments)), nil
	}
}

func finalText(text string) step {
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse(text), nil
	}
}

// stubbedGraph replaces the executor with a recorder, leaving the report hook —
// the events and the steering note — exactly as the session builds it.
func stubbedGraph(agent *Agent, run func(*TaskNode)) *TaskGraph {
	graph := agent.graph()
	graph.mu.Lock()
	graph.run = run
	graph.mu.Unlock()
	return graph
}

// drainAnsweringTasks drains one turn's stream, handing every proposal to
// answer as it arrives. It is consent_test.go's drainAnswering for the other
// question — that one only ever looks at EventConsentRequest — and it fails
// rather than hanging, because a proposal nobody resolves is exactly the fault
// worth catching.
func drainAnsweringTasks(t *testing.T, events <-chan Event, answer func(Event)) []Event {
	t.Helper()
	var collected []Event
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event, open := <-events:
			if !open {
				return collected
			}
			collected = append(collected, event)
			if event.Kind == EventTaskProposal && answer != nil {
				answer(event)
			}
		case <-deadline:
			t.Fatalf("the turn never finished; events so far: %v", kinds(collected))
			return nil
		}
	}
}

// ranNodes is where a stubbed runner reports the nodes it was handed.
//
// It is a CHANNEL and not a counter because the executor is a goroutine: the
// tool call returns the moment a node is admitted, so a test that read a count
// straight after the turn would be asking whether the scheduler had got round
// to it yet.
type ranNodes chan *TaskNode

// await is the next node to run, or a failure. Nothing here polls.
func (r ranNodes) await(t *testing.T) *TaskNode {
	t.Helper()
	select {
	case node := <-r:
		return node
	case <-time.After(5 * time.Second):
		t.Fatal("no node ran")
		return nil
	}
}

// admitted is how many nodes are in the graph, which — unlike a run — is
// settled by the time the tool call answers.
func admitted(graph *TaskGraph) int {
	graph.mu.Lock()
	defer graph.mu.Unlock()
	return len(graph.nodes)
}

// toolResultKind says HOW one call ended — cleanly or as an error — which is
// half of what a decline has to get right (memory_test.go's toolOutput carries
// the other half, the text).
func toolResultKind(events []Event, tool string) (EventKind, bool) {
	for _, event := range events {
		if event.Tool != tool {
			continue
		}
		if event.Kind == EventToolEnd || event.Kind == EventToolFailed {
			return event.Kind, true
		}
	}
	return 0, false
}

// ── the proposal's four endings ─────────────────────────────────────────────

// SILENCE IS A YES. Nobody answers, the countdown runs out, and the node starts
// — because the countdown is the person's window to redirect work the model has
// already groomed, not a gate the work waits behind.
func TestTaskProposalApprovesWhenTheClockRunsOut(t *testing.T) {
	completer := &routedCompleter{parent: []step{
		proposeCall("Fix the nil-map crash", "the whole brief"),
		finalText("handed off"),
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.TaskAutoApproveSeconds = 1
	})
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		ran <- node
		node.finish("did the thing", nil, "", "")
		node.graph.complete(node, TaskDone)
	})

	events, err := agent.Submit(context.Background(), "fix the crash")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	proposal, ok := firstOfKind(collected, EventTaskProposal)
	if !ok {
		t.Fatalf("no proposal reached the surface: %v", kinds(collected))
	}
	if proposal.Task == nil || proposal.Task.Title != "Fix the nil-map crash" {
		t.Fatalf("proposal payload = %+v", proposal.Task)
	}
	if proposal.Task.Deadline.IsZero() {
		t.Fatal("the proposal carries no deadline, so the surface can draw no countdown")
	}
	started := ran.await(t)
	if started.id != proposal.Task.ID {
		t.Fatalf("node %d ran, want the one the proposal named (%d)", started.id, proposal.Task.ID)
	}
	waitDoneNode(t, started)
	if state := graph.node(proposal.Task.ID).stateNow(); state != TaskDone {
		t.Fatalf("node state = %q, want done", state)
	}
	if kind, found := toolResultKind(collected, "propose_task"); !found || kind != EventToolEnd {
		t.Fatalf("the call did not end cleanly: %v", kinds(collected))
	}
	if output := toolOutput(t, collected, "propose_task"); !strings.Contains(output, "started") {
		t.Fatalf("tool result = %q, want it to say the task started", output)
	}
}

// A DECLINE IS A RESULT, NOT AN ERROR, and nothing starts. The model reads the
// reason as feedback on its grooming and keeps working.
func TestTaskDeniedReturnsAResultAndStartsNothing(t *testing.T) {
	completer := &routedCompleter{parent: []step{
		proposeCall("Rewrite the reconciler", "the whole brief"),
		finalText("understood"),
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		// No clock: the answer is the only thing that resolves this proposal.
		config.TaskAutoApproveSeconds = 0
	})
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		ran <- node
		node.graph.complete(node, TaskDone)
	})

	events, err := agent.Submit(context.Background(), "rewrite it")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := drainAnsweringTasks(t, events, func(event Event) {
		if event.Kind == EventTaskProposal {
			agent.ResolveTask(event.Task.ID, TaskAnswer{Approved: false, Redirect: "too big for one task"})
		}
	})

	if proposal, ok := firstOfKind(collected, EventTaskProposal); ok && !proposal.Task.Deadline.IsZero() {
		t.Fatalf("countdown 0 with somebody watching still carried a deadline: %v", proposal.Task.Deadline)
	}
	// Admission is settled by the time the call answers, so this is a fact and
	// not a race: a declined proposal never became a node at all.
	if nodes := admitted(graph); nodes != 0 {
		t.Fatalf("a declined proposal admitted %d nodes", nodes)
	}
	kind, found := toolResultKind(collected, "propose_task")
	if !found {
		t.Fatalf("the call never ended: %v", kinds(collected))
	}
	if kind != EventToolEnd {
		t.Fatal("a decline reached the model as an error result")
	}
	if output := toolOutput(t, collected, "propose_task"); !strings.Contains(output, "the person declined this task: too big for one task") {
		t.Fatalf("tool result = %q, want the decline and the reason", output)
	}
}

// A redirect is an approval WITH a correction, and the correction reaches the
// node — in the person's own voice, at the end of the brief.
func TestTaskRedirectAmendsTheBriefTheNodeGets(t *testing.T) {
	completer := &routedCompleter{parent: []step{
		proposeCall("Sweep the deprecated calls", "replace every call to Frobnicate"),
		finalText("handed off"),
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.TaskAutoApproveSeconds = 0
	})
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) {
		ran <- node
		node.graph.complete(node, TaskDone)
	})

	events, err := agent.Submit(context.Background(), "sweep them")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	drainAnsweringTasks(t, events, func(event Event) {
		if event.Kind == EventTaskProposal {
			agent.ResolveTask(event.Task.ID, TaskAnswer{Approved: true, Redirect: "leave the tests alone"})
		}
	})

	brief := ran.await(t).assembledBrief()
	if !strings.Contains(brief, "replace every call to Frobnicate") {
		t.Fatalf("the brief lost its own text: %q", brief)
	}
	if !strings.Contains(brief, "The person redirecting this task says: leave the tests alone") {
		t.Fatalf("the redirect never reached the brief: %q", brief)
	}
}

// HEADLESS NEVER WAITS. With nobody subscribed there is nobody to answer, so
// the deadline approves — including the 0 that means "wait for an answer" when
// somebody is watching.
func TestHeadlessTaskApprovesItself(t *testing.T) {
	completer := &routedCompleter{parent: []step{
		proposeCall("Run the migration", "the whole brief"),
		finalText("handed off"),
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) {
		ran <- node
		node.graph.complete(node, TaskDone)
	})

	events, err := agent.Submit(context.Background(), "migrate")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	// Nothing answered and nothing was watching, and the node ran anyway.
	ran.await(t)
}

// ── the node's belt ─────────────────────────────────────────────────────────

// A node does not propose and does not watch: there is nobody in its world to
// show a proposal to, and no conversation for a watch's news to arrive in.
func TestTaskNodeBeltLeavesOffProposeAndWatch(t *testing.T) {
	conversation, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if !hasTool(conversation, "propose_task") || !hasTool(conversation, "watch") {
		t.Fatal("the conversation is missing a hand it is supposed to have")
	}

	node, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.InTask = true
	})
	if hasTool(node, "propose_task") {
		t.Fatal("a task node carries propose_task")
	}
	if hasTool(node, "watch") {
		t.Fatal("a task node carries watch")
	}
	// Everything else is the same belt: a node is the same worker somewhere
	// quieter, not a reduced one.
	for _, name := range []string{"read", "write", "edit", "bash", "grep", "find", "ls", "jobs"} {
		if !hasTool(node, name) {
			t.Fatalf("a task node is missing %q", name)
		}
	}
}

// ── the frontier ────────────────────────────────────────────────────────────

// THE SCHEDULING LAW, with no provider and no git under it: a dependent waits
// for its prerequisite, starts the moment that one lands, and is handed what the
// work before it learned.
func TestFrontierRunsDependentsAfterPrerequisitesWithTheirReports(t *testing.T) {
	graph := newTaskGraph()
	var (
		started = make(chan uint64, 4)
		release = make(chan struct{})
		briefs  = map[uint64]string{}
		mu      sync.Mutex
	)
	graph.run = func(node *TaskNode) {
		mu.Lock()
		briefs[node.id] = node.assembledBrief()
		mu.Unlock()
		started <- node.id
		if node.id == 1 {
			<-release
		}
		node.finish(fmt.Sprintf("task %d found the shape of the bug", node.id), nil, "", "")
		node.graph.complete(node, TaskDone)
	}

	first := graph.reserve()
	second := graph.reserve()
	graph.admit(first, taskSpec{title: "find it", brief: "look for the bug", acceptance: "named"})
	graph.admit(second, taskSpec{
		title: "fix it", brief: "fix the bug", acceptance: "tests pass",
		dependsOn: []uint64{first},
	})

	if id := <-started; id != first {
		t.Fatalf("first node to run was %d, want the one with no dependencies", id)
	}
	if state := graph.node(second).stateNow(); state != TaskQueued {
		t.Fatalf("the dependent is %q while its prerequisite runs, want queued", state)
	}

	close(release)
	if id := waitStarted(t, started); id != second {
		t.Fatalf("second node to run was %d, want the dependent", id)
	}
	waitDoneNode(t, graph.node(second))

	mu.Lock()
	brief := briefs[second]
	mu.Unlock()
	if !strings.Contains(brief, "fix the bug") {
		t.Fatalf("the dependent lost its own brief: %q", brief)
	}
	if !strings.Contains(brief, "What the work before you learned") {
		t.Fatalf("the dependent was not told what came before: %q", brief)
	}
	if !strings.Contains(brief, "task 1 found the shape of the bug") {
		t.Fatalf("the prerequisite's report never reached the dependent: %q", brief)
	}
}

// The cap is a QUEUE, not a refusal: a third runnable node waits on the
// frontier and starts when a slot frees.
func TestFrontierQueuesPastTheConcurrencyCap(t *testing.T) {
	graph := newTaskGraph()
	started := make(chan uint64, 4)
	release := make(chan struct{})
	graph.run = func(node *TaskNode) {
		started <- node.id
		<-release
		node.finish("done", nil, "", "")
		node.graph.complete(node, TaskDone)
	}

	var ids []uint64
	for index := 0; index < 3; index++ {
		id := graph.reserve()
		ids = append(ids, id)
		graph.admit(id, taskSpec{title: fmt.Sprintf("node %d", id), brief: "b", acceptance: "a"})
	}

	waitStarted(t, started)
	waitStarted(t, started)
	if state := graph.node(ids[2]).stateNow(); state != TaskQueued {
		t.Fatalf("the third node is %q with %d running, want queued", state, taskMaxRunning)
	}

	close(release)
	if id := waitStarted(t, started); id != ids[2] {
		t.Fatalf("the freed slot went to node %d, want the queued one", id)
	}
	waitDoneNode(t, graph.node(ids[2]))
}

// A node whose prerequisite failed can never have its brief assembled, so it
// fails too rather than waiting on something that is not coming.
func TestFrontierFailsDependentsOfAFailedNode(t *testing.T) {
	graph := newTaskGraph()
	graph.run = func(node *TaskNode) {
		node.finish("it broke", nil, "", "")
		node.graph.complete(node, TaskFailed)
	}

	first := graph.reserve()
	second := graph.reserve()
	graph.admit(first, taskSpec{title: "find it", brief: "b", acceptance: "a"})
	graph.admit(second, taskSpec{title: "fix it", brief: "b", acceptance: "a", dependsOn: []uint64{first}})

	waitDoneNode(t, graph.node(second))
	if state := graph.node(second).stateNow(); state != TaskFailed {
		t.Fatalf("the dependent of a failed node is %q, want failed", state)
	}
	if report := graph.node(second).notice().Report; !strings.Contains(report, "did not finish") {
		t.Fatalf("the dependent's report = %q, want it to name what it waited on", report)
	}
}

func waitStarted(t *testing.T, started <-chan uint64) uint64 {
	t.Helper()
	select {
	case id := <-started:
		return id
	case <-time.After(5 * time.Second):
		t.Fatal("no node started")
		return 0
	}
}

func waitDoneNode(t *testing.T, node *TaskNode) {
	t.Helper()
	select {
	case <-node.done:
	case <-time.After(30 * time.Second):
		t.Fatalf("task %d never finished", node.id)
	}
}

// ── the working copy ────────────────────────────────────────────────────────

// A workspace that is not a repository has no isolation to offer, so the node
// runs where the person is and the merge outcome says exactly that.
func TestNonRepositoryRunsInPlace(t *testing.T) {
	workspace := t.TempDir()
	tree, err := prepareTaskTree(workspace, 1, "do the thing")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	if tree.dir != workspace {
		t.Fatalf("dir = %q, want the workspace itself", tree.dir)
	}
	if tree.merge != mergeInPlace || tree.branch != "" {
		t.Fatalf("tree = %+v, want inplace with no branch", tree)
	}
	if merge, _ := tree.comeHome("do the thing"); merge != mergeInPlace {
		t.Fatalf("comeHome = %q, want inplace", merge)
	}
}

// A merge that cannot be made keeps the branch and says so. Nothing the node
// wrote is thrown away because two people edited the same lines.
func TestConflictingMergeKeepsTheBranch(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(repo, 2, "edit the shared file")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "shared.txt"), "the node's line\n")

	// The person's branch moves under the node, on the same line.
	writeFile(t, filepath.Join(repo, "shared.txt"), "the person's line\n")
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "person")

	merge, detail := tree.comeHome("edit the shared file")
	if merge != mergeConflicted {
		t.Fatalf("merge = %q (%s), want conflicted", merge, detail)
	}
	if !strings.Contains(detail, tree.branch) {
		t.Fatalf("the detail does not name the kept branch: %q", detail)
	}
	if branches := gitOut(t, repo, "branch", "--list", tree.branch); !strings.Contains(branches, tree.branch) {
		t.Fatal("the conflicted branch was deleted: the node's work is gone")
	}
	if _, err := os.Stat(tree.dir); err != nil {
		t.Fatalf("the conflicted worktree was removed: %v", err)
	}
	// And the repository is left usable rather than mid-merge.
	if status := gitOut(t, repo, "status", "--porcelain"); strings.Contains(status, "UU ") {
		t.Fatalf("the merge was not aborted:\n%s", status)
	}
}

// END TO END: the node writes a file in its own worktree, and the work comes
// home as a merge on the person's branch with the branch cleaned up after it.
func TestTaskNodeWorkMergesIntoThePersonsBranch(t *testing.T) {
	repo := newTestRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := &routedCompleter{
		parent: []step{
			proposeCall("Add the greeting", "write hello.txt containing hi"),
			finalText("handed off"),
		},
		child: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("call-write", "write",
					`{"path":"hello.txt","content":"hi\n"}`), nil
			},
			finalText("Wrote hello.txt with the greeting.\nNothing else changed."),
		},
		// Nothing merges unverified any more (task_audit.go), so the audit is
		// part of the end-to-end path: this one reads the diff and passes it.
		audit: []step{
			bashCall("call-diff", "git diff --cached"),
			verdictFromEvidence("hello.txt", "VERIFIED — git diff --cached · hello.txt added", "REFUTED — no such change"),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	// The graph is real here: this test is about the executor.
	graph := agent.graph()
	updates := agent.TaskUpdates()

	events, err := agent.Submit(context.Background(), "add a greeting")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	node := graph.node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	waitDoneNode(t, node)
	notice := node.notice()

	if notice.State != TaskDone {
		t.Fatalf("state = %q, report = %q", notice.State, notice.Report)
	}
	if notice.Merge != mergeMerged {
		t.Fatalf("merge = %q, report = %q", notice.Merge, notice.Report)
	}
	if content := readFile(t, filepath.Join(repo, "hello.txt")); content != "hi\n" {
		t.Fatalf("the work did not land on the person's branch: %q", content)
	}
	if branches := gitOut(t, repo, "branch", "--list", notice.Branch); strings.TrimSpace(branches) != "" {
		t.Fatalf("the merged branch was kept: %q", branches)
	}
	if _, err := os.Stat(filepath.Join(repo, ".aforge-v3", "tasks", "1")); !os.IsNotExist(err) {
		t.Fatal("the merged worktree was left behind")
	}
	if len(notice.Changed) != 1 || notice.Changed[0] != "hello.txt" {
		t.Fatalf("changed = %v, want the one file the node wrote", notice.Changed)
	}
	if !strings.Contains(notice.Report, "Wrote hello.txt") {
		t.Fatalf("report = %q, want the node's own last words", notice.Report)
	}

	// The node was asked with the brief and the acceptance, and never with the
	// conversation: the brief is its whole world.
	asked := completer.childAsked()
	if len(asked) == 0 {
		t.Fatal("the node never reached the provider")
	}
	instruction := messageText(asked[len(asked)-1])
	if !strings.Contains(instruction, "write hello.txt containing hi") ||
		!strings.Contains(instruction, "Acceptance: the file is there") {
		t.Fatalf("the node was asked %q", instruction)
	}
	for _, message := range asked {
		if strings.Contains(messageText(message), "add a greeting") {
			t.Fatal("the conversation leaked into the node's context")
		}
	}

	// And the standing subscription carried the node's life, ending in done.
	if final := lastTaskUpdate(t, updates); final.State != TaskDone || final.Merge != mergeMerged {
		t.Fatalf("the update lane's last word = %+v", final)
	}
}

// A KILL KEEPS THE WORK. `jobs kill` cancels the node, and its branch and
// worktree are left exactly where they are — the point of the branch is that
// stopping a task never throws anything away.
func TestKilledTaskKeepsItsBranch(t *testing.T) {
	repo := newTestRepo(t)
	t.Setenv("HOME", t.TempDir())

	working := make(chan struct{})
	completer := &routedCompleter{
		parent: []step{
			proposeCall("Grind on the build", "keep building until it passes"),
			finalText("handed off"),
		},
		child: []step{
			func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				close(working)
				<-ctx.Done()
				return nil, ctx.Err()
			},
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	graph := agent.graph()

	events, err := agent.Submit(context.Background(), "grind on it")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	select {
	case <-working:
	case <-time.After(10 * time.Second):
		t.Fatal("the node never reached the provider")
	}
	if listed := agent.jobs.list(); !strings.Contains(listed, "task 1") {
		t.Fatalf("the node is not in the jobs list:\n%s", listed)
	}
	if text, isError := agent.jobs.kill(1); isError {
		t.Fatalf("jobs kill: %s", text)
	}

	node := graph.node(1)
	waitDoneNode(t, node)
	notice := node.notice()
	if notice.State != TaskFailed {
		t.Fatalf("a killed node is %q, want failed", notice.State)
	}
	if notice.Merge != mergeAborted {
		t.Fatalf("merge = %q, want aborted", notice.Merge)
	}
	if branches := gitOut(t, repo, "branch", "--list", notice.Branch); !strings.Contains(branches, notice.Branch) {
		t.Fatal("killing a task deleted its branch: the work is gone")
	}
}

// ── the verified frontier ───────────────────────────────────────────────────
//
// The four tests below are the whole law: a node's done-state is NOT its own
// last words. The auditor runs the repository's real verification in the node's
// worktree, and only VERIFIED merges.

// A REAL CHANGE, VERIFIED BY A REAL TEST RUN. The node writes a package and a
// test for it; the auditor runs `go test ./...` through its own bash, sees it
// pass, and its verdict — with the evidence — is what rides the report.
// With the audit row off the gate stands open BY the person's own choice: the
// node merges on its own report, the report says unaudited in so many words,
// and no auditor is ever constructed — the cost row means the cost is not
// spent either.
func TestAuditOffMergesUnaudited(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := &routedCompleter{
		parent: []step{
			proposeCall("Add the greeting", "write greet.go and its test"),
			finalText("handed off"),
		},
		child: []step{
			writeCall("call-src", "greet.go", "package greet\n\nfunc Greet() string { return \"hi\" }\n"),
			writeCall("call-test", "greet_test.go",
				"package greet\n\nimport \"testing\"\n\nfunc TestGreet(t *testing.T) {\n\tif Greet() != \"hi\" {\n\t\tt.Fatal(\"no greeting\")\n\t}\n}\n"),
			finalText("Wrote greet.go and greet_test.go."),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskAudit = false
	})
	graph := agent.graph()

	events, err := agent.Submit(context.Background(), "add a greeting")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	node := graph.node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	waitDoneNode(t, node)
	notice := node.notice()

	if notice.State != TaskDone {
		t.Fatalf("state = %q, report = %q", notice.State, notice.Report)
	}
	if !strings.HasPrefix(notice.Report, "unaudited") {
		t.Fatalf("an unaudited merge must say so first: %q", notice.Report)
	}
	if notice.Merge != mergeMerged {
		t.Fatalf("the open gate still merges: merge = %q", notice.Merge)
	}
	if content := readFile(t, filepath.Join(repo, "greet.go")); !strings.Contains(content, "func Greet") {
		t.Fatalf("the unaudited work is not on the person's branch: %q", content)
	}
	if asked := completer.auditAsked(); len(asked) != 0 {
		t.Fatalf("the auditor was constructed %d times with the row off", len(asked))
	}
}

func TestAuditVerifiesAChangeThatPassesItsTest(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := &routedCompleter{
		parent: []step{
			proposeCall("Add the greeting", "write greet.go and its test"),
			finalText("handed off"),
		},
		child: []step{
			writeCall("call-src", "greet.go", "package greet\n\nfunc Greet() string { return \"hi\" }\n"),
			writeCall("call-test", "greet_test.go",
				"package greet\n\nimport \"testing\"\n\nfunc TestGreet(t *testing.T) {\n\tif Greet() != \"hi\" {\n\t\tt.Fatal(\"no greeting\")\n\t}\n}\n"),
			finalText("Wrote greet.go and greet_test.go."),
		},
		audit: []step{
			bashCall("call-verify", "go test ./..."),
			// The verdict is read off what the run actually printed, so a
			// VERIFIED here means the test really passed.
			verdictFromEvidence("ok  \t", "VERIFIED — go test ./... ok · 2 files", "REFUTED — go test ./... did not pass"),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	graph := agent.graph()

	events, err := agent.Submit(context.Background(), "add a greeting")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	node := graph.node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	waitDoneNode(t, node)
	notice := node.notice()

	if notice.State != TaskDone {
		t.Fatalf("state = %q, report = %q", notice.State, notice.Report)
	}
	if !strings.HasPrefix(notice.Report, "VERIFIED — go test ./... ok") {
		t.Fatalf("the verdict does not lead the report: %q", notice.Report)
	}
	if !strings.Contains(notice.Report, "Wrote greet.go") {
		t.Fatalf("the node's own words were lost from the report: %q", notice.Report)
	}
	if notice.Merge != mergeMerged {
		t.Fatalf("verified work did not come home: merge = %q", notice.Merge)
	}
	if content := readFile(t, filepath.Join(repo, "greet.go")); !strings.Contains(content, "func Greet") {
		t.Fatalf("the verified work is not on the person's branch: %q", content)
	}

	// The auditor was asked against the FROZEN acceptance, was told where to
	// look, and was never handed the conversation.
	asked := completer.auditAsked()
	if len(asked) == 0 {
		t.Fatal("no audit ever ran: the frontier advanced on a self-report")
	}
	question := messageText(asked[len(asked)-1])
	if !strings.Contains(question, "ACCEPTANCE") || !strings.Contains(question, "the file is there") {
		t.Fatalf("the auditor was not given the acceptance: %q", question)
	}
	if !strings.Contains(question, "git diff --cached") {
		t.Fatalf("the auditor was not told how to see the change: %q", question)
	}
	for _, message := range asked {
		if strings.Contains(messageText(message), "add a greeting") {
			t.Fatal("the conversation leaked into the auditor's context")
		}
	}
}

// A HOLLOW NODE IS REFUTED. It says it is finished and it wrote nothing; the
// auditor runs the same verification, watches it fail, and the node FAILS with
// the auditor's evidence as its report — taking its dependents with it.
func TestAuditRefutesANodeThatOnlyClaimsToBeDone(t *testing.T) {
	repo := newGoModuleRepo(t)
	writeFile(t, filepath.Join(repo, "hollow_test.go"),
		"package greet\n\nimport \"testing\"\n\nfunc TestHollow(t *testing.T) {\n\tt.Fatal(\"nothing was fixed\")\n}\n")
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "failing")
	t.Setenv("HOME", t.TempDir())

	completer := &routedCompleter{
		parent: []step{
			proposeCall("Fix the failing test", "make TestHollow pass"),
			finalText("handed off"),
		},
		child: []step{
			// No tools, no edits: the whole node is a confident sentence.
			finalText("All done — the test passes now."),
		},
		audit: []step{
			bashCall("call-verify", "go test ./..."),
			verdictFromEvidence("FAIL", "REFUTED — go test ./... still fails: TestHollow", "VERIFIED — nothing failed"),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	graph := agent.graph()

	events, err := agent.Submit(context.Background(), "fix it")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)

	node := graph.node(1)
	waitDoneNode(t, node)
	notice := node.notice()

	if notice.State != TaskFailed {
		t.Fatalf("a node that only claimed to be done is %q, want failed (report %q)", notice.State, notice.Report)
	}
	if !strings.HasPrefix(notice.Report, "REFUTED — ") {
		t.Fatalf("report = %q, want the auditor's verdict", notice.Report)
	}
	if !strings.Contains(notice.Report, "TestHollow") {
		t.Fatalf("report = %q, want the auditor's evidence", notice.Report)
	}
	if strings.Contains(notice.Report, "All done") {
		t.Fatalf("the node's own claim survived its refutation: %q", notice.Report)
	}
	// NOTHING MERGED, and nothing was thrown away either: the branch is kept
	// exactly as a killed node's is.
	if notice.Merge != mergeAborted {
		t.Fatalf("merge = %q, want aborted — refuted work must not land", notice.Merge)
	}
	if branches := gitOut(t, repo, "branch", "--list", notice.Branch); !strings.Contains(branches, notice.Branch) {
		t.Fatal("a refuted node's branch was deleted: the work is gone")
	}

	// And the cascade: a dependent of a refuted node cannot start, because the
	// work it was going to build on does not hold.
	dependent := graph.reserve()
	graph.admit(dependent, taskSpec{
		title: "build on it", brief: "b", acceptance: "a", dependsOn: []uint64{node.id},
	})
	waitDoneNode(t, graph.node(dependent))
	if state := graph.node(dependent).stateNow(); state != TaskFailed {
		t.Fatalf("the dependent of a refuted node is %q, want failed", state)
	}
}

// THE BELT IS THE SAFETY ARGUMENT. An auditor has no hand that writes, and its
// bash runs the repository's verification and refuses everything else —
// including a destructive command, a command that runs the node's own code, and
// a verification with a second command chained onto it.
func TestAuditBeltIsReadOnly(t *testing.T) {
	belt := auditBelt(t.TempDir(), auditCommands)

	byName := map[string]bare.Tool{}
	for _, tool := range belt {
		byName[tool.Name] = tool
	}
	for _, name := range []string{"read", "grep", "find", "ls", "bash"} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("the auditor cannot %q, so it cannot gather evidence", name)
		}
	}
	for _, name := range []string{"edit", "write", "jobs", "propose_task"} {
		if _, ok := byName[name]; ok {
			t.Fatalf("the auditor carries %q: it can change what it is judging", name)
		}
	}

	for _, refused := range []struct {
		command string
		why     string
	}{
		{"rm -rf .", "destructive"},
		{"rm", "destructive"},
		{"go generate ./...", "runs the code it is judging"},
		{"go run ./cmd/thing", "runs the code it is judging"},
		{"go test ./... && rm -rf /", "a second command chained onto a verification"},
		{"go test ./... > /tmp/out", "a redirect"},
		{"echo $(rm -rf .)", "a substitution"},
		{"", "nothing at all"},
	} {
		arguments := json.RawMessage(`{"command":` + strconv.Quote(refused.command) + `}`)
		text, isError, err := byName["bash"].Execute(context.Background(), arguments)
		if err != nil {
			t.Fatalf("%q: the refusal was an error, not a result: %v", refused.command, err)
		}
		if !isError || !strings.HasPrefix(text, "refused:") {
			t.Fatalf("the auditor ran %q (%s): %q", refused.command, refused.why, text)
		}
	}

	// And what it IS for is allowed, spelled how a model actually spells it.
	for _, allowed := range []string{
		"go test ./...", "go  test ./... -run TestX", "go build ./...", "go vet ./...",
		"git diff --cached", "git status --porcelain", "git log --oneline -5",
	} {
		if refusal, ok := auditRefusal(allowed, auditCommands); !ok {
			t.Fatalf("the auditor may not run %q: %s", allowed, refusal)
		}
	}
	// A prefix is matched at a word boundary, not as a string prefix.
	if _, ok := auditRefusal("go testify", auditCommands); ok {
		t.Fatal("the allowlist matched a command that merely starts like one")
	}
}

// A VERDICT NOBODY GAVE IS A REFUTATION. Everything that is not the word
// VERIFIED leaves the work unmerged, because the frontier fails closed.
func TestAuditVerdictFailsClosed(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		answer   string
		verified bool
		report   string
	}{
		{"the word and its evidence", "VERIFIED — go test ./... ok · 3 files", true, "VERIFIED — go test ./... ok · 3 files"},
		{"the word on its own line", "VERIFIED\ngo build ./... ok", true, "VERIFIED — go build ./... ok"},
		{"a refutation with evidence", "REFUTED — TestX still fails: want 3, got 0", false, "REFUTED — TestX still fails: want 3, got 0"},
		{"an essay", "I looked at the diff and it seems VERIFIED to me.", false, "REFUTED — the auditor answered neither VERIFIED nor REFUTED"},
		{"nothing at all", "", false, "REFUTED — the auditor answered neither VERIFIED nor REFUTED"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := parseAuditVerdict(testCase.answer)
			if got.verified != testCase.verified {
				t.Fatalf("verified = %v for %q", got.verified, testCase.answer)
			}
			if got.report() != testCase.report {
				t.Fatalf("report = %q, want %q", got.report(), testCase.report)
			}
		})
	}
	// The evidence is bounded: a verdict is read off a card, not scrolled.
	long := parseAuditVerdict("REFUTED — one\ntwo\nthree\nfour\nfive")
	if lines := strings.Count(long.report(), "\n") + 1; lines > auditEvidenceLines {
		t.Fatalf("the verdict carried %d lines:\n%s", lines, long.report())
	}
}

// EXPLORATION IS PROGRESS: a research node that never writes a file is doing
// its job, and the no-progress threshold must read NEW INFORMATION as the
// progress it is. What the threshold kills is the spin — the same query
// again — not the searching.
func TestNewInformationResetsTheNoProgressClock(t *testing.T) {
	build := func(queries ...string) *routedCompleter {
		child := make([]step, 0, len(queries)+1)
		for index, query := range queries {
			child = append(child, searchCall(fmt.Sprintf("call-%d", index), query))
		}
		return &routedCompleter{
			parent: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					arguments, _ := json.Marshal(taskArguments{
						Title: "Research", Summary: "s", Brief: "research\n" + taskBriefMark,
						Acceptance: "a", NoProgress: 3, MaxSteps: 30,
					})
					return toolResponse("call-task", "propose_task", string(arguments)), nil
				},
				finalText("handed off"),
			},
			child: append(child, finalText("found things")),
		}
	}

	t.Run("distinct targets finish", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		agent, _ := newTestAgent(t, build("alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta"), func(config *Config) {
			config.AskConsent = false
			config.TaskAutoApproveSeconds = 0
			config.TaskAudit = false
			config.SearchProvider = &scriptedSearch{}
		})
		graph := agent.graph()
		collect(t, mustSubmit(t, agent, "research"))
		node := graph.node(1)
		waitDoneNode(t, node)
		if notice := node.notice(); notice.State != TaskDone {
			t.Fatalf("seven distinct searches died as a spin: %q", notice.Report)
		}
	})

	t.Run("paging one file is exploration", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		// The same path at six offsets: every call is new information, and a
		// node reading a long document to its end must not die as a spinner.
		child := make([]step, 0, 7)
		for index := 0; index < 6; index++ {
			offset := index * 100
			child = append(child, func(context.Context, []ai.Message) (*ai.Response, error) {
				arguments, _ := json.Marshal(struct {
					Path   string `json:"path"`
					Offset int    `json:"offset"`
				}{Path: "notes.md", Offset: offset})
				return toolResponse("call-page", "read", string(arguments)), nil
			})
		}
		completer := &routedCompleter{
			parent: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					arguments, _ := json.Marshal(taskArguments{
						Title: "Read", Summary: "s", Brief: "read\n" + taskBriefMark,
						Acceptance: "a", NoProgress: 3, MaxSteps: 30,
					})
					return toolResponse("call-task", "propose_task", string(arguments)), nil
				},
				finalText("handed off"),
			},
			child: append(child, finalText("read it all")),
		}
		agent, _ := newTestAgent(t, completer, func(config *Config) {
			config.AskConsent = false
			config.TaskAutoApproveSeconds = 0
			config.TaskAudit = false
		})
		graph := agent.graph()
		collect(t, mustSubmit(t, agent, "read"))
		node := graph.node(1)
		waitDoneNode(t, node)
		if notice := node.notice(); notice.State != TaskDone {
			t.Fatalf("paging a file died as a spin: %q", notice.Report)
		}
	})

	t.Run("the same target twice is the spin", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		agent, _ := newTestAgent(t, build("alpha", "alpha", "alpha", "alpha", "alpha", "alpha"), func(config *Config) {
			config.AskConsent = false
			config.TaskAutoApproveSeconds = 0
			config.TaskAudit = false
			config.SearchProvider = &scriptedSearch{}
		})
		graph := agent.graph()
		collect(t, mustSubmit(t, agent, "research"))
		node := graph.node(1)
		waitDoneNode(t, node)
		notice := node.notice()
		if notice.State != TaskFailed {
			t.Fatalf("the same search six times lived: %q", notice.Report)
		}
		if !strings.Contains(notice.Report, "stopped: 3 steps without progress") {
			t.Fatalf("report = %q, want the threshold's name", notice.Report)
		}
	})
}

// READ-HEAVY EXPLORATION IS NOT A STALL, and the counter has to know the WHOLE
// read-only belt to say so. A node that reads a scanned page, asks after the
// build it started, recalls its own state, or runs a command that only inspects
// is working; what the counter kills is the same call again, changing nothing.
func TestTheProgressCounterReadsTheWholeReadOnlyBelt(t *testing.T) {
	// Not a repository, deliberately: worktreeDirt answers "" forever here, so
	// every bash below is judged by novelty alone — which is exactly the case
	// that used to count every look at the world as a stall.
	dir := t.TempDir()
	seen := map[string]bool{}
	var dirt string
	step := func(tool, args string) bool {
		return taughtSomething(Event{Kind: EventToolEnd, Tool: tool, Args: args}, seen, dir, &dirt)
	}

	for _, call := range []struct{ tool, args string }{
		{"read", `{"path":"a.go"}`},
		{"read_document", `{"path":"scan.pdf"}`},
		{"grep", `{"pattern":"belt"}`},
		{"ls", `{"path":"internal"}`},
		{"find", `{"pattern":"*.go"}`},
		{"web_search", `{"query":"argus"}`},
		{"web_fetch", `{"url":"https://example.com"}`},
		{"jobs", `{"id":1}`},
		{"recall", `{}`},
		{"bash", `{"command":"go test ./..."}`},
		{"bash", `{"command":"git log -1"}`},
	} {
		if !step(call.tool, call.args) {
			t.Fatalf("%s %s counted as a stall", call.tool, call.args)
		}
	}

	// The spin is the SAME target again, and it is the spin for bash on exactly
	// the terms it is for everything else.
	if step("bash", `{"command":"go test ./..."}`) {
		t.Fatal("the same command twice counted as progress")
	}
	if step("read", `{"path":"a.go"}`) {
		t.Fatal("the same file twice counted as progress")
	}
	// A hand that only writes is counted as the diff it is, one branch up — it
	// must not also be spendable here as a fresh target.
	if step("edit", `{"path":"a.go"}`) || step("write", `{"path":"b.go"}`) {
		t.Fatal("a mutation counted as knowledge")
	}
}

// ── the named thresholds ────────────────────────────────────────────────────

// A SPIN DIES BY NAME. A node that keeps calling a tool that changes nothing is
// stopped at its no-progress threshold, and a node that never stops is stopped
// at its step budget — and the report says which, rather than leaving the
// person to read thirty minutes of nothing.
func TestThresholdsStopANodeThatIsNotGettingAnywhere(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		arguments taskArguments
		want      string
	}{
		{
			name: "no progress",
			arguments: taskArguments{
				Title: "Grind", Summary: "s", Brief: "spin\n" + taskBriefMark,
				Acceptance: "a", NoProgress: 3, MaxSteps: 30,
			},
			want: "stopped: 3 steps without progress",
		},
		{
			name: "the step budget",
			arguments: taskArguments{
				Title: "Grind", Summary: "s", Brief: "spin\n" + taskBriefMark,
				Acceptance: "a", NoProgress: 50, MaxSteps: 2,
			},
			want: "stopped: 2 steps and no finish",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			arguments, _ := json.Marshal(testCase.arguments)

			// The scripted child never edits and never stops: every step is one
			// more look at a directory. Fifty entries is far past either
			// threshold, so the test proves the threshold ends it rather than
			// the script running out.
			spin := make([]step, 50)
			for index := range spin {
				spin[index] = lsCall(fmt.Sprintf("call-%d", index))
			}
			completer := &routedCompleter{
				parent: []step{
					func(context.Context, []ai.Message) (*ai.Response, error) {
						return toolResponse("call-task", "propose_task", string(arguments)), nil
					},
					finalText("handed off"),
				},
				child: spin,
			}
			agent, _ := newTestAgent(t, completer, func(config *Config) {
				config.AskConsent = false
				config.TaskAutoApproveSeconds = 0
			})
			graph := agent.graph()

			events, err := agent.Submit(context.Background(), "grind")
			if err != nil {
				t.Fatalf("Submit: %v", err)
			}
			collect(t, events)

			node := graph.node(1)
			waitDoneNode(t, node)
			notice := node.notice()
			if notice.State != TaskFailed {
				t.Fatalf("a spinning node is %q, want failed (report %q)", notice.State, notice.Report)
			}
			if !strings.Contains(notice.Report, testCase.want) {
				t.Fatalf("report = %q, want it to name the threshold %q", notice.Report, testCase.want)
			}
		})
	}
}

// The defaults apply when the model names nothing, and a threshold it did name
// is the one that is used.
func TestTaskThresholdsDefaultAndOverride(t *testing.T) {
	graph := newTaskGraph()
	graph.run = func(*TaskNode) {}

	plain := graph.reserve()
	graph.admit(plain, taskSpec{title: "t", brief: "b", acceptance: "a"})
	if limits := graph.node(plain).limits(); limits.maxSteps != taskMaxSteps || limits.noProgress != taskNoProgress {
		t.Fatalf("a node that named no threshold got %+v, want the defaults", limits)
	}

	own := graph.reserve()
	graph.admit(own, taskSpec{title: "t", brief: "b", acceptance: "a", maxSteps: 120, noProgress: 20})
	if limits := graph.node(own).limits(); limits.maxSteps != 120 || limits.noProgress != 20 {
		t.Fatalf("a node that named its thresholds got %+v", limits)
	}

	// And a negative one is a mistake said out loud rather than a default
	// quietly substituted.
	arguments, _ := json.Marshal(taskArguments{
		Title: "t", Summary: "s", Brief: "b", Acceptance: "a", MaxSteps: -1,
	})
	if _, problem := parseTaskArguments(arguments); !strings.Contains(problem, "max_steps cannot be negative") {
		t.Fatalf("a negative max_steps was accepted: %q", problem)
	}
}

// ── the goal contract ───────────────────────────────────────────────────────

// A REDIRECT AMENDS BEFORE ADMISSION, NEVER AFTER. The person's correction is
// part of the node's brief from the first instant it exists; once admitted, the
// brief and the acceptance are frozen — a later answer to the same proposal
// changes nothing, and the auditor judges the SAME text the node was finished
// against.
func TestGoalContractFreezesAtAdmission(t *testing.T) {
	completer := &routedCompleter{parent: []step{
		proposeCall("Sweep the deprecated calls", "replace every call to Frobnicate"),
		finalText("handed off"),
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.TaskAutoApproveSeconds = 0
	})
	ran := make(ranNodes, 2)
	hold := make(chan struct{})
	graph := stubbedGraph(agent, func(node *TaskNode) {
		ran <- node
		<-hold
		node.finish("done", nil, "", "")
		node.graph.complete(node, TaskDone)
	})

	events, err := agent.Submit(context.Background(), "sweep them")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	var proposed uint64
	drainAnsweringTasks(t, events, func(event Event) {
		if event.Kind == EventTaskProposal {
			proposed = event.Task.ID
			agent.ResolveTask(proposed, TaskAnswer{Approved: true, Redirect: "leave the tests alone"})
		}
	})

	node := ran.await(t)
	admittedBrief := node.assembledBrief()
	if !strings.Contains(admittedBrief, "The person redirecting this task says: leave the tests alone") {
		t.Fatalf("the redirect never reached the admitted brief: %q", admittedBrief)
	}
	admittedAcceptance := node.acceptance()

	// A SECOND ANSWER, arriving while the node runs, moves nothing. There is no
	// path from here to the running node's goal, which is the whole point: an
	// acceptance that can move while the work runs is an acceptance the work can
	// always be made to hit.
	agent.ResolveTask(proposed, TaskAnswer{Approved: true, Redirect: "actually rewrite the tests too"})

	if brief := node.assembledBrief(); brief != admittedBrief {
		t.Fatalf("the brief moved after admission:\n%q\n%q", admittedBrief, brief)
	}
	if acceptance := node.acceptance(); acceptance != admittedAcceptance {
		t.Fatalf("the acceptance moved after admission: %q -> %q", admittedAcceptance, acceptance)
	}
	if strings.Contains(node.instruction(), "rewrite the tests too") {
		t.Fatal("a late redirect reached the running node's instruction")
	}

	// And the auditor reads that same frozen text — one acceptance, two
	// readers, so the work cannot be finished against one and judged against
	// another.
	question := auditQuestion(node, taskTree{root: "/repo"}, nil, "it claims it is done")
	if !strings.Contains(question, admittedAcceptance) {
		t.Fatalf("the auditor was given a different acceptance:\n%s", question)
	}
	if strings.Contains(question, "rewrite the tests too") {
		t.Fatal("the late redirect reached the auditor")
	}
	close(hold)
	waitDoneNode(t, graph.node(proposed))
}

// ── go-module helpers ───────────────────────────────────────────────────────

// newGoModuleRepo is a repository the auditor can actually verify: a module
// with nothing in it, so `go test ./...` is a real command with a real answer.
func newGoModuleRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not on PATH")
	}
	// The build cache is PINNED to the machine's own before these tests move
	// HOME (the node's journal needs a scratch home). Left to follow HOME, every
	// `go test ./...` an auditor runs would rebuild the standard library into an
	// empty directory, which is six seconds per test to prove nothing.
	cache, err := exec.Command("go", "env", "GOCACHE").Output()
	if err != nil {
		t.Skipf("go env GOCACHE: %v", err)
	}
	t.Setenv("GOCACHE", strings.TrimSpace(string(cache)))

	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "go.mod"), "module taskaudit\n\ngo 1.25\n")
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "module")
	return repo
}

// writeCall is the node writing one file.
func writeCall(id, path, content string) step {
	arguments, _ := json.Marshal(struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}{Path: path, Content: content})
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse(id, "write", string(arguments)), nil
	}
}

// searchCall is one successful web_search step against the given query —
// the shape of research.
func searchCall(id, query string) step {
	arguments, _ := json.Marshal(struct {
		Query string `json:"query"`
	}{Query: query})
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse(id, "web_search", string(arguments)), nil
	}
}

// lsCall is one step that succeeds and changes nothing — the shape of a spin.
func lsCall(id string) step {
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse(id, "ls", `{"path":"."}`), nil
	}
}

// lastTaskUpdate drains the standing lane until the node is final.
func lastTaskUpdate(t *testing.T, updates <-chan Event) TaskNotice {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case event := <-updates:
			if event.Task == nil {
				continue
			}
			if event.Task.State == TaskDone || event.Task.State == TaskFailed {
				return *event.Task
			}
		case <-deadline:
			t.Fatal("no final update reached the standing lane")
			return TaskNotice{}
		}
	}
}

// ── git helpers ─────────────────────────────────────────────────────────────

func newTestRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	repo := t.TempDir()
	mustGit(t, repo, "init")
	mustGit(t, repo, "checkout", "-b", "work")
	writeFile(t, filepath.Join(repo, "shared.txt"), "the original line\n")
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "first")
	return repo
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := git(dir, args...); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := git(dir, args...)
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

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
	"errors"
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
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/search"
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

// auditCalls is how many times an AUDITOR was asked anything at all. It is the
// only place a retry is visible from the outside: one audit that answered and
// one audit that was asked twice look the same on the node, and the difference
// between them is the whole of the retry law (task_audit.go).
func (c *routedCompleter) auditCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.auditRequests)
}

// auditAskedAt is one particular call to an auditor, which is how the LADDER is
// told apart from the outside: a nudge carries the demand for the word and a
// fresh auditor carries the evidence packet again.
func (c *routedCompleter) auditAskedAt(index int) []ai.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	if index < 0 || index >= len(c.auditRequests) {
		return nil
	}
	return c.auditRequests[index]
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
func proposeCall(title, brief string, checks ...string) step {
	arguments, _ := json.Marshal(taskArguments{
		Title:       title,
		Summary:     "two lines the person reads",
		Brief:       brief + "\n" + taskBriefMark,
		Deliverable: "the file, at the path named in the brief",
		Acceptance:  "the file is there",
		// The proposal is where a command becomes something the node's checker may
		// run, and a test that wants its checker to run one has to declare it here
		// exactly as a real proposal would (task_checks.go).
		Checks: checks,
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

// V2: Holding a proposal in the engine survives the old deadline and leaves
// the question pending until a real answer arrives.
func TestTaskProposalHoldSurvivesTheOldDeadline(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.AskConsent = true
		config.TaskAutoApproveSeconds = 15
	})
	hub := newEventHub()
	firstWatcher := hub.subscribe()
	secondWatcher := hub.subscribe()
	agent.mu.Lock()
	agent.hub = hub
	agent.mu.Unlock()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	expiry := make(chan time.Time, 1)
	stopped := make(chan struct{})
	timerAsked := make(chan time.Duration, 1)
	agent.taskNow = func() time.Time { return now }
	agent.taskTimer = func(after time.Duration) (<-chan time.Time, func()) {
		timerAsked <- after
		return expiry, func() { close(stopped) }
	}

	type result struct {
		answer TaskAnswer
		err    error
	}
	finished := make(chan result, 1)
	go func() {
		answer, err := agent.askTask(context.Background(), 41, taskSpec{title: "Hold this proposal"}, "")
		finished <- result{answer: answer, err: err}
	}()

	if got := <-timerAsked; got != 15*time.Second {
		t.Fatalf("proposal timer = %s, want 15s", got)
	}
	for at, watcher := range []<-chan Event{firstWatcher, secondWatcher} {
		event := <-watcher
		if event.Kind != EventTaskProposal || event.Task == nil || event.Task.ID != 41 {
			t.Fatalf("watcher %d initial proposal = %+v", at+1, event)
		}
	}
	agent.mu.Lock()
	deadline := agent.taskAnswers[41].notice.Deadline
	agent.mu.Unlock()
	if want := now.Add(15 * time.Second); !deadline.Equal(want) {
		t.Fatalf("proposal deadline = %s, want %s", deadline, want)
	}

	agent.HoldTask(41)
	agent.mu.Lock()
	heldDeadline := agent.taskAnswers[41].notice.Deadline
	agent.mu.Unlock()
	if !heldDeadline.IsZero() {
		t.Fatalf("held proposal kept deadline %s", heldDeadline)
	}
	for at, watcher := range []<-chan Event{firstWatcher, secondWatcher} {
		event := <-watcher
		if event.Kind != EventTaskProposal || event.Task == nil || !event.Task.Deadline.IsZero() {
			t.Fatalf("watcher %d hold update = %+v", at+1, event)
		}
	}

	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("holding the proposal did not stop its engine timer")
	}
	now = now.Add(16 * time.Second)
	expiry <- now
	select {
	case got := <-finished:
		t.Fatalf("old deadline answered the held proposal: %+v", got)
	default:
	}

	agent.ResolveTask(41, TaskAnswer{Approved: false})
	select {
	case got := <-finished:
		if got.err != nil || got.answer.Approved {
			t.Fatalf("decline after hold = %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the held proposal did not accept a later answer")
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
// frontier and starts when a slot frees. Two is what the frontier used to hold
// unconditionally, and it is what a person who writes 2 into task.parallel
// still gets.
func TestFrontierQueuesPastTheConcurrencyCap(t *testing.T) {
	graph := newTaskGraph()
	graph.limit = 2
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
		t.Fatalf("the third node is %q with %d running, want queued", state, graph.limit)
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
	tree, err := prepareTaskTree(Place{}, workspace, "s1", 1, "do the thing")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	if tree.dir != workspace {
		t.Fatalf("dir = %q, want the workspace itself", tree.dir)
	}
	if tree.merge != mergeInPlace || tree.branch != "" {
		t.Fatalf("tree = %+v, want inplace with no branch", tree)
	}
	if merge, _, _ := tree.comeHome("do the thing", nil); merge != mergeInPlace {
		t.Fatalf("comeHome = %q, want inplace", merge)
	}
}

// A merge that cannot be made keeps the branch and says so. Nothing the node
// wrote is thrown away because two people edited the same lines.
func TestConflictingMergeKeepsTheBranch(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "s1", 2, "edit the shared file")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "shared.txt"), "the node's line\n")

	// The person's uncommitted work stands in the merge's way on the same line.
	// Their branch has not moved, so the merge itself still gets to answer.
	writeFile(t, filepath.Join(repo, "shared.txt"), "the person's line\n")

	merge, detail, _ := tree.comeHome("edit the shared file", []string{"shared.txt"})
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

// C6: END TO END, the node writes a file in its own worktree, and the work
// comes home as a merge on the person's feature branch with the branch and
// working copy cleaned up after it.
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
		config.Place = Place{Dir: t.TempDir(), Workspace: repo}
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

	// The node was asked with the opening message and nothing else: the person's
	// own words, the work, what to produce and what done means (task_brief.go).
	// THE CONVERSATION ITSELF STILL DOES NOT TRAVEL — the summary the person was
	// shown on the card stays in the conversation, and so does everything else
	// said in the turn.
	asked := completer.childAsked()
	if len(asked) == 0 {
		t.Fatal("the node never reached the provider")
	}
	instruction := messageText(asked[len(asked)-1])
	for _, want := range []string{
		briefAskHeading, "add a greeting",
		"write hello.txt containing hi",
		briefDoneHeading, "the file is there",
	} {
		if !strings.Contains(instruction, want) {
			t.Fatalf("the node was asked %q, missing %q", instruction, want)
		}
	}
	for _, message := range asked {
		if strings.Contains(messageText(message), "two lines the person reads") {
			t.Fatal("the conversation leaked into the node's context")
		}
	}

	// And the standing subscription carried the node's life, ending in done.
	if final := lastTaskUpdate(t, updates); final.State != TaskDone || final.Merge != mergeMerged {
		t.Fatalf("the update lane's last word = %+v", final)
	}
}

// C7: END TO END, verified work cut from main finishes done but leaves main
// byte-for-byte where it was; the task branch is the durable finished result.
func TestTaskNodeWorkIsKeptOffThePersonsProtectedBranch(t *testing.T) {
	repo := newTestRepo(t)
	mustGit(t, repo, "checkout", "-b", "main")
	t.Setenv("HOME", t.TempDir())
	before := strings.TrimSpace(gitOut(t, repo, "rev-parse", "main"))
	beforeStatus := gitOut(t, repo, "status", "--porcelain")

	completer := &routedCompleter{
		parent: []step{
			proposeCall("Add the protected greeting", "write protected.txt containing safe"),
			finalText("handed off"),
		},
		child: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("call-write", "write",
					`{"path":"protected.txt","content":"safe\n"}`), nil
			},
			finalText("Wrote protected.txt with the greeting."),
		},
		audit: []step{
			bashCall("call-diff", "git diff --cached"),
			verdictFromEvidence("protected.txt", "VERIFIED — protected.txt added", "REFUTED — no such change"),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.Place = Place{Dir: t.TempDir(), Workspace: repo}
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	graph := agent.graph()
	updates := agent.TaskUpdates()
	events, err := agent.Submit(context.Background(), "add a protected greeting")
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
	if notice.State != TaskDone || notice.Merge != mergeKept {
		t.Fatalf("settled notice = %+v, want done with kept branch", notice)
	}
	if got := strings.TrimSpace(gitOut(t, repo, "rev-parse", "main")); got != before {
		t.Fatalf("main moved from %s to %s", before, got)
	}
	if got := gitOut(t, repo, "status", "--porcelain"); got != beforeStatus {
		t.Fatalf("checkout status changed from %q to %q", beforeStatus, got)
	}
	if _, err := os.Stat(filepath.Join(repo, "protected.txt")); !os.IsNotExist(err) {
		t.Fatalf("protected.txt reached the live checkout: %v", err)
	}
	if got := gitOut(t, repo, "show", notice.Branch+":protected.txt"); got != "safe\n" {
		t.Fatalf("kept branch contains %q", got)
	}
	if branches := gitOut(t, repo, "branch", "--list", notice.Branch); !strings.Contains(branches, notice.Branch) {
		t.Fatalf("finished branch was not kept: %q", branches)
	}
	if list := gitOut(t, repo, "worktree", "list", "--porcelain"); strings.Contains(list, notice.Where) {
		t.Fatalf("finished working copy stayed registered:\n%s", list)
	}
	want := "its branch " + notice.Branch + " was kept: your checkout is on main, which aforge never writes to — merge it when you are ready"
	if !strings.Contains(notice.Report, want) {
		t.Fatalf("report = %q, want protected sentence %q", notice.Report, want)
	}
	if final := lastTaskUpdate(t, updates); final.State != TaskDone || final.Merge != mergeKept {
		t.Fatalf("the update lane's last word = %+v", final)
	}
}

// A surface owns the Submit context and the subscription, never the task. A
// replacement subscription sees the landing after the original surface leaves.
func TestTaskOutlivesSurfaceDetachAndReattachSeesLanding(t *testing.T) {
	repo := newTestRepo(t)
	t.Setenv("HOME", t.TempDir())
	started, release := make(chan struct{}), make(chan struct{})
	completer := &routedCompleter{
		parent: []step{proposeCall("Keep working", "finish after the surface leaves"), finalText("handed off")},
		child: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(started)
			select {
			case <-release:
				return textResponse("finished after detach"), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAudit = false
	})
	surfaceCtx, detach := context.WithCancel(context.Background())
	events, err := agent.Submit(surfaceCtx, "hand this off")
	if err != nil {
		t.Fatal(err)
	}
	// THE SURFACE SIDE IS ONLY DRAINED, never judged: this test is about what
	// the detached task does, and a helper that could fail the test from a
	// goroutine after the test returned is a vet finding and a flake in waiting.
	go func() {
		for range events {
		}
	}()
	waitSignal(t, started, "the task to start")
	detach()
	rejoined := agent.TaskUpdates()
	close(release)
	notice := awaitTaskState(t, rejoined, 1, TaskDone)
	if !strings.Contains(notice.Report, "finished after detach") {
		t.Fatalf("reattached surface saw report %q", notice.Report)
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
	if text, isError := agent.jobs.kill(context.Background(), 1); isError {
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

func TestCloseJournalsAnInflightTaskAsResumableNotFailed(t *testing.T) {
	repo := newTestRepo(t)
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	started := make(chan struct{})
	completer := &routedCompleter{
		parent: []step{proposeCall("Long task", "keep working"), finalText("handed off")},
		child: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		}},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.SessionFile = journal
		config.AskConsent = false
	})
	collect(t, mustSubmit(t, agent, "start it"))
	waitSignal(t, started, "the task to enter its provider call")
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	record := recordOf(t, readCheckpoint(t, taskCheckpointPath(journal)), 1)
	if record.State != TaskRunning || strings.Contains(record.Report, "failed") {
		t.Fatalf("close checkpoint = %+v, want a resumable running interrupt", record)
	}
	if !strings.Contains(record.Report, "paused — it resumes") {
		t.Fatalf("close report = %q, want the resume promise", record.Report)
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
			proposeCall("Add the greeting", "write greet.go and its test", "go test ./..."),
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
	if !strings.HasPrefix(notice.Report, "nothing checked this work") {
		t.Fatalf("an unchecked merge must say so first: %q", notice.Report)
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
			proposeCall("Add the greeting", "write greet.go and its test", "go test ./..."),
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
	// THE WORK'S OWN ACCOUNT LEADS, AND THE MACHINERY IS NOT THERE. The state
	// says done; what the report adds under the account is what was run and what
	// was seen (task_audit.go's vocabulary law).
	if !strings.HasPrefix(notice.Report, "Wrote greet.go and greet_test.go.") {
		t.Fatalf("the node's own account does not lead the report: %q", notice.Report)
	}
	if !strings.Contains(notice.Report, "go test ./... ok") {
		t.Fatalf("the evidence was lost from the report: %q", notice.Report)
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
			proposeCall("Fix the failing test", "make TestHollow pass", "go test ./..."),
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
	// The person reads "incomplete", never the harness's own word for it — and
	// with the repair loop turned off (the zero value of Config.TaskRepairRounds)
	// the first finding lands the node, exactly as it always did.
	if !strings.HasPrefix(notice.Report, incompleteLead) {
		t.Fatalf("report = %q, want the plain finding", notice.Report)
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

// ── when the auditor does not answer ────────────────────────────────────────

// A BROKEN AUDITOR MUST NOT FAIL GOOD WORK. The node did its job and the
// auditor came back with prose instead of a verdict — twice, having been asked
// again — so the node lands UNVERIFIED: not done, not failed, branch kept,
// nothing merged, and the auditor's own words on the card so the person can see
// what they are being asked to decide about.
func TestAuditNonVerdictRetriesAndLandsUnverified(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := &routedCompleter{
		parent: []step{
			proposeCall("Add the greeting", "write greet.go"),
			finalText("handed off"),
		},
		child: []step{
			writeCall("call-src", "greet.go", "package greet\n\nfunc Greet() string { return \"hi\" }\n"),
			finalText("Wrote greet.go with the greeting."),
		},
		// Not one of the four is a verdict, and four is the whole ladder: the
		// first auditor is asked and then NUDGED, then a fresh one is asked and
		// nudged in its turn. The first answer is the failure seen in the wild —
		// an auditor that reasoned and never said the word.
		audit: []step{
			verdict("I had a look at the change and honestly it is hard to say either way."),
			verdict("Still weighing it up, sorry."),
			verdict("A second pair of eyes here, and it is no clearer."),
			verdict("Same again: I am not able to give you a firm answer here."),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "add a greeting"))

	node := graph.node(1)
	waitDoneNode(t, node)
	notice := node.notice()

	if notice.State != TaskUnverified {
		t.Fatalf("state = %q, want unverified — a non-verdict is not a failure (report %q)", notice.State, notice.Report)
	}
	if !strings.HasPrefix(notice.Report, needsLookLead) {
		t.Fatalf("report = %q, want it to lead with the plain non-answer", notice.Report)
	}
	if strings.HasPrefix(notice.Report, incompleteLead) {
		t.Fatalf("the report calls a non-answer a finding: %q", notice.Report)
	}
	// THE LADDER RAN ONCE AND ONLY ONCE: two auditors, each asked and then
	// nudged for the word, and nothing after that.
	if calls := completer.auditCalls(); calls != 4 {
		t.Fatalf("the audit ran %d times, want 4: two auditors, each nudged once", calls)
	}
	// THE SECOND RUNG IS A FRESH AUDITOR AND IT IS REALLY FRESH: the evidence
	// packet again, and not one word of what the first one said. A "fresh"
	// auditor that could read the last one's reply would be the retry priming
	// itself (task_audit.go's newAuditAgent mints a journal per attempt).
	fresh := completer.auditAskedAt(2)
	if len(fresh) == 0 || !strings.Contains(messageText(fresh[len(fresh)-1]), "ACCEPTANCE") {
		t.Fatal("the third call was not a fresh evidence packet")
	}
	for _, message := range fresh {
		if strings.Contains(messageText(message), "hard to say either way") {
			t.Fatal("the fresh auditor opened with the first one's words in front of it")
		}
	}
	if !strings.Contains(notice.Report, "asked twice") {
		t.Fatalf("the report does not say the auditor was asked again: %q", notice.Report)
	}
	// THE AUDITOR'S OWN WORDS ARE THE OUTCOME TEXT: that is the whole basis on
	// which somebody is being asked to decide.
	if !strings.Contains(notice.Report, "A second pair of eyes here") {
		t.Fatalf("the checker's words were dropped from the report: %q", notice.Report)
	}
	// And the node's own claim is kept under it — the other half of the
	// decision.
	if !strings.Contains(notice.Report, "Wrote greet.go") {
		t.Fatalf("the node's claim was dropped from the report: %q", notice.Report)
	}
	// NOTHING MERGED and nothing was thrown away.
	if notice.Merge != mergeAborted {
		t.Fatalf("merge = %q, want aborted — unverified work must not land", notice.Merge)
	}
	if branches := gitOut(t, repo, "branch", "--list", notice.Branch); !strings.Contains(branches, notice.Branch) {
		t.Fatal("an unverified node's branch was deleted: the work is gone")
	}
	if _, err := os.Stat(filepath.Join(repo, "greet.go")); !os.IsNotExist(err) {
		t.Fatal("unverified work landed on the person's branch")
	}
	// The harness is not wedged: the session still runs a turn.
	collect(t, mustSubmit(t, agent, "what now"))
}

// THE RETRY IS THE POINT. One truncated reply is a blip, not a broken auditor:
// the second call gets a real verdict, and the node lands on THAT — verified,
// merged, exactly as if the first attempt had never happened.
func TestAuditRetryRecoversAVerdict(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := &routedCompleter{
		parent: []step{
			proposeCall("Add the greeting", "write greet.go"),
			finalText("handed off"),
		},
		child: []step{
			writeCall("call-src", "greet.go", "package greet\n\nfunc Greet() string { return \"hi\" }\n"),
			finalText("Wrote greet.go."),
		},
		audit: []step{
			// A REPLY THAT ARRIVED AND MISSED THE WORD, which is what the nudge
			// ladder is for. It is deliberately not an empty 200: that is a call
			// that did not happen, the response boundary reads it as the wire and
			// asks again on the spot, and the auditor is never nudged because it
			// was never heard from (taxonomy_boundary.go).
			verdict("I read the diff and it looks about right to me."),
			verdict("VERIFIED — go test ./... ok · 1 file"),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "add a greeting"))

	node := graph.node(1)
	waitDoneNode(t, node)
	notice := node.notice()

	if notice.State != TaskDone {
		t.Fatalf("state = %q, report = %q — the retry's verdict was not read", notice.State, notice.Report)
	}
	if !strings.Contains(notice.Report, "go test ./... ok") {
		t.Fatalf("report = %q, want the second attempt's evidence", notice.Report)
	}
	if notice.Merge != mergeMerged {
		t.Fatalf("merge = %q, want merged", notice.Merge)
	}
	if calls := completer.auditCalls(); calls != 2 {
		t.Fatalf("the audit ran %d times, want 2", calls)
	}
	// AND THE SECOND CALL WAS THE NUDGE, not a second investigation: the first
	// auditor delivered a reply, so it was asked for the word rather than
	// replaced (task_audit.go's ladder).
	if asked := completer.auditAskedAt(1); !strings.Contains(messageText(asked[len(asked)-1]), auditNudge) {
		t.Fatal("the second call was not the nudge: a fresh auditor re-paid the whole investigation")
	}
}

// A PROVIDER THAT FELL OVER IS NOT EVIDENCE ABOUT THE WORK. The audit call
// itself errors, twice, and the node lands unverified rather than failed —
// which is the same law as a non-answer, because it is the same absence.
func TestAuditProviderErrorLandsUnverified(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	broken := func(context.Context, []ai.Message) (*ai.Response, error) {
		return nil, errors.New("the auditor's provider fell over")
	}
	completer := &routedCompleter{
		parent: []step{
			proposeCall("Add the greeting", "write greet.go"),
			finalText("handed off"),
		},
		child: []step{
			writeCall("call-src", "greet.go", "package greet\n\nfunc Greet() string { return \"hi\" }\n"),
			finalText("Wrote greet.go."),
		},
		audit: []step{broken, broken},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "add a greeting"))

	node := graph.node(1)
	waitDoneNode(t, node)
	notice := node.notice()

	if notice.State != TaskUnverified {
		t.Fatalf("state = %q, want unverified — a provider error is not a refutation (report %q)", notice.State, notice.Report)
	}
	if calls := completer.auditCalls(); calls != 2 {
		t.Fatalf("the audit ran %d times, want 2: a provider error is worth one more call", calls)
	}
	if branches := gitOut(t, repo, "branch", "--list", notice.Branch); !strings.Contains(branches, notice.Branch) {
		t.Fatal("the branch was dropped after an audit nobody could run")
	}
	// The turn after it still works: no wedge, no swallowed turn.
	collect(t, mustSubmit(t, agent, "and now"))
}

// ── resolving what nobody could verify ──────────────────────────────────────

// AN UNVERIFIED CLAIM IS NOT EVIDENCE, SO A DEPENDENT WAITS. It does not fail
// in a cascade — nobody said the work is wrong — and it does not start either,
// because its brief would be assembled from a report nothing stands behind.
// What moves it is a person accepting the work.
func TestDependentWaitsOnUnverifiedAndRunsWhenItIsAccepted(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	ran := make(ranNodes, 4)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		ran <- node
		// The first node lands unverified; anything after it is ordinary work.
		if node.id == 1 {
			node.finish("UNVERIFIED — the auditor answered neither VERIFIED nor REFUTED", nil, "", "")
			node.graph.complete(node, TaskUnverified)
			return
		}
		node.finish("done", nil, "", "")
		node.graph.complete(node, TaskDone)
	})

	first := graph.reserve()
	graph.admit(first, taskSpec{title: "Research the thing", brief: "b", acceptance: "a"})
	ran.await(t)
	waitDoneNode(t, graph.node(first))

	second := graph.reserve()
	graph.admit(second, taskSpec{title: "Build on it", brief: "b", acceptance: "a", dependsOn: []uint64{first}})
	if state := graph.node(second).stateNow(); state != TaskQueued {
		t.Fatalf("the dependent of an unverified node is %q, want queued — it must neither fail nor start", state)
	}

	// ACCEPTED. The frontier turns on the settle, and the dependent goes.
	if err := agent.ResolveUnverified(first, TaskAccept, "I read the diff myself"); err != nil {
		t.Fatalf("ResolveUnverified: %v", err)
	}
	if state := graph.node(first).stateNow(); state != TaskDone {
		t.Fatalf("an accepted node is %q, want done", state)
	}
	report := graph.node(first).notice().Report
	if !strings.HasPrefix(report, "you looked at this yourself and took it as done") || !strings.Contains(report, "I read the diff myself") {
		t.Fatalf("report = %q, want the person's decision and their reason", report)
	}
	if started := ran.await(t); started.id != second {
		t.Fatalf("task %d ran, want the dependent %d", started.id, second)
	}
	waitDoneNode(t, graph.node(second))

	// The slot came back exactly once: a re-settle must not hand the graph a
	// second one and quietly raise the cap.
	graph.mu.Lock()
	running := graph.running
	graph.mu.Unlock()
	if running != 0 {
		t.Fatalf("running = %d after everything landed, want 0", running)
	}
	// And a node that is not unverified cannot be resolved at all.
	if err := agent.ResolveUnverified(first, TaskAccept, ""); err == nil {
		t.Fatal("a node that is already done was resolved a second time")
	}
}

// ASKING AGAIN IS AN ANSWER TOO. The person does not have to decide themselves:
// a re-audit sends a fresh auditor at the same working copy, the node stays
// unverified while it looks, and the verdict it gives lands the node exactly as
// the first one would have — verified work merges.
func TestReauditingAnUnverifiedNodeLandsItsVerdict(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := &routedCompleter{
		parent: []step{
			proposeCall("Add the greeting", "write greet.go"),
			finalText("handed off"),
		},
		child: []step{
			writeCall("call-src", "greet.go", "package greet\n\nfunc Greet() string { return \"hi\" }\n"),
			finalText("Wrote greet.go."),
		},
		// Four non-answers land it unverified — two auditors, each asked and
		// then nudged — and the fifth, the person's re-audit, is a real verdict.
		audit: []step{
			verdict("hard to say"),
			verdict("still hard to say"),
			verdict("a second look, and still hard to say"),
			verdict("no clearer than it was"),
			verdict("VERIFIED — go test ./... ok · 1 file"),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	graph := agent.graph()
	updates := agent.TaskUpdates()
	collect(t, mustSubmit(t, agent, "add a greeting"))

	node := graph.node(1)
	waitDoneNode(t, node)
	if state := node.stateNow(); state != TaskUnverified {
		t.Fatalf("state = %q, want unverified", state)
	}

	if err := agent.ResolveUnverified(1, TaskReaudit, ""); err != nil {
		t.Fatalf("ResolveUnverified: %v", err)
	}
	notice := awaitTaskState(t, updates, 1, TaskDone)

	if !strings.Contains(notice.Report, "go test ./... ok") {
		t.Fatalf("report = %q, want the re-audit's evidence", notice.Report)
	}
	if notice.Merge != mergeMerged {
		t.Fatalf("merge = %q, want merged — a re-audit that verifies brings the work home", notice.Merge)
	}
	if content := readFile(t, filepath.Join(repo, "greet.go")); !strings.Contains(content, "func Greet") {
		t.Fatalf("the re-verified work is not on the person's branch: %q", content)
	}
	if calls := completer.auditCalls(); calls != 5 {
		t.Fatalf("the audit ran %d times, want 5: four at the gate and one the person asked for", calls)
	}
}

// awaitTaskState reads the standing task lane until one node reaches a state.
// Nothing here polls: the settle a resolution turns is announced on the same
// lane every other landing rides.
func awaitTaskState(t *testing.T, updates <-chan Event, id uint64, want TaskState) TaskNotice {
	t.Helper()
	deadline := time.After(30 * time.Second)
	for {
		select {
		case event, open := <-updates:
			if !open {
				t.Fatalf("the task lane closed before task %d was %s", id, want)
			}
			if event.Task != nil && event.Task.ID == id && event.Task.State == want {
				return *event.Task
			}
		case <-deadline:
			t.Fatalf("task %d never reached %s", id, want)
		}
	}
}

// REFUTING BY HAND IS A REAL REFUTATION, and it cascades exactly as the
// auditor's would: the person looked, the work does not hold, and everything
// built on it fails with it.
func TestRefutingAnUnverifiedNodeCascades(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	ran := make(ranNodes, 4)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		ran <- node
		node.finish("UNVERIFIED — nobody could say", nil, "", "")
		node.graph.complete(node, TaskUnverified)
	})

	first := graph.reserve()
	graph.admit(first, taskSpec{title: "Research the thing", brief: "b", acceptance: "a"})
	ran.await(t)
	waitDoneNode(t, graph.node(first))

	second := graph.reserve()
	graph.admit(second, taskSpec{title: "Build on it", brief: "b", acceptance: "a", dependsOn: []uint64{first}})

	if err := agent.ResolveUnverified(first, TaskRefute, "the tests it claims do not exist"); err != nil {
		t.Fatalf("ResolveUnverified: %v", err)
	}
	if state := graph.node(first).stateNow(); state != TaskFailed {
		t.Fatalf("a refuted node is %q, want failed", state)
	}
	waitDoneNode(t, graph.node(second))
	if state := graph.node(second).stateNow(); state != TaskFailed {
		t.Fatalf("the dependent of a hand-refuted node is %q, want failed", state)
	}
	// The refusal keeps what the node said: the auditor never answered, so the
	// node's own account is still the only account there is.
	if report := graph.node(first).notice().Report; !strings.Contains(report, "nobody could say") {
		t.Fatalf("report = %q, want the earlier account kept under the refusal", report)
	}
}

// THE BELT IS THE SAFETY ARGUMENT. An auditor has no hand that writes, and its
// bash runs the repository's verification and refuses everything else —
// including a destructive command, a command that runs the node's own code, and
// a verification with a second command chained onto it. A trailing redirect
// on an allowed check is not that: it is the first stage, and it is run
// (task_audit_waste_test.go).
func TestAuditBeltIsReadOnly(t *testing.T) {
	// The door is built the way a real audit builds it: the checks the work
	// itself named, and the always-safe reading commands under them
	// (task_checks.go). Nothing about the belt knows what `go test` is.
	door := auditDoor{
		checks:  []string{"go test", "go build", "go vet"},
		allowed: append([]string{"go test", "go build", "go vet"}, auditReadCommands...),
	}
	belt := auditBelt(t.TempDir(), door)

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
		if refusal, ok := auditRefusal(allowed, door.allowed); !ok {
			t.Fatalf("the auditor may not run %q: %s", allowed, refusal)
		}
	}
	// A prefix is matched at a word boundary, not as a string prefix.
	if _, ok := auditRefusal("go testify", door.allowed); ok {
		t.Fatal("the allowlist matched a command that merely starts like one")
	}
}

// A VERDICT NOBODY GAVE IS NOT A REFUTATION. Nothing merges without the word
// VERIFIED — the frontier still fails closed — but an answer with neither word
// in it is read as the NON-VERDICT it is, and it carries what the auditor
// actually said so that whoever has to decide can see it.
func TestAuditVerdictFailsClosed(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		answer   string
		verified bool
		answered bool
		report   string
	}{
		{"the word and its evidence", "VERIFIED — go test ./... ok · 3 files", true, true, "VERIFIED — go test ./... ok · 3 files"},
		{"the word on its own line", "VERIFIED\ngo build ./... ok", true, true, "VERIFIED — go build ./... ok"},
		{"a refutation with evidence", "REFUTED — TestX still fails: want 3, got 0", false, true, "REFUTED — TestX still fails: want 3, got 0"},
		{"an essay", "I looked at the diff and it seems VERIFIED to me.", false, false,
			"UNVERIFIED — the checker answered neither way\nI looked at the diff and it seems VERIFIED to me."},
		{"nothing at all", "", false, false, "UNVERIFIED — the checker answered neither way"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := parseAuditVerdict(testCase.answer)
			if got.verified != testCase.verified {
				t.Fatalf("verified = %v for %q", got.verified, testCase.answer)
			}
			if got.answered != testCase.answered {
				t.Fatalf("answered = %v for %q — a non-verdict must not pass as a finding", got.answered, testCase.answer)
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
	// And so is a non-verdict's: an auditor that wrote an essay instead of a
	// word does not get to write the card either.
	essay := parseAuditVerdict("Well,\nthere are\nseveral\nconsiderations\nhere")
	if lines := strings.Count(essay.report(), "\n") + 1; lines > auditSaidLines+1 {
		t.Fatalf("the non-verdict carried %d lines:\n%s", lines, essay.report())
	}
	// The ZERO VALUE is the safe one, and safe here means "nobody said
	// anything" rather than "somebody refuted it".
	if zero := (auditVerdict{}); zero.answered || zero.verified || !strings.HasPrefix(zero.report(), auditUnverified) {
		t.Fatalf("the zero verdict reads as %q", zero.report())
	}
}

// answeringSearch is a back end that ANSWERS DIFFERENTLY per query, which is
// what makes a run of distinct searches a run of distinct findings.
//
// A back end that returned the same nothing to every query would be a back end
// that taught the node nothing seven times, and the counter is right to say so
// — PROGRESS IS INFORMATION, NEVER ACTIVITY — so a test about exploration has to
// supply the information for the exploration to find.
type answeringSearch struct{}

func (*answeringSearch) Name() string { return "answering" }

func (*answeringSearch) Search(_ context.Context, query string, _ int) ([]search.Result, error) {
	return []search.Result{{
		Title:   "About " + query,
		URL:     "https://example.com/" + query,
		Snippet: "Everything anybody knows about " + query + ".",
	}}, nil
}

// EXPLORATION IS PROGRESS: a research node that never writes a file is doing
// its job, and the no-progress threshold must read NEW INFORMATION as the
// progress it is. What the threshold kills is the spin — the same answer
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
						Deliverable: "d", Acceptance: "a", NoProgress: 3, MaxSteps: 30,
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
			config.SearchProvider = &answeringSearch{}
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
		// The same path at six offsets: every page is new information, and a
		// node reading a long document to its end must not die as a spinner.
		//
		// THE FILE IS REAL AND ITS LINES ARE DISTINCT, because the pages have to
		// come back different for the reading to be exploration. A missing file
		// answers the same error six times, and a node repeating a read that
		// keeps failing is spinning — correctly.
		var document strings.Builder
		for line := 1; line <= 600; line++ {
			fmt.Fprintf(&document, "line %d of the long document\n", line)
		}
		notes := filepath.Join(t.TempDir(), "notes.md")
		if err := os.WriteFile(notes, []byte(document.String()), 0o644); err != nil {
			t.Fatalf("could not write the document: %v", err)
		}
		child := make([]step, 0, 7)
		for index := 0; index < 6; index++ {
			offset := index*100 + 1
			child = append(child, func(context.Context, []ai.Message) (*ai.Response, error) {
				arguments, _ := json.Marshal(struct {
					Path   string `json:"path"`
					Offset int    `json:"offset"`
					Limit  int    `json:"limit"`
				}{Path: notes, Offset: offset, Limit: 100})
				return toolResponse("call-page", "read", string(arguments)), nil
			})
		}
		completer := &routedCompleter{
			parent: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					arguments, _ := json.Marshal(taskArguments{
						Title: "Read", Summary: "s", Brief: "read\n" + taskBriefMark,
						Deliverable: "d", Acceptance: "a", NoProgress: 3, MaxSteps: 30,
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
		// THE ASK READS THE WHOLE DOCUMENT, not one page: the spawn floor
		// (spawnfloor.go) keeps a one-page read in the conversation, and a
		// node exists here because the person asked for the sweep.
		collect(t, mustSubmit(t, agent, "read every line of the long document"))
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
// is working; what the counter kills is the same ANSWER again, learning nothing.
func TestTheProgressCounterReadsTheWholeReadOnlyBelt(t *testing.T) {
	seen := newProgressLedger()
	step := func(tool, args, output string) bool {
		return taughtSomething(Event{Kind: EventToolEnd, Tool: tool, Args: args, Output: output}, seen)
	}

	for _, call := range []struct{ tool, args, output string }{
		{"read", `{"path":"a.go"}`, "package a"},
		{"read_document", `{"path":"scan.pdf"}`, "invoice 4471"},
		{"grep", `{"pattern":"belt"}`, "tools.go:12: belt"},
		{"ls", `{"path":"internal"}`, "session/\ntui3/"},
		{"find", `{"pattern":"*.go"}`, "main.go"},
		{"web_search", `{"query":"argus"}`, "argus — the hundred-eyed"},
		{"web_fetch", `{"url":"https://example.com"}`, "Example Domain"},
		{"jobs", `{"id":1}`, "job 1 · running · 3.0s"},
		{"recall", `{}`, "the workspace is a monorepo"},
		{"view_image", `{"path":"marketing/linkedin.png"}`, "a blue banner"},
		{"manual", `{"question":"what does /cost show"}`, "/cost prints the session's spend"},
		{"tasks", `{}`, "no tasks"},
		{"settings", `{}`, "model: opus"},
		{"list_harnesses", `{}`, "none saved"},
		{"services", `{}`, "gmail: connected"},
		{"gmail_search", `{"query":"invoice"}`, "3 threads"},
		{"gmail_read", `{"id":"abc"}`, "Dear customer"},
		{"calendar_list", `{}`, "standup 09:30"},
		{"slack_search", `{"query":"launch"}`, "#launch · alice · 2026-08-01 09:00\nwe ship Friday"},
		{"slack_read_thread", `{"channel":"C123","ts":"1710000000.100"}`, "alice · 2026-03-09 16:26\nlaunch is go"},
		{"slack_list_channels", `{"filter":"launch"}`, "#launch · C123 · 4 members"},
		{"bash", `{"command":"go test ./..."}`, "ok  github.com/x  0.4s"},
		{"bash", `{"command":"git log -1"}`, "commit 36e058e9"},
	} {
		if !step(call.tool, call.args, call.output) {
			t.Fatalf("%s %s counted as a stall", call.tool, call.args)
		}
	}

	// The spin is the SAME ANSWER again, and it is the spin for bash on exactly
	// the terms it is for everything else.
	if step("bash", `{"command":"go test ./..."}`, "ok  github.com/x  0.4s") {
		t.Fatal("the same command twice counted as progress")
	}
	if step("read", `{"path":"a.go"}`, "package a") {
		t.Fatal("the same file twice counted as progress")
	}
	// LOOKING AT THE SAME PICTURE AGAIN IS THE SPIN, exactly as re-reading the
	// same file is. Novelty is the whole test, for every hand on this half.
	if step("view_image", `{"path":"marketing/linkedin.png"}`, "a blue banner") {
		t.Fatal("the same picture twice counted as progress")
	}
	// AND A NEW COMMAND THAT ANSWERS SOMETHING ALREADY KNOWN IS NOT A DISCOVERY.
	// This is the whole of the rule and the defect it was written for: a model
	// waiting on a job wrote `sleep 30 && tail`, then `sleep 45 && tail`, then
	// `sleep 60 && tail` — every command string different, every answer the same
	// — and the counter used to read each one as a step forward.
	if step("bash", `{"command":"sleep 45 && tail jobs/1.log"}`, "ok  github.com/x  0.4s") {
		t.Fatal("a new command with an answer already seen counted as progress")
	}
	// THE SAME COMMAND WITH A DIFFERENT ANSWER IS A DISCOVERY, which is the same
	// rule read from the other side: `go test` after an edit is the node finding
	// out whether the edit worked.
	if !step("bash", `{"command":"go test ./..."}`, "FAIL github.com/x [build failed]") {
		t.Fatal("the same command with a new answer counted as a stall")
	}
	// A hand that saves a file is counted as the file it saved, one branch up —
	// it must not also be spendable here as a fresh target.
	for _, saving := range []struct{ tool, args string }{
		{"edit", `{"path":"a.go"}`},
		{"write", `{"path":"b.go"}`},
		{"generate_image", `{"prompt":"a blueprint","path":"marketing/x.png"}`},
		{"generate_video", `{"prompt":"a reel","path":"marketing/x.mp4"}`},
		{"speak", `{"text":"hello","path":"voice.wav"}`},
	} {
		if step(saving.tool, saving.args, "wrote 12 lines") {
			t.Fatalf("%s counted as knowledge", saving.tool)
		}
	}
	// And the node's own bookkeeping is neither half.
	for _, own := range []string{"note", "forget", "track", "commit", "change_setting"} {
		if step(own, `{}`, "done") {
			t.Fatalf("%s counted as knowledge", own)
		}
	}
}

// A HAND THAT SAVES A FILE IS PROGRESS, AND THE FILE IT SAVED IS THE PERSON'S.
// generate_image, generate_video and speak all put a real file on disk at a path
// the call names, and a counter that only knew edit and write read a minute of
// real picture-making as six steps of nothing — which is the run this test is
// written from.
func TestTheProgressCounterCountsEveryHandThatSavesAFile(t *testing.T) {
	dir := t.TempDir()
	for _, call := range []struct{ tool, args, want string }{
		{"write", `{"path":"notes.md"}`, "notes.md"},
		{"edit", `{"path":"main.go"}`, "main.go"},
		{"generate_image", `{"prompt":"blueprint","path":"marketing/linkedin.png"}`, "marketing/linkedin.png"},
		{"generate_video", `{"prompt":"reel","path":"marketing/teaser.mp4"}`, "marketing/teaser.mp4"},
		{"speak", `{"text":"hello","path":"voice/intro.wav"}`, "voice/intro.wav"},
	} {
		path, saved := changedPath(Event{Kind: EventToolEnd, Tool: call.tool, Args: call.args}, dir)
		if !saved {
			t.Fatalf("%s did not read as a saved file", call.tool)
		}
		if path != call.want {
			t.Fatalf("%s saved %q, want %q", call.tool, path, call.want)
		}
	}
	// Looking at a picture is not saving one.
	if _, saved := changedPath(Event{Kind: EventToolEnd, Tool: "view_image", Args: `{"path":"marketing/linkedin.png"}`}, dir); saved {
		t.Fatal("view_image read as a saved file")
	}
}

// THE WORKTREE IS THE BACKSTOP UNDER BOTH LISTS: a hand nobody classified is
// still progress on any step that left something behind. And the fingerprint has
// to see a SECOND new file in a folder that was already new — the exact case a
// node making two pictures in one marketing/ folder hits.
func TestTheWorktreeFingerprintSeesEveryNewFile(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "--allow-empty", "-m", "root"},
	} {
		if out, err := git(dir, args...); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	var dirt string
	// The first look is the baseline and moves the fingerprint off "".
	worktreeMoved(dir, &dirt)
	if worktreeMoved(dir, &dirt) {
		t.Fatal("a step that touched nothing moved the fingerprint")
	}

	if err := os.MkdirAll(filepath.Join(dir, "marketing"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"linkedin.png", "twitter.png"} {
		if err := os.WriteFile(filepath.Join(dir, "marketing", name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
		if !worktreeMoved(dir, &dirt) {
			t.Fatalf("%s did not move the fingerprint", name)
		}
	}

	// The harness's own droppings are not the node's work: a background job
	// writing its log every second must not make every step look like progress.
	if err := os.MkdirAll(filepath.Join(dir, aforgeDroppings, "jobs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, aforgeDroppings, "jobs", "1.log"), []byte("building"), 0o644); err != nil {
		t.Fatal(err)
	}
	if worktreeMoved(dir, &dirt) {
		t.Fatal("a job log counted as the node's own work")
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
				Deliverable: "d", Acceptance: "a", NoProgress: 3, MaxSteps: 30,
			},
			want: "stopped: 3 steps without progress",
		},
		{
			name: "the step budget",
			arguments: taskArguments{
				Title: "Grind", Summary: "s", Brief: "spin\n" + taskBriefMark,
				Deliverable: "d", Acceptance: "a", NoProgress: 50, MaxSteps: 2,
			},
			want: "stopped at 2-step checkpoint: still circling",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			arguments, _ := json.Marshal(testCase.arguments)

			// The scripted child never edits and never stops: every step is one
			// more look at a directory. Fifty entries is far past either
			// threshold, so the test proves the threshold ends it rather than
			// the script running out.
			steps := testCase.arguments.MaxSteps
			if testCase.arguments.NoProgress+1 < steps {
				steps = testCase.arguments.NoProgress + 1
			}
			spin := make([]step, 0, steps+1)
			for index := 0; index < steps; index++ {
				spin = append(spin, lsCall(fmt.Sprintf("call-%d", index)))
			}
			spin = append(spin, finalText("landed from what I had"))
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
				config.TaskAudit = false
				config.TaskProgressCheck = func(string, []string) (bool, string) { return false, "still circling" }
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
		Title: "t", Summary: "s", Brief: "b", Deliverable: "d", Acceptance: "a", MaxSteps: -1,
	})
	if _, problem := parseTaskArguments(arguments); !strings.Contains(problem, "max_steps cannot be negative") {
		t.Fatalf("a negative max_steps was accepted: %q", problem)
	}
}

func TestTaskSpawnSwapsANoToolsModelBeforeItsFirstCall(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Model = "no-tools"
		config.RolesSource = tierSettings(map[string]string{roles.TierKey(roles.TierWorker): "worker-with-tools"})
		config.SupportsParameter = func(model, parameter string) (bool, bool) {
			if parameter != "tools" {
				return false, false
			}
			return model != "no-tools", true
		}
	})
	graph := agent.graph()
	graph.run = func(*TaskNode) {}
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "t", brief: "b", acceptance: "a", model: "no-tools"})
	node := graph.node(id)
	child, err := agent.newTaskAgent(context.Background(), t.TempDir(), node, "")
	if err != nil {
		t.Fatalf("spawn refused the usable fallback: %v", err)
	}
	defer child.Close()
	if child.model != "worker-with-tools" {
		t.Fatalf("task model = %q, want worker-tier fallback", child.model)
	}
	if !strings.Contains(node.notice().Mending, "no-tools") || !strings.Contains(node.notice().Mending, "worker-with-tools") {
		t.Fatalf("the model swap was not named: %+v", node.notice())
	}
}

func TestStepCheckpointExtendsFourTimesThenLands(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	arguments, _ := json.Marshal(taskArguments{Title: "Long", Summary: "s", Brief: "keep going\n" + taskBriefMark, Deliverable: "d", Acceptance: "a", MaxSteps: 1, NoProgress: 20})
	spin := []step{
		lsCall("one"), lsCall("two"), lsCall("three"), lsCall("four"), lsCall("five"),
		finalText("the active turn drained"),
		finalText("landed from the evidence already gathered"),
	}
	completer := &routedCompleter{parent: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-task", "propose_task", string(arguments)), nil
		},
		finalText("handed off"),
	}, child: spin}
	checks := 0
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = false
		config.TaskProgressCheck = func(string, []string) (bool, string) { checks++; return true, "still advancing" }
	})
	collect(t, mustSubmit(t, agent, "do the long task"))
	node := agent.graph().node(1)
	waitDoneNode(t, node)
	if checks != 5 {
		t.Fatalf("progress checks = %d, want four renewals and the final capped check", checks)
	}
	if report := node.notice().Report; !strings.Contains(report, "used all 4 extensions") {
		t.Fatalf("report = %q, want the named extension cap", report)
	}
	completer.mu.Lock()
	asked := fmt.Sprint(completer.childRequests)
	for _, request := range completer.childRequests {
		for _, message := range request {
			asked += "\n" + messageText(message)
		}
	}
	completer.mu.Unlock()
	if !strings.Contains(asked, "LAND NOW") {
		t.Fatalf("child was never given the landing turn: %q", asked)
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
	question := auditQuestion(node, taskTree{root: "/repo"}, auditGround{}, auditDoor{}, landingFiles{}, "it claims it is done", nil)
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

// A DOOMED DEPENDENCY IS REFUSED AT THE DOOR, NOT FAILED ON THE FRONTIER. A
// model that saw "run 1" in the conversation and wrote depends_on: [1] used to
// get a task the person watched appear and die in the same breath — admitted,
// then failed as a wait that could never resolve. The proposal is refused
// before anybody is asked anything, in words that name the fix.
func TestAProposalNamingNoTaskIsRefusedBeforeAnyoneIsAsked(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	arguments, _ := json.Marshal(taskArguments{
		Title: "t", Summary: "s", Brief: "b", Deliverable: "d", Acceptance: "a",
		DependsOn: []uint64{99},
	})
	// A refusal returns without asking the person, so a direct call must come
	// straight back; an admission here would hang on the proposal card.
	result, isError, err := agent.proposeTask(context.Background(), arguments)
	if err != nil {
		t.Fatalf("proposeTask errored the turn: %v", err)
	}
	if !isError {
		t.Fatalf("a phantom dependency was not refused: %q", result)
	}
	for _, want := range []string{"task 99", "propose_task itself returned", "adaptive-run"} {
		if !strings.Contains(result, want) {
			t.Fatalf("the refusal does not say %q: %q", want, result)
		}
	}
	graph := agent.graph()
	graph.mu.Lock()
	admitted := len(graph.nodes)
	graph.mu.Unlock()
	if admitted != 0 {
		t.Fatalf("the graph admitted %d nodes for a refused proposal", admitted)
	}
}

// A dependency on work that already failed is the same refusal with a
// different reason: the wait can only end in the cascade, so the proposal is
// turned back with the way out named.
func TestAProposalWaitingOnFailedWorkIsRefused(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := stubbedGraph(agent, func(*TaskNode) {})
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "t", brief: "b", deliverable: "d", acceptance: "a", owner: agent})
	graph.complete(graph.node(id), TaskFailed)

	arguments, _ := json.Marshal(taskArguments{
		Title: "t2", Summary: "s", Brief: "b", Deliverable: "d", Acceptance: "a",
		DependsOn: []uint64{id},
	})
	result, isError, err := agent.proposeTask(context.Background(), arguments)
	if err != nil {
		t.Fatalf("proposeTask errored the turn: %v", err)
	}
	if !isError || !strings.Contains(result, "already failed") {
		t.Fatalf("a dependency on failed work was not refused as one: error=%v %q", isError, result)
	}
}

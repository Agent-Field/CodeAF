package session

import (
	"context"
	"errors"
	"go/ast"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// shaperSettings puts a model on the careful tier, which is where the shaper's
// role sits, and asks it to think — so one assertion covers both the ladder and
// the level riding through.
func shaperSettings() func(string) (string, bool) {
	return func(key string) (string, bool) {
		if key == roles.TierKey(roles.TierHigh) {
			return "careful/model:high", true
		}
		return "", false
	}
}

// isShapeCall reports whether this request is the shaper's, by the one thing
// only it sends: the embedded meta-prompt as its system message.
func isShapeCall(messages []ai.Message) bool {
	return len(messages) > 0 && messages[0].Role == "system" &&
		messageContentText(messages[0]) == shapePrompt
}

// shapeAgent is a session whose task runner does nothing. Admission STARTS a
// node (TaskGraph.runFrontier), and what these tests are about is the brief the
// node was admitted with — not a worktree, a branch and a merge, which would
// also still be writing into the workspace while the test's temp directory was
// being taken away underneath them.
func shapeAgent(t *testing.T, client Completer) (*Agent, <-chan uint64) {
	t.Helper()
	agent, _ := newTestAgent(t, client, func(c *Config) { c.RolesSource = shaperSettings() })
	ran := make(chan uint64, 4)
	stubbedGraph(agent, func(node *TaskNode) {
		node.graph.complete(node, TaskDone)
		ran <- node.id
	})
	return agent, ran
}

// settled waits for the stubbed runner to have finished with the node, so the
// test ends with nothing of its own still running.
func settled(t *testing.T, ran <-chan uint64) {
	t.Helper()
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the admitted node never ran")
	}
}

const (
	shapedAsk        = "write a blog post about our launch"
	shapedAcceptance = "The post is at blog/launch.md and names the three features by their shipped spelling."
	// shapedMarker is a sentence only the shaper's brief contains, so a request
	// that carries it carried the written brief and not the person's stand-in.
	shapedMarker = "No opening that restates the question"
)

// shapedAnswer is what a shaper that did its job returns: the person's sentence
// quoted whole, and the brief and the done-condition written around it.
var shapedAnswer = `{"brief":"The ask, in their words: \"` + shapedAsk +
	`\". Write it for people who already use the product. ` + shapedMarker + `, no three-item lists, ` +
	`no sentence that would be true of any launch.","acceptance":"` + shapedAcceptance + `"}`

// THE DOOR ASKS NO MODEL ANYTHING (issue #936). `/task` used to hold the command
// behind the sizing judge and then the shaper, twenty-eight seconds on the
// measured run, before the node existed; both are read beside the node's first
// worker now. Read off the source, the way the worker's constructor is held to
// the same rule (task_beside_test.go): nothing the door calls may wait on a
// model, whatever it would make better.
func TestThePersonsTaskDoorAsksNoModel(t *testing.T) {
	asks := map[string]bool{
		"shapeBrief": true, "judgeDecomposable": true, "callRole": true, "callRoleChecked": true,
		"completeWithModel": true, "taskName": true, "taskNameWithin": true, "memoryBlock": true,
	}
	_, files := packageSources(t)
	// EVERY FUNCTION THIS PACKAGE DECLARES, BY THE NAME A CALL SPELLS IT WITH, so
	// that the walk below can follow the door into what it calls. A method and a
	// function of the same name are both kept: a call site says only the name, so
	// the law reads every body that name could reach, which errs toward failing.
	bodies := map[string][]*ast.FuncDecl{}
	for _, file := range files {
		for _, declaration := range file.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok && function.Body != nil {
				bodies[function.Name.Name] = append(bodies[function.Name.Name], function)
			}
		}
	}
	door := bodies["StartTask"]
	if len(door) == 0 {
		t.Fatal("StartTask was not found, so this law is reading the wrong source")
	}
	// ONE LEVEL DOWN AS WELL AS THE BODY ITSELF. A model call moved out of the
	// door and into something the door calls — the ground ladder, the admission
	// compiler — is the same twenty-eight seconds with one more frame on the
	// stack, so the walk follows the door's own callees into this package.
	walk := func(function *ast.FuncDecl, path string, follow bool) {
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := calledName(call.Fun)
			if asks[name] {
				t.Errorf("%s calls %s: a person's task must be admitted before any model is asked about it", path, name)
			}
			if follow {
				for _, callee := range bodies[name] {
					if callee.Name.Name == "StartTask" {
						continue
					}
					ast.Inspect(callee.Body, func(inner ast.Node) bool {
						if call, ok := inner.(*ast.CallExpr); ok && asks[calledName(call.Fun)] {
							t.Errorf("%s calls %s, which calls %s: a person's task must be admitted before any model is asked about it",
								path, name, calledName(call.Fun))
						}
						return true
					})
				}
			}
			return true
		})
	}
	for _, function := range door {
		if function.Recv == nil {
			continue
		}
		walk(function, "StartTask", true)
	}
}

// AND WHAT IT ADMITS IS THE PERSON'S OWN WORDS, marked as waiting for both
// readings. The runner here does nothing, so no worker ever exists — and with no
// worker nothing is read beside one, which is the runtime half of the law above:
// a door that still asked the shaper or the judge would be seen asking here.
func TestAPersonsTaskIsAdmittedOnTheirOwnWords(t *testing.T) {
	// This law covers the legacy task tree. An ambient bash-belt setting takes
	// StartTask through the plan-backed road, whose worker brief also carries
	// the plan identity and lifecycle instructions.
	t.Setenv("CODEAF_TASK_BELT", "node")
	client := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse(shapedAnswer), nil
	}}}
	agent, ran := shapeAgent(t, client)

	long := "please have a look at why the nightly build keeps falling over on port b"
	id, title, _, err := agent.StartTask(t.Context(), long, false)
	if err != nil {
		t.Fatal(err)
	}
	settled(t, ran)
	node := agent.graph().node(id)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	if node.spec.brief != long || node.spec.request != long || node.spec.summary != long {
		t.Fatalf("brief %q, request %q, summary %q: want the person's sentence in all three",
			node.spec.brief, node.spec.request, node.spec.summary)
	}
	if node.spec.acceptance != taskPersonAcceptance {
		t.Fatalf("acceptance = %q, want the stand-in until the shaper writes one", node.spec.acceptance)
	}
	if !node.spec.unshaped || !node.spec.unsized {
		t.Fatalf("unshaped=%v unsized=%v: the node must be waiting for both readings", node.spec.unshaped, node.spec.unsized)
	}
	// THE NAME IS THE MECHANICAL CUT UNTIL THE NAMER ANSWERS, and that cut is
	// the first eight words of their opening line. Only the door's own answer is
	// read here: the namer is asked the moment the node exists, and this script
	// answers it too, so the node's title may already be the namer's.
	if want := "please have a look at why the nightly"; title != want {
		t.Fatalf("title = %q, want %q", title, want)
	}
	if got := shapeCalls(client); got != 0 {
		t.Fatalf("the door asked the shaper %d times before there was a worker to hand the brief to", got)
	}

	// AND SOLO IS THE ONE THING THE JUDGE IS TOLD: the person said the work is
	// one worker's, so nobody reads it for width.
	id, _, _, err = agent.StartTask(t.Context(), shapedAsk, true)
	if err != nil {
		t.Fatal(err)
	}
	settled(t, ran)
	if solo := agent.graph().node(id); solo.spec.unsized || !solo.spec.unshaped {
		t.Fatalf("solo: unsized=%v unshaped=%v, want no width reading and the brief still written", solo.spec.unsized, solo.spec.unshaped)
	}
}

// THE WORKER'S FIRST REQUEST GOES OUT WHILE THE SHAPER AND THE JUDGE ARE STILL
// THINKING, and the brief reaches it when it lands — the replication of issue
// #936, run against the real runner.
//
// THE ORDER IS MADE BY THE TEST, NOT OBSERVED BY IT. The judge here never answers
// at all and the shaper answers only once the worker has been asked something, so
// a door that still stood either of them in front of the work would never get a
// first request, and the bound on the wait is what fails it. Nothing sleeps: each
// side waits on the other's own signal.
func TestTheWorkersFirstRequestGoesOutBeforeTheShaperOrTheJudgeAnswers(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())
	completer := newDoorCompleter()
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.RolesSource = shaperSettings()
		config.Divide = true
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskAudit = false
		config.TaskRepairRounds = 0
	})
	graph := agent.graph()

	id, _, _, err := agent.StartTask(t.Context(), shapedAsk, false)
	if err != nil {
		t.Fatal(err)
	}
	first := completer.worker(t, 0)
	if strings.Contains(first, shapedMarker) || strings.Contains(first, briefWrittenBeside) {
		t.Fatalf("the worker's FIRST request already carried the written brief, so it waited for the shaper: %q", first)
	}
	if !strings.Contains(first, shapedAsk) {
		t.Fatalf("the worker's first request lost the person's own words: %q", first)
	}
	// The judge never answers at all, so a first request that arrived is already
	// the proof it was not waited for; the shaper is asked what it had done.
	if completer.shapedBeforeTheWorker() {
		t.Fatal("the shaper had answered before the worker's first request: it stood in front of it")
	}

	// AND THE BRIEF REACHES THE WORKER'S QUEUE WHILE IT IS STILL AT WORK, and the
	// worker reads it at its next step, in the document it opened on.
	completer.releaseShaper()
	worker := graph.node(id).openRoom().speaker()
	if worker == nil {
		t.Fatal("nobody was in the room while the worker was at work")
	}
	waitFor(t, "the written brief on the worker's queue", func() bool {
		return strings.Contains(queuedFor(worker), briefWrittenBeside)
	})
	if node := graph.node(id); !node.briefWaiting() {
		t.Fatal("the contract was written before the worker had read it")
	}
	close(completer.hold)
	second := completer.worker(t, 1)
	if !strings.Contains(second, briefWrittenBeside) || !strings.Contains(second, shapedMarker) {
		t.Fatalf("the worker never read the written brief: %q", second)
	}
	if !strings.Contains(second, briefAskHeading) || !strings.Contains(second, shapedAsk) {
		t.Fatalf("the written brief lost the person's own words above it: %q", second)
	}
	node := graph.node(id)
	select {
	case <-node.done:
	case <-time.After(30 * time.Second):
		t.Fatal("the task never landed")
	}
	// AND IT IS WHAT THE WORK IS JUDGED BY FROM HERE, written once, inside the
	// assembled brief rather than beside it.
	graph.mu.Lock()
	brief, acceptance, unshaped, assembled := node.spec.brief, node.spec.acceptance, node.spec.unshaped, node.brief
	graph.mu.Unlock()
	if !strings.Contains(brief, shapedMarker) || acceptance != shapedAcceptance || unshaped {
		t.Fatalf("spec after the brief landed: brief %q, acceptance %q, unshaped %v", brief, acceptance, unshaped)
	}
	if !strings.HasPrefix(assembled, brief) {
		t.Fatalf("the assembled brief does not open on the written one: %q", assembled)
	}
	if request := node.spec.request; request != shapedAsk {
		t.Fatalf("the request is %q, want the person's sentence untouched", request)
	}
}

// A `/task` THAT WAS QUEUED WHEN THE ENGINE STOPPED STILL GETS ITS BRIEF
// WRITTEN BESIDE THE WORKER IT FINALLY GETS.
//
// The two readings happen beside the node's FIRST WORKER, and a task admitted
// while the lanes are full has no worker at all — so what is owed has to survive
// the process. Without [taskRecord.Unshaped] the resumed node came back owing
// nothing: [Agent.shapeBeside] returned nil, no shaper was ever asked, and the
// work ran on the person's raw sentence and the canned done-condition for ever,
// with nothing on screen saying so.
func TestAQueuedTaskThatSurvivedARestartStillHasItsBriefWritten(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())
	journal, checkpoint := journalIn(t)

	// THE FIRST PROCESS ADMITS AND NEVER RUNS IT, which is a task typed while
	// every lane is busy: the node is on disk, queued, with both readings owed.
	first, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile, config.Workspace = journal, repo
	})
	stubbedGraph(first, func(*TaskNode) {})
	id, _, _, err := first.StartTask(t.Context(), shapedAsk, false)
	if err != nil {
		t.Fatal(err)
	}
	document := readCheckpoint(t, checkpoint)
	record := recordOf(t, document, id)
	if !record.Unshaped || !record.Unsized {
		t.Fatalf("the record owes unshaped=%v unsized=%v, want both readings owed",
			record.Unshaped, record.Unsized)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close the first session: %v", err)
	}
	// AND WHAT THE KILLED PROCESS LEFT IS THAT RECORD, QUEUED. Admission is
	// checkpointed BEFORE the frontier turns ([TaskGraph.admit]), so a process
	// that died in between wrote exactly this node in exactly this state; the
	// runner here has since marked it started, and the state is put back rather
	// than raced for.
	for i := range document.Nodes {
		if document.Nodes[i].ID == id {
			document.Nodes[i].State = TaskQueued
		}
	}
	writeCheckpoint(t, checkpoint, document)

	// AND THE SECOND PROCESS OPENS THE SAME JOURNAL. Recovery puts the node back
	// on the frontier, and the worker it gets is the first this task ever had.
	completer := newDoorCompleter()
	second, _ := newTestAgent(t, completer, func(config *Config) {
		config.SessionFile, config.Workspace = journal, repo
		config.RolesSource = shaperSettings()
		config.Divide = true
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskAudit = false
		config.TaskRepairRounds = 0
	})
	graph := second.graph()
	node := graph.node(id)
	if node == nil {
		t.Fatal("the queued task did not survive the restart")
	}
	// THE FLAGS ARE READ UNDER THE GRAPH'S OWN LOCK, because the reading beside
	// the worker spends `unsized` under it ([TaskNode.widthToRead]) and the
	// resumed node is already running by the time this line is reached.
	graph.mu.Lock()
	unshaped, unsized := node.spec.unshaped, node.spec.unsized
	graph.mu.Unlock()
	if !unshaped || !unsized {
		t.Fatalf("the resumed node owes unshaped=%v unsized=%v, want both readings still owed", unshaped, unsized)
	}
	// THE SHAPER IS HELD UNTIL THE WORKER HAS ASKED SOMETHING, because that is
	// the only order in which the queue can be read at all. A brief that lands
	// BEFORE the worker's first request is taken by that request's own drain —
	// which is correct, and the best thing that can happen to it — but then it
	// never sits on the queue for a poll to see, and a test that waited for it
	// there would be waiting for a frame the engine had already gone past. That
	// is what made this fail seven runs in eight on a loaded laptop. The worker's
	// own first answer is held too, so the order is MADE here rather than hoped
	// for: the request goes out, then the brief lands, then it is read.
	if opening := completer.worker(t, 0); !strings.Contains(opening, shapedAsk) {
		t.Fatalf("the resumed worker opened on %q, want the person's own words", opening)
	} else if strings.Contains(opening, briefWrittenBeside) {
		t.Fatalf("the resumed worker's FIRST request already carried the written brief: %q", opening)
	}
	completer.releaseShaper()
	worker := node.openRoom().speaker()
	if worker == nil {
		t.Fatal("nobody was in the room while the resumed worker was at work")
	}
	waitFor(t, "the written brief on the resumed worker's queue", func() bool {
		return strings.Contains(queuedFor(worker), briefWrittenBeside)
	})
	close(completer.hold)
	if read := completer.worker(t, 1); !strings.Contains(read, shapedMarker) {
		t.Fatalf("the resumed worker never read the written brief: %q", read)
	}
	select {
	case <-node.done:
	case <-time.After(30 * time.Second):
		t.Fatal("the resumed task never landed")
	}
	graph.mu.Lock()
	brief, acceptance, unshaped := node.spec.brief, node.spec.acceptance, node.spec.unshaped
	graph.mu.Unlock()
	if !strings.Contains(brief, shapedMarker) || acceptance != shapedAcceptance || unshaped {
		t.Fatalf("after the resume: brief %q, acceptance %q, unshaped %v", brief, acceptance, unshaped)
	}
}

// doorCompleter is a worker, a shaper, a judge and a namer sharing one provider,
// told apart by the one thing each sends that the others do not: its system
// prompt. The shaper waits for the test's word and the judge never answers.
type doorCompleter struct {
	shaper  chan struct{}
	arrived chan struct{}
	// hold is what the worker's first answer waits for: the test closes it once
	// the written brief is on the worker's queue.
	hold chan struct{}

	mu               sync.Mutex
	asked            []string
	shaped           bool
	shapedAtFirst    bool
	releasedTheBrief sync.Once
}

func newDoorCompleter() *doorCompleter {
	return &doorCompleter{shaper: make(chan struct{}), arrived: make(chan struct{}, 16), hold: make(chan struct{})}
}

func (c *doorCompleter) releaseShaper() { c.releasedTheBrief.Do(func() { close(c.shaper) }) }

func (c *doorCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	system := ""
	if len(messages) > 0 {
		system = messageText(messages[0])
	}
	switch system {
	case titleSystem, taskNameSystem:
		return textResponse("launch post"), nil
	case shapePrompt:
		select {
		case <-c.shaper:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		c.mu.Lock()
		c.shaped = true
		c.mu.Unlock()
		return textResponse(shapedAnswer), nil
	case taskJudgePrompt:
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if len(messages) > 0 && strings.Contains(messageText(messages[len(messages)-1]), "[still asked]") {
		return textResponse(checkpointNothingLeft), nil
	}
	var asked strings.Builder
	for _, message := range messages {
		if message.Role == "user" {
			asked.WriteString(messageText(message))
			asked.WriteString("\n")
		}
	}
	c.mu.Lock()
	c.asked = append(c.asked, asked.String())
	firstAsk := len(c.asked) == 1
	if firstAsk {
		c.shapedAtFirst = c.shaped
	}
	c.mu.Unlock()
	c.arrived <- struct{}{}
	if firstAsk {
		// THE FIRST ANSWER WAITS UNTIL THE TEST HAS SEEN THE BRIEF ON THE QUEUE, and
		// it is a step rather than a last word, so the worker's turn goes on to a
		// next request — the one the brief is read in.
		select {
		case <-c.hold:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return toolResponse("look-1", "read", `{"path":"go.mod"}`), nil
	}
	return textResponse("Done."), nil
}

// worker waits for the worker's nth request and answers what it carried.
func (c *doorCompleter) worker(t *testing.T, n int) string {
	t.Helper()
	for {
		c.mu.Lock()
		if len(c.asked) > n {
			asked := c.asked[n]
			c.mu.Unlock()
			return asked
		}
		c.mu.Unlock()
		select {
		case <-c.arrived:
		case <-time.After(30 * time.Second):
			t.Fatalf("the worker was never asked a request %d", n+1)
		}
	}
}

// shapedBeforeTheWorker is whether the shaper had answered by the time the
// worker's first request went out.
func (c *doorCompleter) shapedBeforeTheWorker() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.shapedAtFirst
}

// A BRIEF THAT LANDS AFTER THE WORKER HAS SAID ITS LAST WORD IS DROPPED, AND
// NOTHING IS WRITTEN. The work was done on the person's sentence, and a
// done-condition the worker never read must never become what it is judged by.
func TestABriefIsWrittenOnlyWhenTheWorkerReadsIt(t *testing.T) {
	spec := taskSpec{title: "launch post", request: shapedAsk, brief: shapedAsk,
		acceptance: taskPersonAcceptance, unshaped: true}
	nest := newDivideNestFrom(t, spec, 0, &scriptedCompleter{}, nil)
	shaped, ok := parseShapedBrief(shapedAnswer)
	if !ok {
		t.Fatal("the scripted answer does not parse")
	}
	room := nest.parent.openRoom()
	// The runner withdraws the worker at its last read (task_child_run.go).
	room.speaking(nil)
	if nest.parent.handShaped(room, taskTree{}, shaped) {
		t.Fatal("a worker that had finished was handed the brief")
	}
	if queued := queuedFor(nest.node); queued != "" {
		t.Fatalf("a finished worker had %q queued for a request it will never make", queued)
	}

	// A WORKER STILL READING TAKES IT, AND NOTHING IS WRITTEN YET: taken is not
	// read (mailbox.go), and the contract moves only when the worker's own record
	// holds the note.
	room.speaking(nest.node)
	if !nest.parent.handShaped(room, taskTree{}, shaped) {
		t.Fatal("a worker still reading was not handed the brief")
	}
	queued := queuedMessages(nest.node)
	if len(queued) != 1 || !strings.Contains(queued[0].text(), briefWrittenBeside) ||
		!strings.Contains(queued[0].text(), shapedMarker) || !strings.Contains(queued[0].text(), shapedAsk) {
		t.Fatalf("the worker's queue holds %+v", queued)
	}
	if !nest.parent.briefWaiting() || nest.parent.spec.acceptance != taskPersonAcceptance {
		t.Fatal("the contract was written when the queue took the note, before anybody read it")
	}
	// THE READ IS THE WORKER'S RECORD TAKING THE NOTE ([durableDelivery]), and it
	// is what writes the contract — once.
	for _, owed := range queued[0].delivered {
		owed.settled()
	}
	if nest.parent.briefWaiting() || nest.parent.spec.brief != shaped.Brief || nest.parent.spec.acceptance != shapedAcceptance {
		t.Fatalf("the read did not write the contract: %q / %q", nest.parent.spec.brief, nest.parent.spec.acceptance)
	}
	if nest.parent.writeBrief(shapedBrief{Brief: "a second brief", Acceptance: "a second done"}) {
		t.Fatal("the brief was written twice")
	}
}

// queuedMessages copies a worker's queue, marks and all, under its own lock.
func queuedMessages(worker *Agent) []userMessage {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	return append([]userMessage(nil), worker.steering...)
}

// IT IS BILLED THE WAY EVERY CALL NOBODY TYPED IS: to the session's total and
// its call count, never to a turn — and it is asked on the careful tier, at the
// level that tier names.
func TestShapingIsBilledAsAnAuxiliaryCall(t *testing.T) {
	client := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse(shapedAnswer), nil
	}}}
	agent, _ := shapeAgent(t, client)

	shaped, ok := agent.shapeBrief(t.Context(), shapedAsk)
	if !ok || !strings.Contains(shaped.Brief, shapedMarker) || shaped.Acceptance != shapedAcceptance {
		t.Fatalf("shaped = %+v ok=%v", shaped, ok)
	}
	usage := agent.Usage()
	if usage.Calls != 1 || usage.Input == 0 || usage.Output == 0 {
		t.Fatalf("usage = %+v, want one billed call with tokens on it", usage)
	}
	if usage.Turns != 0 {
		t.Fatalf("turns = %d, want the shaping call charged to no turn", usage.Turns)
	}
	if got := client.model(0); got != "careful/model" {
		t.Fatalf("the shaper ran on %q, want the careful tier", got)
	}
	if got := client.efforts[0]; got != provider.EffortHigh {
		t.Fatalf("effort = %q, want the level the tier named", got)
	}
}

// EVERY FAILURE WRITES NOTHING. The worker is already doing what the person
// typed, which is what a shaper that cannot answer always meant, so each of these
// answers false and the node stays exactly as it was admitted.
func TestAShaperThatCannotAnswerWritesNothing(t *testing.T) {
	garbage := func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("Sure! Here is a brief for you:"), nil
	}
	empty := func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(""), nil }
	offline := func(context.Context, []ai.Message) (*ai.Response, error) { return nil, errors.New("offline") }
	// The deadline's path, without waiting out taskShapeWindow: what a stall
	// reaches this code as is a context that ended, and a call that ends is a
	// call that failed.
	stalled := func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	for _, tc := range []struct {
		name     string
		steps    []step
		requests int
		ended    bool
	}{
		{"prose where JSON was asked for", []step{garbage, garbage}, 2, false},
		{"nothing at all", []step{empty}, 1, false},
		{"a provider having a bad minute", []step{offline}, 1, false},
		{"a call that ran out of time", []step{stalled}, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &scriptedCompleter{steps: tc.steps}
			agent, _ := shapeAgent(t, client)
			ctx := t.Context()
			if tc.ended {
				ended, cancel := context.WithCancel(ctx)
				cancel()
				ctx = ended
			}
			if shaped, ok := agent.shapeBrief(ctx, shapedAsk); ok {
				t.Fatalf("a shaper that could not answer wrote %+v", shaped)
			}
			// AN EMPTY ANSWER IS NOT REPAIRED, and neither is a call that never
			// landed: the second request would ask the identical question of the
			// thing that just failed to answer it.
			if got := shapeCalls(client); got != tc.requests {
				t.Fatalf("%d shaping requests, want %d", got, tc.requests)
			}
		})
	}
	// AND NO SHAPER CONFIGURED IS NO CALL AT ALL: the documented absence of the
	// capability, not a failure of it.
	agent, _ := shapeAgentNoShaper(t, &scriptedCompleter{})
	if _, ok := agent.shapeBrief(t.Context(), shapedAsk); ok {
		t.Fatal("a session with no shaper wrote a brief")
	}
}

// A PASTED ISSUE'S BACKTICKED REPRODUCTION NEVER REACHES THE DOOR IT STARTED
// THROUGH.
//
// The measured ask named `chmod 000 tox.ini` in the punctuation nearly every
// bug report uses, and the door once treated it as the work's check. Driving
// the ask through StartTask proves the admitted node keeps the account boundary —
// and the node is admitted on the person's own words now, so the backticked
// command is IN its brief, which is the harder case; reading that node's real
// door and the file's mode proves the command was neither offered nor run. The
// explicit-contract control proves an independently declared check still opens
// the door.
func TestAPastedIssuesBacktickedCommandNeverReachesTheDoorItStartedThrough(t *testing.T) {
	tree := t.TempDir()
	tox := writeCheckFile(t, tree, "tox.ini", "[tox]\n", 0o644)
	writeCheckFile(t, tree, "check.sh", "#!/bin/sh\nexit 0\n", 0o755)
	ask := "Pasted issue body. Repro: `chmod 000 tox.ini`, then watch the suite fail."
	client := &scriptedCompleter{}
	agent, _ := newTestAgent(t, client, func(config *Config) {
		config.Workspace = tree
		config.TaskAudit = true
		config.RolesSource = shaperSettings()
	})
	ran := make(chan uint64, 1)
	stubbedGraph(agent, func(node *TaskNode) {
		node.graph.complete(node, TaskDone)
		ran <- node.id
	})

	id, _, _, err := agent.StartTask(t.Context(), ask, false)
	if err != nil {
		t.Fatal(err)
	}
	settled(t, ran)
	node := agent.graph().node(id)
	if node == nil {
		t.Fatal("the pasted issue did not produce an admitted node")
	}
	door := auditDoorFor(node, standingOn(tree))
	if len(door.checks) != 0 {
		t.Fatalf("the admitted node inferred executable checks from prose: %q", door.checks)
	}
	declared := auditDoorFor(declaringNode("check.sh"), standingOn(tree))
	if len(declared.checks) != 1 || declared.checks[0] != "check.sh" {
		t.Fatalf("the explicit verification contract lost its check: %q", declared.checks)
	}
	if containsWord(door.allowed, "chmod 000 tox.ini") {
		t.Fatalf("the pasted reproduction entered the admitted node's door: %q", door.allowed)
	}
	if strings.Contains(door.offer(), "chmod 000 tox.ini") {
		t.Fatalf("the admitted node offered the pasted reproduction:\n%s", door.offer())
	}
	if _, ok := auditRefusal("chmod 000 tox.ini", door.allowed); ok {
		t.Fatal("the admitted node's gate allowed the pasted reproduction")
	}
	info, err := os.Stat(tox)
	if err != nil {
		t.Fatalf("stat tox.ini after the task started: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o644 {
		t.Fatalf("tox.ini mode after the task started = %04o, want 0644", mode)
	}
}

// THE SHAPER TYPES NOTHING INTO ANYBODY'S ROOM. The observer on the context it is
// handed is taken off the call, so the brief it writes beside a worker cannot be
// streamed into the room where somebody is reading that worker.
func TestTheShaperIsSilentToAnObserverThatIsNotItsOwn(t *testing.T) {
	client := &scriptedCompleter{steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		provider.Emit(ctx, provider.StreamDelta, shapedAnswer)
		return textResponse(shapedAnswer), nil
	}}}
	agent, _ := shapeAgent(t, client)
	var heard []string
	ctx := provider.WithStreamObserver(context.Background(), func(event provider.StreamEvent) {
		heard = append(heard, event.Delta)
	})
	_, _ = agent.shapeBrief(ctx, shapedAsk)
	if len(heard) != 0 {
		t.Fatalf("THE SHAPER TYPED INTO THE ROOM: %q", heard)
	}
}

// A shaped brief that came back with nothing in it is not an answer: admitting a
// blank brief would lose the work outright, which is the one thing this path is
// written not to do.
func TestAnEmptyShapedBriefIsNotAnAnswer(t *testing.T) {
	if _, ok := parseShapedBrief(`{"brief":"   ","acceptance":"checkable"}`); ok {
		t.Fatal("a blank brief was taken as a shaped one")
	}
	// An empty ACCEPTANCE is survivable, because there is a line to stand in for
	// it and none to stand in for the work.
	shaped, ok := parseShapedBrief("```json\n{\"brief\":\"do the thing\"}\n```")
	if !ok || shaped.Brief != "do the thing" || shaped.Acceptance != taskPersonAcceptance {
		t.Fatalf("fenced answer = %+v ok=%v", shaped, ok)
	}
}

// shapeCalls counts the requests that were the shaper's, by the one thing only
// it sends.
func shapeCalls(client *scriptedCompleter) int {
	count := 0
	for i := 0; i < client.requests(); i++ {
		if isShapeCall(client.request(i)) {
			count++
		}
	}
	return count
}

// shapeAgentNoShaper is [shapeAgent] without anything on the careful tier, so
// no model is ever resolved for the shaper and the request passes through
// untouched — the documented absence of the capability.
func shapeAgentNoShaper(t *testing.T, client Completer) (*Agent, <-chan uint64) {
	t.Helper()
	agent, _ := newTestAgent(t, client, nil)
	ran := make(chan uint64, 4)
	stubbedGraph(agent, func(node *TaskNode) {
		node.graph.complete(node, TaskDone)
		ran <- node.id
	})
	return agent, ran
}

package session

// THE BASH BELT'S PER-STEP FRAME AND ITS RESUME CLAUSE, AS TESTS.
//
// Wave 2 of the bash-only task-loop experiment
// (docs/design/bash-task-loop/DESIGN.md): the frame a bash-belt worker reads at
// each step boundary, and the one sentence a resumed node's opening carries.
// What is scripted is only the provider and, for the resume, the flag the
// interrupt door leaves behind; everything the assertions read — the room's
// recorder, the node's spend, the family queue, the transcript — is the real
// instrument the frame renders from.

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/approval"
)

// bashBeltFrameFixture is one conversation, one admitted node, and the agent
// that IS that node — the shape [newNest] builds, with the belt named and the
// worker's completer scripted, so a test can drive [runTaskChild] directly and
// read the transcript the worker actually kept.
func bashBeltFrameFixture(t *testing.T, belt bool, steps []step) (*TaskGraph, *TaskNode, *Agent, *taskRoom, string) {
	t.Helper()
	session, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := session.graph()
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "frame the work", brief: "b", acceptance: "a", depth: 1})
	node := graph.node(id)
	dir := t.TempDir()
	child, err := newAgent(Config{
		Workspace: dir,
		Model:     "test/model",
		System:    "SYSTEM",
		InTask:    true,
		tasker:    graph,
		taskID:    id,
		taskDepth: 1,
		// The allowance the real runner arms on every worker (task_run.go's
		// [Agent.newTaskAgentOn]); a scripted worker's bash calls are real
		// calls and stand under the same floor.
		ApprovalPolicy: &approval.Policy{Default: approval.ActionAllow},
		AskConsent:     false,
		bashBelt:       belt,
	}, &scriptedCompleter{steps: steps})
	if err != nil {
		t.Fatalf("newAgent for the bash-belt worker: %v", err)
	}
	t.Cleanup(func() { _ = child.Close() })
	room := node.openRoom()
	room.speaking(child)
	return graph, node, child, room, dir
}

// frameNotes collects the per-step frames a transcript carries, oldest first.
// The frames are the session's own notes in the user role, recognized by the
// opening they were built with and never by guessing at wording.
func frameNotes(t *testing.T, messages []ai.Message) []string {
	t.Helper()
	var frames []string
	for _, message := range messages {
		if message.Role != "user" {
			continue
		}
		if text := messageContentText(message); strings.HasPrefix(text, bashBeltFrameOpening) {
			frames = append(frames, text)
		}
	}
	return frames
}

// openingBrief is the first user-role message that is not one of the session's
// own notes: the worker's opening document, whole.
func openingBrief(t *testing.T, messages []ai.Message) string {
	t.Helper()
	for _, message := range messages {
		if message.Role != "user" {
			continue
		}
		if text := messageContentText(message); !isVolatileNote(text) {
			return text
		}
	}
	t.Fatal("no opening brief in the transcript")
	return ""
}

// runBashBeltResume opens one node whose interrupt flag is the state an
// interrupt leaves behind, and gives back the fresh worker's whole transcript.
// The flag is set the way [TaskNode.resumeTree] finds it after a process death
// (task_store.go's interrupt marks the record; restoreNode reads it back).
func runBashBeltResume(t *testing.T, belt, interrupted bool) []ai.Message {
	t.Helper()
	_, node, child, room, dir := bashBeltFrameFixture(t, belt, []step{finalText("done")})
	if interrupted {
		node.graph.mu.Lock()
		node.interrupted = true
		node.graph.mu.Unlock()
	}
	if _, _, err := runTaskChild(context.Background(), child, node, "finish the work", dir,
		taskLimits{maxSteps: taskMaxSteps, noProgress: taskNoProgress}, room, io.Discard); err != nil {
		t.Fatalf("runTaskChild: %v", err)
	}
	return child.snapshot()
}

// THE FRAME'S NUMBERS ARE THE RUNNER'S OWN, or the frame is a second account of
// the run and the experiment is measuring a fiction. The steps are asserted
// against the room's recorder — the count the runner's own thresholds are
// counted in (task_live.go) — and the frames must count up one per finished
// call, in the runner's own order, with no money invented for a model that has
// published no price and no family line for a node that never divided.
func TestBashBeltFrameNumbersAreTheRunnerSOwnCounters(t *testing.T) {
	t.Parallel()
	commands := []string{"true", "echo one", "echo two"}
	_, node, child, room, dir := bashBeltFrameFixture(t, true, bashSteps(commands))
	if _, _, err := runTaskChild(context.Background(), child, node, "do the work", dir,
		taskLimits{maxSteps: taskMaxSteps, noProgress: taskNoProgress}, room, io.Discard); err != nil {
		t.Fatalf("runTaskChild: %v", err)
	}
	live := room.recorder().state(0)
	if live.Steps != len(commands) {
		t.Fatalf("the runner counted %d steps, want %d (one per finished call)", live.Steps, len(commands))
	}
	frames := frameNotes(t, child.snapshot())
	if len(frames) != len(commands) {
		t.Fatalf("%d frames landed, want %d: the first drain has no finished call behind it and renders nothing", len(frames), len(commands))
	}
	for index, frame := range frames {
		if !strings.Contains(frame, fmt.Sprintf("step %d/", index+1)) {
			t.Fatalf("frame %d does not count step %d: %q", index+1, index+1, frame)
		}
	}
	last := frames[len(frames)-1]
	// THE CAP IS THE NODE'S OWN, the same record the checkpoint seam reads
	// ([TaskNode.limits]); the steps are the recorder's own.
	if want := fmt.Sprintf("step %d/%d", live.Steps, node.limits().maxSteps); !strings.Contains(last, want) {
		t.Fatalf("the last frame does not read the sources it renders: want %q in %q", want, last)
	}
	// AND THE MONEY RENDERS AS NOTHING WHEN NOBODY PUBLISHED A PRICE. The
	// scripted model is unpriced, so a $0.00 in the frame would be a figure the
	// spend's own comment calls a lie ([TaskNode.spend]).
	if strings.Contains(last, "$") {
		t.Fatalf("an unpriced model drew money: %q", last)
	}
	if strings.Contains(last, "family:") {
		t.Fatalf("a node with no family drew a family line: %q", last)
	}
	if !isVolatileNote(frames[0]) {
		t.Fatalf("the frame is not recognized as one of the session's own notes: %q", frames[0])
	}
}

// THE FRAME RENDERS THE SAME SOURCES IT READS, at the same precision the room
// renders them. The recorder and the ledger are written here the way a run
// writes them, and the frame is compared against [TaskNode.spend] and the
// recorder's own count — the runner's figures, not literals.
func TestBashBeltFrameRendersTheSourcesItReads(t *testing.T) {
	t.Parallel()
	_, node, child, room, _ := bashBeltFrameFixture(t, true, nil)
	room.live.steps = 5
	child.mu.Lock()
	child.usage = Usage{CostUSD: 0.183}
	child.mu.Unlock()

	wantStep := fmt.Sprintf("step %d/%d", 5, node.limits().maxSteps)
	wantSpend := " · $" + strconv.FormatFloat(node.spend(), 'f', 2, 64) + " so far"
	wantLeft := fmt.Sprintf(" · %d steps left", node.limits().maxSteps-5)
	if frame := child.bashBeltFrame(); frame != wantStep+wantSpend+wantLeft {
		t.Fatalf("frame = %q, want %q", frame, wantStep+wantSpend+wantLeft)
	}
	// AND AN UNPRICED MODEL DRAWS NO MONEY AT ALL — the room's own zero law,
	// restated for the frame's line.
	child.mu.Lock()
	child.usage = Usage{}
	child.mu.Unlock()
	if frame := child.bashBeltFrame(); frame != wantStep+wantLeft {
		t.Fatalf("unpriced frame = %q, want %q", frame, wantStep+wantLeft)
	}
	// AND A STEP COUNT OF ZERO IS NOTHING: a node that has finished no call has
	// no step to be on, and the emptiness law renders that as no frame at all
	// rather than as step 0.
	room.live.steps = 0
	if frame := child.bashBeltFrame(); frame != "" {
		t.Fatalf("frame at zero steps = %q, want nothing", frame)
	}
}

// THE FAMILY LINE IS THE QUEUE AND THE GRAPH, AND NOTHING ELSE. A node that
// never divided draws no family line; one report owed draws one; a part still
// working says so beside it; and when the last report has been handed over the
// parts half goes quiet before the count does.
func TestBashBeltFrameFamilyLineFollowsTheQueue(t *testing.T) {
	t.Parallel()
	graph, node, child, room, _ := bashBeltFrameFixture(t, true, nil)
	room.live.steps = 2
	if frame := child.bashBeltFrame(); strings.Contains(frame, "family:") {
		t.Fatalf("a node with no family drew a family line: %q", frame)
	}
	child.postTaskNews()
	if frame := child.bashBeltFrame(); !strings.Contains(frame, "family: 1 report in hand") || strings.Contains(frame, "parts still working") {
		t.Fatalf("frame with one report owed = %q", frame)
	}
	kid := graph.reserve()
	graph.admit(kid, taskSpec{title: "a part", brief: "b", acceptance: "a", parent: node.id, depth: 2})
	if frame := child.bashBeltFrame(); !strings.Contains(frame, "family: 1 report in hand; parts still working") {
		t.Fatalf("frame with a part outstanding = %q", frame)
	}
	kidNode := graph.node(kid)
	kidNode.graph.mu.Lock()
	kidNode.noted = true
	kidNode.graph.mu.Unlock()
	if frame := child.bashBeltFrame(); !strings.Contains(frame, "family: 1 report in hand") || strings.Contains(frame, "parts still working") {
		t.Fatalf("frame after the part's news was handed over = %q", frame)
	}
	child.mu.Lock()
	child.taskNotes = 0
	child.mu.Unlock()
	if frame := child.bashBeltFrame(); strings.Contains(frame, "family:") {
		t.Fatalf("frame with an empty queue and no parts = %q, want no family line", frame)
	}
}

// putChild grafts one child of parent onto the graph in the state the frame
// reads, without admitting work.
//
// A RENDERING TEST WANTS A GRAPH IT HOLDS STILL. [TaskGraph.admit] starts a
// worker goroutine whose own state moves under the test's feet, and this frame
// reads children and their state — so the shape is built here by hand, the way
// the other rendering fixtures build it, rather than raced for by a run.
func putChild(graph *TaskGraph, parent uint64, state TaskState) *TaskNode {
	id := graph.reserve()
	child := &TaskNode{graph: graph, id: id, parent: parent, state: state, done: make(chan struct{})}
	graph.mu.Lock()
	graph.nodes[id] = child
	graph.order = append(graph.order, id)
	graph.mu.Unlock()
	return child
}

// setSlots records how many fan-out slots a node has claimed, the figure the
// frame reads. The map is grown the way [TaskGraph.claimChild] grows it, so a
// test never writes a slot count into a map that was never made.
func setSlots(graph *TaskGraph, parent uint64, n int) {
	graph.mu.Lock()
	defer graph.mu.Unlock()
	if graph.claims == nil {
		graph.claims = make(map[uint64]int)
	}
	graph.claims[parent] = n
}

// THE FREE CHILD SLOTS ARE THE CAP [TaskGraph.claimChild] ENFORCES, RENDERED
// FROM WHAT THAT CAP ALREADY HOLDS. A node that never divided says nothing
// about slots; once it has fanned out the frame reads what is left, counted the
// way the cap counts it — the slots claimed for proposals in flight plus the
// admitted children — and a node with none left draws nothing, because zero is
// the emptiness law's silence.
func TestBashBeltFrameShowsFreeChildSlots(t *testing.T) {
	t.Parallel()
	graph, node, child, room, _ := bashBeltFrameFixture(t, true, nil)
	room.live.steps = 1
	if frame := child.bashBeltFrame(); strings.Contains(frame, "free slots") {
		t.Fatalf("a node that never divided drew a slot line: %q", frame)
	}
	// FOUR CLAIMED SLOTS ARE FOUR TAKEN, and the frame reads the remainder.
	setSlots(graph, node.id, 4)
	if frame := child.bashBeltFrame(); !strings.Contains(frame, fmt.Sprintf("free slots: %d", taskFanLimit-4)) {
		t.Fatalf("frame does not show the free child slots: %q", frame)
	}
	// AND A NODE AT THE CAP DRAWS NOTHING: zero renders as nothing.
	setSlots(graph, node.id, taskFanLimit)
	if frame := child.bashBeltFrame(); strings.Contains(frame, "free slots") {
		t.Fatalf("a node with no slot left drew a slot line: %q", frame)
	}
	// AND AN ADMITTED CHILD IS COUNTED BESIDE THE CLAIM, which is the whole of
	// the cap's arithmetic: eighteen claimed and one admitted leaves one.
	setSlots(graph, node.id, 18)
	putChild(graph, node.id, TaskRunning)
	if frame := child.bashBeltFrame(); !strings.Contains(frame, "free slots: 1") {
		t.Fatalf("frame does not count an admitted child against the cap: %q", frame)
	}
}

// THE CHILD NEWS IS THIS NODE'S OWN CHILDREN AND WHERE EACH STANDS: a child
// still running or queued is new, any ending has settled it, each half drawn
// only when it is not zero, and a node with no children drawing no line at all.
func TestBashBeltFrameChildNewsFollowsTheGraph(t *testing.T) {
	t.Parallel()
	graph, node, child, room, _ := bashBeltFrameFixture(t, true, nil)
	room.live.steps = 2
	if frame := child.bashBeltFrame(); strings.Contains(frame, "parts:") {
		t.Fatalf("a node with no children drew child news: %q", frame)
	}
	putChild(graph, node.id, TaskRunning)
	if frame := child.bashBeltFrame(); !strings.Contains(frame, "parts: 1 new") {
		t.Fatalf("frame with one child running = %q", frame)
	}
	putChild(graph, node.id, TaskDone)
	if frame := child.bashBeltFrame(); !strings.Contains(frame, "parts: 1 new, 1 settled") {
		t.Fatalf("frame with a new and a settled child = %q", frame)
	}
	// AND A CHILD THAT HAS SETTLED SINGS ITS OWN HALF ONLY: with every child
	// done the line carries no "new" half at all.
	graph.mu.Lock()
	for _, id := range graph.order {
		if n := graph.nodes[id]; n != nil && n.parent == node.id {
			n.state = TaskDone
		}
	}
	graph.mu.Unlock()
	// AND A QUEUED CHILD IS NEW LIKE A RUNNING ONE.
	putChild(graph, node.id, TaskQueued)
	if frame := child.bashBeltFrame(); !strings.Contains(frame, "parts: 1 new, 2 settled") {
		t.Fatalf("frame with a queued child = %q", frame)
	}
}

// THE FLAG-OFF ROAD IS THE ROAD THAT WAS THERE BEFORE WAVE 2. A worker built
// without the belt draws no frames at all, and its opening is the instruction
// byte for byte — the frame is a rendering the flag gates whole, not a block
// with an off switch inside it.
func TestBashBeltFrameAbsentWhenTheFlagIsOff(t *testing.T) {
	t.Parallel()
	commands := []string{"true", "echo one"}
	_, node, child, room, dir := bashBeltFrameFixture(t, false, bashSteps(commands))
	if _, _, err := runTaskChild(context.Background(), child, node, "do the work", dir,
		taskLimits{maxSteps: taskMaxSteps, noProgress: taskNoProgress}, room, io.Discard); err != nil {
		t.Fatalf("runTaskChild: %v", err)
	}
	if frames := frameNotes(t, child.snapshot()); len(frames) != 0 {
		t.Fatalf("%d frames landed with the flag off, want none: %q", len(frames), frames)
	}
	if brief := openingBrief(t, child.snapshot()); brief != "do the work" {
		t.Fatalf("the flag-off opening is not the instruction byte for byte: %q", brief)
	}
}

// A RESUMED NODE'S OPENING CARRIES THE CLAUSE EXACTLY ONCE — and the roads that
// are not a resumed bash-belt worker carry it never, each with an opening byte
// for byte what a first worker has always opened on.
func TestBashBeltResumeOpeningCarriesTheClauseOnce(t *testing.T) {
	t.Parallel()
	resumed := runBashBeltResume(t, true, true)
	want := "finish the work\n" + taskResumeClause
	if brief := openingBrief(t, resumed); brief != want {
		t.Fatalf("the resumed opening = %q, want %q", brief, want)
	}
	var whole strings.Builder
	for _, message := range resumed {
		whole.WriteString(messageContentText(message))
		whole.WriteString("\n")
	}
	if got := strings.Count(whole.String(), taskResumeClause); got != 1 {
		t.Fatalf("the clause appears %d times across the fresh worker's transcript, want exactly once", got)
	}
	// AND THE WORKER THAT WAS NOT RESUMED opens on what it always opened on.
	fresh := runBashBeltResume(t, true, false)
	if brief := openingBrief(t, fresh); brief != "finish the work" {
		t.Fatalf("a first worker's opening changed: %q", brief)
	}
	// AND SO DOES THE WORKER ON TODAY'S BELT, whatever the interrupt left
	// behind: the clause renders for bash-belt workers only.
	offBelt := runBashBeltResume(t, false, true)
	if brief := openingBrief(t, offBelt); brief != "finish the work" {
		t.Fatalf("the flag-off opening is not the instruction byte for byte: %q", brief)
	}
}

package session

// CUSTODY, ON THE ROAD (issue #567).
//
// The measured run is one sentence: a turn was moved to a task, and the brief it
// was moved on told the worker to wait for two pieces THIS conversation was
// holding, integrate their branches, and open the pull request. A task admitted
// by this road is a root, and `tasks` inside a task answers only that task's own
// children, so the worker's first look at the rail answered "No tasks have run in
// this project yet." and every minute after that was spent guessing branch names.
//
// checkpoint_custody.go states the law and its two readings. THESE ARE THE ROAD
// TESTS: a scripted turn, driven through the seam the real incident came out of,
// with two pieces genuinely out in the graph while it runs — because the whole
// question is what the ENGINE'S OWN LEDGER says at the moment the brief is
// written, and a unit over a sketch cannot ask it.
//
// The fourth test is the control and it is the point of the other three: the same
// drawing, over a conversation holding NOTHING, converts exactly as it did before
// any of this existed. A refusal bought by turning the road off is worth nothing.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The drawing the incident turned on, in miniature: one part that stands on its
// own, one part that is this conversation's own coordination of pieces it handed
// out, and the legend the reader is asked for.
const (
	custodyMixedShape = "finish the local edit > test | (tasks 1 and 2 still out) > " +
		"integrate their branches > review"
	custodyMixedLegend = "The local edit is the part that stands alone; " +
		"the other half waits on the two pieces already out."
	custodyMixedSketch = custodyMixedShape + "\n" + custodyMixedLegend

	// custodyOwnPart and custodyHeldPart are the two halves by the words a test
	// looks for them by. They are the sidecar's own, lifted out of the shape
	// above, so a test asserts on what a worker would actually read.
	custodyOwnPart  = "finish the local edit"
	custodyHeldPart = "integrate their branches"

	// custodyDraft is what the running model writes down when it is asked what is
	// left. It names the part that stands on its own and NOTHING that is out,
	// which is what makes it the rung the ladder is allowed to fall to.
	custodyDraft = "Finish the local edit, then run the test that goes with it."
)

// A CONVERSATION HANDS OVER THE WORK IT CAN HAND OVER, AND KEEPS THE WORK IT IS
// HOLDING.
//
// Both halves in one test on purpose. The self-contained part still travels —
// that is the road doing its job — and the half that names pieces this
// conversation is still holding does not ride the brief, because the worker it
// would ride to cannot see them.
func TestWorkThisConversationIsHoldingStaysHereAndTheRestStillTravels(t *testing.T) {
	const asked = "tidy up the local edit and land everything once the other two report"

	completer := &scriptedCompleter{steps: writingSteps(12, custodyMixedSketch, custodyDraft)}
	agent := custodyAgent(t, completer)
	graph := stubbedGraph(agent, custodyRunner(t))
	// TWO PIECES GENUINELY OUT, by the numbers the drawing names them by.
	first := handOutPiece(t, graph, 1, "the folder picker rail")
	second := handOutPiece(t, graph, 2, "the settings pane copy")

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	// THE REMAINDER STILL TRAVELS. The seam moved the turn, and it moved it onto a
	// task, which is the half of this that a refusal would have cost.
	if !saidSomething(noticeTexts(collected), writeSeamNote) {
		t.Fatalf("the turn was never moved; the person read %q", noticeTexts(collected))
	}
	node := custodyHandover(t, graph, first.id, second.id)

	// AND WHAT THE WORKER OPENS ON IS THE PART THAT STANDS ON ITS OWN. The fold is
	// the sidecar's capital letter and nothing else.
	if !strings.Contains(strings.ToLower(node.spec.brief), custodyOwnPart) {
		t.Errorf("the brief lost the part that was really handable:\n%s", node.spec.brief)
	}
	// AND NOT ONE WORD OF THE CONVERSATION'S OWN COORDINATION, in either of the
	// two places the drawing puts it: the shape at the head of the brief, and the
	// stages that wait behind every part of it.
	for _, phrase := range []string{custodyHeldPart, "tasks 1 and 2 still out"} {
		if strings.Contains(node.spec.brief, phrase) {
			t.Errorf("the brief hands a worker %q, which is work it cannot see:\n%s",
				phrase, node.spec.brief)
		}
	}

	// AND THE PART THAT DID NOT TRAVEL CAME BACK TO THE CONVERSATION. It is the
	// thing this session still owes, and the turn woken when those two pieces land
	// opens on the transcript this line is written into — so a remainder dropped
	// on the floor here is a remainder nobody ever does.
	if ending := messageText(lastMessage(agent)); !strings.Contains(ending, custodyHeldPart) {
		t.Errorf("the coordination this conversation kept is nowhere in what the turn ended on:\n%s",
			ending)
	}

	// AND THE TWO PIECES ARE STILL THIS CONVERSATION'S OWN. The other repair — the
	// one this law refused — would have moved them under the new task to make them
	// visible to it, and that is the boundary that makes a worker's world knowable.
	for _, piece := range []*TaskNode{first, second} {
		if piece.parent != 0 {
			t.Errorf("%q was re-parented under %d; the pieces this conversation handed out are its own",
				piece.spec.title, piece.parent)
		}
	}
}

// THE DRAWING FROM THE RUN THAT CAUSED THIS, AS IT WAS DRAWN.
//
// Same law, the sidecar's own sentence rather than a miniature of it: five stages
// of coordination behind a bracket naming the two pieces by number, with one real
// job beside them. The numbers in the shape are the numbers the fixture mints, so
// the ledger and the drawing are talking about the same two pieces.
func TestTheDrawingThatCoordinatedPiecesItCouldNotSeeHandsOverOnlyItsOwnWork(t *testing.T) {
	const asked = "get the tree-7 copy finished and land the folder picker work"
	const shape = "finish the tree-7 copy > build and test the merged worktree | " +
		"(tasks 4 and 8 still out) > integrate all branches into feat/folder-picker-v2 > " +
		"fire the parallel review propose_tasks > gh PR to origin/dev > make build for the binary"
	const legend = "The tree-7 copy is this turn's own work; the rest waits on tasks 4 and 8."
	const draft = "Finish the tree-7 copy, then build and test the merged worktree."
	const own = "finish the tree-7 copy"
	const held = "integrate all branches into feat/folder-picker-v2"

	completer := &scriptedCompleter{steps: writingSteps(12, shape+"\n"+legend, draft)}
	agent := custodyAgent(t, completer)
	graph := stubbedGraph(agent, custodyRunner(t))
	fourth := handOutPiece(t, graph, 4, "the folder picker rail")
	eighth := handOutPiece(t, graph, 8, "the settings pane copy")

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)
	if !saidSomething(noticeTexts(collected), writeSeamNote) {
		t.Fatalf("the turn was never moved; the person read %q", noticeTexts(collected))
	}
	node := custodyHandover(t, graph, fourth.id, eighth.id)

	if !strings.Contains(strings.ToLower(node.spec.brief), own) {
		t.Errorf("the brief lost the one job that was this turn's own:\n%s", node.spec.brief)
	}
	for _, phrase := range []string{held, "tasks 4 and 8 still out", "gh PR to origin/dev"} {
		if strings.Contains(node.spec.brief, phrase) {
			t.Errorf("the brief hands a worker %q, which is work it cannot see:\n%s",
				phrase, node.spec.brief)
		}
	}
	if ending := messageText(lastMessage(agent)); !strings.Contains(ending, held) {
		t.Errorf("the coordination this conversation kept is nowhere in what the turn ended on:\n%s",
			ending)
	}
	for _, piece := range []*TaskNode{fourth, eighth} {
		if piece.parent != 0 {
			t.Errorf("%q was re-parented under %d; the pieces this conversation handed out are its own",
				piece.spec.title, piece.parent)
		}
	}
}

// AND THE DRAWING IS NOT THE ONLY ROAD INTO A BRIEF.
//
// The reader here draws one honest job. What assigns the two pieces is the
// MASTERMIND THAT WRITES THE BRIEF, and it does so for the ordinary reason: it is
// shown an account of a turn that was spent standing between two running pieces,
// so it writes them into the instruction. A document nobody could work from is
// degenerate whatever wrote it, and it descends the ladder exactly as a looping
// one does — down to the draft, which named only this turn's own work.
func TestABriefThatAssignsWorkAlreadyOutIsNotTheBriefAWorkerOpensOn(t *testing.T) {
	const asked = "finish the local edit"
	const written = "Wait for tasks 1 and 2 to land, then integrate their branches and open " +
		"the pull request against dev."

	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: custodyWritingSteps(12, checkpointChainSketch, custodyDraft, written)}
	agent := custodyAgent(t, completer, func(config *Config) { config.SessionFile = path })
	graph := stubbedGraph(agent, custodyRunner(t))
	first := handOutPiece(t, graph, 1, "the folder picker rail")
	second := handOutPiece(t, graph, 2, "the settings pane copy")

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	node := custodyHandover(t, graph, first.id, second.id)

	// THE WRITER'S DOCUMENT IS NOT THE BRIEF.
	if strings.Contains(node.spec.brief, "integrate their branches") {
		t.Errorf("the writer's own coordination became the worker's instruction:\n%s", node.spec.brief)
	}
	// AND THE RUNG UNDER IT IS. A blind worker beats a stalled chat, and here the
	// draft was still there to carry.
	if !strings.Contains(node.spec.brief, custodyDraft) {
		t.Errorf("the ladder did not fall to the draft:\n%s", node.spec.brief)
	}

	// AND THE FILE SAYS WHY, in the same shape a writer that timed out is written
	// down in: the rung, what it did, and the reason under it.
	ladder := journaledCarries(t, path)
	if len(ladder) == 0 {
		t.Fatal("the ladder wrote nothing")
	}
	if ladder[0].Rung != carryRungHandoff || ladder[0].Outcome != carryDegenerate {
		t.Fatalf("the writer's rung reads %+v, want a degenerate handoff", ladder[0])
	}
	if ladder[0].Reason != carryHeldWork {
		t.Errorf("the degenerate rung reads %q, want %q", ladder[0].Reason, carryHeldWork)
	}
	if used := carriedRung(ladder); used != carryRungDraft {
		t.Errorf("the ladder says %q supplied the brief, want %q", used, carryRungDraft)
	}
}

// AND A CONVERSATION HOLDING NOTHING IS UNTOUCHED, BYTE FOR BYTE.
//
// THIS IS THE CONTROL AND IT IS WHY THE OTHER THREE ARE WORTH ANYTHING. The same
// mixed drawing, over a session that has handed nothing out, converts exactly as
// it did before any of this existed and the WHOLE drawing rides the brief —
// including the half that reads as coordination, which is #304's ruling that one
// wait beside real work is a division worth making. Without a piece out there is
// no custody to protect, and a drawing rewritten for no reason is a harness
// editing what it did not draw.
func TestAConversationHoldingNothingStillHandsTheWholeDrawingOver(t *testing.T) {
	const asked = "tidy up the local edit and land everything once the other two report"

	completer := &scriptedCompleter{steps: writingSteps(12, custodyMixedSketch, custodyDraft)}
	agent := custodyAgent(t, completer)
	graph := stubbedGraph(agent, custodyRunner(t))

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)
	if !saidSomething(noticeTexts(collected), writeSeamNote) {
		t.Fatalf("the turn was never moved; the person read %q", noticeTexts(collected))
	}
	node := custodyHandover(t, graph)

	// THE DRAWING TRAVELS WHOLE: the shape at the head, the legend under it, and
	// the dowry below that.
	if !strings.HasPrefix(node.spec.brief, "WHAT IS LEFT, AS PARTS: "+custodyMixedShape) {
		t.Fatalf("the brief does not open on the drawing the sidecar made:\n%s", node.spec.brief)
	}
	for _, phrase := range []string{custodyMixedLegend, custodyDraft} {
		if !strings.Contains(node.spec.brief, phrase) {
			t.Errorf("the brief lost %q:\n%s", phrase, node.spec.brief)
		}
	}
}

// ── the fixture ─────────────────────────────────────────────────────────────

// custodyAgent is [checkpointAgent] with the two things the write seam needs to
// be reachable at all: work the model is allowed to actually do to the disk, and
// the division road armed, which is [writeSeamAgent]'s posture. It takes mutators
// so a test that reads the journal back can name its own file.
func custodyAgent(t *testing.T, completer Completer, mutate ...func(*Config)) *Agent {
	t.Helper()
	answerTheNamerOffTheQueue(completer)
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.Divide = true
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		config.RolesSource = tierSettings(map[string]string{
			roles.TierKey(roles.TierMastermind): checkpointMarkModel,
		})
		for _, extra := range mutate {
			extra(config)
		}
	})
	return agent
}

// custodyRunner is a stubbed executor that records nothing and SETTLES NOTHING,
// which is the whole of what these tests need from it: a node it has been handed
// is running and stays running, so the conversation is genuinely still holding it
// at the moment the brief is written.
func custodyRunner(t *testing.T) func(*TaskNode) {
	t.Helper()
	return func(*TaskNode) {}
}

// handOutPiece puts one root piece into the graph under the number the drawing
// names it by.
//
// THE NUMBER IS THE POINT. A drawing that says `tasks 4 and 8 still out` is read
// against the ledger's own ids, so a fixture whose pieces came out as 1 and 2
// would be testing nothing at all — it would pass with the reading switched off.
// Ids are minted in order, so the ones in between are reserved and left unused,
// which is exactly what a conversation that proposed work and declined it does.
func handOutPiece(t *testing.T, graph *TaskGraph, want uint64, title string) *TaskNode {
	t.Helper()
	var id uint64
	for id = graph.reserve(); id < want; id = graph.reserve() {
	}
	if id != want {
		t.Fatalf("the graph is already past %d; the next number it would mint is %d", want, id)
	}
	graph.admit(id, taskSpec{
		title: title,
		// A NAME A MODEL ALREADY WROTE, so nothing asks for one — a namer taking a
		// turn of the script is a test failing on the scheduler (#392).
		named: true,
		brief: "Work through " + title + " and report back.",
	})
	node := graph.node(id)
	if node == nil {
		t.Fatalf("task %d was not admitted", id)
	}
	if node.parent != 0 {
		t.Fatalf("task %d was admitted under %d, so it is nobody's root piece", id, node.parent)
	}
	if node.stateNow().settled() {
		t.Fatalf("task %d had already reported, so this conversation is not holding it", id)
	}
	return node
}

// custodyHandover is the node this road admitted: the one root in the graph that
// was not handed out by the fixture.
func custodyHandover(t *testing.T, graph *TaskGraph, held ...uint64) *TaskNode {
	t.Helper()
	out := make(map[uint64]bool, len(held))
	for _, id := range held {
		out[id] = true
	}
	graph.mu.Lock()
	defer graph.mu.Unlock()
	var found *TaskNode
	for _, id := range graph.order {
		node := graph.nodes[id]
		if node == nil || node.parent != 0 || out[id] {
			continue
		}
		if found != nil {
			t.Fatalf("two tasks were admitted off one turn: %d and %d", found.id, node.id)
		}
		found = node
	}
	if found == nil {
		t.Fatalf("no task was admitted; the graph holds %d nodes", len(graph.nodes))
	}
	return found
}

// custodyWritingSteps is [writingSteps] with the mastermind that WRITES the brief
// scripted too, which is [handoffSteps]'s composition over the seam's own script.
func custodyWritingSteps(count int, sketch, draft, written string) []step {
	steps := writingSteps(count, sketch, draft)
	for index := range steps {
		inner := steps[index]
		steps[index] = func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedToWriteHandoff(messages) {
				return textResponse(written), nil
			}
			return inner(ctx, messages)
		}
	}
	return steps
}

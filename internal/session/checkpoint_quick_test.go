package session

// THE QUICK ROAD: what happens to a turn that only READ.
//
// checkpoint_test.go holds the full handover — the worktree, the brief a second
// model writes, the arming, the division. Everything on this page is the other
// answer to the same moment: a drawing with parts in it, out of a turn that
// touched nothing under the workspace, is one worker working through the parts
// IN ORDER, WHERE THE PERSON IS STANDING (checkpoint_quick.go).
//
// The three things that could quietly stop being true are pinned here: that the
// road is chosen off the write seam's own counter and nothing else, that the
// drawing arrives as the node's items rather than as a division to hand out, and
// that a turn which wrote is untouched by any of it.

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A WRITE-FREE TURN WHOSE DRAWING HAS PARTS IS TAKEN BY A QUICK TASK.
//
// The turn here is the ordinary research grind: rounds of `ls`, nothing written,
// and a sidecar at the first mark that draws three independent parts. Before
// this road existed that bought a worktree, a ninety-second writer and a checker
// for what is a reading; now it buys one node, started here, with the drawing as
// its list.
func TestAWriteFreeTurnWithPartsIsTakenByAQuickTask(t *testing.T) {
	const asked = "work through the four things I listed and report back"
	const dowry = "Finish the four pieces\nwhat is left, and everything this turn already found out"

	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: loopingGrindSteps(checkpointMarkAt(checkpointMarks), checkpointSplitSketch, dowry)}
	agent := checkpointAgent(t, completer, func(config *Config) {
		config.Divide = true
		config.SessionFile = path
	})
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)
	node := ran.await(t)

	// EXACTLY ONE NODE, on the one road.
	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks were admitted at the first mark, want exactly one", count)
	}
	// AND IT IS A QUICK ONE.
	if node.spec.quick == nil {
		t.Fatalf("a turn that wrote nothing was handed to a full task; the brief was %q", node.spec.brief)
	}
	// THE LINE IS THE PERSON'S OWN SENTENCE, verbatim, exactly as the request is.
	if node.spec.quick.line != asked {
		t.Errorf("the quick node's line is %q, want the person's own words %q", node.spec.quick.line, asked)
	}
	if node.spec.request != asked {
		t.Errorf("the node's request is %q, want the person's own words %q", node.spec.request, asked)
	}
	// AND THE ITEMS ARE THE DRAWING, in the legend's words and in shape order.
	want := []string{"the validation workflow", "the arithmetic module", "the currency module."}
	if !reflect.DeepEqual(node.spec.quick.items, want) {
		t.Errorf("the quick node's items are %q, want the parts the sidecar drew %q", node.spec.quick.items, want)
	}
	// NOTHING IS CLAIMED, because a turn that wrote nothing has named no file it
	// is going to write ([quickTaskSpec.files], the emptiness law).
	if len(node.spec.quick.files) != 0 {
		t.Errorf("the quick node claimed %q, want nothing", node.spec.quick.files)
	}
	// AND THE DRAWING IS NOT ALSO CARRIED AS A DIVISION. The items ARE the parts;
	// a spec holding both would hand the same work out twice
	// (task_divide_sketch.go's [Agent.sizeBeside]).
	if node.spec.drawn.proposes() {
		t.Error("the quick node also carries the drawing as a division to hand out")
	}
	if node.dividing() {
		t.Error("a quick node was armed to split")
	}

	// THE ONE LINE, EXACTLY — and neither of the two it stands in for.
	said := noticeTexts(collected)
	if !saidSomething(said, checkpointQuickNote) {
		t.Fatalf("the quick road never said its line; notices were %q", said)
	}
	if saidSomething(said, checkpointCeilingNote) {
		t.Errorf("a quick start drew the watched road's line as well; notices were %q", said)
	}
	if saidSomething(said, "this looked like work, so task ") {
		t.Errorf("a quick start said the started-task line under its own; notices were %q", said)
	}
	// AND IT IS ONE NOTICE AND NOT TWO. The whole point of the wording is that a
	// person meets one small event.
	if quick := linesSaying(said, checkpointQuickNote); len(quick) != 1 {
		t.Errorf("the quick line was said %d times, want once: %q", len(quick), quick)
	}

	// THE TRANSCRIPT IS NOT LEFT WITH A REQUEST NOBODY REPLIED TO.
	if last := lastMessage(agent); last.Role != "assistant" ||
		!strings.Contains(messageText(last), checkpointQuickNote) {
		t.Errorf("the turn did not end on its own line; the transcript ends with a %s saying %q",
			last.Role, messageText(last))
	}

	// AND THE FILE SAYS WHICH ROAD IT WAS. `quick` is the word a bench reads to
	// tell this handover from every other, and it stands where a rung would
	// because no ladder was walked ([carryRungQuick]).
	ceilings := journaledCeilings(t, path)
	if len(ceilings) != 1 {
		t.Fatalf("the handover journaled %+v, want exactly one ending", ceilings)
	}
	if ceilings[0].Decision != checkpointCeilingMoved {
		t.Errorf("the ending journaled %q, want %q", ceilings[0].Decision, checkpointCeilingMoved)
	}
	if ceilings[0].Carry != carryRungQuick {
		t.Errorf("the ending journaled carry %q, want %q", ceilings[0].Carry, carryRungQuick)
	}
	if ceilings[0].TaskID != node.id {
		t.Errorf("the ending named task %d, want the node it started (%d)", ceilings[0].TaskID, node.id)
	}
}

// AND A TURN THAT WROTE A FILE STILL TAKES THE WATCHED ROAD.
//
// The counter is the whole of the difference: the same drawing, the same mark,
// the same ask — one write call under the workspace, and what the work is handed
// to is the task in its own copy of the folder, briefed and armed.
func TestATurnThatWroteAFileStillTakesTheWatchedRoad(t *testing.T) {
	const asked = "work through the four things I listed and report back"
	const dowry = "Finish the four pieces\nwhat is left, and everything this turn already found out"

	completer := &scriptedCompleter{steps: writingGrindSteps(checkpointMarkAt(checkpointMarks)+checkpointSlack, checkpointSplitSketch, dowry)}
	agent := checkpointWritingAgent(t, completer, func(config *Config) { config.Divide = true })
	ran := make(ranNodes, 2)
	stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	collected := collect(t, mustSubmit(t, agent, asked))
	node := ran.await(t)

	if node.spec.quick != nil {
		t.Fatalf("a turn that wrote a file was handed to a quick node: %+v", node.spec.quick)
	}
	if !strings.HasPrefix(node.spec.brief, "WHAT IS LEFT, AS PARTS: A | B | C") {
		t.Errorf("the brief does not open on the parts the sidecar drew:\n%s", node.spec.brief)
	}
	if !node.dividing() {
		t.Error("a writing turn's split armed nothing")
	}
	said := noticeTexts(collected)
	if !saidSomething(said, checkpointCeilingNote) {
		t.Fatalf("the watched road never said its line; notices were %q", said)
	}
	if saidSomething(said, checkpointQuickNote) {
		t.Errorf("a turn that wrote a file was announced as a quick start; notices were %q", said)
	}
}

// ── the drawing, read out as a list ─────────────────────────────────────────

// EVERY LETTER IS AN ITEM, IN THE ORDER IT WAS DRAWN.
//
// This is where the quick road reads a shape differently from the division road,
// and the difference is the whole of why it has a reader of its own: `A | B | C
// > D` is THREE parts to hand out side by side and FOUR things for one worker to
// do in order. A reader that reused the division's would drop D or bury it.
func TestTheItemsAreEveryLetterOfTheDrawingInOrder(t *testing.T) {
	for _, shape := range []struct {
		name   string
		sketch checkpointSketch
		want   []string
	}{
		{
			name: "an arrow behind a part is still an item",
			sketch: parseCheckpointSketch("A | B | C > D\n" +
				"A is the parser, B is the handlers, C is the migration, D is the release notes."),
			want: []string{"the parser", "the handlers", "the migration", "the release notes."},
		},
		{
			name: "a gathering stage behind the parts is the last item",
			sketch: parseCheckpointSketch("(A | B | C) > D\n" +
				"A is the parser, B is the handlers, C is the migration, D is the summary."),
			want: []string{"the parser", "the handlers", "the migration", "the summary."},
		},
		{
			name: "a drawing that needs no legend is its own list",
			sketch: parseCheckpointSketch("read the parser | read the handlers | read the migration\n" +
				"Three files, one each."),
			want: []string{"read the parser", "read the handlers", "read the migration"},
		},
	} {
		t.Run(shape.name, func(t *testing.T) {
			if got := sketchItems(shape.sketch); !reflect.DeepEqual(got, shape.want) {
				t.Errorf("the drawing reads as %q, want %q", got, shape.want)
			}
		})
	}
}

// AND THE QUICK LINE IS IN THE HOUSE REGISTER, like every other dim one-liner
// the harness writes over somebody's turn.
//
// It says something the other three cannot: not that the work is watched, not
// that it can split, but WHERE it is happening — here, in this folder — which is
// the whole of what tells a quick node from the task a person would otherwise
// assume had started somewhere else.
func TestTheQuickLineIsTheLineAndCarriesNoMachinery(t *testing.T) {
	const want = "this is running long · carrying on here, in this folder, with everything already read: "
	if checkpointQuickNote != want {
		t.Fatalf("the quick line reads %q, want %q", checkpointQuickNote, want)
	}
	inTheHouseRegister(t, checkpointQuickLine("the four pieces"))
	if checkpointQuickNote == checkpointCeilingNote {
		t.Error("the quick line says what another moment says, and the three have seen different things")
	}
	// AND IT SAYS WHAT THE OTHER TWO CANNOT: the work went WITH what was already
	// read. That half is the whole difference between a promotion and a restart,
	// and a person cannot see it anywhere else (inherit.go).
	if !strings.Contains(checkpointQuickNote, "already read") {
		t.Errorf("the quick line does not say the work took what was read with it: %q", checkpointQuickNote)
	}
}

// ── the gate ────────────────────────────────────────────────────────────────

// A SESSION WITH NO COUNTER CANNOT PROVE THE DISK WAS LEFT ALONE.
//
// The fail-safe direction, and it is the opposite of the one the completion
// claim takes: a doubt about whether a turn wrote is not a proof that it did
// not, and the road it would open is the one that skips the worktree.
func TestATurnWithNoWriteCounterIsNeverTakenAsWriteFree(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if agent.turnWroteNothing() {
		t.Error("a session that never ran an episode claimed its turn wrote nothing")
	}
	meter := newWriteMeter()
	if !meter.untouched() {
		t.Error("a fresh counter says something was written")
	}
	meter.wrote([]string{"notes.md"})
	if meter.untouched() {
		t.Error("a counter that took a write still says the turn wrote nothing")
	}
	// AND IT IS THE SAME COUNTER THE ALLOWANCE IS READ OFF, so one write is not
	// yet past it.
	if meter.past() {
		t.Error("one write spent the whole allowance")
	}
}

// A QUICK NODE HANDED A DRAWING CAN TICK ITS OWN LIST.
//
// THIS ROAD BUILT ITS SPEC WITH A LITERAL OF ITS OWN AND LEFT THE TICKS OUT,
// which nothing noticed until a worker used them: `items {"done": 1}` indexed
// off the end of a slice of length zero, the fault was recovered into the
// model's result as "tool panicked: items did not return a result", and the
// node never landed. Measured on a real run, 2026-09-10.
//
// The door the `items` tool calls is driven here directly, on a node this road
// actually admitted, because that is the whole of what was broken: the spec
// that arrived, and the first tick against it.
func TestAQuickNodeFromADrawingTicksItsListRatherThanFaulting(t *testing.T) {
	const asked = "work through the four things I listed and report back"
	const dowry = "Finish the four pieces\nwhat is left, and everything this turn already found out"

	completer := &scriptedCompleter{steps: loopingGrindSteps(checkpointMarkAt(checkpointMarks), checkpointSplitSketch, dowry)}
	agent := checkpointAgent(t, completer, func(config *Config) { config.Divide = true })
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collect(t, events)
	node := ran.await(t)
	if node.spec.quick == nil {
		t.Fatal("a write-free turn with parts was not handed to a quick node")
	}

	// THE TICKS ARRIVE PARALLEL TO THE ITEMS, whatever road built the spec.
	graph.mu.Lock()
	items, done := len(node.spec.quick.items), len(node.spec.quick.done)
	graph.mu.Unlock()
	if done != items {
		t.Fatalf("the node arrived with %d items and %d ticks; its worker's first tick would fault", items, done)
	}

	// AND THE FIRST TICK IS ANSWERED WITH THE COUNT, which is the only way the
	// worker can tell how far down its list it is.
	if reply := node.quickItemChange(1, nil); reply != fmt.Sprintf("items 1/%d done", items) {
		t.Fatalf("the first tick answered %q, want the count", reply)
	}
	// AND THE ROW A PERSON IS WATCHING MOVES TO THE NEXT PART.
	want := fmt.Sprintf("quick · 1/%d · the arithmetic module", items)
	if doing := node.notice().Doing; doing != want {
		t.Fatalf("the row reads %q one tick in, want %q", doing, want)
	}
	// AND THE GRAPH'S LOCK IS FREE AFTERWARDS. A door that kept it would leave
	// the node's beat, its row and its landing all blocked (task_quick.go).
	if !graph.mu.TryLock() {
		t.Fatal("the list door left the graph's lock held")
	}
	graph.mu.Unlock()
}

// linesSaying is every notice that opens on one line.
func linesSaying(said []string, opening string) []string {
	var found []string
	for _, line := range said {
		if strings.HasPrefix(line, opening) {
			found = append(found, line)
		}
	}
	return found
}

// ── the errand the quick road does not run ──────────────────────────────────

// A QUICK CARRY-ON ASKS NOBODY FOR A NAME.
//
// The two roads that start work nobody typed ask for the row's name the moment
// they decide to, ahead of the node (taskname.go's [nameAhead]), and the ask used
// to stand ABOVE the branch that chooses this road — so every write-free carry-on
// sent a real request to a real model and cancelled it microseconds later, for a
// row that is never renamed ([Agent.newQuickSpec] admits a quick node `named`).
// The ask is under the branch now, and this is the pair that says so: the quick
// road runs no namer at all, and the full road still runs exactly one, which is
// what keeps this from passing by having broken naming everywhere.
func TestAQuickCarryOnAsksForNoNameAndTheFullRoadStillDoes(t *testing.T) {
	const asked = "work through the four things I listed and report back"
	const dowry = "Finish the four pieces\nwhat is left, and everything this turn already found out"

	// namesAsked drives one carry-on and answers every namer off the queue,
	// counting them. It is the fixture's own aside rather than
	// [answerTheReadingsOffTheQueue] because the count IS the assertion, and it is
	// installed after [checkpointAgent], which installs that one.
	namesAsked := func(t *testing.T, steps []step) (*scriptedCompleter, *TaskNode, *int64) {
		t.Helper()
		completer := &scriptedCompleter{steps: steps}
		agent := checkpointAgent(t, completer, func(config *Config) { config.Divide = true })
		var names int64
		completer.mu.Lock()
		completer.aside = func(messages []ai.Message) (*ai.Response, bool) {
			if !isNameCall(messages) {
				return nil, false
			}
			atomic.AddInt64(&names, 1)
			return textResponse("the four pieces"), true
		}
		completer.mu.Unlock()
		ran := make(ranNodes, 2)
		stubbedGraph(agent, func(node *TaskNode) { ran <- node })
		events, err := agent.Submit(context.Background(), asked)
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}
		collect(t, events)
		return completer, ran.await(t), &names
	}

	// THE QUICK ROAD: a turn that wrote nothing and is going in circles, which is
	// the shape the net takes a turn away from now — a count of rounds moves
	// nothing (inherit.go, [loopingGrindSteps]).
	_, quick, quickNames := namesAsked(t, loopingGrindSteps(checkpointMarkAt(checkpointMarks), checkpointSplitSketch, dowry))
	if quick.spec.quick == nil {
		t.Fatal("the write-free turn was not handed to a quick node")
	}
	if !quick.spec.named {
		t.Fatal("a quick node was admitted unnamed, so the graph's own namer would ask for one")
	}
	// The node has been admitted and its turn is sealed, so any namer this road
	// was going to start has been started. A cancelled call is still a call.
	if asked := atomic.LoadInt64(quickNames); asked != 0 {
		t.Fatalf("a quick carry-on asked a model for %d name(s) it can never use", asked)
	}

	// THE FULL ROAD: the same drawing out of a turn that wrote one file, which is
	// the one turn the watched road still takes ([writingGrindSteps]).
	_, full, fullNames := namesAsked(t, writingGrindSteps(checkpointMarkAt(checkpointMarks)+checkpointSlack,
		checkpointSplitSketch, dowry))
	if full.spec.quick != nil {
		t.Fatal("a turn that wrote was handed to a quick node")
	}
	// It is asked on a goroutine of its own, so it is waited for rather than read
	// once: what would be wrong is no namer at all, not one a moment late.
	waitFor(t, "the watched road to ask for its name", func() bool {
		return atomic.LoadInt64(fullNames) >= 1
	})
	if asked := atomic.LoadInt64(fullNames); asked != 1 {
		t.Fatalf("the watched road asked for %d names, want exactly one", asked)
	}
}

// THE TWO REFUSALS THIS ROAD CAN MEET ARE WRITTEN DOWN AS TWO DIFFERENT THINGS.
//
// A carry-on inside a task hands the turn to a CHILD of that task, so the fan cap
// can refuse it ([TaskGraph.claimChild]) where a conversation's own carry-on has no
// parent to be refused for. That ending used to be journalled as
// `dropped:no-brief`, which sent whoever read the session file looking for a brief
// that was never the problem.
func TestAFullFanIsWrittenDownAsAFullFanAndNotAsNoBrief(t *testing.T) {
	const parent = uint64(7)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.InTask = true
		config.taskID = parent
	})
	graph := agent.graph()
	graph.mu.Lock()
	graph.claims = map[uint64]int{parent: taskFanLimit}
	graph.mu.Unlock()

	ask := quickAsk{line: "check the three call sites and report", items: []string{"one", "two", "three"}}
	handed := agent.handOverAsQuick(context.Background(), nil, &Usage{}, time.Now(), "test/model", ask)
	if handed.moved || handed.decision != checkpointCeilingFanFull {
		t.Fatalf("a carry-on refused by the fan cap ended as moved=%v %q, want %q",
			handed.moved, handed.decision, checkpointCeilingFanFull)
	}
	if nodes := graph.children(parent); len(nodes) != 0 {
		t.Fatalf("%d node(s) were born under a parent whose fan was full", len(nodes))
	}
	// AND THE OTHER REFUSAL KEEPS ITS OWN WORD, which is what makes the one above
	// a distinction rather than a rename.
	empty := agent.handOverAsQuick(context.Background(), nil, &Usage{}, time.Now(), "test/model", quickAsk{})
	if empty.moved || empty.decision != checkpointCeilingNoBrief {
		t.Fatalf("a carry-on with nothing on it ended as moved=%v %q, want %q",
			empty.moved, empty.decision, checkpointCeilingNoBrief)
	}
}

// loopingGrindSteps is a turn that GOES IN CIRCLES: the same failing command,
// round after round, until looped.go's watch has spent its two notes and the
// third signal ends the turn through the ceiling ([Agent.handOverLoopingTurn]).
//
// IT IS THE RUNAWAY THE PROMOTION ROAD IS FOR, and the fixture says so by being
// the shape it is. The net has three rungs and only two of them can promote: a
// turn whose CONTEXT has run out cannot hand that context to a worker on the
// same model with the same window, so its move is the brief road with the
// results compiled into it ([checkpointMeter.outOfRoom]). A turn that is looping
// has a context that is perfectly fine and work that is not moving, which is
// exactly the turn worth carrying on somewhere else with everything it read.
func loopingGrindSteps(count int, sketch, brief string) []step {
	steps := make([]step, count)
	for index := range steps {
		round := index
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForSketch(messages) {
				return textResponse(sketch), nil
			}
			if askedForHandoff(messages) {
				return textResponse(brief), nil
			}
			if askedToWriteHandoff(messages) {
				return toolResponse("no-writer", "ls", `{"path":"."}`), nil
			}
			if askedForRemains(messages) {
				return textResponse(checkpointNothingLeft), nil
			}
			return toolResponseWithText(fmt.Sprintf("spin-%d", round), "bash",
				`{"command":"exit 7"}`, "trying the build again."), nil
		}
	}
	return steps
}

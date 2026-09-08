package session

// THE DOOR OF AN UNATTENDED RUN, AS TESTS (issue #513).
//
// Three cells — one model, `--yolo`, a wall — each finished their work, went
// green on the tree, and still ran to the wall with their top task reading
// "needs you". One shape, three laws, and this file holds the laws that are not
// already at home somewhere else:
//
//  1. A HANDOVER IS AN ENDING. The three roads that seal a running turn — a
//     split at a mark, the write seam, the ceiling — put the same reading to the
//     session's goal owner that a stopped turn does, before they seal
//     (checkpoint.go's [Agent.endTurnUnderSteward]).
//  2. A LANDING THAT CANNOT COME HOME FOR A REASON ABOUT THE TREE IS NOT OFFERED
//     AGAIN. The tree half of that lives in task_unsaved_test.go, beside the law
//     it amends; the conflict half — the reason that IS about the work, which
//     still goes back to somebody — is here as the control.
//  3. A CHECKER'S WINDOW IS SPENT ON CHECKS, NOT ON ONE HUNG CALL, and a check
//     that could not run is decided by the posture rather than handed to a
//     person who is not there.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── 1. a handover is an ending ──────────────────────────────────────────────

// WORK IN FLIGHT IS CARRY ON, AND THE TURN HANDS OVER EXACTLY AS IT DID.
//
// This is the ordinary handover and it must not have moved: a session with a
// unit of work still going has not finished the ask, so the reading answers
// carry on, the task is started, and the person reads the same line they always
// read. What is new is only that the answer is WRITTEN DOWN — a run that carried
// on and a run that stopped read identically in the journal before this.
func TestAHandoverWithWorkRunningCarriesOnAndHandsOver(t *testing.T) {
	agent, transcript := stewardCheckpointAgent(t, splitSketchSteps(), nil)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	started := make(chan uint64, 4)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		started <- node.id
		<-release
	})
	held := graph.reserve()
	graph.admit(held, taskSpec{title: "write the tests", brief: "b", acceptance: "a"})
	waitStarted(t, started)

	collected := collect(t, mustSubmit(t, agent, "work through the four things I listed and report back"))

	if count := admitted(graph); count != 2 {
		t.Fatalf("%d nodes are in the graph, want the one still running and the one the handover started", count)
	}
	if !saidSomething(noticeTexts(collected), checkpointSplitNote) {
		t.Fatalf("the handover never said its line; notices were %q", noticeTexts(collected))
	}
	if saidSomething(noticeTexts(collected), checkpointStoppedNote) {
		t.Fatal("a session with work still running was stopped at its handover")
	}
	if lines := closedJournal(t, agent, transcript); !strings.Contains(lines, `"decision":"carry on"`) {
		t.Fatalf("the goal owner's answer to the handover reached no line of the journal:\n%s", lines)
	}
}

// DONE SEALS: A HANDOVER AT WHICH THE GOAL OWNER SAYS THE ASK IS MET ENDS THE
// RUN, AND STARTS NOTHING.
//
// This law used to read the other way — a handover is reached at a step boundary
// the model has not read its results at, so "finished" waited for the next
// stopped turn and the handover handed over. Measured (#513), that put two tasks
// on the rail nineteen seconds after the goal owner had said done over a green
// tree, and they ran to the fifteen-minute wall. The principal's done is not the
// model's claim over unread results: it is read off the landings, the tree, the
// checks and a second reader, none of which another round would have added to.
// So the turn ends on its own line, no task is started, and the row says done.
func TestAHandoverAtWhichTheGoalOwnerSaysDoneEndsTheRun(t *testing.T) {
	agent, transcript := stewardCheckpointAgent(t, splitSketchSteps(), nil)
	// THE LANDED UNIT IS ADMITTED THROUGH THE GRAPH'S OWN DOOR, because the
	// handover admits one too and a node placed beside the id space would be
	// written over by it (principal_audit_test.go's [landOne] says the same).
	var landed uint64
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		if node.id == landed {
			node.finish("the parser is ported and its tests pass", nil, "", mergeMerged)
			node.graph.complete(node, TaskDone)
			return
		}
		ran <- node
	})
	landed = graph.reserve()
	graph.admit(landed, taskSpec{title: "port the parser", brief: "b", acceptance: "a"})
	waitDoneNode(t, graph.node(landed))

	collected := collect(t, mustSubmit(t, agent, "work through the four things I listed and report back"))

	// NOTHING MOVED. The only node in the graph is the one that had landed, the
	// handover never said its line, and the person reads the done line instead.
	if count := admitted(graph); count != 1 {
		t.Fatalf("%d nodes are in the graph, want only the landed one: a finished ask started a task", count)
	}
	if saidSomething(noticeTexts(collected), checkpointSplitNote) {
		t.Fatalf("the handover said its line over an ask the goal owner had just called done: %q", noticeTexts(collected))
	}
	if !saidSomething(noticeTexts(collected), checkpointDoneNote) {
		t.Fatalf("the run ended without its done line; notices were %q", noticeTexts(collected))
	}
	if last := lastMessage(agent); last.Role != "assistant" ||
		!strings.Contains(messageText(last), checkpointDoneNote) {
		t.Fatalf("the turn did not end on its own line; the transcript ends with a %s saying %q",
			last.Role, messageText(last))
	}
	// AND THE ROW SAYS WHAT WAS DECIDED.
	if lines := closedJournal(t, agent, transcript); !strings.Contains(lines, `"decision":"done"`) {
		t.Fatalf("the goal owner's answer to the handover reached no line of the journal:\n%s", lines)
	}
}

// TestNoTaskStartsOverAReaderThatCouldNotBeReached proves C9 and C10, with C7
// as its red control: the real write seam seals green checked inline work
// instead of admitting a task, journals the done and its reason, and still
// carries on with a red command named when the stand-in check fails.
func TestNoTaskStartsOverAReaderThatCouldNotBeReached(t *testing.T) {
	for _, red := range []bool{false, true} {
		name := "green check"
		if red {
			name = "red check"
		}
		t.Run(name, func(t *testing.T) {
			writing := writingSteps(12, checkpointDoneSketch, checkpointNothingLeft)
			steps := make([]step, len(writing))
			for index := range writing {
				inner := writing[index]
				steps[index] = func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
					if askedForRemains(messages) {
						return nil, errors.New("context deadline exceeded")
					}
					return inner(ctx, messages)
				}
			}

			completer := &scriptedCompleter{steps: steps}
			agent, transcript := seamAgent(t, completer, true)
			steward := agent.steward()
			const asked = "rename the parser and fix everything that calls it"
			steward.hear(asked)
			marker := filepath.Join(agent.deliverableTree(), "the-check-ran")
			acceptance := "the parser is renamed and `touch " + marker + "` passes"
			redCommand := ""
			if red {
				// The before-reading is green over the empty tree and the same
				// command is red after the first inline write. A permanently red
				// `false` would correctly be subtracted as a pre-existing failure
				// and would not exercise this control.
				redCommand = "test ! -f file0.txt"
				acceptance = "the parser is renamed and `" + redCommand + "` passes"
			}
			if !steward.setAcceptance(acceptance) {
				t.Fatal("the fixture could not set the goal owner's acceptance")
			}
			if red {
				// Settle the before-reading while the tree is certainly empty. The
				// submit road calls this door too, where it is deliberately
				// asynchronous; taking it here removes scheduler order from a test
				// whose subject is the write seam after that reading.
				agent.openBaseline(context.Background())
				agent.awaitBaseline(context.Background())
			}
			graph := stubbedGraph(agent, func(*TaskNode) {})

			collected := collect(t, mustSubmit(t, agent, asked))
			lines := closedJournal(t, agent, transcript)
			if red {
				if !strings.Contains(lines, `"who":"steward","event":"decided","decision":"carry on"`) {
					t.Fatalf("the red stand-in reading was not journaled as carry on:\n%s", lines)
				}
				if !strings.Contains(lines, redCommand+" does not pass") {
					t.Fatalf("the journal does not name the red command:\n%s", lines)
				}
				if strings.Contains(lines, nothingFinishedYet) {
					t.Fatalf("the red ending claims the inline work finished nothing:\n%s", lines)
				}
				if !strings.Contains(lines, checksStoodInForTheReader) {
					t.Fatalf("the red journal row does not carry the stand-in reason:\n%s", lines)
				}
				return
			}

			if count := admitted(graph); count != 0 {
				t.Fatalf("%d tasks were admitted over finished green inline work", count)
			}
			if !saidSomething(noticeTexts(collected), checkpointDoneNote) {
				t.Fatalf("the write seam did not end on its done notice: %q", noticeTexts(collected))
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("the declared check did not run over the tree: %v", err)
			}
			if !strings.Contains(lines, `"who":"steward","event":"decided","decision":"done"`) {
				t.Fatalf("the green stand-in reading was not journaled as done:\n%s", lines)
			}
			if !strings.Contains(lines, checksStoodInForTheReader) {
				t.Fatalf("the journaled done does not carry the stand-in reason:\n%s", lines)
			}
		})
	}
}

// AND WITH THE MODEL'S OWN SENTENCE LAST, THE SAME READING DOES END THE TURN.
//
// This is the other half of the law and it is the road that was already there
// ([Agent.checkpointReopen], #507): the turn stopped, the model has read
// everything it ran, and a goal owner shown a landed unit of work with nothing
// unmet says the ask is finished. Nothing is carried on and no task is started.
func TestAStoppedTurnWithNothingLeftEndsTheRun(t *testing.T) {
	// THE TURN WRITES AND THEN STOPS, which is what buys it a reading: a small
	// read-only turn that stops is the person's to carry on and pays no reader,
	// and a turn whose last call changed the tree and then said nothing is read
	// whatever it cost ([turnLeftTheTreeUnchecked]).
	completer := &scriptedCompleter{steps: []step{
		// A SESSION WITH A CEILING WRITES ITS DONE-WHEN SENTENCE BEFORE ITS FIRST
		// TURN, on this same lane and out of the ask alone
		// (principal_acceptance.go). It is one call and it is answered here so the
		// turn's own script is not read a step out.
		finalText(`{"acceptance":"the parser is ported and its tests pass"}`),
		writeCall("call-src", "parser.go", "package parse\n"),
		finalAnswer("the parser is ported and the tests pass"),
		finalAnswer("the parser is ported and the tests pass"),
	}}
	agent, transcript := stewardCheckpointAgent(t, completer, nil)
	landOne(agent, TaskDone, "port the parser", "the parser is ported and its tests pass")
	graph := stubbedGraph(agent, func(node *TaskNode) {})

	collected := collect(t, mustSubmit(t, agent, "port the parser"))

	if count := admitted(graph); count != 1 {
		t.Fatalf("%d nodes are in the graph, want only the one that already landed", count)
	}
	if said := noticeTexts(collected); saidSomething(said, checkpointCarryOnNote) {
		t.Fatalf("a finished ask was carried on: %q", said)
	}
	if lines := closedJournal(t, agent, transcript); !strings.Contains(lines, `"decision":"done"`) {
		t.Fatalf("the stopped turn's own decision reached no line of the journal:\n%s", lines)
	}
}

// A GOAL OWNER THAT HAS STOPPED ENDS THE TURN AT THE HANDOVER, AND SAYS WHY.
//
// This is what the reading at a handover is FOR. The ceiling is gone, so there is
// no version of this turn worth paying for — not the two model calls the handover
// itself costs, and not the task it would start — and the run ends here with its
// reason instead of moving work onto a rail nobody will read.
func TestAHandoverStopsTheRunWhenTheBudgetIsGone(t *testing.T) {
	agent, transcript := stewardCheckpointAgent(t, splitSketchSteps(), func(config *Config) {
		// A ceiling that is already behind us: the run is over on the clock
		// before its first turn reaches a mark.
		config.Budget = Budget{Wall: time.Nanosecond}
	})
	graph := stubbedGraph(agent, func(node *TaskNode) {})

	collected := collect(t, mustSubmit(t, agent, "work through the four things I listed and report back"))

	if count := admitted(graph); count != 0 {
		t.Fatalf("%d tasks were started by a run that had already spent its ceiling", count)
	}
	if !saidSomething(noticeTexts(collected), checkpointStoppedNote) {
		t.Fatalf("the run ended without saying why; notices were %q", noticeTexts(collected))
	}
	if last := lastMessage(agent); last.Role != "assistant" ||
		!strings.Contains(messageText(last), checkpointStoppedNote) {
		t.Fatalf("the turn did not end on its own line; the transcript ends with a %s saying %q",
			last.Role, messageText(last))
	}
	if lines := closedJournal(t, agent, transcript); !strings.Contains(lines, `"decision":"stop"`) {
		t.Fatalf("the goal owner's stop reached no line of the journal:\n%s", lines)
	}
}

// AND THE READING AND THE SEAL ARE ONE STEP: WORK THAT IS MOVING KEEPS THE TURN
// OPEN.
//
// A task admitted between the reading and the seal — by a landing, by another
// window, by the turn's own last batch — is work this session has that the
// reading did not, and a turn sealed over it would be an ending declared across
// live work. So the flight is read and the turn sealed WITHOUT LETTING GO OF THE
// GRAPH'S LOCK in between, and a stop that meets moving work becomes the carry-on
// it actually is: the work moves onto a task exactly as it always did, and the
// journal's row says carry on rather than a stop that did not happen. The goal
// owner has not changed its mind and says the same thing at the next ending.
func TestAHandoverDoesNotSealOverWorkThatIsMoving(t *testing.T) {
	agent, transcript := stewardCheckpointAgent(t, splitSketchSteps(), nil)
	// A STOP THAT IS ALREADY LATCHED, which is the only way a stop and moving
	// work meet: a standstill is never answered while something is in flight
	// ([Steward.Decide]), so the stop this handover meets was reached at an
	// earlier ending, with nothing moving, and held ([Steward.standstill]).
	steward := agent.steward()
	steward.mu.Lock()
	steward.stopped = "nothing moved since the last look and what is left is the same"
	steward.mu.Unlock()

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	started := make(chan uint64, 4)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		started <- node.id
		<-release
	})
	held := graph.reserve()
	graph.admit(held, taskSpec{title: "write the tests", brief: "b", acceptance: "a"})
	waitStarted(t, started)

	collected := collect(t, mustSubmit(t, agent, "work through the four things I listed and report back"))

	if count := admitted(graph); count != 2 {
		t.Fatalf("%d nodes are in the graph, want the running one and the one the handover started", count)
	}
	if saidSomething(noticeTexts(collected), checkpointStoppedNote) {
		t.Fatal("the turn was sealed over a unit of work that was still running")
	}
	if !saidSomething(noticeTexts(collected), checkpointSplitNote) {
		t.Fatalf("the work did not move; notices were %q", noticeTexts(collected))
	}
	lines := closedJournal(t, agent, transcript)
	if strings.Contains(lines, `"decision":"stop"`) {
		t.Fatalf("the journal recorded a stop the run did not take:\n%s", lines)
	}
	if !strings.Contains(lines, stopHeldReason) {
		t.Fatalf("the journal never says why the stop was not taken:\n%s", lines)
	}
}

// AND THE ONE STOP THAT SEALS ANYWAY IS THE BUDGET'S, WHICH SAYS SO.
//
// Waiting for the moving work to come home is more hours and more money, which
// is the whole of what a ceiling forbids: a run that has spent it cannot buy the
// wait. So this stop ends the turn over work that is still going — and neither
// the line the person reads nor the row in the journal is allowed to sound like a
// quiet ending, so both say the work was left where it was.
func TestOnlyASpentBudgetSealsOverWorkThatIsMoving(t *testing.T) {
	agent, transcript := stewardCheckpointAgent(t, splitSketchSteps(), func(config *Config) {
		config.Budget = Budget{Wall: time.Nanosecond}
	})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	started := make(chan uint64, 4)
	graph := stubbedGraph(agent, func(node *TaskNode) {
		started <- node.id
		<-release
	})
	held := graph.reserve()
	graph.admit(held, taskSpec{title: "write the tests", brief: "b", acceptance: "a"})
	waitStarted(t, started)

	collected := collect(t, mustSubmit(t, agent, "work through the four things I listed and report back"))

	if count := admitted(graph); count != 1 {
		t.Fatalf("%d nodes are in the graph, want only the one that was already running", count)
	}
	if !saidSomething(noticeTexts(collected), checkpointStoppedNote) {
		t.Fatalf("the run did not end on its ceiling; notices were %q", noticeTexts(collected))
	}
	if !saidSomething(noticeTexts(collected), stopLeftItMovingTail) {
		t.Fatalf("the line never says the running work was left where it was; notices were %q",
			noticeTexts(collected))
	}
	lines := closedJournal(t, agent, transcript)
	if !strings.Contains(lines, `"decision":"stop"`) {
		t.Fatalf("the goal owner's stop reached no line of the journal:\n%s", lines)
	}
	if !strings.Contains(lines, stopLeftItMovingTail) {
		t.Fatalf("the journal's row does not say the work was left moving:\n%s", lines)
	}
}

// AND A SESSION SOMEBODY IS SITTING IN FRONT OF IS UNCHANGED.
//
// [Person] holds no acceptance, no budget and no opinion about what is left, and
// the handover road must not learn one on their behalf: the work moves, the same
// line is read, and nothing whatever is written into their transcript.
func TestAPersonsHandoverStillMovesTheWork(t *testing.T) {
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")
	agent := checkpointAgent(t, splitSketchSteps(), func(config *Config) {
		config.Workspace = dir
		config.SessionFile = transcript
		config.Divide = true
	})
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	collected := collect(t, mustSubmit(t, agent, "work through the four things I listed and report back"))
	ran.await(t)

	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks were started at the mark, want exactly the one the handover moved", count)
	}
	if !saidSomething(noticeTexts(collected), checkpointSplitNote) {
		t.Fatalf("a person's handover never said its line; notices were %q", noticeTexts(collected))
	}
	if saidSomething(noticeTexts(collected), checkpointStoppedNote) {
		t.Fatal("a person's own turn was stopped by something that decided for them")
	}
	if lines := closedJournal(t, agent, transcript); strings.Contains(lines, `"type":"principal"`) {
		t.Fatalf("a person's own transcript grew a line about machinery:\n%s", lines)
	}
}

// splitSketchSteps is a turn that grinds past the first mark with a sidecar
// saying the work has parts — the shortest road to a handover there is.
func splitSketchSteps() *scriptedCompleter {
	return &scriptedCompleter{
		steps: grindingSteps(checkpointMarkAt(1)+6, checkpointSplitSketch,
			"Finish the four pieces\nwhat is left, and everything this turn already found out"),
	}
}

// stewardCheckpointAgent is [checkpointAgent] for a session somebody left
// running with a ceiling: the one thing in this build that puts a [Steward]
// behind the roads under test, plus the journal its decisions are written to.
func stewardCheckpointAgent(t *testing.T, completer Completer, mutate func(*Config)) (*Agent, string) {
	t.Helper()
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")
	agent := checkpointAgent(t, completer, func(config *Config) {
		config.Workspace = dir
		config.SessionFile = transcript
		config.Unattended = true
		config.Budget = Budget{Wall: time.Hour}
		config.Divide = true
		if mutate != nil {
			mutate(config)
		}
	})
	if agent.steward() == nil {
		t.Fatal("the fixture built a session with no goal owner behind it")
	}
	return agent, transcript
}

// closedJournal closes the session and hands back what its transcript holds, which
// is the only way to read a principal's decisions: they are appended by the file
// and never held in memory.
func closedJournal(t *testing.T, agent *Agent, path string) string {
	t.Helper()
	if err := agent.Close(); err != nil {
		t.Fatalf("closing: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the journal: %v", err)
	}
	return string(content)
}

// ── the goal owner's carry-on outranks the two-minds decline ────────────────

// A HANDOVER THE SESSION'S GOAL OWNER ASKED FOR IS NEVER DROPPED.
//
// The decline rests on two readers of the WORK agreeing that none is left: the
// running model answering the dowry ask with the remains token, and the mark's
// own sketch saying done. Both are readings of the transcript. The goal owner
// has just read the same ending against THE ASK — what landed, what the checks
// said, what finished means — and said it is not finished, and that is not a
// third opinion to be outvoted by the two.
//
// Measured (#513): the attrs cell's write seam fired at round 34 and its ceiling
// at round 40, the goal owner answered carry on at both — "not yet confirmed" —
// and the decline threw both handovers away. The turn ran 830 seconds and ended
// inside a git stash with the fix uncommitted.
//
// AND A PERSON'S SESSION IS UNTOUCHED, which is the second arm: nobody reads the
// ending on their behalf, so the two minds are the whole of the evidence and the
// decline stands exactly as it did.
func TestAHandoverTheGoalOwnerAsksForIsNeverDropped(t *testing.T) {
	for _, unattended := range []bool{true, false} {
		name := "watched"
		if unattended {
			name = "left running with a ceiling"
		}
		t.Run(name, func(t *testing.T) {
			// BOTH MINDS SAY THE WORK IS FINISHED: the sketch reads `(done)` and
			// the dowry ask is answered with the remains token. THE GOAL OWNER
			// DOES NOT — its own reader names a gap, which is the disagreement
			// this test is about.
			completer := &scriptedCompleter{
				steps: gapReadingSteps(12, checkpointDoneSketch, checkpointNothingLeft, theGapTheReaderNamed),
			}
			agent, transcript := seamAgent(t, completer, unattended)
			ran := make(ranNodes, 2)
			graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })

			collected := collect(t, mustSubmit(t, agent, "rename the parser and fix everything that calls it"))

			if !unattended {
				if count := admitted(graph); count != 0 {
					t.Fatalf("%d tasks were started out of a turn two readers agreed was finished", count)
				}
				if saidSomething(noticeTexts(collected), writeSeamNote) {
					t.Fatalf("a person was told their answer was being moved; notices were %q",
						noticeTexts(collected))
				}
				return
			}
			ran.await(t)
			if count := admitted(graph); count != 1 {
				t.Fatalf("%d tasks were started, want the one the goal owner asked for", count)
			}
			if !saidSomething(noticeTexts(collected), writeSeamNote) {
				t.Fatalf("the handover never said its line; notices were %q", noticeTexts(collected))
			}
			// AND IT MOVED ON THE GOAL OWNER'S OWN ACCOUNT OF WHAT IS LEFT. The
			// running model spent its answer on the token, so there is no draft;
			// the only remainder anybody in the building has written down is the
			// brief that overruled the decline, and handing the worker the
			// person's bare sentence instead would be this road throwing away the
			// reason it did not drop.
			node := graph.node(1)
			if node == nil {
				t.Fatal("the handover started no node")
			}
			if !strings.Contains(node.spec.brief, theGapTheReaderNamed) {
				t.Fatalf("the task runs on %q, want the goal owner's own remainder", node.spec.brief)
			}
			if lines := closedJournal(t, agent, transcript); !strings.Contains(lines, `"decision":"carry on"`) {
				t.Fatalf("the reading that overruled the decline reached no line of the journal:\n%s", lines)
			}
		})
	}
}

// AND THE CEILING IS A HANDOVER ROAD, SO IT IS READ AND THE READING IS WRITTEN
// DOWN.
//
// The "not over unread results" law is about DONE and nothing else — a ceiling
// can only carry on or stop — so skipping the reading because the last thing in
// the transcript was a batch left the one ending that matters decided with the
// one opinion that matters unasked. In the measured cell there was no principal
// row against the ceiling at all.
func TestTheCeilingUnderAStewardJournalsItsDecision(t *testing.T) {
	rounds := checkpointMarkAt(checkpointMarks)
	steps := grindingSteps(rounds+checkpointSlack, checkpointDoneSketch, checkpointNothingLeft)
	agent, transcript := stewardCheckpointAgent(t, &scriptedCompleter{steps: steps}, nil)
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	collect(t, mustSubmit(t, agent, "write the eight files I listed and smoke-check them"))
	ran.await(t)

	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks were started, want the one the ceiling moved", count)
	}
	lines := closedJournal(t, agent, transcript)
	if !strings.Contains(lines, `"who":"steward"`) || !strings.Contains(lines, `"event":"decided"`) {
		t.Fatalf("the ceiling decided an ending with nobody asked:\n%s", lines)
	}
	if !strings.Contains(lines, `"decision":"carry on"`) {
		t.Fatalf("the goal owner's answer at the ceiling reached no line of the journal:\n%s", lines)
	}
}

// theGapTheReaderNamed is what the goal owner's own reader answers when it is
// asked what is left, so a session whose running model says nothing remains
// still has somebody saying otherwise.
const theGapTheReaderNamed = "three call sites still use the old name"

// gapReadingSteps is [writingSteps] with the goal owner's reading answered
// separately from the handover's: the dowry ask still gets the remains token and
// the mark still draws its sketch, and the question "what is left" gets a gap.
// They are different asks and a fixture that answers them with one string cannot
// put the two readers in disagreement.
func gapReadingSteps(count int, sketch, brief, left string) []step {
	writing := writingSteps(count, sketch, brief)
	steps := make([]step, len(writing))
	for index := range writing {
		inner := writing[index]
		steps[index] = func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForRemains(messages) {
				return textResponse(left), nil
			}
			return inner(ctx, messages)
		}
	}
	return steps
}

// seamAgent is [writeSeamAgent] with the one knob these two arms turn: whether
// somebody left the run going with a ceiling, and so whether there is a goal
// owner to read the ending at all.
func seamAgent(t *testing.T, completer Completer, unattended bool) (*Agent, string) {
	t.Helper()
	answerTheNamerOffTheQueue(completer)
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = dir
		config.SessionFile = transcript
		config.AskConsent = true
		config.Divide = true
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		config.RolesSource = tierSettings(map[string]string{
			roles.TierKey(roles.TierMastermind): checkpointMarkModel,
		})
		if unattended {
			config.Unattended = true
			config.Budget = Budget{Wall: time.Hour}
		}
	})
	if got := agent.steward() != nil; got != unattended {
		t.Fatalf("the fixture built a session with steward=%v, want %v", got, unattended)
	}
	return agent, transcript
}

// ── 2. the control: a reason that is about the WORK still goes back ──────────

// A MERGE CONFLICT IS NOT A TREE THAT REFUSED THE WORK, AND IT IS STILL OFFERED.
//
// The two failures wear different marks for exactly this reason
// (task_land_unsaved.go's [treeRefused]): a branch that would not merge is two
// versions of one file and a person choosing between them, so the accept lands
// the node back where somebody can answer it. Nothing about that moved.
func TestAcceptingWorkOverAMergeConflictStillNeedsALook(t *testing.T) {
	agent, node := unverifiedNode(t, nil)
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "cccc7777cccc8888", 23, "edit the shared file")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	// The person's uncommitted work and the node's copy changed the same file,
	// which leaves the branch at the cut while still giving the merge two
	// versions it cannot decide between for anybody.
	writeFile(t, filepath.Join(tree.dir, "shared.txt"), "the node's line\n")
	writeFile(t, filepath.Join(repo, "shared.txt"), "the person's line\n")
	node.setTree(tree)
	node.finish("edited the shared file", []string{"shared.txt"}, tree.branch, tree.merge)

	if err := agent.acceptTask(node, "I read it myself"); err != nil {
		t.Fatalf("acceptTask: %v", err)
	}

	if state := node.stateNow(); state != TaskUnverified {
		t.Fatalf("state = %q, want it back with somebody: two versions of a file is theirs to decide", state)
	}
	report, _, _, merge := node.leavings()
	if merge != mergeConflicted {
		t.Fatalf("merge = %q, want %q", merge, mergeConflicted)
	}
	if !strings.HasPrefix(report, needsLookLead) {
		t.Fatalf("the report leads with %q, want the words a person reads for a landing nobody could finish", report)
	}
	if strings.Contains(report, keptWhereItIsLead) {
		t.Fatalf("a conflict about the work was settled as though the disk had refused it:\n%s", report)
	}
	if !strings.Contains(report, "shared.txt") {
		t.Fatalf("the report does not name what clashed:\n%s", report)
	}
}

// AND A TREE REFUSAL IS TOLD FROM A WORK REFUSAL WHERE IT HAPPENS, NEVER BY
// READING THE SENTENCE AFTERWARDS.
//
// [stageTaskWork] asks git whether the place is a repository at all, so the
// commonest tree refusal of the three measured cells — a working copy with no
// repository under it — is answered by a question rather than by a phrase. Every
// other refusal is put to the tree itself ([askTheTree]), and everything neither
// of them recognises is the WORK, which keeps a landing on the road it has always
// taken rather than settling a node on a guess.
func TestATreeThatIsNoRepositorySettlesTheAcceptWhereItStands(t *testing.T) {
	repo := newTestRepo(t)
	// The conversation stands in a repository, exactly as the measured cells did:
	// what is not a repository is the directory the NODE worked in.
	agent, node := unverifiedNode(t, func(c *Config) { c.Workspace = repo })
	// A WORKING COPY WITH NO REPOSITORY UNDER IT ANYWHERE, which is the shape all
	// three measured cells landed in: the node's own directory, its work in it,
	// and `git rev-parse` walking every parent and finding nothing.
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "parser.py"), "def parse():\n    return 1\n")
	tree := taskTree{dir: outside, root: repo, branch: "task/add-the-parser-aaaa1111"}
	node.setTree(tree)
	node.finish("wrote the parser", []string{"parser.py"}, tree.branch, tree.merge)

	if err := agent.acceptTask(node, "I read it myself"); err != nil {
		t.Fatalf("acceptTask: %v", err)
	}

	if state := node.stateNow(); state != TaskDone {
		t.Fatalf("state = %q, want the accept to stand: a directory that is no repository will not be one next time", state)
	}
	report, _, _, merge := node.leavings()
	if !strings.HasPrefix(report, keptWhereItIsLead) {
		t.Fatalf("the report leads with %q, want the words for work that stayed where it is", report)
	}
	if merge != mergeAborted {
		t.Fatalf("merge = %q, want %q", merge, mergeAborted)
	}
	if _, err := os.Stat(filepath.Join(outside, "parser.py")); err != nil {
		t.Fatalf("the accept destroyed the only copy of the work: %v", err)
	}
	if !strings.Contains(report, outside) {
		t.Fatalf("the report never says where the work is:\n%s", report)
	}
	if err := agent.ResolveUnverified(node.id, TaskAccept, "again"); !errors.Is(err, ErrTaskDecided) {
		t.Fatalf("a second accept answered %v, want the already-settled sentence", err)
	}
}

// AND A LEDGER THAT NAMES NOTHING IS NOT A REFUSAL AT ALL.
//
// An empty diff is the ordinary answer for a node that only read, and for one
// whose ledger a round before it already committed: there is nothing to commit,
// no commit is made, and the landing goes on and merges. It must never reach the
// road above — that road is for a landing that could not be saved, and this one
// was.
func TestAnAcceptWithNothingToCommitStillComesHome(t *testing.T) {
	agent, node := unverifiedNode(t, nil)
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "bbbb3333cccc4444", 37, "read the parser")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	node.setTree(tree)
	node.finish("read the parser and found nothing to change", nil, tree.branch, tree.merge)

	if err := agent.acceptTask(node, "I read it myself"); err != nil {
		t.Fatalf("acceptTask: %v", err)
	}

	if state := node.stateNow(); state != TaskDone {
		t.Fatalf("state = %q, want done: nothing refused this landing", state)
	}
	report, _, _, _ := node.leavings()
	if strings.Contains(report, keptWhereItIsLead) {
		t.Fatalf("a landing nothing refused was settled as one that could not come home:\n%s", report)
	}
}

// AND THE ONE ARM THAT HAS NO TYPED QUESTION ASKS THE TREE ITSELF.
//
// Git's prose is not evidence: a hook that refuses a commit and prints
// "permission denied", a ref this run cannot lock, a message quoting somebody
// else's error all wear the words a read-only mount wears. So the tree is asked
// the question that was actually refused — can this repository still be written
// to — and only its answer settles a node for good.
func TestTheTreeItselfIsAskedWhetherItStillTakesAWrite(t *testing.T) {
	repo := newTestRepo(t)

	if got := askTheTree(repo); got != refusedByTheWork {
		t.Fatalf("a repository that takes a write was read as %v, want the work refusing", got)
	}

	if os.Geteuid() == 0 {
		t.Skip("root writes through a read-only directory, so the place cannot refuse")
	}
	// The repository's own git directory, made unwritable: it can still be read
	// and walked, so git answers every question about it — and nothing can be
	// written into it, which is the whole of what a landing needs.
	place := filepath.Join(repo, ".git")
	if err := os.Chmod(place, 0o500); err != nil {
		t.Fatalf("making %s read-only: %v", place, err)
	}
	t.Cleanup(func() { _ = os.Chmod(place, 0o755) })

	if got := askTheTree(repo); got != refusedByTheTree {
		t.Fatalf("a repository that will not take a write was read as %v, want the place refusing", got)
	}
}

// AND A COMMIT REFUSED IN A TREE THAT STILL TAKES A WRITE IS ABOUT THE WORK,
// WHATEVER GIT SAID ABOUT IT.
//
// This is the refusal the substring got wrong. Git cannot lock the branch's ref
// and says "Permission denied" — and the repository is perfectly writable, the
// person can fix what is in the way, and accepting again is worth doing. Under
// the old reading the node was settled for good on the strength of those two
// words.
func TestACommitRefusedInAWritableTreeIsAboutTheWork(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes through a read-only directory, so the commit cannot be refused")
	}
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "dddd7777eeee8888", 41, "add the parser")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "parser.py"), "def parse():\n    return 1\n")
	// The directory holding the task's own branch ref: staging works, and the
	// commit that would move the branch cannot lock it.
	refs := filepath.Join(repo, ".git", "refs", "heads", "task")
	if err := os.Chmod(refs, 0o555); err != nil {
		t.Fatalf("making %s read-only: %v", refs, err)
	}
	t.Cleanup(func() { _ = os.Chmod(refs, 0o755) })

	_, problem, refusal := tree.comeHome("add the parser", []string{"parser.py"})

	if !strings.Contains(strings.ToLower(problem), "permission denied") {
		t.Skipf("git refused the commit with %q, which is not the sentence this test is about", problem)
	}
	if refusal != refusedByTheWork {
		t.Fatalf("a commit git refused in a writable repository was read as %v, want the work refusing", refusal)
	}
}

// ── 3. the checker's window ─────────────────────────────────────────────────

// A STALLED CALL IS ABANDONED AT ITS OWN BOUND AND THE CHECK IS ASKED AGAIN
// INSIDE THE SAME WINDOW.
//
// The measured cell spent its whole five minutes on one provider stream: 183
// seconds on a single call, no refusal, no error, and a landing that said nobody
// could check the work in five minutes when nobody had in fact been asked twice.
// Here the first call hangs, is cut at its share ([auditCallShare]), and the
// second answers — with the landing saying which try it was.
func TestAStalledCheckIsAbandonedAndTheSecondCallAnswers(t *testing.T) {
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
		audit: []step{
			// THE HUNG STREAM. It answers nothing and never refuses, which is
			// exactly what the wire did.
			func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
			verdict("VERIFIED — greet.go has the greeting the brief asked for"),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		// The first call must time out, while the second still needs room for
		// real repository setup when other package suites share the machine.
		config.auditWindow = 10 * time.Second
	})
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "add a greeting"))

	node := graph.node(1)
	waitDoneNode(t, node)
	notice := node.notice()

	if notice.State != TaskDone {
		t.Fatalf("state = %q, want done: the second call answered inside the window (report %q)",
			notice.State, notice.Report)
	}
	if !strings.Contains(notice.Report, checkedOnTheSecondTry) {
		t.Fatalf("the landing does not say which try answered:\n%s", notice.Report)
	}
	// AND THE WINDOW WAS NOT SPENT ON THE ONE CALL: the check really was asked
	// twice, which is the whole of what the bound buys.
	if calls := completer.auditCalls(); calls != 2 {
		t.Fatalf("the check ran %d times, want 2: one call abandoned and one answered", calls)
	}
	if strings.Contains(notice.Report, "nobody could check it in") {
		t.Fatalf("a check that answered still says nobody could check it:\n%s", notice.Report)
	}
}

// AND A CALL THAT WOULD GET ALMOST NOTHING IS NOT MADE AT ALL.
//
// What is left of the window when the second attempt starts is whatever the
// first one did not spend, less closing one checker and building another. A bound
// of a few hundred milliseconds is not a bound — it guarantees the non-answer it
// then writes down as a call that STALLED, which is a sentence about a call that
// never had a chance. Below a tenth of the window ([auditCallFloorShare]) the
// retry is not made and the landing says what actually happened.
func TestACallWithLessThanTheFloorLeftIsNotMade(t *testing.T) {
	window := auditDeadline
	opened := time.Now()
	pace := newAuditPace(window, opened)

	// A first call, at its share, leaves half — which is worth asking with.
	bound, worth := pace.bound(opened.Add(window / 2))
	if !worth || bound != window/2 {
		t.Fatalf("a retry with half the window left was given %v (worth=%v), want %v", bound, worth, window/2)
	}
	// A retry that reaches this line with a twentieth left is not made.
	if bound, worth := pace.bound(opened.Add(window - window/20)); worth {
		t.Fatalf("a retry with %v left was made anyway with a bound of %v", window/20, bound)
	}
	// Nor is one that arrives after the window has closed altogether.
	if _, worth := pace.bound(opened.Add(window + time.Second)); worth {
		t.Fatal("a call was made after the window had closed")
	}
	// AND THE FLOOR IS THE WINDOW'S OWN TENTH, interpolated rather than spelled
	// twice.
	if pace.floor != window/auditCallFloorShare {
		t.Fatalf("the floor is %v, want %v", pace.floor, window/auditCallFloorShare)
	}
}

// AND A WINDOW THE SHARES DIVIDE DOWN TO NOTHING IS NOT A CALL EITHER.
//
// The share and the floor are both the window cut by a whole number, so a small
// enough window truncates them to zero — and a call bounded at zero is a request
// submitted on a context that has already expired. It would come back looking
// exactly like a stalled stream, and the person would read that a call ran
// without answering when no call was ever made.
func TestAWindowTooSmallToBoundACallMakesNone(t *testing.T) {
	opened := time.Now()
	pace := newAuditPace(time.Nanosecond, opened)

	if pace.floor != 0 || pace.call != 0 {
		t.Fatalf("a 1ns window cut into a call of %v and a floor of %v, want both to divide down to nothing",
			pace.call, pace.floor)
	}
	if bound, worth := pace.bound(opened); worth {
		t.Fatalf("a call was made with a bound of %v inside a window nothing fits in", bound)
	}
}

// AND A SECOND CALL THAT COULD NOT BE MADE DOES NOT ERASE THE FIRST.
//
// One call was asked and abandoned, and the window closed before another could
// be built. The landing owes both facts: a report that only said nobody could
// check the work would be the harness claiming nobody was asked, which is the
// same lie the whole of this section is about — and the sentence the work is
// taken as it stands on quotes the first line, so the account has to lead it.
func TestAStalledCallSurvivesAWindowThatClosedBeforeASecond(t *testing.T) {
	opened := time.Now()
	pace := newAuditPace(auditDeadline, opened)
	first := noVerdict(checkerStalled(pace.call), "the checker never said a word")

	if _, worth := pace.bound(opened.Add(auditDeadline)); worth {
		t.Fatal("a second call was worth making after the window had closed")
	}

	kept := first.andTheWindowClosed()
	account := strings.Join(kept.evidence, " · ")
	if !strings.Contains(account, "without answering and was abandoned") {
		t.Fatalf("the account forgets the call that was made:\n%s", account)
	}
	if !strings.Contains(account, checkerWindowClosed) {
		t.Fatalf("the account never says why nobody was asked again:\n%s", account)
	}
	if strings.Contains(account, checkerRanOut(pace.window)) {
		t.Fatalf("the account says nobody was asked, and somebody was:\n%s", account)
	}
	if stands := takenAsItStands(kept); !strings.Contains(stands, checkerWindowClosed) {
		t.Fatalf("the reason the work is taken as it stands stops short:\n%s", stands)
	}
	if kept.answered {
		t.Fatal("a call nobody answered was read as an answer")
	}
}

// AND WHAT THE LANDING SAYS THEN IS THAT THE WINDOW CLOSED.
//
// A window too small to hold a call at all is the same shape as a retry that
// arrives with nothing left: no call is worth making, and the one thing the
// landing must not say is that somebody's call ran without answering, because
// nobody's did.
func TestAWindowTooSmallToAskInSaysTheWindowClosed(t *testing.T) {
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
		audit: []step{
			func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
			func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		// Smaller than the floor under one call, so no call is worth making at
		// all: the window is already closed by the time the first bound is read.
		config.auditWindow = time.Nanosecond
	})
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "add a greeting"))

	node := graph.node(1)
	waitDoneNode(t, node)
	notice := node.notice()

	if !strings.Contains(notice.Report, "nobody could check it in") {
		t.Fatalf("the landing does not say the window closed:\n%s", notice.Report)
	}
	if strings.Contains(notice.Report, "without answering and was abandoned") {
		t.Fatalf("the landing blames a call that was never made:\n%s", notice.Report)
	}
	if calls := completer.auditCalls(); calls != 0 {
		t.Fatalf("%d calls were made inside a window nothing fits in", calls)
	}
}

// AND A WINDOW ONE STALL CLOSED IS DECIDED BY THE POSTURE, WHICHEVER POSTURE IT
// IS.
//
// Same fixture, twice. On a run somebody left going with a ceiling there is
// nobody to hand the question to, so the work is taken as it stands and the
// landing says what it did and why. On every other run the card stands exactly
// as it did: the work needs somebody's look, and it waits for them.
//
// AND WHAT IT SAYS IT DID IS WHAT HAPPENED. Both checkers were asked and both
// held their stream until they were cut, so the sentence the work is taken as it
// stands on names the stall — never "nobody could check it", which is the one
// account of this evening that is not true.
func TestAWindowOneStallClosedIsDecidedByThePosture(t *testing.T) {
	for _, unattended := range []bool{true, false} {
		name := "watched"
		if unattended {
			name = "left running with a ceiling"
		}
		t.Run(name, func(t *testing.T) {
			repo := newGoModuleRepo(t)
			t.Setenv("HOME", t.TempDir())

			hang := func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			parent := []step{
				proposeCall("Add the greeting", "write greet.go"),
				finalText("handed off"),
			}
			if unattended {
				// A SESSION WITH A CEILING WRITES ITS DONE-WHEN SENTENCE BEFORE
				// ITS FIRST TURN, on the conversation's own lane and out of the
				// ask alone (principal_acceptance.go). It is one call and it is
				// answered here so the turn's own script is not read a step out.
				parent = append([]step{finalText(`{"acceptance":"greet.go has the greeting"}`)}, parent...)
			}
			completer := &routedCompleter{
				parent: parent,
				child: []step{
					writeCall("call-src", "greet.go", "package greet\n\nfunc Greet() string { return \"hi\" }\n"),
					finalText("Wrote greet.go with the greeting."),
				},
				// Nobody answers on either attempt, so the window closes across
				// the two of them.
				audit: []step{hang, hang},
			}
			agent, _ := newTestAgent(t, completer, func(config *Config) {
				config.Workspace = repo
				config.AskConsent = false
				config.TaskAutoApproveSeconds = 0
				config.auditWindow = 40 * time.Millisecond
				if unattended {
					config.Unattended = true
					config.Budget = Budget{Wall: time.Hour}
				}
			})
			graph := agent.graph()
			collect(t, mustSubmit(t, agent, "add a greeting"))

			node := graph.node(1)
			waitDoneNode(t, node)
			notice := node.notice()

			if !unattended {
				if notice.State != TaskUnverified {
					t.Fatalf("state = %q, want it waiting on somebody (report %q)", notice.State, notice.Report)
				}
				if !strings.HasPrefix(notice.Report, needsLookLead) {
					t.Fatalf("a watched run stopped asking:\n%s", notice.Report)
				}
				return
			}
			if notice.State != TaskDone {
				t.Fatalf("state = %q, want the posture to have decided it (report %q)", notice.State, notice.Report)
			}
			// THE LANDING SAYS WHAT IT DID AND WHY, in the words a person reads:
			// the checker's own account of what became of it, and the fact that
			// there was nobody to ask.
			for _, want := range []string{
				takenAsItStandsLead,
				"without answering and was abandoned",
				checkerWindowClosed,
				takenAsItStandsTail,
			} {
				if !strings.Contains(notice.Report, want) {
					t.Fatalf("the landing is missing %q:\n%s", want, notice.Report)
				}
			}
			if strings.Contains(notice.Report, "nobody could check it in") {
				t.Fatalf("the landing says nobody was asked, and two checkers were:\n%s", notice.Report)
			}
			if strings.Contains(notice.Report, needsLookLead) {
				t.Fatalf("an unattended run still asks somebody who is not there:\n%s", notice.Report)
			}
			// AND THE VOCABULARY LAW HOLDS ON THE NEW SENTENCE.
			for _, banned := range []string{"auditor", "verdict", "verified", "refuted"} {
				if strings.Contains(strings.ToLower(notice.Report), banned) {
					t.Fatalf("the landing says %q to a person:\n%s", banned, notice.Report)
				}
			}
		})
	}
}

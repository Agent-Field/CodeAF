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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	agent, transcript := stewardCheckpointAgent(t, splitSketchSteps())
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
	if saidSomething(noticeTexts(collected), checkpointHandoverDoneNote) {
		t.Fatal("a session with work still running was told the ask was finished")
	}
	if lines := closedJournal(t, agent, transcript); !strings.Contains(lines, `"decision":"carry on"`) {
		t.Fatalf("the goal owner's answer to the handover reached no line of the journal:\n%s", lines)
	}
}

// AND NOTHING IN FLIGHT WITH NOTHING LEFT IS DONE: THE TURN ENDS AND NO TASK IS
// STARTED.
//
// This is the whole of the measured failure. The head's only turn was handed to
// a task inside two minutes, the handover sealed it without asking anybody
// anything, and the run then had no road back at all — every landing woke a turn
// that handed over again. Here one unit of work has landed, nothing is moving,
// and the session says so and stops rather than spending the wall on another
// task nobody asked for.
func TestAHandoverWithNothingLeftEndsTheTurnWithoutATask(t *testing.T) {
	agent, transcript := stewardCheckpointAgent(t, splitSketchSteps())
	landOne(agent, TaskDone, "port the parser", "the parser is ported and its tests pass")
	graph := stubbedGraph(agent, func(node *TaskNode) {})

	collected := collect(t, mustSubmit(t, agent, "work through the four things I listed and report back"))

	// NO TASK. The landed node is the only one in the graph, and the line the
	// person reads is the ending rather than the handover.
	if count := admitted(graph); count != 1 {
		t.Fatalf("%d nodes are in the graph, want only the one that already landed", count)
	}
	if !saidSomething(noticeTexts(collected), checkpointHandoverDoneNote) {
		t.Fatalf("the session never said why it stopped; notices were %q", noticeTexts(collected))
	}
	if saidSomething(noticeTexts(collected), checkpointSplitNote) {
		t.Fatal("the work was handed over on top of an ask that was finished")
	}
	// AND THE TURN IS OVER, with the transcript ending on the line that says so
	// rather than on a batch of tool results nothing explains.
	if last := lastMessage(agent); last.Role != "assistant" ||
		!strings.Contains(messageText(last), checkpointHandoverDoneNote) {
		t.Fatalf("the turn did not end on its own line; the transcript ends with a %s saying %q",
			last.Role, messageText(last))
	}
	if lines := closedJournal(t, agent, transcript); !strings.Contains(lines, `"decision":"done"`) {
		t.Fatalf("the goal owner's answer to the handover reached no line of the journal:\n%s", lines)
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
	if saidSomething(noticeTexts(collected), checkpointHandoverDoneNote) {
		t.Fatal("a person was told their session had decided the ask was finished")
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
func stewardCheckpointAgent(t *testing.T, completer Completer) (*Agent, string) {
	t.Helper()
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")
	agent := checkpointAgent(t, completer, func(config *Config) {
		config.Workspace = dir
		config.SessionFile = transcript
		config.Unattended = true
		config.Budget = Budget{Wall: time.Hour}
		config.Divide = true
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
	// The same file changed on both sides, which is the one thing a merge cannot
	// decide for anybody.
	writeFile(t, filepath.Join(tree.dir, "shared.txt"), "the node's line\n")
	writeFile(t, filepath.Join(repo, "shared.txt"), "the person's line\n")
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "person")
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
		// The test clock: a window a test can wait out, cut into calls a test can
		// wait out twice.
		config.auditWindow = 200 * time.Millisecond
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

// AND A WINDOW THAT CLOSES IS DECIDED BY THE POSTURE, WHICHEVER POSTURE IT IS.
//
// Same fixture, twice. On a run somebody left going with a ceiling there is
// nobody to hand the question to, so the work is taken as it stands and the
// landing says what it did and why. On every other run the card stands exactly
// as it did: the work needs somebody's look, and it waits for them.
func TestAWindowThatClosedIsDecidedByThePosture(t *testing.T) {
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
			for _, want := range []string{takenAsItStandsLead, "nobody could check it in", takenAsItStandsTail} {
				if !strings.Contains(notice.Report, want) {
					t.Fatalf("the landing is missing %q:\n%s", want, notice.Report)
				}
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

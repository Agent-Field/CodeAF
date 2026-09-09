package session

// WHAT THE ENGINE TRIES BEFORE ANYTHING IS THE PERSON'S CALL.
//
// Three automatic attempts, one bound each, one plain line in the node's journal
// apiece: a check that answers neither word is put to another model, a branch
// that will not fasten gets one resolver round, and an ending that says nothing
// about the work buys one rerun from the branch. Every one of them exists
// because the card it replaces asked a person to press a key meaning "try that
// again", which is not a decision — it is an errand.

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the checker's failover ──────────────────────────────────────────────────

// laneCompleter is a routed completer that also records WHICH MODEL each
// checker's call rode, and offers a fallback chain the way the provider adapter
// does ([provider.Client.FallbackModels]). It is the only place the failover can
// be observed from the outside.
type laneCompleter struct {
	mu sync.Mutex
	// chain is what the adapter would move to; the first entry is what a
	// non-answering check is asked again on.
	chain []string
	// judged is the model of every auditor call, in order.
	judged []string
	// says is what each model answers when it is asked to judge. A model with
	// nothing here answers neither word, which is the whole of what this fixture
	// is about.
	says map[string]string
}

func (c *laneCompleter) FallbackModels(string) []string { return c.chain }

func (c *laneCompleter) CompleteWithMessages(_ context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	if isNameCall(messages) || isCaptionCall(messages) || isTitleCall(messages) {
		return textResponse(""), nil
	}
	if len(messages) == 0 || messages[0].Role != "system" ||
		!strings.Contains(messageText(messages[0]), "You are an AUDITOR") {
		return textResponse(""), nil
	}
	c.mu.Lock()
	c.judged = append(c.judged, request.Model)
	said := c.says[request.Model]
	c.mu.Unlock()
	return textResponse(said), nil
}

func (c *laneCompleter) models() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.judged...)
}

// A CHECK THAT ANSWERS NEITHER WORD IS PUT TO ANOTHER MODEL, ONCE, AND THEN THE
// NODE LANDS AS ONE NOBODY COULD CHECK.
//
// Before this, both attempts rode the same model: a model that reads the diff,
// runs the verification and then says neither word is a model this question does
// not fit, and asking it twice bought the same silence at twice the price.
func TestACheckerThatAnswersNothingIsAskedAgainOnAnotherModel(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "aaaa1111aaaa1111", 1, "port the parser")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "parser.go"), "package main\n")

	const (
		judge = "first/judge"
		next  = "second/judge"
	)
	completer := &laneCompleter{chain: []string{next}, says: map[string]string{
		// Neither model says the word. The first is the failure this exists for;
		// the second is what makes the landing honest — the retry happened, it
		// happened somewhere else, and there is still no answer.
		judge: "I had a look and it seems broadly reasonable.",
		next:  "Hard to say either way from here.",
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.Model = judge
	})
	node := loneTestNode(t, "port the parser")

	verdict := agent.auditNode(context.Background(), node, tree, []string{"parser.go"}, "the parser is ported", io.Discard)
	if verdict.answered {
		t.Fatalf("the check answered %s; this fixture is about the answer nobody gave", verdict.report())
	}

	rode := completer.models()
	if len(rode) < 2 {
		t.Fatalf("the checker was asked %d times (%v), want a retry after the non-answer", len(rode), rode)
	}
	if rode[0] != judge {
		t.Fatalf("the first check rode %q, want the session's own judge %q", rode[0], judge)
	}
	if !containsString(rode, next) {
		t.Fatalf("the checker rode %v and never moved to %q: the retry is on the same model", rode, next)
	}

	// AND THE NODE LANDS AS ONE NOBODY COULD CHECK — not done, not failed.
	state := agent.landUnchecked(context.Background(), node, tree, []string{"parser.go"}, "the parser is ported", verdict, io.Discard)
	if state != TaskUnverified {
		t.Fatalf("the node landed %q, want the state that says nobody could check it", state)
	}
}

// AND WITH NOWHERE TO MOVE TO, THE RETRY STILL HAPPENS — on the model it
// already had. An install with no chain, and `--one-model`, both make the move
// ABSENT rather than broken: the check is asked twice, as it always was.
func TestWithNoChainTheCheckerRetryStaysOnTheModelItHas(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "bbbb2222bbbb2222", 2, "port the parser")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "parser.go"), "package main\n")

	const judge = "only/judge"
	completer := &laneCompleter{says: map[string]string{judge: "no idea, really"}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.Model = judge
	})
	node := loneTestNode(t, "port the parser")

	verdict := agent.auditNode(context.Background(), node, tree, []string{"parser.go"}, "the parser is ported", io.Discard)
	if verdict.answered {
		t.Fatalf("the check answered %s", verdict.report())
	}
	for _, rode := range completer.models() {
		if rode != judge {
			t.Fatalf("a build with no chain moved the check to %q", rode)
		}
	}
}

// ── the merge round ─────────────────────────────────────────────────────────

// conflictingRepo is the shape every merge-round fixture starts from: one file
// committed on the person's branch, a task branch that changed it, and the
// person changing the same line afterwards so the two cannot both stand.
//
// It answers the tree with the node's work already committed on its branch,
// which is where [taskTree.comeHome] leaves a branch that would not fasten.
func conflictingRepo(t *testing.T, session string, id uint64, title string) (string, taskTree) {
	t.Helper()
	// THE WORKSPACE IS AN OWNED ONE, which is the ground a landing actually
	// attempts the merge on: a checkout the PERSON is standing in and has
	// committed to since the work was cut is kept rather than merged, by a policy
	// older than this file (task_branch_protection.go's [keptLandingSentence]),
	// and a conflict never arises there to resolve.
	place, repo := newOwnedPlace(t)
	writeFile(t, filepath.Join(repo, "shared.txt"), "the original line\n")
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "-c", "user.name=aforge", "-c", "user.email=aforge@localhost", "commit", "-m", "the shared file")

	tree, err := prepareTaskTree(place, repo, session, id, title)
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "shared.txt"), "the task's line\n")
	mustGit(t, tree.dir, "add", "-A")
	mustGit(t, tree.dir, "-c", "user.name=aforge", "-c", "user.email=aforge@localhost", "commit", "-m", "task: "+title)

	writeFile(t, filepath.Join(repo, "shared.txt"), "the other side's line\n")
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "-c", "user.name=aforge", "-c", "user.email=aforge@localhost", "commit", "-m", "the other side")
	return repo, tree
}

// resolvableTestNode is [loneTestNode] with a brief the routed fixture can
// recognise, which is what tells a resolver's request from the conversation's.
func resolvableTestNode(t *testing.T, title string) *TaskNode {
	t.Helper()
	node := loneTestNode(t, title)
	node.graph.mu.Lock()
	node.brief = "keep the task's line in shared.txt\n" + taskBriefMark
	node.spec.acceptance = "shared.txt carries the task's line"
	node.graph.mu.Unlock()
	return node
}

// A BRANCH THAT WILL NOT FASTEN GETS ONE ROUND, AND A ROUND THAT RESOLVES IT
// LANDS THE WORK. The person is never shown the conflict at all.
func TestAConflictGetsOneRoundAndLandsWhenTheWorkerResolvesIt(t *testing.T) {
	repo, tree := conflictingRepo(t, "cccc3333cccc3333", 3, "edit the shared file")

	completer := &routedCompleter{
		child: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("fix", "write",
					`{"path":"shared.txt","content":"the other side's line\nthe task's line\n"}`), nil
			},
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse("both lines are kept"), nil
			},
		},
		audit: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse("VERIFIED — both lines are in shared.txt"), nil
			},
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Workspace = repo })
	node := resolvableTestNode(t, "edit the shared file")

	detail := conflictSentence(tree.branch, []string{"shared.txt"}, "")
	state := agent.landConflicted(context.Background(), node, tree, []string{"shared.txt"},
		"the shared file now carries the task's line", mergeConflicted, detail, io.Discard)

	if state != TaskDone {
		t.Fatalf("the round resolved the conflict and the node landed %q, want it done", state)
	}
	landed := readFile(t, filepath.Join(repo, "shared.txt"))
	if !strings.Contains(landed, "the task's line") || !strings.Contains(landed, "the other side's line") {
		t.Fatalf("the resolved file did not reach the person's branch:\n%s", landed)
	}
	if strings.Contains(landed, "<<<<<<<") {
		t.Fatalf("a merge round wrote conflict markers onto the person's branch:\n%s", landed)
	}
	// AND NOTHING WAS REWRITTEN IN PLACE: the branch as the node left it is still
	// nameable.
	assertBeforeMergeRef(t, repo, tree.branch)
}

// AND A ROUND THAT CANNOT RESOLVE IT LANDS ON THE CARD WITH THE FILES NAMED.
// The worker left the markers where they were, so nothing is committed, nothing
// merges, and the person is asked — which is what the round exists to be the
// last resort of, not the first.
func TestAConflictTheRoundCannotResolveLandsOnTheCardWithTheFilesNamed(t *testing.T) {
	repo, tree := conflictingRepo(t, "dddd4444dddd4444", 4, "edit the shared file")

	completer := &routedCompleter{
		child: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse("I cannot tell which of these two lines is wanted"), nil
			},
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Workspace = repo })
	node := resolvableTestNode(t, "edit the shared file")

	detail := conflictSentence(tree.branch, []string{"shared.txt"}, "")
	state := agent.landConflicted(context.Background(), node, tree, []string{"shared.txt"},
		"the shared file now carries the task's line", mergeConflicted, detail, io.Discard)

	if state != TaskUnverified {
		t.Fatalf("a round that could not resolve the conflict landed %q, want it on the card", state)
	}
	report, _, _, _ := node.leavings()
	if !strings.Contains(report, "shared.txt") {
		t.Fatalf("the card does not name the file that stood in the way:\n%s", report)
	}
	if !strings.Contains(report, "a round was spent") {
		t.Fatalf("the card does not say a round was tried:\n%s", report)
	}
	// THE PERSON'S CHECKOUT IS UNTOUCHED, which is the law the whole round runs
	// inside the task's own copy to keep.
	if got := readFile(t, filepath.Join(repo, "shared.txt")); got != "the other side's line\n" {
		t.Fatalf("the ground's file reads %q — the round reached the checkout it must not touch", got)
	}
	assertBeforeMergeRef(t, repo, tree.branch)
	// AND THE COPY IS NOT LEFT MID-MERGE for the next round to trip over.
	if _, err := git(tree.dir, "rev-parse", "--verify", "--quiet", "MERGE_HEAD"); err == nil {
		t.Fatal("the task's working copy was left mid-merge")
	}
}

// AND THE ROUND IS SPENT ONCE. A node that has already had its round lands on
// the card without buying a second worker.
func TestTheAutomaticMergeRoundIsSpentOnlyOnce(t *testing.T) {
	repo, tree := conflictingRepo(t, "eeee5555eeee5555", 5, "edit the shared file")
	completer := &routedCompleter{}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Workspace = repo })
	node := loneTestNode(t, "edit the shared file")
	node.graph.mu.Lock()
	node.mergeRounds = mergeRoundLimit
	node.graph.mu.Unlock()

	detail := conflictSentence(tree.branch, []string{"shared.txt"}, "")
	state := agent.landConflicted(context.Background(), node, tree, []string{"shared.txt"},
		"the shared file now carries the task's line", mergeConflicted, detail, io.Discard)
	if state != TaskUnverified {
		t.Fatalf("a node with no round left landed %q", state)
	}
	completer.mu.Lock()
	spent := completer.seen.child
	completer.mu.Unlock()
	if spent != 0 {
		t.Fatalf("a node with no round left bought %d worker calls", spent)
	}
	if _, err := git(repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+beforeMergeRef(tree.branch)); err == nil {
		t.Fatal("a round nothing spent still wrote the before-merge ref")
	}
}

// assertBeforeMergeRef holds the law that nothing is rewritten in place: the tip
// the task branch stood on before the round is still a ref anybody can name.
func assertBeforeMergeRef(t *testing.T, repo, branch string) {
	t.Helper()
	ref := beforeMergeRef(branch)
	if _, err := git(repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+ref); err != nil {
		t.Fatalf("%s does not exist: the round rewrote the branch in place", ref)
	}
}

// ── the door a person reaches ───────────────────────────────────────────────

// THE DOOR REFUSES IN ONE LINE, and the two refusals are different facts: a node
// with no working copy left has nothing to resolve IN, and a node whose round is
// already running must not be given a second worker in the same directory.
func TestResolveConflictRefusesWithoutAWorktreeOrWhileARoundIsRunning(t *testing.T) {
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.tasker = graph })

	inPlace := &TaskNode{graph: graph, id: 1, spec: taskSpec{title: "edit in place"}, state: TaskUnverified, merge: mergeInPlace}
	_, tree := conflictingRepo(t, "ffff6666ffff6666", 2, "edit the shared file")
	onABranch := &TaskNode{graph: graph, id: 2, spec: taskSpec{title: "edit the shared file"},
		state: TaskUnverified, worktree: tree.dir, branch: tree.branch, merge: mergeConflicted}
	graph.mu.Lock()
	graph.nodes[1], graph.nodes[2] = inPlace, onABranch
	graph.order = append(graph.order, 1, 2)
	graph.mu.Unlock()

	if err := agent.ResolveConflict(9); err == nil {
		t.Fatal("a task this session does not have was resolved")
	}
	err := agent.ResolveConflict(1)
	if err == nil {
		t.Fatal("a node with no working copy was sent a resolver round")
	}
	if strings.Contains(err.Error(), "\n") {
		t.Fatalf("the refusal is more than one line:\n%s", err)
	}
	if !strings.Contains(err.Error(), "no working copy") {
		t.Fatalf("the refusal does not say what is missing: %v", err)
	}

	// AND A ROUND ALREADY IN FLIGHT REFUSES THE SECOND ASK.
	if !onABranch.claimResolving() {
		t.Fatal("a node with nothing in flight refused the claim")
	}
	err = agent.ResolveConflict(2)
	if err == nil {
		t.Fatal("a second round was started beside the first")
	}
	if !strings.Contains(err.Error(), "already being resolved") {
		t.Fatalf("the refusal does not say a round is running: %v", err)
	}
	onABranch.releaseResolving()
}

// ── the one rerun ───────────────────────────────────────────────────────────

// AN ENDING THAT SAYS NOTHING ABOUT THE WORK BUYS ONE MORE ATTEMPT, AND THE
// SECOND ONE LANDS. The connection dropped, the provider refused, the brief was
// measured against a world that had moved: none of those is a finding, and none
// of them is a question worth putting in front of a person.
func TestAWireEndingRerunsOnceAndTheSecondLandsIncomplete(t *testing.T) {
	for _, ending := range []TaskEnding{TaskEndingWire, TaskEndingUpstream, TaskEndingStale} {
		agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
		node := loneTestNode(t, "port the parser")
		var journal strings.Builder

		if !agent.rerunsFromItsBranch(node, ending, &journal) {
			t.Fatalf("%s bought no rerun", ending)
		}
		if !strings.Contains(journal.String(), "running it again from its branch") {
			t.Fatalf("%s left no line in the journal:\n%s", ending, journal.String())
		}
		if !strings.Contains(journal.String(), rerunBecause(ending)) {
			t.Fatalf("the journal line does not say why:\n%s", journal.String())
		}
		node.graph.run = func(*TaskNode) {}
		if !node.graph.runAgainFromItsBranch(node) {
			t.Fatalf("%s: the node would not go back on the frontier", ending)
		}
		if got := node.stateNow(); got != TaskQueued && got != TaskRunning {
			t.Fatalf("%s: the node is %q after its rerun, want it back on the frontier", ending, got)
		}
		node.graph.mu.Lock()
		reruns, continuing := node.reruns, node.continuing
		node.graph.mu.Unlock()
		if reruns != rerunLimit || !continuing {
			t.Fatalf("%s: reruns=%d continuing=%v, want the one rerun recorded", ending, reruns, continuing)
		}
		// AND THE SECOND ONE LANDS. One rerun, because a provider that is down
		// stays down for the second attempt.
		if agent.rerunsFromItsBranch(node, ending, io.Discard) {
			t.Fatalf("%s bought a second rerun", ending)
		}
		node.graph.mu.Lock()
		node.state = TaskRunning
		node.graph.mu.Unlock()
		if node.graph.runAgainFromItsBranch(node) {
			t.Fatalf("%s: a second transition was taken", ending)
		}
	}
}

// AND AN ENDING THAT IS A FINDING BUYS NOTHING. A check that named gaps, a
// person's stop and a run that went in circles are all answers about the work,
// and running the same work again over the top of one is spending money to be
// told the same thing.
func TestAnEndingAboutTheWorkBuysNoRerun(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	for _, ending := range []TaskEnding{TaskEndingRefused, TaskEndingStopped, TaskEndingCircling, TaskEndingSteps, TaskEndingError, ""} {
		node := loneTestNode(t, "port the parser")
		if agent.rerunsFromItsBranch(node, ending, io.Discard) {
			t.Fatalf("%q bought a rerun", ending)
		}
	}
}

// AND THE STALE BRIEF TAKES THE RERUN THROUGH THE RUN'S OWN ROAD, answering in
// the state that says the node has not settled at all.
func TestABriefThatDoesNotMatchItsWorldRerunsOnceBeforeItLandsIncomplete(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "9999aaaa9999aaaa", 7, "port the parser")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.Workspace = repo })
	node := loneTestNode(t, "port the parser")
	node.graph.mu.Lock()
	node.spec.expects = []Expectation{{Path: "nothing/here.go", Fact: "the parser lives here"}}
	node.graph.mu.Unlock()
	if len(node.expectations()) == 0 {
		t.Skip("this build's handoff contract takes its expectations from somewhere else")
	}
	if _, err := os.Stat(filepath.Join(tree.dir, "nothing")); err == nil {
		t.Fatal("the fixture's expectation is met, so nothing goes stale")
	}

	if got := agent.briefMatchesItsWorld(node, tree, io.Discard); got != taskRerunFromBranch {
		t.Fatalf("a stale brief answered %q, want the rerun", got)
	}
	node.graph.run = func(*TaskNode) {}
	if !node.graph.runAgainFromItsBranch(node) {
		t.Fatal("the node would not go back on the frontier")
	}
	if got := agent.briefMatchesItsWorld(node, tree, io.Discard); got != TaskFailed {
		t.Fatalf("the second stale brief answered %q, want the node to land incomplete", got)
	}
}

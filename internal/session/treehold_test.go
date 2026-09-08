package session

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE TREE THAT TWO WRITERS SHARED ────────────────────────────────────────
//
// SWE-Marathon run s10: a node repaired /workspace/rust-java-lsp in place — no
// git in the container, so its working copy was the workspace itself — and six
// minutes in the conversation overwrote src/analysis.rs whole, from a reading of
// the file taken before the node started. See treehold.go for the law.

// (v) THE CHAT IS A READER OF A TREE A NODE IS HOLDING.
func TestTheChatMayNotWriteIntoATreeAnInPlaceNodeIsHolding(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.config.tasker = inPlaceGraph(t, workspace, runningNode{id: 4, title: "repair the parser"})

	_, result, ok := (treeClaimGuard{agent: agent}).PreAction(context.Background(), nil, nil,
		scopedCall("write", filepath.Join(workspace, "src/analysis.rs")))
	if ok {
		t.Fatal("the chat overwrote a file inside a tree a node was working in")
	}
	if !result.isError {
		t.Fatal("the refusal did not come back as an error the model must read")
	}
	// AND IT NAMES THE HOLDER. A refusal that says "somebody has this" is one a
	// worker argues with; a refusal that says who has it is one it can act on.
	if !strings.Contains(result.text, "task 4") || !strings.Contains(result.text, "repair the parser") {
		t.Fatalf("the refusal does not name the holder: %q", result.text)
	}
	if !strings.Contains(result.text, "wait for its report") {
		t.Fatalf("the refusal offers nothing to do instead: %q", result.text)
	}
	if !strings.Contains(result.text, "src/analysis.rs") {
		t.Fatalf("the refusal does not name the file: %q", result.text)
	}
}

// (vi) AND THE SAME WRITE LANDS THE MOMENT THE NODE HAS. The claim is not a
// lock somebody has to remember to give back — it is the node running, so it
// ends when the node does, on every road out including a stop.
func TestTheSameWriteLandsOnceTheNodeHasFinished(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := inPlaceGraph(t, workspace, runningNode{id: 4, title: "repair the parser"})
	agent.config.tasker = graph
	call := scopedCall("write", filepath.Join(workspace, "src/analysis.rs"))

	if _, _, ok := (treeClaimGuard{agent: agent}).PreAction(context.Background(), nil, nil, call); ok {
		t.Fatal("the write was allowed while the node was still running")
	}
	for _, ended := range []TaskState{TaskDone, TaskFailed, TaskQueued, TaskUnverified} {
		graph.nodes[4].state = ended
		if _, _, ok := (treeClaimGuard{agent: agent}).PreAction(context.Background(), nil, nil, call); !ok {
			t.Fatalf("the tree was still held after the node reached %q", ended)
		}
	}
}

// A WORKTREE NODE THAT HAS WRITTEN A FILE OWNS THAT FILE. The directory stays
// the person's (the test above); the path does not. F36 was the chat editing
// cart.py while a running worktree task already had it, then spawning another
// task at the same files — three writers, one logical file, a tree that
// matched none of them.
func TestAChatEditIsBlockedWhileATaskOwnsTheFile(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}, order: []uint64{2}}
	graph.nodes[2] = &TaskNode{
		graph: graph, id: 2, state: TaskRunning, started: time.Now(),
		spec:     taskSpec{title: "discount code entry"},
		worktree: filepath.Join(workspace, ".aforge", "trees", "2"),
		branch:   "aforge/task-2",
		wrote:    []string{"cart.py"},
	}
	agent.config.tasker = graph
	call := scopedCall("edit", filepath.Join(workspace, "cart.py"))

	_, result, ok := (treeClaimGuard{agent: agent}).PreAction(context.Background(), nil, nil, call)
	if ok {
		t.Fatal("the chat edited a file a running task owns")
	}
	if !result.isError {
		t.Fatal("the refusal did not come back as an error the model must read")
	}
	if strings.Contains(result.text, "\n") {
		t.Fatalf("the refusal is not one line: %q", result.text)
	}
	if !strings.Contains(result.text, "task 2") || !strings.Contains(result.text, "discount code entry") {
		t.Fatalf("the refusal does not name the owner: %q", result.text)
	}
	if !strings.Contains(result.text, "cart.py") {
		t.Fatalf("the refusal does not name the file: %q", result.text)
	}

	// ONE FILE, NOT THE TREE. A path the node has not written is still the
	// person's, which is the half [TestAWorktreeIsolatedNodeDoesNotClaimTheWorkspace]
	// already holds.
	if _, _, ok := (treeClaimGuard{agent: agent}).PreAction(context.Background(), nil, nil,
		scopedCall("edit", filepath.Join(workspace, "readme.md"))); !ok {
		t.Fatal("the task locked a file it does not own")
	}

	// AND THE SAME EDIT LANDS THE MOMENT THE NODE HAS. The claim is the node
	// running with that path on its list, so it ends when the node does.
	graph.nodes[2].state = TaskDone
	if _, _, ok := (treeClaimGuard{agent: agent}).PreAction(context.Background(), nil, nil, call); !ok {
		t.Fatal("the file was still held after the task landed")
	}
}

// (vii) A WORKTREE-ISOLATED NODE CLAIMS NOTHING. Its writes land in a copy
// nobody else is in and come home through a merge, which is the machinery the
// in-place road lacks — so the person keeps their own repository.
func TestAWorktreeIsolatedNodeDoesNotClaimTheWorkspace(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}, order: []uint64{4}}
	graph.nodes[4] = &TaskNode{
		graph: graph, id: 4, state: TaskRunning, started: time.Now(),
		spec:     taskSpec{title: "port the language server"},
		worktree: filepath.Join(workspace, ".aforge", "trees", "4"),
		branch:   "aforge/task-4",
	}
	agent.config.tasker = graph

	if _, _, ok := (treeClaimGuard{agent: agent}).PreAction(context.Background(), nil, nil,
		scopedCall("edit", filepath.Join(workspace, "src/analysis.rs"))); !ok {
		t.Fatal("an isolated node locked the person out of their own workspace")
	}
}

// (viii) TWO IN-PLACE NODES CANNOT BOTH HOLD ONE TREE, and the SECOND is the one
// refused. It has to be a total order every agent computes the same way, or the
// two would refuse each other and the tree would belong to nobody.
func TestTwoInPlaceNodesCannotBothHoldOneTree(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := inPlaceGraph(t, workspace,
		runningNode{id: 4, title: "repair the parser", ago: 20 * time.Minute},
		runningNode{id: 5, title: "add the tests", ago: 2 * time.Minute})
	agent.config.tasker = graph
	call := scopedCall("write", filepath.Join(workspace, "src/analysis.rs"))

	// The later node is refused, and it is told who was there first.
	agent.config.taskID = 5
	_, result, ok := (treeClaimGuard{agent: agent}).PreAction(context.Background(), nil, nil, call)
	if ok {
		t.Fatal("the second in-place node wrote into a tree the first was already holding")
	}
	if !strings.Contains(result.text, "task 4") {
		t.Fatalf("the second node was not told who holds the tree: %q", result.text)
	}
	// And the first writes as it always did: exactly one of them owns the tree.
	agent.config.taskID = 4
	if _, _, ok := (treeClaimGuard{agent: agent}).PreAction(context.Background(), nil, nil, call); !ok {
		t.Fatal("the node that took the tree first was refused its own writes")
	}
}

// A NODE'S OWN FAMILY IS NOT SOMEBODY ELSE — taskground.go's law, asked at write
// time. A part cut out of the node's work, and a fork hand of its worker, are
// that node writing.
func TestTheHoldersOwnFamilyWritesFreelyInItsTree(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := inPlaceGraph(t, workspace, runningNode{id: 4, title: "repair the parser"})
	graph.order = append(graph.order, 6)
	graph.nodes[6] = &TaskNode{graph: graph, id: 6, parent: 4, spec: taskSpec{title: "a piece of it"}}
	agent.config.tasker = graph
	call := scopedCall("edit", filepath.Join(workspace, "src/analysis.rs"))

	for _, writer := range []uint64{4, 6} {
		agent.config.taskID = writer
		if _, _, ok := (treeClaimGuard{agent: agent}).PreAction(context.Background(), nil, nil, call); !ok {
			t.Fatalf("task %d was refused a write in its own family's tree", writer)
		}
	}
	// And a node that is no relation is refused, which is what makes the
	// exemption above an exemption rather than an open door.
	agent.config.taskID = 9
	if _, _, ok := (treeClaimGuard{agent: agent}).PreAction(context.Background(), nil, nil, call); ok {
		t.Fatal("an unrelated node wrote into the holder's tree")
	}
}

// A FORK HAND IS ITS CALLER'S NODE. It shares the caller's working directory, so
// the claim question has to get the same answer for a hand as for the worker
// that forked it — and without the two rows fork.go carries down, a hand of a
// node's worker is indistinguishable from the conversation.
func TestAHandInheritsTheNodeItIsWorkingFor(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := inPlaceGraph(t, workspace, runningNode{id: 4, title: "repair the parser"})
	agent.config.tasker = graph
	agent.config.taskID = 4

	hand, err := agent.newHandAgent(forkPart{Role: "one", Scope: []string{"src"}},
		[]ai.Message{{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "go"}}}},
		"do the thing", &handLeash{limit: forkRounds})
	if err != nil {
		t.Fatalf("newHandAgent: %v", err)
	}
	defer hand.Close()
	if hand.config.taskID != 4 || hand.config.tasker != graph {
		t.Fatalf("the hand carries taskID=%d tasker=%v, want its caller's node",
			hand.config.taskID, hand.config.tasker != nil)
	}
	if _, _, ok := (treeClaimGuard{agent: hand}).PreAction(context.Background(), nil, nil,
		scopedCall("edit", filepath.Join(workspace, "src/analysis.rs"))); !ok {
		t.Fatal("a hand of the node's own worker was refused a write in its node's tree")
	}
}

// A SIBLING WORKING IN ITS OWN COPY IS NOT IN THE HOLDER'S, even when that copy
// happens to live under the held tree — which is where worktrees go when the
// session has no folder of its own to put them in.
func TestASiblingWritingInItsOwnCopyIsNotInTheHeldTree(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	graph := inPlaceGraph(t, workspace, runningNode{id: 4, title: "repair the parser"})
	agent.config.tasker = graph
	agent.config.taskID = 7
	agent.config.Workspace = filepath.Join(workspace, ".aforge", "trees", "7")

	if _, _, ok := (treeClaimGuard{agent: agent}).PreAction(context.Background(), nil, nil,
		scopedCall("write", filepath.Join(agent.config.Workspace, "src/analysis.rs"))); !ok {
		t.Fatal("a node was refused a write inside its own worktree")
	}
}

// AND A CALL WITH NO PATH TO READ IS NOT THIS CITIZEN'S BUSINESS, the same hole
// [writeGuard] names: a shell command's effects are whatever it did.
func TestTheClaimBindsOnlyTheHandsThatKnowTheirPath(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	agent.config.tasker = inPlaceGraph(t, workspace, runningNode{id: 4, title: "repair the parser"})
	for _, call := range []ai.ToolCall{
		scopedCall("bash", ""),
		scopedCall("read", filepath.Join(workspace, "src/analysis.rs")),
	} {
		if _, _, ok := (treeClaimGuard{agent: agent}).PreAction(context.Background(), nil, nil, call); !ok {
			t.Fatalf("%s was refused by the tree claim", call.Function.Name)
		}
	}
	// And a session that never groomed a task is the session it was before this
	// citizen existed.
	plain, plainSpace := newTestAgent(t, &scriptedCompleter{}, nil)
	if _, _, ok := (treeClaimGuard{agent: plain}).PreAction(context.Background(), nil, nil,
		scopedCall("write", filepath.Join(plainSpace, "anywhere.txt"))); !ok {
		t.Fatal("an agent with no task graph was refused a write")
	}
}

// ── plumbing ────────────────────────────────────────────────────────────────

type runningNode struct {
	id    uint64
	title string
	ago   time.Duration
}

// inPlaceGraph is a graph of nodes that are RUNNING IN the given directory —
// `merge: inplace`, no branch — which is what [prepareTaskTree] hands back for a
// workspace it cannot branch.
func inPlaceGraph(t *testing.T, dir string, nodes ...runningNode) *TaskGraph {
	t.Helper()
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	for _, spec := range nodes {
		ago := spec.ago
		if ago == 0 {
			ago = time.Minute
		}
		graph.order = append(graph.order, spec.id)
		graph.nodes[spec.id] = &TaskNode{
			graph: graph, id: spec.id, state: TaskRunning,
			started:  time.Now().Add(-ago),
			spec:     taskSpec{title: spec.title},
			worktree: dir,
			merge:    mergeInPlace,
		}
	}
	return graph
}

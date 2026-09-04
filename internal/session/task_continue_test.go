package session

// CONTINUATION IS THE SAME NODE.
//
// F23/F25: "continue task 4" re-derived a fresh brief and minted a new
// worktree through propose_task. These tests drive the real executor over a
// real repository, for task_repair_test.go's reason: "it resumed the same
// working copy and did not propose a new node" is not a claim a stub can
// make about a tree.

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// CONTINUING A FAILED TASK RESUMES ITS CONTEXT AND WORKTREE.
//
// The first run writes the wrong file and the check names the gap. Continue
// re-arms THAT node — same id, same brief, same branch, same directory —
// hands the checker's evidence back as this round's finding, and the
// conversation does not call propose_task again.
func TestContinuingAFailedTaskResumesItsWorktreeAndDoesNotProposeANewNode(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	const brief = "write greet.go with a greeting"
	completer := &routedCompleter{
		parent: []step{
			proposeCall("Add the greeting", brief),
			finalText("handed off"),
		},
		child: continueLane(12, func(continuing, wrote bool) *ai.Response {
			if !wrote {
				if continuing {
					return writeResponse("call-src", "greet.go",
						"package greet\n\nfunc Greet() string { return \"hi\" }\n")
				}
				return writeResponse("call-notes", "notes.md", "# what I was thinking about\n")
			}
			return pricedResponse("I wrote what I had. Done, I think.", 0.01)
		}),
		audit: []step{
			bashCall("call-look", "git status --porcelain"),
			verdictFromEvidence("greet.go",
				"VERIFIED — git status --porcelain shows greet.go staged",
				"REFUTED — git status --porcelain shows notes.md and no greet.go"),
			bashCall("call-look2", "git status --porcelain"),
			verdictFromEvidence("greet.go",
				"VERIFIED — git status --porcelain shows greet.go staged",
				"REFUTED — git status --porcelain shows notes.md and no greet.go"),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskRepairRounds = 0
	})
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "add a greeting"))

	node := graph.node(1)
	if node == nil {
		t.Fatal("no node was admitted")
	}
	waitDoneNode(t, node)
	if state := node.stateNow(); state != TaskFailed {
		t.Fatalf("state = %q, want failed so there is something to continue (report %q)",
			state, node.notice().Report)
	}

	graph.mu.Lock()
	firstTree, firstBranch, firstBrief, firstSeq := node.worktree, node.branch, node.spec.brief, graph.seq
	graph.mu.Unlock()
	if firstTree == "" || firstBranch == "" {
		t.Fatalf("the failed node left no working copy to resume (tree %q branch %q)", firstTree, firstBranch)
	}
	if firstSeq != 1 {
		t.Fatalf("seq = %d, want 1 — only one node was proposed", firstSeq)
	}

	text, isError := runTool(t, agent, "tasks", `{"id":1,"continue":true}`)
	if isError {
		t.Fatalf("continue failed:\n%s", text)
	}
	if !strings.Contains(text, "continuing task 1") {
		t.Fatalf("the tool did not say it continued task 1:\n%s", text)
	}
	if strings.Contains(text, "task 2") || strings.Contains(text, "propose_task") {
		t.Fatalf("continue minted or named a new proposal:\n%s", text)
	}

	waitDoneNode(t, node)

	graph.mu.Lock()
	secondTree, secondBranch, secondBrief, secondSeq := node.worktree, node.branch, node.spec.brief, graph.seq
	nodes := len(graph.nodes)
	graph.mu.Unlock()

	if nodes != 1 {
		t.Fatalf("graph has %d nodes, want 1 — continue must not admit a new one", nodes)
	}
	if secondSeq != firstSeq {
		t.Fatalf("seq moved %d → %d: continue reserved a new id via propose_task", firstSeq, secondSeq)
	}
	if secondBrief != firstBrief {
		t.Fatalf("brief was rewritten:\n got %q\nwant %q", secondBrief, firstBrief)
	}
	if secondBranch != firstBranch {
		t.Fatalf("branch moved %q → %q: continue cut a fresh one", firstBranch, secondBranch)
	}
	if secondTree != firstTree {
		t.Fatalf("working copy moved %q → %q: continue opened a new directory", firstTree, secondTree)
	}
	// A successful second landing releases the copy the way every finished
	// node does. The path and branch matching above are the proof it was
	// the same tree, not that the directory outlives the merge.
	if state := node.stateNow(); state != TaskDone {
		t.Fatalf("continued node landed %q, want done — the second run should have written greet.go (report %q)",
			state, node.notice().Report)
	}
	if !completer.childSaw(brief) {
		t.Fatal("the continued worker was not handed the original brief")
	}
	if !completer.childSaw(continueFindingLead) {
		t.Fatal("the continued worker was not handed the last attempt's finding")
	}
	if !completer.childSaw("no greet.go") {
		t.Fatal("the continued worker was not handed the checker's evidence")
	}
}

// continueLane is [nodeLane] for a worker that may be on a second attempt:
// the first write is the failed one, and a request carrying
// [continueFindingLead] is the continuation.
func continueLane(turns int, run func(continuing, wrote bool) *ai.Response) []step {
	steps := make([]step, turns)
	for i := range steps {
		steps[i] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if len(messages) > 0 && strings.Contains(messageText(messages[0]), titleSystem) {
				return textResponse("the greeting task"), nil
			}
			continuing, wrote := false, false
			for _, message := range messages {
				if strings.Contains(messageText(message), continueFindingLead) {
					continuing = true
				}
				if message.Role == "tool" {
					wrote = true
				}
			}
			return run(continuing, wrote), nil
		}
	}
	return steps
}

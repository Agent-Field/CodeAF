package session

import (
	"strings"
	"testing"
)

// A NESTED PART ACCEPTED THROUGH THE ONE DOOR leaves the receipt the tmux
// suite waits for on its report. The e2e seed is a finished parent with a
// child that needs a look, merge in place — the same shape
// seedDecidedFamily writes — and `[a]` must reach TaskDone with
// `you took this as done` leading the report.
//
// THE REFUTE PATH WAS THE ONLY ONE THE FAMILY FIXTURE EXERCISED (task_settle_test.go
// says why: refute needs no working copy). Accept is the letter the nested
// landing e2e presses, so this door is the one that has to hold for that screen.
func TestANestedInPlaceLandingAcceptsThroughTheQuestionDoor(t *testing.T) {
	agent := quietAgent(t)
	graph := stubbedGraph(agent, func(node *TaskNode) {})
	parentID := graph.reserve()
	graph.admit(parentID, taskSpec{title: "Rebuild the index", brief: "b", acceptance: "a"})
	childID := graph.reserve()
	graph.admit(childID, taskSpec{title: "Port the parser", brief: "b", acceptance: "a", parent: parentID})
	parent, child := graph.node(parentID), graph.node(childID)
	parent.finish("the index is rebuilt", nil, "", mergeInPlace)
	graph.complete(parent, TaskDone)
	child.finish(yourCallLead(TaskFacts{Merge: mergeInPlace})+"nobody could check it in 5m0s",
		[]string{"parser.go"}, "", mergeInPlace)
	// THE FIXTURE'S GROUND IS THE WORKSPACE ITSELF, folder mode — what the
	// e2e seed writes as groundMode: folder / merge: inplace.
	child.graph.mu.Lock()
	child.Ground = agent.config.Workspace
	child.Mode = TaskModeFolder
	child.graph.mu.Unlock()
	graph.complete(child, TaskUnverified)

	if err := agent.ResolveQuestion(Answer{
		Kind: QuestionLanding, ID: child.id, Key: LandingYesKey, Picked: []string{LandingYesKey},
	}); err != nil {
		t.Fatalf("ResolveQuestion accept: %v", err)
	}
	if state := child.stateNow(); state != TaskDone {
		t.Fatalf("accepted nested part is %q: %s", state, child.notice().Report)
	}
	report := child.notice().Report
	if !strings.HasPrefix(report, acceptedByYou) {
		t.Fatalf("the report leads %q, want %q:\n%s", firstLine(report), acceptedByYou, report)
	}
}

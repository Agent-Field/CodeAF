package session

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
)

func grantCall(id, command string) ai.ToolCall {
	args, _ := json.Marshal(map[string]string{"command": command})
	call := ai.ToolCall{ID: id}
	call.Function.Name = "bash"
	call.Function.Arguments = string(args)
	return call
}

func runsUnasked(agent *Agent, command string) bool {
	_, ok := agent.approve(context.Background(), nil, grantCall("c", command))
	return ok
}

// One "for this task" yes covers the rest of the turn's looks of the same
// family, never a write, a lookalike or another family, and dies with the turn.
func TestAllowForThisTaskCoversLooksUntilTheTurnEnds(t *testing.T) {
	agent, _ := consentAgent(t, &scriptedCompleter{}, promptAll(), false)
	if runsUnasked(agent, "gh issue list") {
		t.Fatal("a gh look ran before anybody said yes")
	}
	agent.rememberLookGrant(grantCall("c0", "gh issue view 1"))
	cases := []struct {
		command string
		want    bool
	}{
		{"gh issue list", true},
		{"gh pr view 7", true},
		{"gh api repos/o/r/issues", true},
		{"gh api -X POST repos/o/r/issues", false},
		{"gh api repos/o/r/issues -f title=x", false},
		{`gh api repos/o/r "-X" DELETE`, false},
		{"gh api repos/o/r -iX POST", false},
		{"GH_HOST=evil.example gh issue list", false},
		{"gh pr merge 7", false},
		{"gh auth token", false},
		{"gh issue list | sh", false},
		{"gh issue list && gh pr merge 7", false},
		{"aws ec2 describe-instances", false},
		{"gcloud compute instances list", false},
	}
	for _, c := range cases {
		if got := runsUnasked(agent, c.command); got != c.want {
			t.Errorf("%q ran unasked = %v, want %v", c.command, got, c.want)
		}
	}
	agent.mu.Lock()
	agent.turnSeq++
	agent.mu.Unlock()
	if runsUnasked(agent, "gh issue list") {
		t.Fatal("the grant outlived its turn")
	}
}

// A write is never banked for the task, even when the answer claims the scope.
func TestAllowForThisTaskIsNotBankedForAWrite(t *testing.T) {
	agent, _ := consentAgent(t, &scriptedCompleter{}, promptAll(), false)
	for _, write := range []string{"gh pr merge 7", "gh api -X DELETE repos/o/r", "aws s3 ls", "rm -rf /"} {
		agent.rememberLookGrant(grantCall("c0", write))
	}
	for _, next := range []string{"gh pr merge 8", "gh issue list", "aws s3 ls"} {
		if runsUnasked(agent, next) {
			t.Fatalf("%q ran on a grant no look ever made", next)
		}
	}
}

// taskWorker is a task's agent with its node's own grant set, as
// [Agent.childConfig] builds one.
func taskWorker(t *testing.T, node *TaskNode) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionPrompt}
		config.InTask = true
		config.lookGrants = &node.looks
	})
	return agent
}

// A task's grant lives on its own node: it ends with the task, and a later
// task of the same name starts with none and asks again.
func TestATaskGrantEndsWithItsTaskAndIsNotInheritedByName(t *testing.T) {
	first := &TaskNode{id: 7}
	worker := taskWorker(t, first)
	worker.rememberLookGrant(grantCall("c0", "gh issue view 1"))
	if !runsUnasked(worker, "gh issue list") {
		t.Fatal("the task's own grant did not cover its next look")
	}
	if runsUnasked(worker, "gh pr merge 7") {
		t.Fatal("a write ran on the task's grant")
	}

	second := &TaskNode{id: 7}
	if runsUnasked(taskWorker(t, second), "gh issue list") {
		t.Fatal("a second task of the same name inherited the grant")
	}

	first.end(TaskEndingStopped)
	if runsUnasked(worker, "gh issue list") {
		t.Fatal("the grant outlived its task")
	}
	if runsUnasked(taskWorker(t, first), "gh issue list") {
		t.Fatal("a continued task kept the ended task's grant")
	}
}

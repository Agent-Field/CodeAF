package session

import (
	"slices"
	"strings"
	"testing"
)

// dependsConversation is a program-carrying conversation whose graph holds a
// node 3 in the state given and a program's run row 5 in the state given.
func dependsConversation(t *testing.T, node, run TaskState) *Agent {
	t.Helper()
	agent := programConversation(t, nil)
	g := agent.graph()
	g.mu.Lock()
	g.nodes[3] = &TaskNode{id: 3, state: node}
	g.mu.Unlock()
	agent.publishRunRow(g, TaskNotice{ID: 5, Title: "rewrite the auth", State: run, Program: "senior-dev", StartedAt: agent.taskClockNow()})
	return agent
}

// A PROGRAM'S PROPOSAL NAMING WORK NOT YET DONE IS REFUSED, because a program
// starts the moment it is approved and has nothing to wait in: it used to start
// at once with its depends_on dropped.
func TestAProgramsProposalNamingUnfinishedWorkIsRefused(t *testing.T) {
	for _, state := range []TaskState{TaskQueued, TaskRunning, TaskUnverified} {
		agent := dependsConversation(t, state, TaskDone)
		spec := taskSpec{via: "senior-dev", dependsOn: []uint64{3}}
		if got, want := agent.proposalDependencyRefusal(spec), programCannotWaitRefusal("senior-dev", []uint64{3}); got != want {
			t.Fatalf("a program's proposal on a %s node answered %q, want %q", state, got, want)
		}
		if refusal := agent.refuseProposedTask(spec); refusal == nil {
			t.Fatalf("the door let a program's proposal on a %s node through", state)
		}
	}
	agent := dependsConversation(t, TaskDone, TaskDone)
	if got := agent.proposalDependencyRefusal(taskSpec{via: "senior-dev", dependsOn: []uint64{3, 5}}); got != "" {
		t.Fatalf("a program's proposal on landed work was refused: %q", got)
	}
	// AN ORDINARY PROPOSAL STILL WAITS in the graph, as it always has.
	agent = dependsConversation(t, TaskRunning, TaskDone)
	if got := agent.proposalDependencyRefusal(taskSpec{dependsOn: []uint64{3}}); got != "" {
		t.Fatalf("an ordinary proposal on a running node was refused: %q", got)
	}
}

// AN ORDINARY PROPOSAL MAY NAME A PROGRAM'S RUN. It was refused with "no task
// in this session has that id" over a run the rail was drawing. A run that
// ended done is a dependency met and is taken out before the graph sees it; one
// still going is refused, because nothing would wake the node when it ends; one
// that ended any other way is refused as failed.
func TestAnOrdinaryProposalMayNameAProgramsRun(t *testing.T) {
	agent := dependsConversation(t, TaskDone, TaskDone)
	spec := taskSpec{dependsOn: []uint64{3, 5}}
	if got := agent.proposalDependencyRefusal(spec); got != "" {
		t.Fatalf("a proposal naming a program's finished run was refused: %q", got)
	}
	if kept := agent.withoutEndedProgramRuns(spec.dependsOn); !slices.Equal(kept, []uint64{3}) {
		t.Fatalf("the dependencies handed on are %v, want the program's finished run taken out", kept)
	}

	agent = dependsConversation(t, TaskDone, TaskRunning)
	if got, want := agent.proposalDependencyRefusal(spec), programRunWaitRefusal([]uint64{5}); got != want {
		t.Fatalf("a proposal naming a running program's run answered %q, want %q", got, want)
	}
	if kept := agent.withoutEndedProgramRuns(spec.dependsOn); !slices.Equal(kept, []uint64{3, 5}) {
		t.Fatalf("a running program's run was taken out of the dependencies: %v", kept)
	}

	agent = dependsConversation(t, TaskDone, TaskFailed)
	if got := agent.proposalDependencyRefusal(spec); !strings.Contains(got, "depends_on names task 5, which already failed") {
		t.Fatalf("a proposal naming a failed program's run answered %q", got)
	}

	if got := agent.proposalDependencyRefusal(taskSpec{dependsOn: []uint64{9}}); !strings.Contains(got, "no task in this session has that id") {
		t.Fatalf("an id nothing holds answered %q", got)
	}
}

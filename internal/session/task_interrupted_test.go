package session

import (
	"context"
	"io"
	"strings"
	"testing"
)

// A RUN MACHINERY CUT WHERE IT STOOD AND A RUN A PERSON STOPPED ARE TWO
// DIFFERENT PIECES OF NEWS, AND THE RECORD HAS TO SAY WHICH IT IS.
//
// [Agent.settleUnfinished] reads two roads out of a context that died. A person's
// stop takes the first: the flag is set before the cut, the node settles failed,
// and it reads `stopped`. Machinery — the session closing under the node, the
// engine going away — takes the second, which on purpose leaves the state alone
// so recovery resumes the node. The whole defect was that the second wrote
// NOTHING about why: no ending, no cancelling party, no reason. A person reading
// the record could not tell an interruption they had caused from machinery that
// cut the work, because one road said `stopped` and the other said nothing at
// all.
//
// This pins the distinction. A node machinery cut keeps its running, resumable
// state and now NAMES the cut on the record; a node a person stopped reads
// `stopped` and settles failed, exactly as it always did. Neither may be mistaken
// for the other.
func TestAMachineryCutNodeAndAPersonsStopDoNotReadTheSame(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)

	// Machinery: the context dies with nothing marking the node stopped. The node
	// is RUNNING — as it is at the moment its run is cut — and the returned state
	// is the empty one the caller reads as "keep it running for recovery".
	cut := &TaskNode{graph: &TaskGraph{}, id: 1, state: TaskRunning}
	cutCtx, cutCancel := context.WithCancel(context.Background())
	cutCancel()
	cutState, ended := agent.settleUnfinished(cutCtx, cut, taskTree{}, nil, "", "", nil, io.Discard)
	if !ended {
		t.Fatal("a run machinery cut fell through the unfinished roads")
	}
	// The empty state is the caller's "keep it running": the node's state is left
	// exactly as it stands (task_run.go's runTaskNode).
	if cutState != "" {
		cut.state = cutState
	}

	// A person: the flag is set first, then the context is cut. The returned
	// state is the failed one the caller settles the node with.
	stopped := &TaskNode{graph: &TaskGraph{}, id: 2, state: TaskRunning}
	stopped.markStopped()
	stopCtx, stopCancel := context.WithCancel(context.Background())
	stopCancel()
	stopState, ended := agent.settleUnfinished(stopCtx, stopped, taskTree{}, nil, "", "", nil, io.Discard)
	if !ended {
		t.Fatal("a run a person stopped fell through the unfinished roads")
	}
	stopped.state = stopState

	machine := cut.notice()
	person := stopped.notice()

	// THE MACHINE CUT STAYS RESUMABLE AND IS NOT TURNED INTO A FINDING ABOUT THE
	// WORK — but it is no longer silent: it carries the reason the context ended.
	if machine.State != TaskRunning {
		t.Fatalf("a run machinery cut reads %q; it must stay resumable, not settle", machine.State)
	}
	if machine.Ending == "" {
		t.Fatal("a run machinery cut the work on says nothing about what ended it")
	}
	if machine.Stopped {
		t.Fatal("a cut the machinery made reads as a person's stop")
	}
	if !strings.Contains(machine.Report, "not by a person") {
		t.Fatalf("a cut the machinery made does not say so on the record: %q", machine.Report)
	}

	// THE PERSON'S STOP IS THE PERSON'S, unchanged: `stopped`, failed, flagged.
	if person.State != TaskFailed || person.Ending != TaskEndingStopped || !person.Stopped {
		t.Fatalf("a person's stop reads state=%q ending=%q stopped=%v", person.State, person.Ending, person.Stopped)
	}

	// AND THE TWO DO NOT READ THE SAME — the whole point of the change.
	if machine.Ending == person.Ending {
		t.Fatalf("machinery and a person both read ending %q", machine.Ending)
	}
	if strings.Contains(machine.Report, taskStoppedWord) {
		t.Fatalf("a cut the machinery made borrows the person's word: %q", machine.Report)
	}
}

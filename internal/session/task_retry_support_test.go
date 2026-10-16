package session

import (
	"github.com/Agent-Field/codeaf/internal/plandb"
	"testing"
)

func TestRetrySupportComesFromTheActualProducerRoad(t *testing.T) {
	agent, _, started, _ := failedConversation(t)
	rows, err := agent.AppTasks()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == "1" && !row.Retryable {
			t.Fatal("graph node lost retry support")
		}
		if row.ID == "7" && row.Retryable {
			t.Fatal("handed-over row falsely offers retry")
		}
	}
	if !agent.graph().node(1).notice().Retryable {
		t.Fatal("actual graph notice lost support")
	}
	if err := agent.AppTaskAction(AppTaskIntent{ID: "7", Action: "retry"}); err == nil {
		t.Fatal("run retry must be refused")
	}
	if started.Load() != 0 {
		t.Fatal("unsupported retry dispatched work")
	}
	graph := nodeFact("a", taskRecord{ID: 1, Kind: TaskKindHarness, State: TaskFailed})
	run := runFact("a", runRecord{ID: 1, Kind: TaskKindHarness, State: TaskFailed}, nil)
	if !graph.Node.Retryable || run.Node.Retryable {
		t.Fatal("same kind does not imply same action support across roads")
	}
	quick := nodeFact("a", taskRecord{ID: 2, Kind: TaskKindQuick, State: TaskFailed})
	part := &plandb.Task{}
	part.ID = "part"
	part.Title = "same title"
	plan := planFact("a", part, nil, nil, nil)
	if quick.Node.Retryable || plan.Node.Retryable {
		t.Fatal("quick and plan rows do not support this retry road")
	}
}

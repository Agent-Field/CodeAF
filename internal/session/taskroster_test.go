package session

import (
	"path/filepath"
	"testing"
	"time"
)

// takeTaskUpdates reads task events off a lane until it has count of them or
// the well runs dry, failing rather than hanging.
func takeTaskUpdates(t *testing.T, lane <-chan Event, count int) []TaskNotice {
	t.Helper()
	var notices []TaskNotice
	deadline := time.After(5 * time.Second)
	for len(notices) < count {
		select {
		case event, open := <-lane:
			if !open {
				t.Fatalf("the lane closed after %d of %d updates", len(notices), count)
			}
			if event.Kind != EventTaskUpdate || event.Task == nil {
				continue
			}
			notices = append(notices, *event.Task)
		case <-deadline:
			t.Fatalf("got %d of %d task updates before the deadline", len(notices), count)
		}
	}
	return notices
}

// A LANE OPENED ON A CONVERSATION WHOSE GRAPH ALREADY HOLDS NODES BEGINS WITH
// THEM. The graph outlives every lane that reported it — a resumed session
// rehydrates its nodes before any surface exists, and a switch back behind
// home re-subscribes long after the events went out — so the roster is
// replayed onto each new lane, finished work included. Without this, a column
// rebuilt on attach starts empty and stays empty until something new happens,
// which is the bug this test pins.
func TestANewTaskLaneOpensOnTheRoster(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)

	graph := agent.graph()
	graph.rehydrate(taskDocument{
		Type: taskDocumentType, Version: taskFileVersion, Seq: 2,
		Nodes: []taskRecord{
			{ID: 1, Title: "Ship the fix", Brief: "b", Acceptance: "a", State: TaskDone},
			{ID: 2, Title: "Chase the flake", Brief: "b", Acceptance: "a", State: TaskFailed},
		},
	}, workspace, TaskSettleAsk)

	lane, stop := agent.WatchTaskUpdates()
	defer stop()
	notices := takeTaskUpdates(t, lane, 2)
	if notices[0].ID != 1 || notices[1].ID != 2 {
		t.Fatalf("roster order = %d, %d — want admission order 1, 2", notices[0].ID, notices[1].ID)
	}
	if notices[0].State != TaskDone || notices[0].Title != "Ship the fix" {
		t.Fatalf("done node came back as %q %q", notices[0].State, notices[0].Title)
	}
	if notices[1].State != TaskFailed {
		t.Fatalf("failed node came back as %q", notices[1].State)
	}

	// EVERY new lane, not only the first: the rows are facts about the graph
	// rather than news, and a surface that detaches and comes back opens a
	// fresh lane with nothing drawn. Standing news is consumed once; the
	// roster must not be.
	again, stopAgain := agent.WatchTaskUpdates()
	defer stopAgain()
	if second := takeTaskUpdates(t, again, 2); second[0].ID != 1 || second[1].ID != 2 {
		t.Fatalf("second lane's roster = %d, %d — want 1, 2", second[0].ID, second[1].ID)
	}
}

// A graph with nothing in it replays nothing: the lane opens silent, exactly
// as it always has, and the emptiness law holds one layer down — a surface is
// not sent zero rows to draw nothing with.
func TestANewTaskLaneOnAnEmptyGraphStaysSilent(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	lane, stop := agent.WatchTaskUpdates()
	defer stop()
	select {
	case event := <-lane:
		t.Fatalf("an empty graph sent %v", event.Kind)
	case <-time.After(200 * time.Millisecond):
	}
}

// A real deletion must agree with the roster on a live engine and a new engine
// restored from the same checkpoint. The untouched last row is a stream barrier.
func TestDeletedTaskNeverReturnsThroughLiveOrRestoredRoster(t *testing.T) {
	dir, _ := deletionFixture(t)
	file := filepath.Join(dir, "transcript.jsonl")
	document := taskDocument{Type: taskDocumentType, Version: taskFileVersion, Seq: 3, Nodes: []taskRecord{
		{ID: 1, Title: "Delete parent", State: TaskDone, PlanID: "one"},
		{ID: 2, Title: "Delete child", State: TaskDone, Parent: 1},
		{ID: 3, Title: "Keep sibling", State: TaskDone},
	}}
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = file })
	agent.graph().rehydrate(document, workspace, TaskSettleAsk)
	live := agent.liveTaskRows()
	for i := range live {
		live[i].SessionID = "one"
	}
	if _, err := DeleteTaskTree(dir, "one", "1", live); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if attempt == 1 {
			agent.Close()
			agent, workspace = newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = file })
			agent.graph().rehydrate(document, workspace, TaskSettleAsk)
		}
		lane, stop := agent.WatchTaskUpdates()
		if got := takeTaskUpdates(t, lane, 1)[0].ID; got != 3 {
			stop()
			t.Fatalf("replayed deleted task %d", got)
		}
		agent.emitTaskUpdate(TaskNotice{ID: 1, State: TaskDone})
		agent.emitTaskUpdate(TaskNotice{ID: 2, Parent: 1, State: TaskDone})
		agent.emitTaskUpdate(TaskNotice{ID: 99, PlanID: "t-one", State: TaskDone})
		agent.emitTaskUpdate(TaskNotice{ID: 3, State: TaskDone})
		if got := takeTaskUpdates(t, lane, 1)[0].ID; got != 3 {
			stop()
			t.Fatalf("emitted deleted task %d", got)
		}
		stop()
	}
}

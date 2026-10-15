package session

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// AN APPROVED HAND-OFF THE RUN STORE REFUSED IS SETTLED FAILED, NEVER LEFT
// QUEUED.
//
// The case the owner's screen caught: a run is underway as task 1, and a
// proposal for task 2 that depends on task 1 is approved. Joining the run makes
// task 2 a child of task 1, the store refuses a hard dependency on its own
// ancestor, and nothing starts. The card's last word was the approval, which
// every surface reads as queued work, and nothing after it named task 2 — so
// the app's task list kept a queued task forever that the graph had never
// heard of. Now the refusal publishes the row as failed with the store's own
// reason, live to every watcher after the approval, in the engine's app read,
// and on the checkpoint a reopened conversation reads back.
func TestAnApprovedHandoffTheStoreRefusedSettlesFailed(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("the run did the work")
	registerBeltRunEngine(t, double)
	dir := t.TempDir()
	transcript := filepath.Join(dir, placeTranscript)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "the run did the work"}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
		config.SessionFile = transcript
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	defer endBeltRun(t, agent, double)
	root, _, _, err := agent.StartTask(context.Background(), "open the run", false)
	if err != nil {
		t.Fatal(err)
	}
	<-double.entered
	lane, stop := agent.WatchTaskUpdates()
	defer stop()

	staged := agent.stageTask(context.Background(), beltProposalArgs("Consume the run", "the output uses the run", root))
	proposal, ok := staged.(*stagedProposal)
	if !ok {
		answer, _, _ := staged.Commit(context.Background())
		t.Fatalf("the proposal was not staged: %q", answer)
	}
	agent.ResolveTask(proposal.id, TaskAnswer{Approved: true})
	answer, failed, err := proposal.Commit(context.Background())
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if !failed || !strings.Contains(answer, "did not start") || !strings.Contains(answer, "containment lineage") {
		t.Fatalf("the refused hand-off's receipt = failed %v %q, want it to say it did not start and why", failed, answer)
	}

	// LIVE: the last word any watcher hears about this id is the failure, after
	// the approval, with the reason.
	var last *TaskNotice
	approved := false
	deadline := time.After(5 * time.Second)
	for last == nil || last.State != TaskFailed {
		select {
		case event := <-lane:
			if event.Task == nil || event.Task.ID != proposal.id {
				continue
			}
			if event.Kind == EventTaskProposal && event.Task.Decided != nil && event.Task.Decided.Approved {
				approved = true
			}
			if event.Kind == EventTaskUpdate {
				notice := *event.Task
				last = &notice
			}
		case <-deadline:
			t.Fatalf("no failed row for task %d reached a watcher; last update %+v", proposal.id, last)
		}
	}
	if !approved {
		t.Error("the watcher never heard the approval the failure follows")
	}
	if !strings.Contains(last.Report, "did not start") || !strings.Contains(last.Report, "containment lineage") {
		t.Errorf("the failed row's report = %q, want the store's reason", last.Report)
	}
	if !last.StartedAt.IsZero() {
		t.Errorf("a task that never started carries a start time %v", last.StartedAt)
	}

	// THE ENGINE'S OWN READ agrees: the id exists, failed, with the reason.
	view, found, err := agent.AppTask(strconv.FormatUint(proposal.id, 10))
	if err != nil || !found {
		t.Fatalf("AppTask(%d) found=%v err=%v, want the failed row", proposal.id, found, err)
	}
	if view.State != string(TaskFailed) || !strings.Contains(view.Result, "containment lineage") {
		t.Errorf("AppTask(%d) = state %q result %q, want failed with the reason", proposal.id, view.State, view.Result)
	}

	// AND A REOPEN READS IT BACK FAILED, not queued and not gone: the
	// checkpoint and the index are what a surface lists a closed conversation's
	// tasks from.
	var kept *TaskNotice
	for _, notice := range ReadSessionTasks(transcript) {
		if notice.ID == proposal.id {
			notice := notice
			kept = &notice
		}
	}
	if kept == nil {
		t.Fatalf("the checkpoint keeps no row for task %d", proposal.id)
	}
	if kept.State != TaskFailed || !strings.Contains(kept.Report, "containment lineage") {
		t.Errorf("the checkpoint's row for task %d = state %q report %q, want failed with the reason", proposal.id, kept.State, kept.Report)
	}
	indexed := false
	for _, row := range ReadTaskIndex(TaskIndexPath(transcript)) {
		if row.ID == strconv.FormatUint(proposal.id, 10) {
			indexed = true
			if row.Status != string(TaskFailed) {
				t.Errorf("the index's row for task %d is %q, want failed", proposal.id, row.Status)
			}
		}
	}
	if !indexed {
		t.Errorf("the index keeps no row for task %d", proposal.id)
	}
	if agent.graph().node(proposal.id) != nil {
		t.Error("the refused hand-off became a node of the older tree")
	}
}

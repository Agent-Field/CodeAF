package session

// planrow_identity_test.go pins the IDENTITY THE RUN'S DOOR PUBLISHES: the row a
// person is answered with says which task of the plan store it is, and that id
// is the one the store's own read answers under.
//
// THE SURFACE CANNOT WORK THIS OUT FOR ITSELF. The row and the store task wear
// the same title, and a title cannot tell a run's row — which the graph holds no
// node for — from a node that merely says the same words; the tasks place drew
// the wrong half of the pair and its Enter opened an empty room (#1355). So the
// door that mints both halves in one breath states the link
// ([TaskNotice.PlanTask]), and this test is the two ends of it meeting.

import (
	"context"
	"testing"
)

func TestARunsRowNamesTheStoreTaskThePlanReadAnswersUnder(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("done")
	registerBeltRunEngine(t, double)
	conversation := newTestRepo(t)
	sessionDir := t.TempDir()
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = conversation
		config.Place = Place{Dir: sessionDir}
		config.AskConsent = false
	})
	stand := taskStand{dir: conversation, mode: TaskModeWorktree}
	if err := agent.startKnownTaskRun(context.Background(), 41, "write HELLO.md", "brief", nil, stand, ""); err != nil {
		t.Fatalf("start run: %v", err)
	}
	<-double.entered

	// THE ROW SAYS WHICH TASK IT IS. Without this the place is back to matching
	// the pair on the title they share.
	row, found := runRowOf(agent.graph(), 41)
	if !found {
		t.Fatal("the run's door published no row for the work it started")
	}
	if row.PlanTask == "" {
		t.Fatal("the run's row names no store task, so nothing can tell it from a node of this " +
			"session's own tree wearing the same title (TaskNotice.PlanTask)")
	}

	// AND THE STORE ANSWERS UNDER THAT ID. The two halves are one piece of work
	// read from two ends only if the id the row names is the id the plan read
	// files its own row under — a row naming an id nothing answers for would
	// leave the place with an identity it cannot use.
	rows := agent.PlanTasks()
	if len(rows) == 0 {
		t.Fatal("the conversation's plan read answers no rows while its run is live")
	}
	var titles []string
	for _, task := range rows {
		if task.ID == row.PlanTask {
			close(double.release)
			return
		}
		titles = append(titles, task.ID+" "+task.Title)
	}
	close(double.release)
	t.Fatalf("the run's row names store task %q and the plan read answers under %v: the place cannot "+
		"join the pair by an id only one of them uses", row.PlanTask, titles)
}

package session

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

func TestConversationCancellationKeepsTimeLimitApartFromStop(t *testing.T) {
	for _, timed := range []bool{true, false} {
		t.Run(map[bool]string{true: "time limit", false: "person Stop"}[timed], func(t *testing.T) {
			work := newWallWork(t, time.Hour)
			if timed {
				work.agent.endConversationOnWall()
			} else if err := work.agent.StopWork(); err != nil {
				t.Fatal(err)
			}
			waitSignal(t, work.settled, "cancelled task")
			assertCancellationReading(t, work.node.notice(), timed)
		})
	}
}

func TestRunCancellationKeepsTimeLimitApartFromStop(t *testing.T) {
	for _, timed := range []bool{true, false} {
		t.Run(map[bool]string{true: "time limit", false: "person Stop"}[timed], func(t *testing.T) {
			agent, _, _, dir := stoppableBeltRun(t, 71)
			if timed {
				agent.endConversationOnWall()
			} else if err := agent.StopWork(); err != nil {
				t.Fatal(err)
			}
			beltRunWaitFor(t, "cancelled run", func() bool {
				agent.beltMu.Lock()
				defer agent.beltMu.Unlock()
				return agent.beltRun == nil
			})
			rows := agent.graph().runRows(71)
			if len(rows) != 1 {
				t.Fatalf("cancelled run has %d rows", len(rows))
			}
			assertCancellationReading(t, rows[0], timed)
			root := beltRunTaskAt(t, dir, "71")
			if timed {
				if root.Status != plandb.StatusFailed || root.Error != "a time limit you set stopped it" {
					t.Fatalf("time limit has the person's stop in its saved task: %s, %s", root.Status, root.Error)
				}
				_, ending, stopped := interruptedProgramEnding(nil, root, delegate.ProgramRecord{})
				if ending != TaskEndingTimeLimit || stopped {
					t.Fatal("a recovered time limit became the person's stop")
				}
			} else if root.Status != plandb.StatusCancelled {
				t.Fatalf("person Stop saved as %s", root.Status)
			}
		})
	}
}

func assertCancellationReading(t *testing.T, notice TaskNotice, timed bool) {
	t.Helper()
	status := ProjectTask(notice.StatusFacts())
	if timed {
		if status.Word != "incomplete" || status.Reason != "a time limit you set stopped it" || notice.Stopped || notice.Ending != TaskEndingTimeLimit {
			t.Fatalf("time limit: word=%q reason=%q stopped=%v ending=%q", status.Word, status.Reason, notice.Stopped, notice.Ending)
		}
	} else if status.Word != "stopped" || status.Reason != "" || !notice.Stopped {
		t.Fatalf("person Stop: word=%q reason=%q stopped=%v", status.Word, status.Reason, notice.Stopped)
	}
}

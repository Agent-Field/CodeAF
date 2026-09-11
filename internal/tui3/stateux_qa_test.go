package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

func TestAutomaticTaskCountdownDoesNotClaimTheUserMustAnswer(t *testing.T) {
	a, _, _ := roomApp(t)
	a.state = stateWorking
	a.task = &taskCard{id: 21, title: "Repair the reporting pipeline", born: a.now(), deadline: a.now().Add(10 * time.Second)}
	if word, _ := a.stateWord(); word == waitingWord || strings.Contains(word, "your call") {
		t.Fatalf("automatic start is presented as a required response: %q", word)
	}
	a.task.deadline = time.Time{}
	if word, _ := a.stateWord(); word != waitingWord {
		t.Fatalf("a held proposal lost its required response: %q", word)
	}
}

func TestTaskListKeepsLiveDesignApprovalWhenTheIndexOnlySaysRunning(t *testing.T) {
	a, _ := awaitingDesign(t)
	a.file = "/tmp/status-qa/conversation/session.jsonl"
	a.comp.tasks = []session.TaskIndexEntry{{ID: "4", SessionID: "conversation", Label: "harness · flake triage", Status: string(session.TaskRunning), Kind: session.TaskKindHarness}}
	reading := readTasks(session.World{}, a.taskSheetMine(), session.LastDays(a.now(), 7), time.Time{}, a.now())
	if len(reading.items) != 1 {
		t.Fatalf("want one task, got %d", len(reading.items))
	}
	if item := reading.items[0]; item.section != tasksNeeds || item.status().On != session.TaskWaitPerson {
		t.Fatalf("live design approval disappeared behind the index: %+v", item)
	}
	a.tasks[4].doing = "designing"
	reading = readTasks(session.World{}, a.taskSheetMine(), session.LastDays(a.now(), 7), time.Time{}, a.now())
	if reading.items[0].section != tasksRunning {
		t.Fatal("resumed design still asks for approval")
	}
}

func TestTaskListDoesNotBorrowLiveApprovalFromAnotherConversation(t *testing.T) {
	a, _ := awaitingDesign(t)
	a.file = "/tmp/status-qa/conversation/session.jsonl"
	a.comp.tasks = []session.TaskIndexEntry{{ID: "4", SessionID: "other", Label: "harness · flake triage", Status: string(session.TaskDone)}}
	for _, row := range a.taskSheetMine().rows {
		if row.entry.SessionID == "other" && row.live != nil {
			t.Fatal("foreign index row borrowed local task approval")
		}
	}
}

func TestKeepingATaskBranchDoesNotDemandAnUnrequestedMerge(t *testing.T) {
	a, _, _ := taskApp(t)
	a.tasks = make(map[uint64]*taskNode)
	for _, state := range []session.TaskState{session.TaskDone, session.TaskFailed} {
		node := &taskNode{id: 71, title: "Deliver on the requested branch", state: state, merge: "kept", branch: "fix/reporting"}
		a.tasks[71] = node
		status := a.taskStatus(node)
		if !status.ChangesUnlanded() || status.Branch != "fix/reporting" {
			t.Fatal("retained branch became invisible")
		}
		if status.Attention || a.railGroupOf(node) != railDone {
			t.Fatalf("%s: keeping a branch claims a person must act", state)
		}
	}
	a.tasks[71].merge = "conflicted"
	if !a.taskStatus(a.tasks[71]).Attention {
		t.Fatal("a real conflict lost its attention flag")
	}
}

func TestRunBudgetDecisionHasARequiredInputFooter(t *testing.T) {
	snap := orchRun4()
	snap.Paused = true
	a, _ := orchApp(t, snap)
	a.state = stateWorking
	drive(t, a, streamEventMsg{gen: a.gen, ev: orchPause()})
	if word, _ := a.stateWord(); word != waitingWord {
		t.Fatalf("budget decision says %q", word)
	}
	a.orchOf().gate = nil
	if word, _ := a.stateWord(); word == waitingWord {
		t.Fatal("answered gate still requires a response")
	}
}

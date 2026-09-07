package tui3

import (
	"errors"
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

func TestGuestFinishedTaskCannotShowOrAnswerTheLocalSettleQuestion(t *testing.T) {
	a, agent := roomSettleApp(t, session.TaskUnverified)
	local := a.doneCardFor(7)
	if !a.settleAsking(local) {
		t.Fatal("fixture must have a real local decision")
	}
	a.room.guest = &taskGuest{session: "/another/conversation/session.jsonl", node: &taskNode{id: 7, title: "Someone else's result", state: session.TaskDone}}
	if card := a.roomSettleCard(); card != nil {
		t.Error("foreign task exposed the local decision card")
	}
	if a.roomSettleKey(key("a")) {
		t.Error("foreign task consumed the local accept key")
	}
	if len(agent.resolved) != 0 || local.decided != "" {
		t.Fatal("answering on the guest page resolved the local task")
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

func TestHandingReviewToTheModelStopsAskingTheUser(t *testing.T) {
	a, agent := roomSettleApp(t, session.TaskUnverified)
	card := a.doneCardFor(7)
	node := a.tasks[7]
	if !a.taskStatus(node).Attention {
		t.Fatal("fixture has no human decision")
	}
	a.settleCard(card, settleAlways)
	if len(agent.handed) != 1 {
		t.Fatal("handoff did not reach the engine")
	}
	status := a.taskStatus(node)
	if status.Attention || status.On != session.TaskWaitMachine || a.railGroupOf(node) != railParked {
		t.Fatalf("a handed-off review still needs the user: %+v", status)
	}
	if word, _ := a.stateWord(); word != taskReviewPendingWord {
		t.Fatalf("room footer says %q", word)
	}
	if !strings.Contains(a.doneTail(card), taskReviewPendingWord) {
		t.Fatal("landing card still asks the user")
	}
	if a.roomSettleAsking() {
		t.Fatal("handed-off decision still accepts another answer")
	}
}

func TestFailedReviewHandoffKeepsTheHumanDecision(t *testing.T) {
	a, agent := roomSettleApp(t, session.TaskUnverified)
	agent.refuse = errors.New("review unavailable")
	card := a.doneCardFor(7)
	a.settleCard(card, settleAlways)
	if card.reviewByModel || !a.taskStatus(a.tasks[7]).Attention || !a.roomSettleAsking() {
		t.Fatal("failed handoff hid the unresolved decision")
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

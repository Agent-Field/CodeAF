package session

import (
	"testing"
	"time"
)

func TestAutomaticProposalIsNotARequiredHumanResponse(t *testing.T) {
	agent, _ := newPresenceSession(t, t.TempDir(), "ffff6666ffff6666")
	agent.mu.Lock()
	agent.running = true
	agent.taskAnswers = map[uint64]*taskQuestion{9: {
		answer: make(chan TaskAnswer, 1), hold: make(chan struct{}),
		notice: TaskNotice{Deadline: time.Now().Add(time.Minute)},
	}}
	agent.mu.Unlock()
	defer agent.presenceAsking(QuestionTask, 9, "wants to start a task: repair the pipeline")()
	if agent.NeedsPerson() {
		t.Fatal("automatic start claims a person is blocking progress")
	}
	snapshot := agent.presenceSnapshot(time.Now())
	if snapshot.State != PresenceWorking || snapshot.Question.Kind != "" {
		t.Fatalf("automatic proposal presence: %+v", snapshot)
	}
	agent.HoldTask(9)
	if !agent.NeedsPerson() {
		t.Fatal("holding the clock did not require an answer")
	}
	snapshot = agent.presenceSnapshot(time.Now())
	if snapshot.State != PresenceWaiting || snapshot.Question.ID != 9 {
		t.Fatalf("held proposal presence: %+v", snapshot)
	}
}

func TestAutomaticProposalDoesNotHideAnotherRequiredQuestion(t *testing.T) {
	agent, _ := newPresenceSession(t, t.TempDir(), "ffff7777ffff7777")
	agent.mu.Lock()
	agent.taskAnswers = map[uint64]*taskQuestion{
		9:  {notice: TaskNotice{Deadline: time.Now().Add(time.Minute)}},
		10: {},
	}
	agent.mu.Unlock()
	defer agent.presenceAsking(QuestionTask, 9, "starts automatically")()
	defer agent.presenceAsking(QuestionTask, 10, "requires your answer")()
	snapshot := agent.presenceSnapshot(time.Now())
	if !agent.NeedsPerson() || snapshot.Question.ID != 10 || snapshot.Reason != "requires your answer" {
		t.Fatalf("automatic card hid required input: %+v", snapshot)
	}
}

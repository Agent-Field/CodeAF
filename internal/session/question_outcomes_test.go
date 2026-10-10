package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// A decided question and a withdrawn one both leave a receipt, newest first, in
// words a person reads; the decision comes from the durable record.
func TestRecentQuestionOutcomesHoldDecisionsAndWithdrawals(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Place.Dir = t.TempDir()
	})
	earlier := time.Now().Add(-time.Minute)
	agent.recordDecision(DecisionRecord{
		ID: 7, Kind: QuestionConsent, Head: "needs your ok to run bash",
		Subject: SubjectRef{Kind: SubjectCall, CallID: "c1"},
		Picked:  []string{"1"}, Labels: []string{"allow once"}, By: DecidedByPerson, At: earlier,
	})
	agent.sayWithdrawn(Question{
		ID: 9, Kind: QuestionTask, Head: "wants to start a task: X",
		Asker:   Asker{Kind: AskerTask},
		Subject: SubjectRef{Kind: SubjectCall, CallID: "c2"},
	}, "the work is no longer waiting on it")

	got := agent.RecentQuestionOutcomes(10)
	if len(got) != 2 {
		t.Fatalf("outcomes = %+v, want two", got)
	}
	if got[0].Outcome != QuestionWithdrawn || got[0].Token != "9" || got[0].CallID != "c2" ||
		got[0].Words != "No longer needed — the work is no longer waiting on it" {
		t.Fatalf("newest = %+v", got[0])
	}
	if got[1].Outcome != QuestionDecided || got[1].Token != "7" || got[1].CallID != "c1" || got[1].Words != "Allow once" || got[1].By != "person" {
		t.Fatalf("older = %+v", got[1])
	}
	if one := agent.RecentQuestionOutcomes(1); len(one) != 1 || one[0].Token != "9" {
		t.Fatalf("limit 1 = %+v", one)
	}
}

// A plain question about no call is anchored to the `ask` call that raised it,
// and a question about a call keeps that call, so a reloaded window can place
// either receipt.
func TestQuestionOutcomeAnchorsToTheAskingCall(t *testing.T) {
	asked := Question{ID: 3, Kind: QuestionAsk, Head: "Which name?", AskedIn: "ask-1", Asker: Asker{Kind: AskerModel}, Withdrawn: &Withdrawal{Reason: "moved on"}}
	if got := withdrawnOutcome(asked).CallID; got != "ask-1" {
		t.Fatalf("withdrawn anchor = %q, want the asking call", got)
	}
	record := DecisionRecord{ID: 3, Kind: QuestionAsk, Head: "Which name?", AskedIn: "ask-1", Picked: []string{"1"}}
	if got := decidedOutcome(record).CallID; got != "ask-1" {
		t.Fatalf("decided anchor = %q, want the asking call", got)
	}
	record.Subject = SubjectRef{Kind: SubjectCall, CallID: "c9"}
	if got := decidedOutcome(record).CallID; got != "c9" {
		t.Fatalf("subject call must win, got %q", got)
	}
}

// A session that has ended nothing has no receipts, and the default limit holds.
func TestRecentQuestionOutcomesAreEmptyBeforeAnythingEnds(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if got := agent.RecentQuestionOutcomes(0); len(got) != 0 {
		t.Fatalf("outcomes = %+v", got)
	}
}

// The model's `ask` records the call that raised it, so a plain question about
// no row can be found again in the replayed history.
func TestAskRecordsTheCallThatRaisedIt(t *testing.T) {
	a := askTestAgent(t, true)
	ctx := withCallID(context.Background(), "call-ask-1")
	if _, _, err := a.executeAsk(ctx, json.RawMessage(askNotWaiting)); err != nil {
		t.Fatal(err)
	}
	open := a.OpenQuestions()
	if len(open) != 1 || open[0].AskedIn != "call-ask-1" {
		t.Fatalf("open = %+v, want one question asked in call-ask-1", open)
	}
}

// The recorded wait survives replay and never substitutes the policy's nominal duration.
func TestClockReceiptKeepsActualElapsedSeconds(t *testing.T) {
	asked := time.Date(2026, 10, 9, 14, 1, 0, 0, time.UTC)
	q := Question{ID: 4, Kind: QuestionAsk, Asked: asked, Deadline: asked.Add(30 * time.Second), Options: []AnswerOption{{Key: "strict", Label: "Keep strict"}}}
	answer := Answer{Key: "strict", DecidedBy: DecidedByDial, At: asked.Add(37200 * time.Millisecond)}
	record := decisionRecordOf(q, answer)
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var replay DecisionRecord
	if err := json.Unmarshal(data, &replay); err != nil {
		t.Fatal(err)
	}
	if got := decidedOutcome(replay); got.ElapsedSeconds != 37.2 || got.Words != "Keep strict" {
		t.Fatalf("replayed receipt = %+v", got)
	}
	answer.DecidedBy = DecidedByPerson
	if got := decisionRecordOf(q, answer); got.ElapsedSeconds != 0 {
		t.Fatalf("person receipt = %+v", got)
	}
	answer.DecidedBy = DecidedByDial
	q.Asked = time.Time{}
	if got := decisionRecordOf(q, answer); got.ElapsedSeconds != 0 {
		t.Fatalf("unknown wait = %+v", got)
	}
	q.Asked, q.Deadline = asked, time.Time{}
	if got := decisionRecordOf(q, answer); got.ElapsedSeconds != 0 {
		t.Fatalf("immediate policy = %+v", got)
	}
}

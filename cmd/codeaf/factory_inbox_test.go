package main

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/session"
)

// fakeInboxTurn is one manager turn on a step's question: it replies through
// the door it was opened with, as `factory_answer` would, or not at all.
type fakeInboxTurn struct {
	door      session.RunDoor
	answer    string
	why       string
	asked     *[]string
	secondErr *error
}

func (f *fakeInboxTurn) SubmitRunnerNote(ctx context.Context, text string) (<-chan session.Event, error) {
	*f.asked = append(*f.asked, text)
	inbox, _ := f.door.(session.InboxDoor)
	switch {
	case f.answer != "":
		_, _ = inbox.AnswerStep(ctx, "", f.answer)
		_, *f.secondErr = inbox.SendOn(ctx, "", "again")
	case f.why != "":
		_, _ = inbox.SendOn(ctx, "", f.why)
	}
	ch := make(chan session.Event, 1)
	close(ch)
	return ch, nil
}
func (f *fakeInboxTurn) Interrupt()   {}
func (f *fakeInboxTurn) Close() error { return nil }

// THE MANAGER'S TURN ON A STEP'S QUESTION: the ask carries the question, its
// answers and the step's pick; the first reply is kept and a second refused;
// a turn that replies nothing sends the question to the person; and the
// turn's `factory_run` changes no stage.
func TestTheInboxTurnKeepsTheManagersReply(t *testing.T) {
	var asked []string
	var second error
	answer, why := "sqlite, the recipe says so", ""
	s := &shapeTurns{
		dirs: func(string) string { return "" },
		open: func(_ context.Context, _ factory.Item, door session.RunDoor, _ bool) (managerTurn, error) {
			if _, _, err := door.EditRun(context.Background(), "", factory.RunEdit{Skip: []string{"neaten"}}); err == nil {
				t.Error("an inbox turn's factory_run changed the stages")
			}
			return &fakeInboxTurn{door: door, answer: answer, why: why, asked: &asked, secondErr: &second}, nil
		},
	}
	it := factory.Item{ID: 3, Talk: "/x/transcript.jsonl"}
	q := factory.Asked{Stage: "plan", Question: "which storage shape?", Options: []string{"sqlite", "jsonl"}, Pick: "jsonl, because it streams"}
	reply, err := s.Inbox(context.Background(), it, q)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Answer != answer || reply.Why != "" {
		t.Errorf("reply = %+v", reply)
	}
	if second == nil {
		t.Error("a second reply on one turn was kept")
	}
	if len(asked) != 1 || !strings.HasPrefix(asked[0], "plan asks: which storage shape? · its answers: sqlite / jsonl · it would take: jsonl, because it streams · Reply with factory_answer") {
		t.Errorf("asked %q", asked)
	}
	answer, why = "", "a taste call"
	if reply, _ = s.Inbox(context.Background(), it, q); reply.Answer != "" || reply.Why != "a taste call" {
		t.Errorf("sent on = %+v", reply)
	}
	answer, why = "", ""
	if reply, _ = s.Inbox(context.Background(), it, q); reply != (factoryrun.InboxReply{}) {
		t.Errorf("a turn that replied nothing = %+v", reply)
	}
	// THE MANAGER'S BRIEF SAYS IT IS THE INBOX, and when to ask the person.
	for _, want := range []string{"You are the inbox", "factory_answer", "taste, scope, risk, credentials"} {
		if !strings.Contains(talkBlockInbox, want) {
			t.Errorf("the inbox block does not say %q", want)
		}
	}
}

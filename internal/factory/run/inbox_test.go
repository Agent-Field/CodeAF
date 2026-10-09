package run

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// asker is a stage door that takes questions, as internal/session's
// StageAsker reads it.
type asker interface {
	Ask(ctx context.Context, q factory.Asked) (string, error)
}

// askingStage is an executor whose round asks q once and reports what it was
// answered as its output.
func askingStage(q factory.Asked, got chan<- string) Executor {
	return ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
		if job.Ask == nil {
			return factory.StageResult{}, errors.New("no road to the manager")
		}
		said, err := job.Ask(ctx, q)
		if err != nil {
			return factory.StageResult{}, err
		}
		got <- said
		return done(said), nil
	})
}

// THE MANAGER ANSWERS FROM WHAT IT KNOWS: the step goes on with its words at
// once, the person is never asked, and the chat says both lines.
func TestTheManagerAnswersAStepsQuestionAndTheStepGoesOn(t *testing.T) {
	talk := newFakeTalk()
	got := make(chan string, 1)
	var asked []factory.Asked
	var mu sync.Mutex
	g, _ := managed(t, map[factory.StageKind]Executor{
		factory.StageChat: askingStage(factory.Asked{Question: "which storage shape?", Options: []string{"sqlite", "jsonl"}}, got),
	}, talk, time.Hour)
	g.r.opts.Inbox = func(_ context.Context, it factory.Item, q factory.Asked) (InboxReply, error) {
		mu.Lock()
		asked = append(asked, q)
		mu.Unlock()
		if it.Asking == nil || it.Asking.With != factory.AskedManager {
			t.Errorf("the item does not hold the question with the manager: %+v", it.Asking)
		}
		return InboxReply{Answer: "sqlite, the recipe says so"}, nil
	}
	id := g.add("fix it", chat("plan"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	if said := <-got; said != "the manager answered: sqlite, the recipe says so" {
		t.Errorf("the step was answered %q", said)
	}
	it := g.waitState(id, factory.StateLanded)
	if it.Asking != nil {
		t.Errorf("an answered question stays on the item: %+v", it.Asking)
	}
	talk.waitSaid(t, it.Talk, "plan asks: which storage shape?")
	talk.waitSaid(t, it.Talk, "manager answered plan: sqlite, the recipe says so")
	for _, l := range talk.lines(it.Talk) {
		if strings.Contains(l, "asks you") {
			t.Errorf("the person was asked: %q", l)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(asked) != 1 || asked[0].Stage != "plan" || len(asked[0].Options) != 2 {
		t.Errorf("the manager was handed %+v", asked)
	}
}

// THE MANAGER SENDS IT ON: the item needs the person with the question on it,
// the step waits, and words typed into the manager chat answer it.
func TestASentOnQuestionWaitsForThePersonsWordsInTheManagerChat(t *testing.T) {
	talk := newFakeTalk()
	got := make(chan string, 1)
	g, _ := managed(t, map[factory.StageKind]Executor{
		factory.StageChat: askingStage(factory.Asked{Question: "which storage shape?"}, got),
	}, talk, 5*time.Millisecond)
	g.r.opts.Inbox = func(context.Context, factory.Item, factory.Asked) (InboxReply, error) {
		return InboxReply{Why: "a taste call"}, nil
	}
	id := g.add("fix it", chat("plan"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.asked(id, "plan asks: which storage shape?")
	if it.QKind != qkindStep || it.Asking == nil || it.Asking.With != factory.AskedYou || it.Asking.Why != "a taste call" {
		t.Fatalf("the item holds %q %+v", it.QKind, it.Asking)
	}
	talk.waitSaid(t, it.Talk, "plan asks you: which storage shape? · a taste call")
	select {
	case said := <-got:
		t.Fatalf("the step went on before the person answered: %q", said)
	case <-time.After(30 * time.Millisecond):
	}
	talk.typed(it.Talk, "use jsonl, it streams")
	select {
	case said := <-got:
		if said != "the person answered: use jsonl, it streams" {
			t.Errorf("the step was answered %q", said)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the person's words never reached the step")
	}
	it = g.waitState(id, factory.StateLanded)
	if it.Asking != nil || it.Question != "" {
		t.Errorf("the question stays on the item: %q %+v", it.Question, it.Asking)
	}
	talk.waitSaid(t, it.Talk, "answered: use jsonl, it streams")
}

// NO MANAGER TURN, OR ONE THAT FAILS, IS THE PERSON'S: the floor's answer
// door reaches the waiting step.
func TestAQuestionTheManagerCannotTakeIsThePersonsAndTheirKeysAnswerIt(t *testing.T) {
	talk := newFakeTalk()
	got := make(chan string, 1)
	g, _ := managed(t, map[factory.StageKind]Executor{
		factory.StageChat: askingStage(factory.Asked{Question: "may it delete the old table?"}, got),
	}, talk, time.Hour)
	g.r.opts.Inbox = func(context.Context, factory.Item, factory.Asked) (InboxReply, error) {
		return InboxReply{}, errors.New("the turn failed")
	}
	id := g.add("fix it", chat("write"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.asked(id, "write asks: may it delete the old table?")
	if err := g.r.Answer(id, true, ""); err != nil {
		t.Fatal(err)
	}
	if said := <-got; said != "the person answered: yes" {
		t.Errorf("the step was answered %q", said)
	}
	g.waitState(id, factory.StateLanded)
}

// A STAGE CONVERSATION'S DOOR TAKES QUESTIONS: the chat executor hands the
// round's road to the manager to the door its conversation asks through.
func TestTheStageDoorPutsAQuestionToTheManager(t *testing.T) {
	talk := newFakeTalk()
	got := make(chan string, 1)
	maker := &FakeMaker{Script: func(ctx context.Context, c *FakeConversation) {
		door, ok := c.Spec.Stage.(asker)
		if !ok {
			t.Error("the stage door takes no questions")
			return
		}
		said, err := door.Ask(ctx, factory.Asked{Question: "which file?"})
		if err != nil {
			t.Error(err)
			return
		}
		got <- said
		_ = c.Spec.Stage.Report(ctx, done(said))
	}}
	g, _ := managed(t, map[factory.StageKind]Executor{factory.StageChat: NewChatExecutor(maker)}, talk, time.Hour)
	g.r.opts.Inbox = func(context.Context, factory.Item, factory.Asked) (InboxReply, error) {
		return InboxReply{Answer: "ledger.go"}, nil
	}
	id := g.add("fix it", chat("plan"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	select {
	case said := <-got:
		if said != "the manager answered: ledger.go" {
			t.Errorf("the conversation was answered %q", said)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the conversation's question was never answered")
	}
	g.waitState(id, factory.StateLanded)
	if opened := maker.Opened(); len(opened) != 1 || !strings.Contains(opened[0].Spec.Brief, briefAsk) {
		t.Errorf("the stage's brief does not say a question goes to the manager")
	}
}

// AN ANSWER OUTLIVES THE ROUND: a question the person holds on an item no run
// holds (a restart) is answered onto the item's notes, which the next run's
// rounds read, and comes off the item.
func TestAnAnswerToAQuestionNoRunHoldsIsKeptOnTheNotes(t *testing.T) {
	talk := newFakeTalk()
	g, _ := managed(t, map[factory.StageKind]Executor{}, talk, time.Hour)
	id := g.add("fix it", chat("plan"))
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.State = factory.StateNeedsYou
		it.Question, it.QKind = "plan asks: which storage shape?", qkindStep
		it.Asking = &factory.Asked{Stage: "plan", Question: "which storage shape?", With: factory.AskedYou}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Answer(id, false, "sqlite"); err != nil {
		t.Fatal(err)
	}
	it, err := g.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if it.Asking != nil || it.Question != "" || it.QKind != "" {
		t.Errorf("the question stays on the item: %q %+v", it.Question, it.Asking)
	}
	want := "plan asked: which storage shape? · the person answered: sqlite"
	if len(it.Notes) != 1 || it.Notes[0] != want {
		t.Errorf("notes = %q, want %q", it.Notes, want)
	}
	// AND A QUESTION WITH THE MANAGER IS NOT THE PERSON'S TO ANSWER.
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.Asking = &factory.Asked{Stage: "plan", Question: "which file?", With: factory.AskedManager}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Answer(id, true, ""); err == nil {
		t.Error("a question the manager holds took the person's answer")
	}
}

// A STEP'S QUESTION TAKES WORDS: any line typed is its answer.
func TestAStepsQuestionTakesAnyWords(t *testing.T) {
	for words, want := range map[string]string{"use sqlite": "use sqlite", "yes": "", "no, jsonl": "no, jsonl"} {
		_, said, ok := typedAnswer(words, qkindStep)
		if !ok || said != want {
			t.Errorf("%q answered %q, %v; want %q", words, said, ok, want)
		}
	}
}

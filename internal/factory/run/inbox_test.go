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

// PAUSED ON A QUESTION, ANSWERED AFTER A RESTART: the item does not stay
// `needs you` with no question; it is paused and running again, its step no
// longer waits, and run carries the step on in its own chat, told the answer.
func TestAQuestionAnsweredWhilePausedReachesTheResumedChat(t *testing.T) {
	p := newPauseExec()
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: p}, nil)
	id := g.add("one", chat("write"))
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.State = factory.StateNeedsYou
		it.Question, it.QKind = "write asks: which storage shape?", qkindStep
		it.Asking = &factory.Asked{Stage: "write", Question: "which storage shape?", With: factory.AskedYou}
		it.Stream = &factory.Stream{Paused: true, Phases: []factory.Phase{{Name: "write", Kind: factory.StageChat, State: factory.PhaseWaiting, Round: 1, Chat: "chat-x"}}}
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
	want := "write asked: which storage shape? · the person answered: sqlite"
	if it.State != factory.StateRunning || it.Question != "" || !storedPaused(it) {
		t.Fatalf("after the answer the item is %s, question %q, paused %v", it.State, it.Question, storedPaused(it))
	}
	if ph := it.Stream.Phases[0]; ph.State != factory.PhaseRunning || len(ph.Carry) != 1 || ph.Carry[0] != want {
		t.Fatalf("the step after the answer: %+v", ph)
	}
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, p)
	j := p.job(0)
	if j.Resume != "chat-x" || len(j.CarryOn) != 1 || j.CarryOn[0] != want {
		t.Fatalf("the resumed round: resume %q, carry-on %q", j.Resume, j.CarryOn)
	}
	if b := carryOnBrief(j.CarryOn); !strings.Contains(b, stageWhilePaused+want) || !strings.HasSuffix(b, briefClosing) {
		t.Fatalf("the carry-on note is %q", b)
	}
	close(p.release)
	it = g.waitState(id, factory.StateLanded)
	if len(it.Stream.Phases[0].Carry) != 0 {
		t.Fatalf("the carried answer stays on the step: %q", it.Stream.Phases[0].Carry)
	}
}

// AN ANSWER THE MANAGER GIVES FOR THE PERSON IS KEPT ON THE JOB: the question
// and the manager's answer stand beside the person's own answers, the Brief
// reads it as the manager's (never as the person's request), and the step
// still goes on at once.
func TestTheManagersAnswerIsRecordedOnTheJob(t *testing.T) {
	talk := newFakeTalk()
	got := make(chan string, 1)
	g, _ := managed(t, map[factory.StageKind]Executor{
		factory.StageChat: askingStage(factory.Asked{Question: "touch README.md?", Options: []string{"yes", "no"}}, got),
	}, talk, time.Hour)
	g.r.opts.Inbox = func(context.Context, factory.Item, factory.Asked) (InboxReply, error) {
		return InboxReply{Answer: "no, leave README.md alone"}, nil
	}
	id := g.add("fix it", chat("write"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	<-got
	it := g.waitState(id, factory.StateLanded)
	if len(it.AnsweredQuestions) != 1 {
		t.Fatalf("the manager's answer was not kept on the job: %+v", it.AnsweredQuestions)
	}
	q := it.AnsweredQuestions[0]
	if q.Text != "touch README.md?" || q.FromStep != "write" || q.With != factory.AskedManager || q.Answer == nil ||
		q.Answer.By != factory.AnsweredByManager || q.Answer.Text != "no, leave README.md alone" {
		t.Fatalf("kept %+v answer %+v", q, q.Answer)
	}
	in := factory.BriefInputOf(it)
	if len(in.ManagerAnswers) != 1 || in.ManagerAnswers[0].Answer != "no, leave README.md alone" {
		t.Fatalf("the Brief does not read the manager's answer: %+v", in.ManagerAnswers)
	}
	for _, r := range in.PersonRequests {
		if strings.Contains(r.Text, "leave README.md alone") {
			t.Fatalf("the manager's answer reads as the person's request: %+v", r)
		}
	}
}

// A QUESTION THE STEP SAID ONLY A PERSON CAN ANSWER, AND EVERY QUESTION OF A
// JOB IN MANUAL, GOES STRAIGHT TO THE PERSON: the manager is never given a
// turn, the question says why it came to them, and their words answer it.
func TestPersonOnlyAndManualQuestionsSkipTheManager(t *testing.T) {
	for name, c := range map[string]struct {
		q      factory.Asked
		manual bool
		why    string
	}{
		"marked for a person": {q: factory.Asked{Question: "which wording do you prefer?", Person: true}, why: factory.WhyOnlyAPerson},
		"a job in Manual":     {q: factory.Asked{Question: "which wording do you prefer?"}, manual: true, why: factory.WhyManualAsksYou},
	} {
		t.Run(name, func(t *testing.T) {
			talk := newFakeTalk()
			got := make(chan string, 1)
			g, _ := managed(t, map[factory.StageKind]Executor{factory.StageChat: askingStage(c.q, got)}, talk, time.Hour)
			g.r.opts.Inbox = func(context.Context, factory.Item, factory.Asked) (InboxReply, error) {
				t.Error("the manager was given a question only the person may answer")
				return InboxReply{Answer: "the manager settled it"}, nil
			}
			id := g.add("fix it", chat("write"))
			if c.manual {
				if err := g.st.Update(id, func(it *factory.Item) error { it.Autonomy = factory.AutonomyManual; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			if err := g.r.Launch(id); err != nil {
				t.Fatal(err)
			}
			it := g.asked(id, "write asks: "+c.q.Question)
			if it.Asking == nil || it.Asking.With != factory.AskedYou || it.Asking.Why != c.why {
				t.Fatalf("the question is held as %+v, want with you because %q", it.Asking, c.why)
			}
			if err := g.r.Answer(id, false, "the short one"); err != nil {
				t.Fatal(err)
			}
			if said := <-got; said != "the person answered: the short one" {
				t.Fatalf("the step was answered %q", said)
			}
		})
	}
}

// A STEP'S OUTPUTS SURVIVE THE STEP FAILING. A step that reported what it did
// (here, the pull request it opened) and then failed keeps that report on its
// step beside the failure, for any step kind.
func TestAFailedStepKeepsWhatItReported(t *testing.T) {
	g, _ := managed(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(context.Context, Job) (factory.StageResult, error) {
			return factory.StageResult{Output: "opened https://github.com/acme/demo/pull/185", Files: []string{"ledger.go"}},
				errors.New("9 of 10 acceptance criteria met")
		}),
	}, newFakeTalk(), time.Hour)
	id := g.add("fix it", chat("write"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.wait(id, "failed", func(it factory.Item) bool { return it.Failure != nil && it.Stream != nil && !it.Stream.Ended.IsZero() })
	res := it.Stream.Phases[0].Result
	if res == nil || !strings.Contains(res.Output, "https://github.com/acme/demo/pull/185") || len(res.Files) != 1 {
		t.Fatalf("the failed step lost what it reported: %+v", res)
	}
	if factory.JobState(it) != "needs_you" {
		t.Fatalf("a failed step's job reads %s", factory.JobState(it))
	}
}

// A STEER SENT TO A STEP THAT IS ENDING IS NOT LOST. The step that was
// running when the person's words arrived ends without reading them; the next
// step's brief carries them, so the words reach the run either way.
func TestASteerToAnEndingStepReachesTheNextStep(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	notes := make(chan []string, 1)
	g, _ := managed(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(_ context.Context, job Job) (factory.StageResult, error) {
			if job.Stage.Name == "write" {
				close(started)
				<-release
				return done("written"), nil
			}
			notes <- append([]string(nil), job.Notes...)
			return done("proved"), nil
		}),
	}, newFakeTalk(), time.Hour)
	id := g.add("fix it", chat("write"), chat("proof"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := g.r.Steer(id, "keep Len() on one line"); err != nil {
		t.Fatalf("the steer was refused while the step ran: %v", err)
	}
	close(release)
	select {
	case got := <-notes:
		if !strings.Contains(strings.Join(got, "\n"), "keep Len() on one line") {
			t.Fatalf("the next step's brief lost the steer: %q", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the next step never ran")
	}
}

// A STEP CAN TELL A STOP FROM LETTING GO. A person's Stop ends the step's
// context with the stop as its cause; a withdrawn control (the engine
// closing, a lease replaced) ends it with none, so work running elsewhere is
// cancelled only by the stop.
func TestAStepTellsAStopFromLettingGo(t *testing.T) {
	for name, end := range map[string]func(*Runner, int) error{
		"stop":     func(r *Runner, id int) error { return r.Stop(id) },
		"withdraw": func(r *Runner, id int) error { r.Withdraw(id); return nil },
	} {
		t.Run(name, func(t *testing.T) {
			started := make(chan struct{})
			stopped := make(chan bool, 1)
			g, _ := managed(t, map[factory.StageKind]Executor{
				factory.StageChat: ExecutorFunc(func(ctx context.Context, _ Job) (factory.StageResult, error) {
					close(started)
					<-ctx.Done()
					stopped <- StoppedHere(ctx)
					return factory.StageResult{}, ctx.Err()
				}),
			}, newFakeTalk(), time.Hour)
			id := g.add("fix it", chat("write"))
			if err := g.r.Launch(id); err != nil {
				t.Fatal(err)
			}
			<-started
			if err := end(g.r, id); err != nil {
				t.Fatal(err)
			}
			if got := <-stopped; got != (name == "stop") {
				t.Fatalf("%s: the step read PersonStopped = %v", name, got)
			}
		})
	}
}

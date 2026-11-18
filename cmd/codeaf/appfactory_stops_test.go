package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/session"
)

// stoppedJob is a job the store wrote at work and then out of rounds: its
// main agent holds the stop.
func stoppedJob(t *testing.T) (factory.Item, factory.Wait) {
	t.Helper()
	at := time.Now().Add(-time.Minute)
	it := factory.Item{ID: 3, Talk: "/x/transcript.jsonl", State: factory.StateRunning, Cap: 10, Autonomy: factory.AutonomyManual,
		Stages: []factory.Stage{{Name: "write", Kind: factory.StageChat, On: true}, {Name: "test", Kind: factory.StageChat, On: true, Until: factory.UntilGreen}},
		Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{
			{Name: "write", Kind: factory.StageChat, State: factory.PhaseDone, Round: 1},
			{Name: "test", Kind: factory.StageChat, State: factory.PhaseRunning, Round: 2},
		}}}
	for i := range 8 {
		it.Stream.Log = append(it.Stream.Log, factory.LogLine{At: at, Tone: "said", Text: "line " + string(rune('a'+i)) + "\nwrapped"})
	}
	factory.SettleWait(factory.Item{Wait: &factory.Wait{Who: factory.WaitBacklog}}, &it, at)
	before := it
	it.State, it.QKind = factory.StateNeedsYou, factory.QKindOutOfAttempts
	it.Question = "test is not green after 2 rounds · try again, send it back, or stop the job?"
	it.Asking = &factory.Asked{Stage: "test", Question: it.Question, With: factory.AskedYou}
	it.Stream = &factory.Stream{Cur: 1, Log: it.Stream.Log, Phases: []factory.Phase{
		it.Stream.Phases[0],
		{Name: "test", Kind: factory.StageChat, State: factory.PhaseWaiting, Round: 2, Result: &factory.StageResult{Findings: 2, Output: "two tests fail"}},
	}}
	factory.SettleWait(before, &it, at.Add(time.Second))
	wait, held := factory.HandedOver(it)
	if !held {
		t.Fatalf("the stop was not handed to the main agent: %+v", it.Wait)
	}
	return it, wait
}

// THE MAIN AGENT'S TURN ON A STOP: the hand-over says what stopped in the
// job's own words, the wait and offers the service stored and the job's last
// log lines, names what the agent may do about this kind of stop by its tool
// actions, and states the limits. The turn carries the door it was handed and
// the stall turn's wait, and what the agent said comes back for the job's log.
func TestTheStopTurnHandsTheStopToTheMainAgent(t *testing.T) {
	var asked []string
	var lent session.RunDoor
	var wait time.Duration
	s := &shapeTurns{
		dirs: func(string) string { return "" },
		open: func(ctx context.Context, _ factory.Item, door session.RunDoor, _ bool) (managerTurn, error) {
			lent = door
			if deadline, ok := ctx.Deadline(); ok {
				wait = time.Until(deadline)
			}
			return &fakeStallTurn{asked: &asked, says: "Gave test another round."}, nil
		},
	}
	it, under := stoppedJob(t)
	door := stallDoor{name: "the main agent's own"}
	said, err := s.Stopped(context.Background(), it, under, door)
	if err != nil {
		t.Fatal(err)
	}
	if said != "Gave test another round." {
		t.Errorf("said = %q", said)
	}
	if lent != session.RunDoor(door) {
		t.Errorf("the turn was lent %v, want the door it was handed", lent)
	}
	if wait <= 0 || wait > stopTurnWait {
		t.Errorf("the turn may take %v, want at most %v", wait, stopTurnWait)
	}
	if len(asked) != 1 {
		t.Fatalf("asked %d times", len(asked))
	}
	for _, want := range []string{
		"This job stopped, and the stop is yours to decide before its person is asked: test is not green after 2 rounds",
		"Waits on: you, the manager, to decide this stop first. If you do not settle it, the person is asked to answer the question.",
		"Accepted from you now: task, another_round, send_back, rerun, accept, hand_to_person.",
		"Its log's last lines: ",
		"line h wrapped",
		"A step used its rounds without finishing.",
		"action another_round",
		"factory_task",
		"factory_act",
		"Never merge",
		"In Manual nothing goes public without the person",
		"factory_answer action hand_to_person and one plain sentence of why",
	} {
		if !strings.Contains(asked[0], want) {
			t.Errorf("the hand-over does not say %q:\n%s", want, asked[0])
		}
	}
	if strings.Contains(asked[0], "line b") || strings.Contains(asked[0], "\n") {
		t.Errorf("the hand-over carries more than the log's last lines on one line:\n%s", asked[0])
	}
}

// EACH KIND OF STOP NAMES ITS OWN DECISIONS, by the actions the service takes.
func TestEveryKindOfStopSaysWhatMayBeDone(t *testing.T) {
	for kind, actions := range map[string][]string{
		factory.StopRounds:   {factory.DecideAnotherRound, factory.DecideSendBack, factory.DecideRerun, factory.DecideAccept},
		factory.StopQuestion: {"action answer"},
		factory.StopFailed:   {factory.DecideRerun, factory.DecideResumePipeline},
		factory.StopUnsaved:  {factory.DecideResumePipeline, factory.DecideRerun},
		factory.StopPublish:  {factory.DecideRetryPublish},
		factory.StopFollow:   {factory.DecideResumePipeline, factory.DecideWaitGitHub},
	} {
		words := stopAskKinds[kind]
		if words == "" {
			t.Errorf("a stop of kind %s says nothing of what may be done", kind)
		}
		for _, action := range actions {
			if !strings.Contains(words, action) {
				t.Errorf("a stop of kind %s does not name %s: %s", kind, action, words)
			}
		}
	}
	if !strings.Contains(stopAskTail, factory.DecideHandToPerson) {
		t.Errorf("the hand-over does not name %s", factory.DecideHandToPerson)
	}
}

// A turn that cannot be given is an error, which the service reads as the
// main agent not having been asked; a quiet factory asks nobody and says so
// with the service's own word for it, so the stop goes to the person with no
// line on the job's log.
func TestAStopTurnThatCannotBeGiven(t *testing.T) {
	refused := errors.New("the conversation could not be opened")
	s := &shapeTurns{
		dirs: func(string) string { return "" },
		open: func(context.Context, factory.Item, session.RunDoor, bool) (managerTurn, error) { return nil, refused },
	}
	it, under := stoppedJob(t)
	if _, err := s.Stopped(context.Background(), it, under, nil); !errors.Is(err, refused) {
		t.Fatalf("err = %v", err)
	}
	h := &appFactoryHost{quiet: true}
	if _, err := h.stopped(context.Background(), it, under); !errors.Is(err, serve.ErrNotAsked) {
		t.Fatalf("a quiet factory: err = %v", err)
	}
}

// A JOB WITH NO CONVERSATION OF ITS OWN HAS NO MAIN AGENT TO ASK, and its
// stop goes to the person with nothing said on its log.
func TestAJobWithNoConversationIsNotAsked(t *testing.T) {
	h := &appFactoryHost{}
	h.loopsOn = true
	it, under := stoppedJob(t)
	it.Talk = ""
	if _, err := h.stopped(context.Background(), it, under); !errors.Is(err, serve.ErrNotAsked) {
		t.Fatalf("err = %v", err)
	}
}

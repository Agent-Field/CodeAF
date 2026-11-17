package run

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/verification"
)

const acceptRedCheck = "'go' 'test' './internal/home' '-count=1' (in /work) · failed"

// A step that judges the work and stops short can be taken as it stands: the
// run goes on, and every later step is told what was accepted, word for word.
func TestAcceptFinishesAStuckStepAndTellsTheStepsAfterIt(t *testing.T) {
	var mu sync.Mutex
	var later []string
	var notes [][]string
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			switch job.Stage.Name {
			case "test":
				return factory.StageResult{Exit: 1, Notes: []string{verification.FailurePrefix + " " + acceptRedCheck}}, nil
			case "tidy":
				mu.Lock()
				later = factory.AcceptedFailures(job.Item)
				notes = append(notes, append([]string(nil), job.Notes...))
				mu.Unlock()
			}
			return done(""), nil
		}),
	}, nil)
	test := chat("test")
	test.Until, test.Max = factory.UntilGreen, 1
	id := g.add("fix", chat("write"), test, chat("tidy"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	asked := g.wait(id, "asking with accept among the answers", func(it factory.Item) bool {
		return it.State == factory.StateNeedsYou && it.QKind == factory.QKindOutOfAttempts
	})
	if !strings.HasSuffix(asked.Question, " · try again, send it back, accept it as it stands, or stop the job?") || !factory.OffersAccept(asked) {
		t.Fatalf("question %q, offers accept %v", asked.Question, factory.OffersAccept(asked))
	}
	if err := g.r.Answer(id, true, factory.ChoiceAccept); err != nil {
		t.Fatal(err)
	}
	it := g.wait(id, "accepted and gone on", func(it factory.Item) bool {
		return it.Stream != nil && it.Stream.Phases[2].State == factory.PhaseDone
	})
	ph := it.Stream.Phases[1]
	if ph.State != factory.PhaseDone || ph.Note != "accepted with 1 failing check" || ph.Round != 1 {
		t.Fatalf("test step = %+v", ph)
	}
	if got := strings.Join(ph.Result.Notes, "\n"); got != factory.AcceptedPrefix+" "+acceptRedCheck {
		t.Fatalf("test notes:\n%s", got)
	}
	if !logHas(it, "test accepted as it stands: "+acceptRedCheck) {
		t.Fatalf("the acceptance was not said: %+v", it.Stream.Log)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(later, "|") != acceptRedCheck {
		t.Fatalf("the step after was told %q", later)
	}
	if len(notes) != 1 || !containsLine(notes[0], factory.AcceptedPrefix+" "+acceptRedCheck) || containsPrefix(notes[0], verification.FailurePrefix) {
		t.Fatalf("the step after was briefed with %q", notes)
	}
}

// A step that makes the work has nothing to accept: the word is not among its
// answers, and saying it asks again. It never falls through to stopping the job.
func TestAcceptOnAStepThatMakesTheWorkAsksAgain(t *testing.T) {
	g := newRig(t, map[factory.StageKind]Executor{
		factory.StageChat: ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
			if job.Stage.Name == "write" {
				return factory.StageResult{Done: false, Output: "could not"}, nil
			}
			return done(""), nil
		}),
	}, nil)
	id := g.add("fix", chat("plan"), chat("write"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	asked := g.wait(id, "asking", func(it factory.Item) bool {
		return it.State == factory.StateNeedsYou && it.QKind == factory.QKindOutOfAttempts
	})
	if strings.Contains(asked.Question, "accept") || factory.OffersAccept(asked) {
		t.Fatalf("a write step offers accept: %q", asked.Question)
	}
	if err := g.r.Answer(id, true, factory.ChoiceAccept); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	it := g.wait(id, "asking again", func(it factory.Item) bool {
		return it.State == factory.StateNeedsYou && it.QKind == factory.QKindOutOfAttempts && it.Question == asked.Question
	})
	if it.Stream.Phases[1].State != factory.PhaseWaiting || it.Stream.Phases[1].Round != asked.Stream.Phases[1].Round {
		t.Fatalf("write = %+v", it.Stream.Phases[1])
	}
}

// The same answer on a job no worker holds: the step is done as it stands and
// the job waits for a worker, with the acceptance on the document.
func TestAcceptOnAJobNoWorkerHolds(t *testing.T) {
	now := time.Date(2026, 11, 9, 22, 40, 0, 0, time.UTC)
	stuck := func() factory.Item {
		return factory.Item{
			ID: 31, Kind: factory.KindIssue, State: factory.StateNeedsYou, QKind: factory.QKindOutOfAttempts,
			Question: "test is not green after 2 rounds: verification blocked: " + acceptRedCheck + " · try again, send it back, or stop the job?",
			Stages: []factory.Stage{
				{Name: "write", Until: factory.UntilDone, On: true},
				{Name: "test", Until: factory.UntilGreen, Max: 2, On: true},
				{Name: "proof", Until: factory.UntilProven, On: true},
			},
			Stream: &factory.Stream{Cur: 1, Phases: []factory.Phase{
				{Name: "write", State: factory.PhaseDone, Result: &factory.StageResult{Done: true}},
				{Name: "test", State: factory.PhaseWaiting, Round: 2, RoundLimit: 2, Result: &factory.StageResult{Exit: 1, Notes: []string{verification.FailurePrefix + " " + acceptRedCheck}}},
				{Name: "proof", State: factory.PhasePending},
			}},
		}
	}
	it := stuck()
	if err := AnswerUnheld(&it, true, factory.ChoiceAccept, now); err != nil {
		t.Fatal(err)
	}
	ph := it.Stream.Phases[1]
	if it.State != factory.StateQueued || it.Question != "" || it.QKind != "" || ph.State != factory.PhaseDone || !ph.At.Equal(now) || ph.Round != 2 || len(it.Notes) != 0 {
		t.Fatalf("job = %s %q, test = %+v, notes %q", it.State, it.Question, ph, it.Notes)
	}
	if strings.Join(factory.AcceptedFailures(it), "|") != acceptRedCheck {
		t.Fatalf("accepted %q", factory.AcceptedFailures(it))
	}
	// Where the job does not offer it, the answer is refused and nothing moves.
	it = stuck()
	it.Stream.Cur = 0
	it.Stream.Phases[0].State = factory.PhaseWaiting
	if err := AnswerUnheld(&it, true, factory.ChoiceAccept, now); err == nil || it.State != factory.StateNeedsYou || it.Stream.Phases[0].State != factory.PhaseWaiting {
		t.Fatalf("accept on a write step: %v, job %s", err, it.State)
	}
}

func TestAcceptReadsTheGuardsOwnPrefix(t *testing.T) {
	ph := factory.Phase{Name: "test", Result: &factory.StageResult{Notes: []string{verification.FailurePrefix + " x · failed"}}}
	if what := factory.AcceptStep(&ph, ""); len(what) != 1 || what[0] != "x · failed" {
		t.Fatalf("factory and verification disagree on the unresolved-check prefix: %q", what)
	}
}

func TestOnlyAPlainAcceptIsTyped(t *testing.T) {
	for words, want := range map[string]bool{"accept": true, "Accept it.": true, "accept and continue": true, "accept the other file": false, "I accept": false, "": false} {
		if typedAccept(words) != want {
			t.Errorf("%q typed accept = %v", words, !want)
		}
	}
}

func containsLine(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

func containsPrefix(lines []string, prefix string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

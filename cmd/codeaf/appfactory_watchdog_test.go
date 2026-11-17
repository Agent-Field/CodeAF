package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// fakeStallTurn is one manager turn on a stall: it keeps what it was asked
// and says its line.
type fakeStallTurn struct {
	asked *[]string
	says  string
}

func (f *fakeStallTurn) SubmitRunnerNote(_ context.Context, text string) (<-chan session.Event, error) {
	*f.asked = append(*f.asked, text)
	ch := make(chan session.Event, 1)
	ch <- session.Event{Kind: session.EventTextDelta, Text: f.says}
	close(ch)
	return ch, nil
}
func (f *fakeStallTurn) Interrupt()   {}
func (f *fakeStallTurn) Close() error { return nil }

// stallDoor is a door the turn is lent, told apart by identity.
type stallDoor struct{ name string }

func (stallDoor) EditRun(context.Context, string, factory.RunEdit) (factory.Item, []string, error) {
	return factory.Item{}, nil, nil
}

// THE MANAGER'S TURN ON A STALL: the report says what tripped, the wait and
// offers the service stored and the job's last log lines; the turn carries the
// door it was handed and the inbox turn's wait; and what the manager said
// comes back for the job's log.
func TestTheStallTurnReportsTheStallToTheManager(t *testing.T) {
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
			return &fakeStallTurn{asked: &asked, says: "Nothing I may press moves it. Start a computer."}, nil
		},
	}
	at := time.Now().Add(-7 * time.Minute)
	it := factory.Item{ID: 3, Talk: "/x/transcript.jsonl", State: factory.StateQueued, Cap: 10,
		Stream: &factory.Stream{Phases: []factory.Phase{{Name: "write", Kind: factory.StageChat, State: factory.PhasePending}}}}
	for i := range 8 {
		it.Stream.Log = append(it.Stream.Log, factory.LogLine{At: at, Tone: "said", Text: "line " + string(rune('a'+i)) + "\nwrapped"})
	}
	factory.SettleWait(factory.Item{}, &it, at)
	st := factory.Stall{Kind: factory.StallRunner, Words: "No computer picked this job up in time.", Wait: "runner:write", At: at, NextAt: at}
	door := stallDoor{name: "the manager's own"}
	said, err := s.Stalled(context.Background(), it, st, door)
	if err != nil {
		t.Fatal(err)
	}
	if said != "Nothing I may press moves it. Start a computer." {
		t.Errorf("said = %q", said)
	}
	if lent != session.RunDoor(door) {
		t.Errorf("the turn was lent %v, want the door it was handed", lent)
	}
	if wait <= 0 || wait > stallTurnWait {
		t.Errorf("the turn may take %v, want the inbox turn's %v", wait, stallTurnWait)
	}
	if len(asked) != 1 {
		t.Fatalf("asked %d times", len(asked))
	}
	for _, want := range []string{
		"The factory's watchdog found this job stalled: No computer picked this job up in time.",
		"First seen 7 minutes ago; this is turn 1 of 4 on it.",
		"Waits on: a computer to run it.",
		"Its log's last lines: ",
		"line c wrapped | ",
		"line h wrapped",
		"Read factory_status",
		"Never merge",
		"say in one or two plain sentences what the person must decide",
	} {
		if !strings.Contains(asked[0], want) {
			t.Errorf("the report does not say %q:\n%s", want, asked[0])
		}
	}
	if strings.Contains(asked[0], "line b") || strings.Contains(asked[0], "\n") || strings.Contains(asked[0], stallAskYours) {
		t.Errorf("the report carries more than its last lines on one line:\n%s", asked[0])
	}

	// A stall already handed to the person says so, in the wait the service
	// stored and in the report's last sentence.
	st.GaveUp, st.Turns = true, 1
	it.Stall = &st
	factory.SettleWait(factory.Item{}, &it, at)
	if _, err := s.Stalled(context.Background(), it, st, door); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"this is turn 2 of 4 on it.", "Waits on: the person, to decide what happens next.", stallAskYours} {
		if !strings.Contains(asked[1], want) {
			t.Errorf("the second report does not say %q:\n%s", want, asked[1])
		}
	}
}

// A turn that cannot be given is an error, which the service reads as the
// manager not having been asked.
func TestAStallTurnThatCannotOpenIsAnError(t *testing.T) {
	refused := errors.New("the conversation could not be opened")
	s := &shapeTurns{
		dirs: func(string) string { return "" },
		open: func(context.Context, factory.Item, session.RunDoor, bool) (managerTurn, error) { return nil, refused },
	}
	it := factory.Item{ID: 3, Talk: "/x/transcript.jsonl"}
	if _, err := s.Stalled(context.Background(), it, factory.Stall{Kind: factory.StallRunner, Words: "stalled"}, nil); !errors.Is(err, refused) {
		t.Fatalf("err = %v", err)
	}
	if _, err := (*shapeTurns)(nil).Stalled(context.Background(), it, factory.Stall{}, nil); err == nil {
		t.Fatal("a factory with no manager turns gave one")
	}
}

// A quiet factory spends nothing by itself: until a person turns its runner
// on, a stall's manager turn is refused, and the service hands the stall to
// the person.
func TestAQuietFactoryGivesNoStallTurn(t *testing.T) {
	h := &appFactoryHost{quiet: true}
	if _, err := h.stalled(context.Background(), factory.Item{ID: 3}, factory.Stall{}); !errors.Is(err, errStallReadsOff) {
		t.Fatalf("err = %v", err)
	}
	if stallAge(30*time.Second) != "under a minute" || stallAge(90*time.Second) != "1 minute" || stallAge(20*time.Minute) != "20 minutes" {
		t.Fatal("ages read wrong")
	}
	if got := stallLog(factory.Item{}); got != stallAskNoLog {
		t.Fatalf("a job with no run logs %q", got)
	}
}

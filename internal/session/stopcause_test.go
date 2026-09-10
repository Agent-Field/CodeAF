package session

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── A TURN NEVER ENDS WITH NOTHING SAID ─────────────────────────────────────
//
// The replication is the shape of the 2026-09-09 defect: a turn that is
// THINKING — hidden deltas, nothing on the screen — and is then ended by
// machinery rather than by the person. There was no partial reply to keep, so
// the whole ending was a transcript with a question in it and nothing after,
// a model-call row saying `context canceled`, and an idle status line.

// thinkingStep streams reasoning nobody can read and then waits to be stopped.
// It is the measured shape: 6,174 reasoning tokens and not one visible word.
func thinkingStep(streaming chan struct{}) step {
	return func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		for range 20 {
			provider.Emit(ctx, provider.StreamReasoning, "weighing it up. ")
		}
		close(streaming)
		<-ctx.Done()
		return nil, ctx.Err()
	}
}

// stopMidThought starts a thinking turn and ends it through one door.
func stopMidThought(t *testing.T, agent *Agent, streaming chan struct{}, stop func()) []Event {
	t.Helper()
	events := mustSubmit(t, agent, "think about this for a while")
	select {
	case <-streaming:
	case <-time.After(10 * time.Second):
		t.Fatal("the scripted step never streamed")
	}
	stop()
	return collect(t, events)
}

func TestATurnEndedByMachineryIsWrittenDownAndSaidOutLoud(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	streaming := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{thinkingStep(streaming)}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SessionFile = journal })

	collected := stopMidThought(t, agent, streaming, func() { agent.InterruptFor(StopByTakeover) })

	if last := collected[len(collected)-1]; last.Kind != EventTurnDone {
		t.Fatalf("the turn ended with %v, want EventTurnDone; events were %v", last.Kind, kinds(collected))
	}
	// THE PERSON'S OWN ACCOUNT, in their words, once.
	note, told := firstOfKind(collected, EventNotice)
	if !told {
		t.Fatalf("a turn taken away by machinery said nothing at all; events were %v", kinds(collected))
	}
	if want := stopSentence(StopByTakeover); note.Text != want {
		t.Fatalf("the person was told %q, want %q", note.Text, want)
	}
	for _, banned := range []string{"context", "cancel", "takeover.json"} {
		if strings.Contains(strings.ToLower(note.Text), banned) {
			t.Errorf("the sentence a person reads carries the machine's word %q: %q", banned, note.Text)
		}
	}
	// AND THE MACHINE'S, in the journal, where an autopsy can find it.
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	rows := journaledErrors(t, journal)
	if len(rows) == 0 {
		t.Fatal("a turn that ended with no answer left no line in the journal")
	}
	found := false
	for _, row := range rows {
		if strings.Contains(row.Message, string(StopByTakeover)) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no journal line names the door that ended the turn; rows were %+v", rows)
	}
}

func TestATurnThePersonStoppedSaysNothingAboutIt(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	streaming := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{thinkingStep(streaming)}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SessionFile = journal })

	collected := stopMidThought(t, agent, streaming, agent.Interrupt)

	if last := collected[len(collected)-1]; last.Kind != EventTurnDone {
		t.Fatalf("the stopped turn ended with %v, want EventTurnDone", last.Kind)
	}
	// TELLING SOMEBODY WHAT THEY JUST PRESSED IS NOISE. The surface drew their
	// stop; the engine adds nothing.
	if note, told := firstOfKind(collected, EventNotice); told {
		t.Fatalf("the person's own stop was explained back to them: %q", note.Text)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	// AND A HEALTHY SESSION'S RECORD IS NOT A LIST OF FAILURES.
	for _, row := range journaledErrors(t, journal) {
		if strings.Contains(row.Message, "turn ended:") {
			t.Fatalf("an ordinary stop was journaled as a failed call: %+v", row)
		}
	}
}

// TestEveryDoorNamesItselfAndOnlyThePersonsIsSilent is the law over the table in
// stopcause.go: a door with no sentence is a door that leaves somebody staring
// at nothing, and there is exactly one that is allowed to.
func TestEveryDoorNamesItselfAndOnlyThePersonsIsSilent(t *testing.T) {
	doors := []StopDoor{
		StopByPerson, StopByTakeover, StopByLeaving, StopByClosing,
		StopByAbandoned, StopByWorkStopped, StopByRetired,
	}
	seen := map[string]StopDoor{}
	for _, door := range doors {
		if door == "" {
			t.Fatal("a door with no machine word is a door nobody can autopsy")
		}
		if other, already := seen[string(door)]; already {
			t.Fatalf("%q and %q are the same word", door, other)
		}
		seen[string(door)] = door
		said := stopSentence(door)
		if door == StopByPerson {
			if said != "" {
				t.Errorf("the person's own stop was explained back to them: %q", said)
			}
			continue
		}
		if strings.TrimSpace(said) == "" {
			t.Errorf("%q leaves the person with nothing said", door)
		}
		// THE WORDS ARE THE PERSON'S. The door's own machine word never
		// reaches them.
		if strings.Contains(said, string(door)) {
			t.Errorf("%q says its own machine word to a person: %q", door, said)
		}
	}
	// A cause reads back as the door it was made for, and a plain cancellation
	// is not one of ours.
	for _, door := range doors {
		if got, ok := StoppedBy(stopFor(door)); !ok || got != door {
			t.Errorf("a %q cause read back as %q (%v)", door, got, ok)
		}
	}
	if _, ok := StoppedBy(context.Canceled); ok {
		t.Error("a plain cancellation was read as one of this package's doors")
	}
	// AND A CAUSE IS STILL A CANCELLATION. Every caller that only asks whether
	// the turn was cancelled must keep its answer.
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(stopFor(StopByTakeover))
	if ctx.Err() != context.Canceled {
		t.Errorf("a cancelled context reports %v", ctx.Err())
	}
	if door, stopped := stopCause(ctx); !stopped || door != StopByTakeover {
		t.Errorf("the door read back as %q (%v)", door, stopped)
	}
	// A context nobody named a door on reads as the session going away, which
	// is the conservative answer: nothing is going to be re-asked.
	plain, stop := context.WithCancel(context.Background())
	stop()
	if door, stopped := stopCause(plain); !stopped || door != StopByClosing {
		t.Errorf("an unnamed cancellation read as %q (%v)", door, stopped)
	}
}

package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE REPLY A PERSON STOPS IS JUDGED THE WAY A REPLY THE GUARD STOPS IS.
//
// On 2026-09-01 a serving endpoint looped the model's own `</think>` into the
// answer, the person watched fourteen seconds of it and pressed esc, and the
// whole run was kept as their assistant's reply — in the transcript, in the
// journal, and in every request after. These pin the other outcome: soup that
// was stopped by hand is not kept, the person is told in the guard's own
// words, and a stopped reply that was still language is kept exactly as
// before (TestInterruptKeepsPartialReply is that half).

// soupStep streams three kilobytes of one token and then waits to be stopped.
func soupStep(streaming chan struct{}) step {
	return func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		for range 400 {
			provider.Emit(ctx, provider.StreamDelta, "</think>")
		}
		close(streaming)
		<-ctx.Done()
		return nil, ctx.Err()
	}
}

func stopMidSoup(t *testing.T, agent *Agent, streaming chan struct{}) []Event {
	t.Helper()
	events := mustSubmit(t, agent, "go")
	select {
	case <-streaming:
	case <-time.After(10 * time.Second):
		t.Fatal("the scripted step never streamed")
	}
	agent.Interrupt()
	return collect(t, events)
}

func TestAStoppedReplyThatLostItsThreadIsNotKept(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	streaming := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{soupStep(streaming)}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SessionFile = journal })

	collected := stopMidSoup(t, agent, streaming)
	if last := collected[len(collected)-1]; last.Kind != EventTurnDone {
		t.Fatalf("interrupted turn ended with %v, want EventTurnDone", last.Kind)
	}
	note, told := firstOfKind(collected, EventNotice)
	if !told || !strings.Contains(note.Text, "lost its thread") {
		t.Fatalf("the person was not told the stopped reply was dropped; events were %v", kinds(collected))
	}
	if message := lastMessage(agent); message.Role != "user" || messageText(message) != "go" {
		t.Fatalf("last message = %s/%q, want the person's own question with nothing kept after it", message.Role, messageText(message))
	}
	// A second Submit must be accepted: the turn is over, not wedged.
	again, err := agent.Submit(context.Background(), "never mind")
	if err != nil {
		t.Fatalf("submit after the stopped soup: %v", err)
	}
	collect(t, again)
	// AND NOT IN THE JOURNAL EITHER: a resumed conversation must not find the
	// soup that the live one refused.
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "</think></think>") {
		t.Fatal("the stopped soup reached the session journal")
	}
}

// With the reply guard off, the person sees whatever arrives and keeps
// whatever they stop — the same promise the guard's own switch makes.
func TestAStoppedReplyIsKeptWhenTheGuardIsOff(t *testing.T) {
	streaming := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{soupStep(streaming)}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.ReplyGuardOff = true })

	collected := stopMidSoup(t, agent, streaming)
	if _, told := firstOfKind(collected, EventNotice); told {
		t.Fatal("the guard is off, and the keep path still judged the reply")
	}
	if message := lastMessage(agent); message.Role != "assistant" || !strings.HasPrefix(messageText(message), "</think>") {
		t.Fatalf("last message = %s/%q, want the streamed text kept verbatim", message.Role, messageText(message))
	}
}

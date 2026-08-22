package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A MARKED DRAFT IS AN ORDER TO PROPOSE, AND NEVER WORK TO DO.
//
// The recognition half of this fails silently — the model does the sentence once
// and nothing says a rule was not made — so the whole value of the gesture is
// that the model is TOLD rather than asked to notice. This is that fact, tested
// where it is true: in the messages the turn actually reasons from.
func TestAMarkedDraftReachesTheModelAsAnOrderToPropose(t *testing.T) {
	store := newFakeStanding(t)
	completer := &scriptedCompleter{steps: []step{
		standCall("s1", aReminder()),
		finalText("that stands now"),
	}}
	agent := standingAgent(t, completer, store, nil)

	events, err := agent.SubmitStanding(context.Background(), "always run the tests before you say you are done")
	if err != nil {
		t.Fatalf("SubmitStanding: %v", err)
	}
	drainAnsweringStanding(t, events, func(event Event) {
		agent.ResolveStanding(event.Standing.ID, StandingAnswer{Approved: true})
	})

	opening := firstUserText(t, completer)
	if !strings.Contains(opening, standingMarkInstruction) {
		t.Fatalf("the model was not told the sentence was marked:\n%s", opening)
	}
	if !strings.HasSuffix(opening, "always run the tests before you say you are done") {
		t.Fatalf("the person's own sentence is not what the instruction leads to:\n%s", opening)
	}
	// AND THE ORDINARY ROAD IS UNCHANGED FROM THERE: the card was drawn, the yes
	// was answered, and what it created is the item the model shaped.
	if len(store.created) != 1 {
		t.Fatalf("a marked draft created %d items, want the one it proposed", len(store.created))
	}
}

// AND THE INSTRUCTION SAYS BOTH HALVES: shape it, and do not do it.
//
// It is asserted as a fact about the sentence rather than about a model's
// behaviour, because no scripted completer can be made to disobey — what this
// build controls is what it says, and a version of it that quietly lost the
// refusal would leave the gesture with no teeth at all.
func TestTheMarkedInstructionForbidsDoingItOnce(t *testing.T) {
	for _, phrase := range []string{"op=propose", "not something that can stand", "ONE short line"} {
		if !strings.Contains(standingMarkInstruction, phrase) {
			t.Errorf("the marked instruction no longer says %q:\n%s", phrase, standingMarkInstruction)
		}
	}
	if !strings.Contains(standingMarkInstruction, "Do NOT carry the sentence out as one-off work") {
		t.Errorf("the marked instruction no longer forbids doing it once:\n%s", standingMarkInstruction)
	}
}

// A SENTENCE THAT CANNOT STAND ANSWERS ONE LINE AND STANDS NOTHING. There is no
// card, no item and no second attempt — which is the ending the instruction asks
// for, and the ending this test pins.
func TestAMarkedDraftThatCannotStandStandsNothing(t *testing.T) {
	store := newFakeStanding(t)
	completer := &scriptedCompleter{steps: []step{
		finalText("that is a question about right now — there is no condition in it to keep true."),
	}}
	agent := standingAgent(t, completer, store, nil)

	events, err := agent.SubmitStanding(context.Background(), "what time is it?")
	if err != nil {
		t.Fatalf("SubmitStanding: %v", err)
	}
	collected := collect(t, events)
	if _, found := firstOfKind(collected, EventStandingProposal); found {
		t.Fatal("a sentence that cannot stand drew a card anyway")
	}
	if len(store.created) != 0 {
		t.Fatalf("a sentence that cannot stand created %d items", len(store.created))
	}
}

// THE PERSON NEVER SEES THE INSTRUCTION. The journal — what a resume replays,
// what an export writes, what the transcript on screen is drawn from — holds
// their own sentence and nothing else.
func TestAMarkedDraftJournalsThePersonsOwnWords(t *testing.T) {
	store := newFakeStanding(t)
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: []step{finalText("noted")}}
	agent := standingAgent(t, completer, store, func(config *Config) {
		config.SessionFile = path
	})

	events, err := agent.SubmitStanding(context.Background(), "never commit straight to main here")
	if err != nil {
		t.Fatalf("SubmitStanding: %v", err)
	}
	collect(t, events)
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the journal was never written: %v", err)
	}
	written := string(raw)
	if !strings.Contains(written, "never commit straight to main here") {
		t.Fatalf("the journal lost the person's sentence:\n%s", written)
	}
	if strings.Contains(written, "MARKED the sentence below") {
		t.Fatalf("the journal kept the instruction the person never typed:\n%s", written)
	}
}

// AND A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN. With no store behind
// the ambient side there is no `stand` verb on the belt, so the door refuses
// before anything is journaled rather than opening a turn that could only
// apologise.
func TestAMarkedDraftIsRefusedWhereNothingCanHoldOne(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	events, err := agent.SubmitStanding(context.Background(), "always run the tests")
	if err == nil {
		t.Fatal("a session with no ambient side took a marked draft")
	}
	if events != nil {
		t.Fatal("a refused marked draft opened a stream")
	}
	if err.Error() != standingMarkAbsent {
		t.Fatalf("refusal = %q, want %q", err.Error(), standingMarkAbsent)
	}
	if len(agent.Transcript()) != 0 {
		t.Fatalf("a refused marked draft was recorded anyway: %+v", agent.Transcript())
	}
}

// firstUserText is the first thing the model was handed that the person is
// supposed to have said.
func firstUserText(t *testing.T, completer *scriptedCompleter) string {
	t.Helper()
	completer.mu.Lock()
	defer completer.mu.Unlock()
	if len(completer.seen) == 0 {
		t.Fatal("the model was never called")
	}
	for _, message := range completer.seen[0] {
		if message.Role == "user" {
			return messageContentText(ai.Message{Role: message.Role, Content: message.Content})
		}
	}
	t.Fatal("the first request carried no user message")
	return ""
}

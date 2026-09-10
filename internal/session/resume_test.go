package session

// resume_test.go is the law over resume.go: a question that was never answered
// is asked again by the window that took the conversation, ONCE, and only when
// the journal really ends in the shape that says nothing was answered.
//
// The replication is #760's own: a turn that is THINKING — hidden deltas,
// nothing on the screen, nothing to keep as a partial — ended by another window
// taking the conversation over. It is run end to end, through two agents on one
// file, because the whole point of the design is that nothing passes between
// the two windows except that file.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// takenOverMidThought runs one thinking turn on a journal and has another window
// take the conversation, leaving the file in the shape resume.go acts on. It
// hands back the journal's path with nothing holding its lock.
func takenOverMidThought(t *testing.T, stop func(*Agent)) string {
	t.Helper()
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	streaming := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{thinkingStep(streaming)}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SessionFile = journal })
	stopMidThought(t, agent, streaming, func() { stop(agent) })
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	return journal
}

// reopened is a second window opening that journal, with a model that answers.
func reopened(t *testing.T, journal, answer string) *Agent {
	t.Helper()
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse(answer), nil },
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SessionFile = journal })
	return agent
}

// personSaid is every line of the journal the PERSON typed, in order. It is what
// proves a resumed question was not written down a second time.
func personSaid(t *testing.T, journal string) []string {
	t.Helper()
	var said []string
	for _, entry := range journaledEntries(t, journal, "message") {
		if entry.Role == "user" && !entry.Note {
			said = append(said, entry.Content)
		}
	}
	return said
}

// THE DEFECT, END TO END. A turn that had only been thinking is taken over, the
// window that asked for the conversation opens it, and the person's question is
// answered there without them typing anything.
func TestAQuestionTakenOverMidThoughtIsAskedAgainByTheWindowThatTookIt(t *testing.T) {
	journal := takenOverMidThought(t, func(a *Agent) { a.InterruptFor(StopByTakeover) })
	agent := reopened(t, journal, "here is the answer")

	events, resumed := agent.ResumeStoppedTurn(context.Background())
	if !resumed {
		t.Fatal("a conversation that arrived on an unanswered question did not ask it again")
	}
	collected := collect(t, events)
	if last := collected[len(collected)-1]; last.Kind != EventTurnDone {
		t.Fatalf("the resumed turn ended with %v, want EventTurnDone; events were %v", last.Kind, kinds(collected))
	}
	if last := lastMessage(agent); last.Role != "assistant" ||
		!strings.Contains(messageContentText(last), "here is the answer") {
		t.Fatalf("the resumed turn left %q as the last message", messageContentText(last))
	}

	// ASKED ONCE. A window may attach to a conversation more than once — the
	// switcher brings it forward, the keeper hands it back — and a second ask
	// would be a second answer and a second bill for one thing the person typed.
	if _, again := agent.ResumeStoppedTurn(context.Background()); again {
		t.Fatal("the same unanswered question was asked a second time")
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}

	// AND THE PERSON SAID IT ONCE. The transcript that arrives must not show
	// somebody asking the same thing twice.
	said := personSaid(t, journal)
	if len(said) != 1 {
		t.Fatalf("the journal holds %d lines of the person's own, want 1: %q", len(said), said)
	}
}

// A PERSON'S OWN STOP IS NEVER ASKED AGAIN. They stopped it; asking it again is
// the program overruling them and spending their money to do it.
func TestATurnThePersonStoppedIsNeverAskedAgain(t *testing.T) {
	journal := takenOverMidThought(t, func(a *Agent) { a.Interrupt() })
	agent := reopened(t, journal, "an answer nobody asked for")
	if _, resumed := agent.ResumeStoppedTurn(context.Background()); resumed {
		t.Fatal("a turn the person stopped themselves was asked again")
	}
}

// AND NEITHER IS A TURN THAT HAD ALREADY SAID SOMETHING. Whatever arrived is in
// the transcript that comes with the conversation, and the work it started —
// tasks, writes, commands — is resumed from its checkpoint rather than run a
// second time.
func TestATurnThatHadAlreadyAnsweredIsNotAskedAgain(t *testing.T) {
	journal := filepath.Join(t.TempDir(), "session.jsonl")
	streaming := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		// Visible words, and then the wait to be stopped. What streamed is kept
		// ([Agent.keepPartial]), so the journal's tail is an assistant message
		// and not the person's question.
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			provider.Emit(ctx, provider.StreamDelta, "half an answer, and then ")
			close(streaming)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SessionFile = journal })
	stopMidThought(t, agent, streaming, func() { agent.InterruptFor(StopByTakeover) })
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}

	next := reopened(t, journal, "a second answer")
	if _, resumed := next.ResumeStoppedTurn(context.Background()); resumed {
		t.Fatal("a turn that had already written part of its reply was asked again")
	}
}

// ── the shape itself ────────────────────────────────────────────────────────

// TestOnlyOneJournalShapeIsAskedAgain reads the decision where it is made. Each
// case is a whole journal, written the way the session writes one, and the
// question is only ever what the file's tail says.
func TestOnlyOneJournalShapeIsAskedAgain(t *testing.T) {
	const asked = `{"type":"message","role":"user","content":"what is this"}`
	const takeover = `{"type":"error","error":{"door":"taken over","message":"turn ended: taken over"}}`
	const named = `{"type":"title","title":"what this is"}`
	for _, probe := range []struct {
		name  string
		lines []string
		want  StopDoor
	}{
		{"a question and a takeover", []string{asked, takeover, named}, StopByTakeover},
		{"a provider's refusal on the way there", []string{asked,
			`{"type":"error","error":{"status":503,"message":"upstream is down"}}`, takeover}, StopByTakeover},
		{"another door, named but not asked again", []string{asked,
			`{"type":"error","error":{"door":"conversation left"}}`}, StopByLeaving},
		{"nothing stopped it", []string{asked}, ""},
		{"the person's own stop writes no row", []string{asked, named}, ""},
		{"words arrived before the stop", []string{asked,
			`{"type":"message","role":"assistant","content":"half an answer"}`, takeover}, ""},
		{"a tool was called before the stop", []string{asked,
			`{"type":"message","role":"assistant","content":"","toolCalls":[{"id":"c1","type":"function","function":{"name":"read","arguments":"{}"}}]}`,
			takeover}, ""},
		{"the session's own note is not the person asking", []string{
			`{"type":"message","role":"user","content":"a task landed","note":true}`, takeover}, ""},
		{"a rewind took the tail back", []string{asked, takeover, `{"type":"rewind","dropped":1}`}, ""},
		{"the question was answered and a new one asked", []string{asked, takeover, asked}, ""},
	} {
		t.Run(probe.name, func(t *testing.T) {
			replayed, err := readJournal(strings.NewReader(strings.Join(probe.lines, "\n")+"\n"), "session.jsonl", true)
			if err != nil {
				t.Fatalf("readJournal: %v", err)
			}
			if replayed.stopped != probe.want {
				t.Fatalf("the journal's tail read as %q, want %q", replayed.stopped, probe.want)
			}
		})
	}
}

// ONE DOOR IS ASKED AGAIN AND EVERY OTHER ONE WAITS TO BE ASKED. A door added to
// stopcause.go tomorrow has to be thought about rather than defaulting into
// spending somebody's money, which is why [resumesUnanswered] is a switch.
func TestOnlyTheTakeoverAsksTheQuestionAgain(t *testing.T) {
	for _, door := range []StopDoor{
		StopByPerson, StopByLeaving, StopByClosing,
		StopByAbandoned, StopByWorkStopped, StopByRetired,
	} {
		if resumesUnanswered(door) {
			t.Errorf("%q asks the question again in a window nobody is sitting in front of", door)
		}
	}
	if !resumesUnanswered(StopByTakeover) {
		t.Error("the one door somebody is demonstrably waiting at does not ask again")
	}
	// THE WORDS ARE THE PERSON'S, and they say what happened and what is being
	// done about it — [cutShortNotice]'s register, not the machine's.
	if !strings.Contains(ResumedWord, "asking again") {
		t.Errorf("the resumed reply's line does not say what is happening: %q", ResumedWord)
	}
	for _, banned := range []string{"takeover", "cancel", "context", "resume"} {
		if strings.Contains(strings.ToLower(ResumedWord), banned) {
			t.Errorf("the sentence a person reads carries the machine's word %q: %q", banned, ResumedWord)
		}
	}
}

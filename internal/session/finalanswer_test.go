package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// THE FINAL ANSWER, END TO END (#225).
//
// These run against the real adapter over a scripted SSE stream, so what is
// pinned is the whole path a reply takes: the wire, internal/provider's one
// decision about what is answer and what is working (answer.go), this package's
// events, and the message the transcript keeps. THE LAST TWO MUST AGREE — what
// a replay shows later has to be what streaming showed live, and the two
// disagreeing is the report's own symptom.

// eventTexts is every delta of one kind, joined.
func eventTexts(events []Event, kind EventKind) string {
	var b strings.Builder
	for _, event := range events {
		if event.Kind == kind {
			b.WriteString(event.Text)
		}
	}
	return b.String()
}

// A TURN WHOSE ONLY WORDS ARRIVED ON THE REASONING FIELD STILL ANSWERED. The
// shape is real — a response with no content deltas at all and a long
// `reasoning` — and read as an empty reply it is a turn that spends money,
// retries, and tells the person nothing ([turnBroke]).
func TestAReplyThatArrivedOnlyAsReasoningIsStreamedAndRecordedAsTheAnswer(t *testing.T) {
	client, err := provider.NewClient(provider.Config{
		APIKey: "k", BaseURL: "http://provider.test", Model: "test/model",
		HTTPClient: fakeHTTP(eventStreamOf(
			`{"choices":[{"index":0,"delta":{"role":"assistant","reasoning":"A semaphore counts "}}]}`,
			`{"choices":[{"index":0,"delta":{"reasoning":"what is available."},"finish_reason":"stop"}]}`,
		)),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	agent, _ := newTestAgent(t, client, nil)

	collected := collect(t, mustSubmit(t, agent, "what is a semaphore?"))

	const want = "A semaphore counts what is available."
	if got := eventTexts(collected, EventTextDelta); got != want {
		t.Fatalf("the surface was streamed %q, want the reply %q", got, want)
	}
	if got := messageText(lastMessage(agent)); got != want {
		t.Fatalf("the transcript kept %q, want %q", got, want)
	}
}

// AND THE WORKING FENCED INSIDE THE ANSWER IS WORKING. A gateway that does not
// split the channels sends `<think>…</think>` in `content`; neither the tag nor
// what it holds may reach the person's answer or the transcript.
func TestWorkingFencedInTheAnswerNeverReachesTheTranscript(t *testing.T) {
	client, err := provider.NewClient(provider.Config{
		APIKey: "k", BaseURL: "http://provider.test", Model: "test/model",
		HTTPClient: fakeHTTP(eventStreamOf(
			`{"choices":[{"index":0,"delta":{"role":"assistant","content":"<think>the user wants "}}]}`,
			`{"choices":[{"index":0,"delta":{"content":"a definition</think>\n\nA semaphore "}}]}`,
			`{"choices":[{"index":0,"delta":{"content":"counts."},"finish_reason":"stop"}]}`,
		)),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	agent, _ := newTestAgent(t, client, nil)

	collected := collect(t, mustSubmit(t, agent, "what is a semaphore?"))

	const want = "A semaphore counts."
	if got := eventTexts(collected, EventTextDelta); got != want {
		t.Fatalf("the surface was streamed %q, want %q", got, want)
	}
	if got := eventTexts(collected, EventReasoning); got != "the user wants a definition" {
		t.Fatalf("the working reached the surface as %q", got)
	}
	if got := messageText(lastMessage(agent)); got != want {
		t.Fatalf("the transcript kept %q, want %q", got, want)
	}
	for _, message := range messagesOf(agent) {
		if strings.Contains(messageText(message), "<think") {
			t.Fatalf("the tag reached the transcript as a %s message", message.Role)
		}
	}
}

// AND WORKING CARVED OUT OF THE ANSWER CHANNEL IS NEVER REPLAYED. It arrived
// under no reasoning field, so there is none to send it back under, and the
// encoder refuses an unnamed field outright rather than guess — which would
// fail the very next request of the conversation
// ([provider.MessageReasoning], [reasoningBuffer.write]).
func TestWorkingCarvedOutOfTheAnswerIsNotReplayedToTheModel(t *testing.T) {
	client, err := provider.NewClient(provider.Config{
		APIKey: "k", BaseURL: "http://provider.test", Model: "test/model",
		HTTPClient: fakeHTTP(eventStreamOf(
			`{"choices":[{"index":0,"delta":{"role":"assistant","content":"<think>working</think>A semaphore counts."},"finish_reason":"stop"}]}`,
		)),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	agent, _ := newTestAgent(t, client, nil)
	collect(t, mustSubmit(t, agent, "what is a semaphore?"))

	agent.mu.Lock()
	defer agent.mu.Unlock()
	for index, carried := range agent.messageReasoning {
		if carried.Text != "" && carried.Field == "" {
			t.Fatalf("message %d carries working with no wire field to replay it under: %q",
				index, carried.Text)
		}
	}
}

// AND THE GATES ARE OUTSIDE IT. A turn asks for prose; the judges and
// checkpoints the loop calls afterwards ask for a value, and a value that never
// arrived is missing rather than hidden in the working. The mark is set on the
// turn's own request and nowhere wider, which is what keeps the two apart
// ([Agent.completeWithRetryReasoning], [provider.WithProseAnswer]).
func TestOnlyTheTurnsOwnRequestAsksForAProseAnswer(t *testing.T) {
	if provider.ProseAnswerAsked(context.Background()) {
		t.Fatalf("a bare context already asks for a prose answer")
	}
	var asked []bool
	completer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			asked = append(asked, provider.ProseAnswerAsked(ctx))
			return textResponse("A semaphore counts."), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "what is a semaphore?"))

	if len(asked) != 1 || !asked[0] {
		t.Fatalf("the turn's own request asked for a prose answer: %v", asked)
	}
}

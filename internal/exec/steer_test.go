package exec

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// steeredCompleter answers the ask it can actually see. It is the smallest
// honest stand-in for a model: give it a transcript mentioning the sea and it
// writes about the sea, otherwise it writes about mountains. Nothing about the
// executor is stubbed — the real loop decides whether the redirect was ever
// shown to it, which is the entire question.
type steeredCompleter struct {
	calls int
	seen  [][]ai.Message
}

const (
	mountainPoem = "POEM: the ridge holds the last of the light."
	seaPoem      = "POEM: the swell holds the last of the light."
)

func (s *steeredCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	s.calls++
	s.seen = append(s.seen, append([]ai.Message(nil), messages...))
	answer := mountainPoem
	for _, message := range messages {
		if message.Role != "user" {
			continue
		}
		for _, part := range message.Content {
			if strings.Contains(strings.ToLower(part.Text), "about the sea") {
				answer = seaPoem
			}
		}
	}
	return &ai.Response{
		Choices: []ai.Choice{{
			Message:      ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: answer}}},
			FinishReason: "stop",
		}},
		Usage: &ai.Usage{PromptTokens: 10, CompletionTokens: 5},
	}, nil
}

// The oldest confirmed gap in the suite, as a unit: commission a poem about
// mountains, say "make it about the sea" while it runs, and receive a poem
// about mountains.
//
// The redirect machinery was never the problem. The words were journaled, the
// broadcast put them in the leaf's mailbox, and the receipt said so — but
// steering was polled only at the TOP of a turn, so a leaf could hear the user
// exclusively during work it had not finished. Words that arrived while its
// final turn was in flight reached a worker that had already written its answer
// and was one statement away from handing it over. The mailbox got them; the
// only reader had stopped reading.
//
// A leaf that has not landed has not delivered. So the deliverable asserted here
// is the post-redirect one, not an acknowledgement that a redirect existed.
func TestARedirectArrivingMidFlightChangesTheDeliverable(t *testing.T) {
	client := &steeredCompleter{}
	linear := NewLinear(client, workspace(t), nil, 10, 1_000_000, time.Minute)

	// The user speaks after the first answer is written and before it lands —
	// exactly the window the old poll could not see into.
	polls, delivered := 0, false
	steer := func() []string {
		polls++
		if polls >= 2 && !delivered {
			delivered = true
			return []string{"redirection from the user: make it about the sea"}
		}
		return nil
	}

	outcome, err := linear.Run(context.Background(), Task{
		NodeID: 1, Brief: "write a two-line poem about mountains", Steer: steer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Text != seaPoem {
		t.Fatalf("delivered %q, want the post-redirect %q — the words reached the mailbox, not the work",
			outcome.Text, seaPoem)
	}
	if outcome.Steered != 1 {
		t.Fatalf("outcome.Steered = %d, want the one line the user said", outcome.Steered)
	}
	if client.calls < 2 {
		t.Fatalf("the leaf made %d model calls — it landed without ever being told", client.calls)
	}
	// And the redirect reached the model as the user's own words, after the
	// draft it is correcting rather than instead of it.
	last := client.seen[len(client.seen)-1]
	var sawDraft, sawRedirect bool
	for _, message := range last {
		for _, part := range message.Content {
			if message.Role == "assistant" && strings.Contains(part.Text, mountainPoem) {
				sawDraft = true
			}
			if message.Role == "user" && strings.Contains(part.Text, "make it about the sea") {
				sawRedirect = sawDraft
			}
		}
	}
	if !sawRedirect {
		t.Fatalf("the final turn did not see the draft followed by the redirect: %+v", last)
	}
}

// The other side of the same boundary: a leaf nobody steered must land on its
// first answer, exactly as it always did. The recheck buys a redirect its
// chance; it must not buy every leaf an extra turn.
func TestAnUnsteeredLeafStillLandsOnItsFirstAnswer(t *testing.T) {
	client := &steeredCompleter{}
	linear := NewLinear(client, workspace(t), nil, 10, 1_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{
		NodeID: 1, Brief: "write a two-line poem about mountains",
		Steer: func() []string { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Text != mountainPoem || outcome.Steered != 0 {
		t.Fatalf("text=%q steered=%d, want the first answer and no steering", outcome.Text, outcome.Steered)
	}
	if client.calls != 1 {
		t.Fatalf("an unsteered leaf made %d model calls, want exactly 1", client.calls)
	}
}

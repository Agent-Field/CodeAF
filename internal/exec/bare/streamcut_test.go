package bare

// A CUT STREAM IS ASKED AGAIN HERE TOO.
//
// A stream the guard cut is not a torn connection and not a refusal: its text
// matches no retryable pattern, so before this it fell straight through to the
// "this will never work" branch and one quiet endpoint ended a whole run. The
// budget is internal/session's, spelled the same way (loop.go says why it is
// copied rather than shared).

import (
	"context"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// cutCompleter answers with a cut until it has been asked `until` times, then
// answers properly.
type cutCompleter struct {
	asked    int
	until    int
	rerouted bool
	reason   provider.CutReason
}

func (c *cutCompleter) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	c.asked++
	if c.asked <= c.until {
		return nil, &provider.StreamCut{Reason: c.reason, Rerouted: c.rerouted}
	}
	return &ai.Response{Choices: []ai.Choice{{
		Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "done"}}},
	}}}, nil
}

func TestABareRunSurvivesAStreamThatWentQuiet(t *testing.T) {
	for _, tc := range []struct {
		name     string
		reason   provider.CutReason
		rerouted bool
		cuts     int
		want     int
	}{
		// The ledger routed the next attempt somewhere else, so the full budget
		// is worth spending: two retries, three requests in all.
		{"a silence the ledger routed around", provider.CutSilent, true, 2, 3},
		// Nothing was routed away, so a third attempt could only land in the same
		// place and the loop stops asking a try earlier.
		{"a silence that routed nowhere", provider.CutSilent, false, 1, 2},
		// Soup is about the transcript, not the endpoint, and gets one retry
		// either way.
		{"a reply that came apart", provider.CutBabble, true, 1, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			completer := &cutCompleter{until: tc.cuts, rerouted: tc.rerouted, reason: tc.reason}
			loop := &loopState{client: completer}
			response, err := loop.completeWithRetry(context.Background(), nil)
			if err != nil {
				t.Fatalf("a cut ended the run: %v", err)
			}
			if response == nil {
				t.Fatal("no response")
			}
			if completer.asked != tc.want {
				t.Fatalf("requests = %d, want %d", completer.asked, tc.want)
			}
		})
	}
}

// AND THE BUDGET IS A BUDGET. A stream that never comes back ends the run, on
// the guard's own sentence rather than on a torn-connection error.
func TestABareRunGivesUpOnAStreamThatNeverComesBack(t *testing.T) {
	completer := &cutCompleter{until: 99, rerouted: true, reason: provider.CutSilent}
	loop := &loopState{client: completer}
	if _, err := loop.completeWithRetry(context.Background(), nil); err == nil {
		t.Fatal("a stream that never answered did not end the run")
	} else if _, isCut := provider.CutFrom(err); !isCut {
		t.Fatalf("the run ended on %v, want the guard's own cut", err)
	}
	if completer.asked != silentRetries+1 {
		t.Fatalf("requests = %d, want %d", completer.asked, silentRetries+1)
	}
}

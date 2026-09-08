package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// narratorSays runs ONE narrator call with a scripted answer and returns the
// caption events it produced and how many times the provider was asked.
//
// It calls [Agent.maybeCaption] directly, the way the dwell goroutine does, so
// the assertions are about the ANSWER and not about a batch's timing.
func narratorSays(t *testing.T, answer string, calls []ai.ToolCall) ([]Event, int) {
	t.Helper()
	client := &scriptedCompleter{}
	asked := answerTheNarrator(client, answer)
	agent, _ := newTestAgent(t, client, nil)
	hub := newEventHub()
	defer hub.close()

	agent.maybeCaption(withEpisode(context.Background(), agent.newEpisode()), hub, calls, nil)

	hub.mu.Lock()
	defer hub.mu.Unlock()
	var captions []Event
	for _, event := range hub.backlog {
		if event.Kind == EventCaption {
			captions = append(captions, event)
		}
	}
	return captions, asked()
}

// NOTHING THE NARRATOR CAN SAY PUTS THE FORMAT ON THE FRAME.
//
// The prefix is machinery — it exists so the surface can pick a mark — and the
// caption row is the one line on this surface that may not carry any. So every
// shape the model can produce is checked at the door the event actually leaves
// by, rather than only at the parser: the bar, the label, and a label with no
// sentence behind it must never reach a person's screen.
//
// AND NOTHING RETRIES. Each case asks exactly once, whatever it got back; the
// call was already reserved before the provider was reached ([episode.reserveCaption]),
// so a refusal costs one call and asks no follow-up question.
func TestNoNarratorAnswerLeavesTheFormatOnTheFrame(t *testing.T) {
	calls := []ai.ToolCall{fixBash("sleep 30")}
	for _, c := range []struct {
		name     string
		answer   string
		text     string
		category ActionCategory
		// ownBar marks the one case whose bar belongs to the SENTENCE rather
		// than to the format, so the no-format-on-the-frame sweep below knows
		// the difference between machinery and somebody's own words.
		ownBar bool
	}{
		{
			name:     "the format as asked for",
			answer:   "run | starting the local server",
			text:     "starting the local server",
			category: ActionRun,
		},
		{
			name:     "a label that is not a family — the words survive, the label does not",
			answer:   "investigate | reading the caption renderer",
			text:     "reading the caption renderer",
			category: "",
		},
		{
			// THE SHAPE THAT USED TO LEAK. `run |` parsed as neither a family nor
			// prose, and the raw line went through cleanCaption unchanged — so
			// the step's title on screen was the two characters `run |`.
			name:   "the format with no sentence behind it",
			answer: "run |",
		},
		{
			name:   "a label attempt that is not a family, with no sentence",
			answer: "investigating |",
		},
		{
			name:   "the family word alone",
			answer: "run",
		},
		{
			name:   "nothing at all",
			answer: "   ",
		},
		{
			// THE LEGACY ANSWER, and it must be untouched: this is what every
			// model that ignores the new clause produces, and what every caption
			// looked like before the clause existed.
			name:   "a plain sentence with no format at all",
			answer: "ranking bugs by end-result quality",
			text:   "ranking bugs by end-result quality",
		},
		{
			name:   "prose that happens to contain a bar",
			answer: "piping the log through grep | sort",
			text:   "piping the log through grep | sort",
			ownBar: true,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			captions, asked := narratorSays(t, c.answer, calls)
			if asked != 1 {
				t.Fatalf("the narrator was asked %d times, want exactly one and no retry", asked)
			}
			if c.text == "" {
				if len(captions) != 0 {
					t.Fatalf("a refused answer still drew %q", captions[0].Text)
				}
				return
			}
			if len(captions) != 1 {
				t.Fatalf("%d caption events, want 1", len(captions))
			}
			if got := captions[0].Text; got != c.text {
				t.Fatalf("the caption reads %q, want %q", got, c.text)
			}
			if got := captions[0].Category; got != c.category {
				t.Fatalf("the family is %q, want %q", got, c.category)
			}
			if !c.ownBar && strings.ContainsRune(captions[0].Text, '|') {
				t.Fatalf("the format's bar reached the frame: %q", captions[0].Text)
			}
		})
	}
}

// EVERY CAPTION NAMES THE STEP IT IS ABOUT.
//
// The anchor is the batch's first call, and it is what a surface keys on instead
// of "the newest tool row". Without it a late answer — this goroutine can pass
// its cancellation check and then be descheduled past the end of its own batch —
// retitles whatever step happens to be running when it lands.
func TestACaptionEventCarriesItsBatchAnchor(t *testing.T) {
	batch := []ai.ToolCall{
		{ID: "call-first", Type: "function", Function: ai.ToolCallFunction{Name: "grep", Arguments: `{"pattern":"x"}`}},
		{ID: "call-second", Type: "function", Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"a.go"}`}},
	}
	captions, _ := narratorSays(t, "search | looking for the caller", batch)
	if len(captions) != 1 {
		t.Fatalf("%d caption events, want 1", len(captions))
	}
	if got := captions[0].CallID; got != "call-first" {
		t.Fatalf("the caption is anchored to %q, want the batch's first call", got)
	}
}

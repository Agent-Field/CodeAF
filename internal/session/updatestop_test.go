package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── a turn that stops on its own interim update ─────────────────────────────

// A TURN WHOSE LAST REPLY OPENS WITH THE INTERIM MARKER AND CALLS NOTHING IS
// SENT BACK TO WORK. This is the measured shape from the connected-canvas eval
// (run 5, board-deck): the model set up five empty slide frames, wrote
// "[update] … Next I write each slide's HTML in one pass", and the turn sealed
// with every slide blank. The marker is the protocol's own word for "not the
// end", so the loop reads it and carries on without spending a reader, on a
// typed turn the end-of-turn reader never looks at.
func TestATurnThatStopsOnItsOwnUpdateIsSentBackToWork(t *testing.T) {
	recorder := &blockRecorder{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "read", `{"path":"notes.md"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("[update] The canvas already holds the facts I need and the five slide frames are in place. Next I write each slide's HTML in one pass."), nil
		},
		// The nudge lands and the model does the work it announced.
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c2", "read", `{"path":"slides.md"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("The five slides are written."), nil
		},
	}}
	agent, _ := newTestAgent(t, recorder, nil)

	events, err := agent.Submit(context.Background(), "5 slide deck for the board meeting")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if got := recorder.requests(); got != 4 {
		t.Fatalf("the loop made %d requests; want 4 — a call, the update, the call it announced, the answer", got)
	}
	transcript := transcriptText(agent)
	if n := strings.Count(transcript, checkpointUpdateNudgeLead); n != 1 {
		t.Errorf("the turn was sent back %d times; want once:\n%s", n, transcript)
	}
	if !saidSomething(noticeTexts(collected), checkpointUpdateNudgeNote) {
		t.Errorf("nobody told the person why the turn went on; notices were %q", noticeTexts(collected))
	}
	// THE PERSON NEVER SEES THE CONTINUATION AS THEIR OWN WORDS. It is the
	// harness speaking, recorded in the user lane only because that is the one
	// lane the model can be told something in.
	if !isContinuationContext(checkpointUpdateNudgeLead) {
		t.Error("the update nudge would be drawn as a line the person typed")
	}
	if !IsCarryOnNotice(checkpointUpdateNudgeNote) {
		t.Error("the update nudge's notice is not folded with the other carry-on lines")
	}
}

// AND IT IS BOUNDED. A model that answers every nudge with another promise
// is sent back [checkpointUpdateNudgeCap] times and then the turn ends, rather
// than paying a request for every sentence of it.
func TestTheUpdateNudgeIsBoundedATurn(t *testing.T) {
	var steps []step
	for range checkpointUpdateNudgeCap + 1 {
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("[update] Next I write the slides."), nil
		})
	}
	recorder := &blockRecorder{steps: steps}
	agent, _ := newTestAgent(t, recorder, nil)

	events, err := agent.Submit(context.Background(), "5 slide deck for the board meeting")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)
	if got, want := recorder.requests(), checkpointUpdateNudgeCap+1; got != want {
		t.Fatalf("the loop made %d requests; want %d — the cap's nudges and then the turn ends", got, want)
	}
	if n := strings.Count(strings.Join(noticeTexts(collected), "\n"), checkpointUpdateNudgeNote); n != checkpointUpdateNudgeCap {
		t.Errorf("the person was told %d times; want %d", n, checkpointUpdateNudgeCap)
	}
}

// AND ONLY THE MARKER, ONLY WHERE IT IS A MARKER. An ordinary final answer, an
// answer that merely mentions the marker, and an update that ends by asking the
// person something all end the turn exactly as before: the first two are
// answers, and the last is waiting on somebody else.
func TestOnlyAnUnaskedLeadingUpdateIsSentBack(t *testing.T) {
	for name, reply := range map[string]string{
		"a final answer":            "The five slides are written.",
		"a mention of the marker":   "Done. Interim notes start with " + UserUpdatePrefix + " and this is not one.",
		"an update asking a person": "[update] The frames are in place. Should the deck use the navy or the cream theme?",
	} {
		t.Run(name, func(t *testing.T) {
			recorder := &blockRecorder{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					return textResponse(reply), nil
				},
			}}
			agent, _ := newTestAgent(t, recorder, nil)
			events, err := agent.Submit(context.Background(), "5 slide deck for the board meeting")
			if err != nil {
				t.Fatalf("Submit: %v", err)
			}
			collected := collect(t, events)
			if got := recorder.requests(); got != 1 {
				t.Fatalf("the loop made %d requests; want 1 — this reply ends the turn", got)
			}
			if saidSomething(noticeTexts(collected), checkpointUpdateNudgeNote) {
				t.Error("a reply that is not an unasked interim update was sent back")
			}
		})
	}
}

// STUB — delete this whole file at merge; the rewind/engine branch owns these.
//
// It covers rewindstub.go and nothing else: the point list's shape, the cut at a
// chosen point, and the two refusals. The real engine's tests replace it.
package session

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The points are every place the conversation can be cut, oldest first: a turn
// at each thing the person said, a step at each thing the model said, and an
// Entry that lands on the right row of [Agent.Transcript].
func TestRewindPointsNameTurnsAndSteps(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the planner walks the graph"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "ls", `{"path":"."}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("an empty directory"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "how does the planner work?"))
	collect(t, mustSubmit(t, agent, "now list the files"))

	points := agent.RewindPoints()
	if len(points) < 4 {
		t.Fatalf("RewindPoints returned %d points, want a turn and a step per turn", len(points))
	}
	turns := 0
	for _, point := range points {
		if point.Turn {
			turns++
		}
	}
	if turns != 2 {
		t.Fatalf("RewindPoints found %d turns, want the two messages that were sent", turns)
	}
	if points[0].Said != "how does the planner work?" {
		t.Fatalf("the first turn point says %q", points[0].Said)
	}

	// Entry indexes the display transcript, so every point lands on a row a
	// surface actually drew.
	transcript := agent.Transcript()
	for _, point := range points {
		if point.Entry < 0 || point.Entry >= len(transcript) {
			t.Fatalf("point %+v is outside a transcript of %d entries", point, len(transcript))
		}
	}
	for _, point := range points {
		if point.Turn && transcript[point.Entry].Text != point.Said {
			t.Fatalf("the turn point %+v lands on %+v", point, transcript[point.Entry])
		}
	}
	if points[len(points)-1].Entry < points[0].Entry {
		t.Fatal("RewindPoints is not oldest first")
	}
}

// A cut at a chosen point drops that point and everything after it, and reports
// what it removed in the display shape the surface un-draws with.
func TestRewindAtCutsAtTheChosenPoint(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the planner walks the graph"), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the executor runs leaves"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "how does the planner work?"))
	collect(t, mustSubmit(t, agent, "and the executor?"))

	points := agent.RewindPoints()
	cut := points[0]
	for _, point := range points {
		if point.Turn && point.Said == "and the executor?" {
			cut = point
		}
	}
	removed, err := agent.RewindAt(cut.Index)
	if err != nil {
		t.Fatalf("RewindAt: %v", err)
	}
	if len(removed) != 2 || removed[0].Text != "and the executor?" {
		t.Fatalf("RewindAt removed %+v, want the last turn", removed)
	}
	if got, want := transcriptRoles(agent), []string{"system", "user", "assistant"}; !equalStrings(got, want) {
		t.Fatalf("transcript after the cut = %v, want the first turn alone", got)
	}

	// And an index nobody offered is refused rather than obeyed.
	if _, err := agent.RewindAt(len(points) + 99); !errors.Is(err, ErrNothingToRewind) {
		t.Fatalf("RewindAt at an unknown index = %v, want ErrNothingToRewind", err)
	}
}

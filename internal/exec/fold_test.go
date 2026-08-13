package exec

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The measured shape: a join that read three files it had already been handed
// and wrote one list, run as a thirteen-turn exploration — 181,354 tokens
// against about 7,700 for the single pass it actually is. A fold has its
// material in its prompt, so the loop asks for one answer and stops.
func TestAFoldIsOneCallWhenTheFirstReplyIsTheAnswer(t *testing.T) {
	space := workspace(t)
	client := &scriptedCompleter{}
	linear := NewLinear(client, space, nil, 200, 1_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{
		NodeID: 1, Brief: "put the three sections in order", Fold: true,
		Inputs: []Input{
			{Title: "n8", Result: "section one, in full", Whole: true},
			{Title: "n9", Result: "section two, in full", Whole: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.seen) != 1 {
		t.Fatalf("a fold whose first reply was the answer made %d model calls", len(client.seen))
	}
	if outcome.Mode != ModeFold {
		t.Fatalf("mode = %q, want %q — a benchmark cannot tell this from a lucky short run",
			outcome.Mode, ModeFold)
	}
}

// The one thing a cap of one would get wrong. A first call that asks for a tool
// has not answered, and settling the node on it would deliver the tool call as
// the deliverable. So the second call exists — and after it the loop stops,
// whatever the model does with it.
func TestAFoldGetsASecondCallForAToolCallAndNoThird(t *testing.T) {
	space := workspace(t)
	// Both calls ask for tools. The first is the one the second call is for; the
	// second is the one that must not buy a third.
	client := &scriptedCompleter{turns: [][]ai.ToolCall{
		{call("a", "write", `{"path":"out.md","content":"the list"}`)},
		{call("b", "write", `{"path":"out.md","content":"again"}`)},
	}}
	linear := NewLinear(client, space, nil, 200, 1_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{
		NodeID: 2, Brief: "write the list", Fold: true,
		Inputs: []Input{{Title: "n8", Result: "one", Whole: true}, {Title: "n9", Result: "two", Whole: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.seen) != FoldTurns {
		t.Fatalf("a fold made %d model calls; the cap is %d", len(client.seen), FoldTurns)
	}
	if outcome.Stop != StopTurnCap {
		t.Fatalf("stop = %q; a fold still calling tools at its cap is a leaf that reached "+
			"its cap, and the judging paths above read that", outcome.Stop)
	}
	// The last call is told it is the last, because the alternative is a model
	// cut off mid-tool-call holding the answer it was about to state.
	last := client.seen[len(client.seen)-1]
	told := false
	for _, message := range last {
		for _, part := range message.Content {
			if strings.Contains(part.Text, "one round of tool calls") {
				told = true
			}
		}
	}
	if !told {
		t.Fatal("the fold's final call was not told it was the final call")
	}
}

// A fold never buys turns its caller did not grant. The cap is one-directional:
// it declines turns, it does not create them.
func TestAFoldNeverExceedsTheGrantItWasGiven(t *testing.T) {
	space := workspace(t)
	client := &scriptedCompleter{turns: [][]ai.ToolCall{
		{call("a", "write", `{"path":"out.md","content":"x"}`)},
		{call("b", "write", `{"path":"out.md","content":"y"}`)},
	}}
	linear := NewLinear(client, space, nil, 1, 1_000_000, time.Minute)
	if _, err := linear.Run(context.Background(), Task{
		NodeID: 3, Brief: "write the list", Fold: true,
	}); err != nil {
		t.Fatal(err)
	}
	if len(client.seen) != 1 {
		t.Fatalf("a one-turn grant made %d model calls", len(client.seen))
	}
}

// An ordinary leaf is untouched: the mode is empty and the loop runs to the
// grant it was given, because a leaf that has things to go and find needs turns
// to find them in.
func TestAnOrdinaryLeafKeepsTheOpenLoop(t *testing.T) {
	space := workspace(t)
	client := &scriptedCompleter{turns: [][]ai.ToolCall{
		{call("a", "sh", `{"command":"echo one"}`)},
		{call("b", "sh", `{"command":"echo two"}`)},
		{call("c", "sh", `{"command":"echo three"}`)},
	}}
	linear := NewLinear(client, space, nil, 200, 1_000_000, time.Minute)
	outcome, err := linear.Run(context.Background(), Task{NodeID: 4, Brief: "go and find out"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Mode != "" {
		t.Fatalf("an ordinary leaf reported mode %q", outcome.Mode)
	}
	if len(client.seen) != 4 {
		t.Fatalf("the open loop made %d model calls, want the three tool turns plus the answer",
			len(client.seen))
	}
}

// The consumer's own words. The block of inputs is headed with a claim that the
// leaf already holds this work and must not gather it again, and for a producer
// whose deliverable was a file that claim was false: the leaf was handed a path
// and an invitation to read it, and it read it. The invitation survives only
// where it is still true.
func TestTheBriefOffersTheFilesOnlyWhenItIsNotAlreadyHoldingThem(t *testing.T) {
	linear := NewLinear(nil, workspace(t), nil, 10, 1000, time.Minute)
	whole := linear.brief(Task{Brief: "assemble", Inputs: []Input{{
		Title: "n8", Result: "Vendor A: 41ms.\nVendor B: 88ms.",
		Artifacts: []string{"/w/job/07-vendors.md"}, Whole: true,
	}}})
	if strings.Contains(whole, "read them if you need") {
		t.Fatalf("a leaf holding the contents was still sent to the files:\n%s", whole)
	}
	if !strings.Contains(whole, "/w/job/07-vendors.md") {
		t.Fatalf("the files were dropped, so nothing can be cited:\n%s", whole)
	}
	partial := linear.brief(Task{Brief: "assemble", Inputs: []Input{{
		Title: "n8", Result: "a summary of it", Artifacts: []string{"/w/job/07-vendors.md"},
	}}})
	if !strings.Contains(partial, "read them if you need") {
		t.Fatalf("a leaf holding only an account of the work was not told where the work is:\n%s", partial)
	}
}

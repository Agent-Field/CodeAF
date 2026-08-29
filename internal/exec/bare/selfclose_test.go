package bare

// The leaf's own closing, on the belt that was written before it existed.

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec"
)

// A MECHANISM ONLY ONE WORKER HAS IS ONE THE RUN DOES NOT. The generalist belt
// and this one both photograph a finished leaf, so both put what the photograph
// finds to the leaf that caused it — and both ask through the same
// exec.SelfCloser, at the one exit where a loop finishes under its own power.
func TestTheBareLoopReopensOnItsOwnFindingAndAsksOnce(t *testing.T) {
	root := t.TempDir()
	completer := &scriptedCompleter{t: t, turns: []oracleTurn{
		{text: "I removed the unused paths and I am done."},
		{text: "I have put them back."},
		{text: "Nothing else to do."},
	}}
	loop := &loopState{
		client: completer,
		tools:  Tools(root),
		system: SystemPrompt(root),
		user:   "rework configs.py",
		cwd:    root,
	}
	asked := 0
	outcome := loop.runClosing(context.Background(), func(*exec.Outcome) string {
		asked++
		if asked == 1 {
			return "Before this lands, your own reading of the finished tree found this: " +
				"this work removed public names that the tree spelled before it."
		}
		return ""
	})
	if outcome.Stop != exec.StopDone {
		t.Fatalf("stop = %v, want done — a close is not an ending", outcome.Stop)
	}
	if asked != 2 {
		t.Fatalf("the loop offered %d answers to the closing, want the one it was asked about "+
			"and the one it landed on", asked)
	}
	if outcome.Turns != 2 {
		t.Fatalf("turns = %d, want the answer and the turn that settled it", outcome.Turns)
	}
	// The note goes in as a user turn behind the draft it is about, which is
	// what makes the next turn a continuation of this leaf's own reasoning
	// rather than a fresh reading of the brief.
	found := false
	for index, message := range loop.messages {
		for _, part := range message.Content {
			if message.Role != "user" || !strings.Contains(part.Text, "Before this lands") {
				continue
			}
			found = true
			if index == 0 || loop.messages[index-1].Role != "assistant" {
				t.Errorf("the finding was not put behind the answer it is about: %+v",
					loop.messages[index-1])
			}
		}
	}
	if !found {
		t.Fatal("the finding never reached the transcript")
	}
}

// AND A LOOP WITH NOTHING TO CLOSE RUNS EXACTLY AS IT ALWAYS DID. Every caller
// with no workspace to photograph — every test, every bench harness — passes
// nothing, and pays nothing.
func TestTheBareLoopWithNothingToCloseIsUnchanged(t *testing.T) {
	root := t.TempDir()
	completer := &scriptedCompleter{t: t, turns: []oracleTurn{{text: "Done."}}}
	loop := &loopState{
		client: completer, tools: Tools(root), system: SystemPrompt(root),
		user: "do the thing", cwd: root,
	}
	outcome := loop.run(context.Background())
	if outcome.Stop != exec.StopDone || outcome.Turns != 1 || outcome.Text != "Done." {
		t.Fatalf("outcome = %+v", outcome)
	}
	if len(loop.messages) != 2 || loop.messages[0].Role != "system" {
		t.Fatalf("messages = %d, want the system and user turns the loop started with",
			len(loop.messages))
	}
}

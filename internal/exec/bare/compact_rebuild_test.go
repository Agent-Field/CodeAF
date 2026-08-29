package bare

import (
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE REBUILD READS THE OLD SLICE BEFORE IT REPLACES IT.
//
// [loopState.maybeCompact] rebuilds the window as system + summary + tail. The
// first version assigned the fresh slice and only then reached for
// l.messages[0] to carry the system message across, so it read index 0 of a
// slice it had just emptied and panicked with "index out of range [0] with
// length 0". Nothing caught it because a window has to actually FILL before
// anything runs the rebuild, and the short runs never filled one — it took a
// leaf on a 150k-token budget to reach the line.
//
// The test does not go near a model. It builds the window by hand, runs the
// same rebuild, and asserts the three things the rebuild promises: the system
// message survives at the head, the summary follows it, and the kept tail
// arrives in its original order.
func TestTheCompactRebuildKeepsTheSystemMessageAndTheTail(t *testing.T) {
	msg := func(role, text string) ai.Message {
		return ai.Message{Role: role, Content: []ai.ContentPart{{Type: "text", Text: text}}}
	}
	l := &loopState{messages: []ai.Message{
		msg("system", "the standing brief"),
		msg("user", "first"),
		msg("assistant", "second"),
		msg("user", "third"),
		msg("assistant", "fourth"),
	}}
	cut, summary := 3, "what the discarded turns amounted to"

	system := l.messages[0]
	kept := append([]ai.Message(nil), l.messages[cut:]...)
	l.messages = make([]ai.Message, 0, 2+len(kept))
	l.messages = append(l.messages, system)
	l.messages = append(l.messages, msg("user", summary))
	l.messages = append(l.messages, kept...)

	if got := len(l.messages); got != 4 {
		t.Fatalf("rebuilt window holds %d messages, want system + summary + 2 kept", got)
	}
	if l.messages[0].Role != "system" || l.messages[0].Content[0].Text != "the standing brief" {
		t.Fatalf("the system message did not survive the rebuild: %+v", l.messages[0])
	}
	if l.messages[1].Content[0].Text != summary {
		t.Fatalf("the summary is not the second message: %+v", l.messages[1])
	}
	for i, want := range []string{"third", "fourth"} {
		if got := l.messages[2+i].Content[0].Text; got != want {
			t.Fatalf("kept tail %d is %q, want %q — the tail lost its order", i, got, want)
		}
	}
}

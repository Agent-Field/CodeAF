package provider

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE MEMO'S ONE PROMISE IS THAT THE WIRE CANNOT TELL IT IS THERE. Everything
// below is that promise: the memoized encoding is compared byte for byte against
// the direct one, over a transcript growing the way a tool loop grows it, so the
// rolling tail breakpoint lands on a different message every step and a stale
// entry would show up immediately as different bytes.

// growingTranscript is the shape a leaf accumulates: a system prompt, then
// rounds of user -> assistant call -> tool result. Each round moves the tail
// breakpoint two messages forward and turns the message that carried it into an
// ordinary one, which is the movement the memo has to survive.
func growingTranscript(turns int) []ai.Message {
	messages := []ai.Message{
		{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: "the working method"}}},
	}
	for index := 0; index < turns; index++ {
		messages = append(messages,
			ai.Message{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: fmt.Sprintf("step %d", index)}}},
			ai.Message{
				Role:      "assistant",
				Content:   []ai.ContentPart{{Type: "text", Text: "looking"}},
				ToolCalls: []ai.ToolCall{{ID: fmt.Sprintf("c%d", index), Type: "function", Function: ai.ToolCallFunction{Name: "sh", Arguments: `{"cmd":"ls"}`}}},
			},
			ai.Message{Role: "tool", ToolCallID: fmt.Sprintf("c%d", index), Content: []ai.ContentPart{{Type: "text", Text: "a.go b.go"}}},
		)
	}
	return messages
}

func assertSameMessageBytes(t *testing.T, memo *encodeMemo, messages []ai.Message, dialect cacheDialect) {
	t.Helper()
	direct, err := encodeMessages(messages, dialect)
	if err != nil {
		t.Fatalf("direct encode of %d messages: %v", len(messages), err)
	}
	memoized, err := memo.encodeMessages(messages, dialect)
	if err != nil {
		t.Fatalf("memoized encode of %d messages: %v", len(messages), err)
	}
	if len(memoized) != len(direct) {
		t.Fatalf("memo encoded %d messages, direct encoded %d", len(memoized), len(direct))
	}
	for index := range direct {
		if !bytes.Equal(memoized[index], direct[index]) {
			t.Fatalf("message %d of %d:\n memo: %s\ndirect: %s", index, len(direct), memoized[index], direct[index])
		}
	}
}

func TestTheMemoEncodesEveryTurnExactlyAsTheDirectPathDoes(t *testing.T) {
	for _, dialect := range []struct {
		name string
		d    cacheDialect
	}{{"automatic", cacheDialectAutomatic}, {"breakpoints", cacheDialectBreakpoints}} {
		t.Run(dialect.name, func(t *testing.T) {
			var memo encodeMemo
			for turns := 0; turns <= 8; turns++ {
				assertSameMessageBytes(t, &memo, growingTranscript(turns), dialect.d)
			}
		})
	}
}

// The dialect changes under a live memo whenever an endpoint refuses a
// breakpoint: client.go learns the refusal and re-encodes the SAME transcript
// without markers, immediately, on the same client. The bytes of both encodes
// have to be right, which is the whole reason the memo keeps the breakpoint-free
// form and re-derives the marked positions.
func TestTheMemoSurvivesTheDialectChangingUnderIt(t *testing.T) {
	var memo encodeMemo
	messages := growingTranscript(4)
	for _, dialect := range []cacheDialect{
		cacheDialectBreakpoints, cacheDialectAutomatic, cacheDialectBreakpoints, cacheDialectAutomatic,
	} {
		assertSameMessageBytes(t, &memo, messages, dialect)
	}
}

// A transcript is append-only until a compaction rewrites its head or a relax
// rung strips its attachments, and neither is an error — both are a miss. The
// memo verifies rather than assumes, so it must notice a message replaced in
// place and a transcript that got SHORTER.
func TestTheMemoNoticesATranscriptThatWasRewritten(t *testing.T) {
	var memo encodeMemo
	messages := growingTranscript(4)
	assertSameMessageBytes(t, &memo, messages, cacheDialectBreakpoints)

	rewritten := append([]ai.Message(nil), messages...)
	rewritten[1] = ai.Message{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "everything before, summarized"}}}
	assertSameMessageBytes(t, &memo, rewritten, cacheDialectBreakpoints)

	assertSameMessageBytes(t, &memo, rewritten[:2], cacheDialectBreakpoints)
	assertSameMessageBytes(t, &memo, growingTranscript(6), cacheDialectBreakpoints)
}

func TestTheToolMemoEncodesTheSameBytesAsTheDirectPath(t *testing.T) {
	assertSameToolBytes := func(t *testing.T, memo *encodeMemo, tools []ai.ToolDefinition, dialect cacheDialect) {
		t.Helper()
		direct, err := encodeTools(tools, dialect)
		if err != nil {
			t.Fatalf("direct encode of %d tools: %v", len(tools), err)
		}
		memoized, err := memo.encodeTools(tools, dialect)
		if err != nil {
			t.Fatalf("memoized encode of %d tools: %v", len(tools), err)
		}
		if len(memoized) != len(direct) {
			t.Fatalf("memo encoded %d tools, direct encoded %d", len(memoized), len(direct))
		}
		for index := range direct {
			if !bytes.Equal(memoized[index], direct[index]) {
				t.Fatalf("tool %d of %d:\n memo: %s\ndirect: %s", index, len(direct), memoized[index], direct[index])
			}
		}
	}

	var memo encodeMemo
	// A belt with room to grow, because arming a family appends IN PLACE when
	// there is capacity: the slice is then the same backing array at a different
	// length, which is exactly the case a memo keyed on the array alone would
	// answer wrongly.
	belt := make([]ai.ToolDefinition, 0, 4)
	belt = append(belt, twoTools()...)
	for _, dialect := range []cacheDialect{cacheDialectBreakpoints, cacheDialectAutomatic, cacheDialectBreakpoints} {
		assertSameToolBytes(t, &memo, belt, dialect)
	}
	// Arming moves the marker onto the new last element and leaves the element
	// that carried it plain.
	belt = append(belt, ai.ToolDefinition{Type: "function", Function: ai.ToolFunction{Name: "read", Description: "read a file"}})
	assertSameToolBytes(t, &memo, belt, cacheDialectBreakpoints)
	assertSameToolBytes(t, &memo, belt[:2], cacheDialectBreakpoints)
	assertSameToolBytes(t, &memo, nil, cacheDialectBreakpoints)
}

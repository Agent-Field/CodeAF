package session

// Held-range ledger probes: a read for bytes the conversation already has is a
// pointer; a changed file, a partial overlap, a stubbed away result and a gap
// in the union are all plain fresh reads. Each probe runs the dispatch door
// itself (loop.go's [Agent.dispatchTool]), so what is asserted is what the
// model would be handed, not only the ledger's arithmetic.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// readHeld runs one `read` through the whole dispatch door and records the
// result in the transcript exactly the way runTurn does, which is the only road
// the ledger's claim will consult later.
func readHeld(t *testing.T, agent *Agent, id, args string) toolResult {
	t.Helper()
	call := ai.ToolCall{ID: id, Function: ai.ToolCallFunction{Name: "read", Arguments: args}}
	result := agent.executeTool(context.Background(), agent.newEpisode(), nil, call, "")
	agent.record(ai.Message{
		Role:       "tool",
		ToolCallID: call.ID,
		Content:    []ai.ContentPart{{Type: "text", Text: result.text}},
	})
	return result
}

func heldArgs(path string, offset, limit int) string {
	args := map[string]any{"path": path}
	if offset > 0 {
		args["offset"] = offset
	}
	if limit > 0 {
		args["limit"] = limit
	}
	out, _ := json.Marshal(args)
	return string(out)
}

// A read whose range the transcript already holds is answered with the pointer
// — and proven so by making the file unreadable between the two calls: a second
// fetch from disk would have been an error, and the pointer is not.
func TestACoveredReadComesBackAsAPointer(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	path := filepath.Join(workspace, "page.md")
	writeFile(t, path, "one\ntwo\nthree\nfour\nfive\n")

	first := readHeld(t, agent, "c1", heldArgs(path, 0, 0))
	if first.isError || !strings.Contains(first.text, "three") {
		t.Fatalf("the plain read did not run: %.80q", first.text)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	second := readHeld(t, agent, "c2", heldArgs(path, 0, 0))
	if second.isError || !strings.HasPrefix(second.text, "[already read] ") {
		t.Fatalf("a held range was fetched rather than pointed at: %.80q", second.text)
	}
}

// A file whose stamp moved since the read is read fresh, and its entry is
// replaced rather than merged: the new bytes are what comes back, and the old
// span no longer covers anything.
func TestAChangedFileIsReadFresh(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	path := filepath.Join(workspace, "page.md")
	writeFile(t, path, "old bytes\n")
	readHeld(t, agent, "c1", heldArgs(path, 0, 0))
	writeFile(t, path, "new bytes, more of them\n")

	again := readHeld(t, agent, "c2", heldArgs(path, 0, 0))
	if strings.Contains(again.text, "[already read] ") {
		t.Fatalf("a changed file was pointed at: %.80q", again.text)
	}
	if !strings.Contains(again.text, "new bytes") {
		t.Fatalf("the fresh read did not carry the new bytes: %.80q", again.text)
	}
}

// Partial overlap is never trimmed and never half-held: the second call reads
// whole from disk, and after it the union covers both ends.
func TestAPartialOverlapReadsFreshAndMerges(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	path := filepath.Join(workspace, "page.md")
	writeFile(t, path, "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\n")

	readHeld(t, agent, "c1", heldArgs(path, 1, 3))
	overlap := readHeld(t, agent, "c2", heldArgs(path, 3, 6))
	if strings.Contains(overlap.text, "[already read] ") {
		t.Fatalf("a partial overlap was pointed at instead of read whole: %.80q", overlap.text)
	}
	if !strings.Contains(overlap.text, "eight") {
		t.Fatalf("the overlap did not carry its full fresh range: %.80q", overlap.text)
	}
	whole := readHeld(t, agent, "c3", heldArgs(path, 1, 8))
	if !strings.HasPrefix(whole.text, "[already read] ") {
		t.Fatalf("the merged union did not cover lines 1–8: %.80q", whole.text)
	}
}

// Two reads of one file hold their own ranges independently: a request over a
// gap between them misses, and a request inside one of them is a pointer.
func TestTwoRangesHoldIndependently(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	path := filepath.Join(workspace, "page.md")
	writeFile(t, path, "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\n")

	readHeld(t, agent, "c1", heldArgs(path, 1, 3))
	readHeld(t, agent, "c2", heldArgs(path, 7, 3))

	gap := readHeld(t, agent, "c3", heldArgs(path, 1, 9))
	if strings.Contains(gap.text, "[already read] ") {
		t.Fatalf("a gap between held ranges was pointed at: %.80q", gap.text)
	}
	inside := readHeld(t, agent, "c4", heldArgs(path, 7, 2))
	if !strings.HasPrefix(inside.text, "[already read] ") {
		t.Fatalf("a request inside one held range missed: %.80q", inside.text)
	}
}

// A result stubbed out of the transcript no longer counts as held: stub.go
// rewrites the message's text to stubMarker, and the claim must then fetch.
func TestAStubbedReadStopsCountingAsHeld(t *testing.T) {
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	path := filepath.Join(workspace, "page.md")
	writeFile(t, path, "one\ntwo\nthree\n")
	readHeld(t, agent, "c1", heldArgs(path, 0, 0))

	agent.mu.Lock()
	for index := range agent.messages {
		if agent.messages[index].Role == "tool" {
			agent.messages[index].Content = []ai.ContentPart{{Type: "text", Text: stubMarker + " read · stubbed]"}}
		}
	}
	agent.mu.Unlock()

	after := readHeld(t, agent, "c2", heldArgs(path, 0, 0))
	if strings.Contains(after.text, "[already read] ") {
		t.Fatalf("a stubbed-out held range was still pointed at: %.80q", after.text)
	}
	if !strings.Contains(after.text, "three") {
		t.Fatalf("the fresh read did not re-fetch the bytes: %.80q", after.text)
	}
}

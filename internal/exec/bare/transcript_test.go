package bare

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The defect these tests pin: this loop could bill 1.29M prompt tokens and
// fifty cents against one node, write no file, and leave a record consisting
// entirely of the harness's own progress lines. Nobody could say what it had
// done. Everything below asks the same question — after the loop ran, can the
// store say what happened — in the three shapes the answer has to hold for: a
// run that finished, a run that died mid-turn, and a run whose tool printed
// more than a record should carry.

// scriptTurn is one scripted assistant response.
type scriptTurn struct {
	text  string
	calls []ai.ToolCall
	// panics makes the completer die on this turn, standing in for every way a
	// leaf can be killed in the middle of its loop.
	panics bool
}

type transcriptCompleter struct {
	turns []scriptTurn
	calls int
}

func (c *transcriptCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	turn := c.turns[c.calls]
	c.calls++
	if turn.panics {
		panic("the model client died mid-turn")
	}
	content := []ai.ContentPart{}
	if turn.text != "" {
		content = append(content, ai.ContentPart{Type: "text", Text: turn.text})
	}
	return &ai.Response{Choices: []ai.Choice{{
		Message:      ai.Message{Role: "assistant", Content: content, ToolCalls: turn.calls},
		FinishReason: "stop",
	}}}, nil
}

func call(id, name, arguments string) ai.ToolCall {
	return ai.ToolCall{ID: id, Type: "function",
		Function: ai.ToolCallFunction{Name: name, Arguments: arguments}}
}

// echoTool is a stand-in for the four wire tools: it returns whatever it is
// told to, so a test can pin the record without pinning the filesystem.
func echoTool(name, output string, isError bool) Tool {
	return Tool{
		Name:        name,
		Description: name,
		Schema:      json.RawMessage(`{"type":"object"}`),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			return output, isError, nil
		},
	}
}

// transcriptFixture opens a graph with one leaf node and returns it beside a
// context armed with that node's recorder — the same arming cmd/aforge does
// before every leaf.
func transcriptFixture(t *testing.T) (*store.Store, context.Context) {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = graph.Close() })
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "leaf", Brief: "read a file and say what is in it", Title: "Leaf"},
	}}, store.Provenance{Origin: store.OriginUser, Intent: "read a file"}); err != nil {
		t.Fatal(err)
	}
	ctx := exec.WithTranscript(context.Background(),
		exec.NewTranscriptRecorder(graph, "leaf", "worker/model"))
	return graph, ctx
}

func kinds(entries []store.TranscriptEntry) []string {
	spelled := make([]string, 0, len(entries))
	for _, entry := range entries {
		spelled = append(spelled, string(entry.Kind))
	}
	return spelled
}

// A three-turn loop with one tool call leaves the store holding the assistant
// turn, the tool call and the tool result, in that order, under the node.
func TestALeafWritesDownWhatItDid(t *testing.T) {
	graph, ctx := transcriptFixture(t)
	loop := &loopState{
		client: &transcriptCompleter{turns: []scriptTurn{
			{text: "I'll read the notes first.", calls: []ai.ToolCall{call("c1", "read", `{"filePath":"notes.md"}`)}},
			{text: "Now the second file.", calls: []ai.ToolCall{call("c2", "read", `{"filePath":"gone.md"}`)}},
			{text: "The notes say the deadline moved."},
		}},
		tools:  []Tool{echoTool("read", "the notes", false)},
		system: "you are a worker",
		user:   "read the notes",
	}
	outcome := loop.run(ctx)
	if outcome.Turns != 3 {
		t.Fatalf("the loop ran %d turns, want 3", outcome.Turns)
	}

	entries, err := graph.TranscriptFor("leaf", 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"assistant", "tool_call", "tool_result",
		"assistant", "tool_call", "tool_result",
		"assistant",
	}
	if got := kinds(entries); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the record reads %v, want %v", got, want)
	}
	if entries[0].Turn != 1 || entries[3].Turn != 2 || entries[6].Turn != 3 {
		t.Fatalf("the turns are numbered %d, %d, %d; want 1, 2, 3",
			entries[0].Turn, entries[3].Turn, entries[6].Turn)
	}
	if entries[1].Tool != "read" || !strings.Contains(entries[1].Text, "notes.md") {
		t.Fatalf("the call did not keep what it asked for: %+v", entries[1])
	}
	if entries[2].Text != "the notes" {
		t.Fatalf("the result did not keep what came back: %+v", entries[2])
	}
	if entries[1].CallID != entries[2].CallID {
		t.Fatalf("the call and its result carry different ids (%q, %q); a turn with six parallel tools could not be read",
			entries[1].CallID, entries[2].CallID)
	}
	if entries[6].Text != "The notes say the deadline moved." {
		t.Fatalf("the last word is wrong: %q", entries[6].Text)
	}
}

// A leaf that dies mid-loop is the case this machinery exists for. The turns
// before the fault must survive it, which is why the record is flushed from a
// defer rather than written at the end.
func TestATranscriptSurvivesTheLoopDyingMidTurn(t *testing.T) {
	graph, ctx := transcriptFixture(t)
	loop := &loopState{
		client: &transcriptCompleter{turns: []scriptTurn{
			{text: "I'll read the notes first.", calls: []ai.ToolCall{call("c1", "read", `{"filePath":"notes.md"}`)}},
			{panics: true},
		}},
		tools:  []Tool{echoTool("read", "the notes", false)},
		system: "you are a worker",
		user:   "read the notes",
	}
	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("the scripted client was supposed to die and did not")
			}
		}()
		loop.run(ctx)
	}()

	entries, err := graph.TranscriptFor("leaf", 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"assistant", "tool_call", "tool_result"}
	if got := kinds(entries); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("a leaf that died mid-turn left %v behind, want %v — the whole point is that the "+
			"turns before the fault survive it", got, want)
	}
}

// A tool that fails says so as a fact beside its text, because a tool that
// failed usefully and one that succeeded both return prose and only this tells
// them apart.
func TestAFailedToolIsMarkedAsOne(t *testing.T) {
	graph, ctx := transcriptFixture(t)
	loop := &loopState{
		client: &transcriptCompleter{turns: []scriptTurn{
			{calls: []ai.ToolCall{call("c1", "read", `{"filePath":"gone.md"}`)}},
			{text: "That file is not there."},
		}},
		tools:  []Tool{echoTool("read", "no such file", true)},
		system: "you are a worker",
		user:   "read the notes",
	}
	loop.run(ctx)

	entries, err := graph.TranscriptFor("leaf", 0)
	if err != nil {
		t.Fatal(err)
	}
	var results []store.TranscriptEntry
	for _, entry := range entries {
		if entry.Kind == store.TranscriptToolResult {
			results = append(results, entry)
		}
	}
	if len(results) != 1 {
		t.Fatalf("the record holds %d tool results, want 1", len(results))
	}
	if !results[0].Failed {
		t.Fatalf("a failed tool was recorded as a successful one: %+v", results[0])
	}
}

// The bound, applied on the way through the loop rather than left to the
// caller: a tool that printed fifty kilobytes must not put fifty kilobytes in
// the journal, and must not lose the end of its output either.
func TestAHugeToolResultIsBoundedOnTheWayToTheRecord(t *testing.T) {
	graph, ctx := transcriptFixture(t)
	output := "START\n" + strings.Repeat("noise\n", 20000) + "EXIT-CODE-1"
	loop := &loopState{
		client: &transcriptCompleter{turns: []scriptTurn{
			{calls: []ai.ToolCall{call("c1", "bash", `{"command":"make"}`)}},
			{text: "The build failed."},
		}},
		tools:  []Tool{echoTool("bash", output, false)},
		system: "you are a worker",
		user:   "build it",
	}
	loop.run(ctx)

	entries, err := graph.TranscriptFor("leaf", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if len(entry.Text) > store.MaxTranscriptTextBytes {
			t.Fatalf("a %s entry is %d bytes; the bound is %d",
				entry.Kind, len(entry.Text), store.MaxTranscriptTextBytes)
		}
		if entry.Kind != store.TranscriptToolResult {
			continue
		}
		if !strings.HasPrefix(entry.Text, "START") || !strings.HasSuffix(entry.Text, "EXIT-CODE-1") {
			t.Fatalf("the bound kept the wrong half of the output: %.30q … %.30q",
				entry.Text, entry.Text[len(entry.Text)-30:])
		}
	}
}

// Nobody listening is the ordinary case — a bench harness, a unit test, any
// caller with no store — and it must cost the loop nothing and change nothing.
func TestALoopWithNobodyListeningRunsExactlyAsBefore(t *testing.T) {
	loop := &loopState{
		client: &transcriptCompleter{turns: []scriptTurn{
			{calls: []ai.ToolCall{call("c1", "read", `{"filePath":"notes.md"}`)}},
			{text: "done"},
		}},
		tools:  []Tool{echoTool("read", "the notes", false)},
		system: "you are a worker",
		user:   "read the notes",
	}
	outcome := loop.run(context.Background())
	if outcome.Stop != exec.StopDone || outcome.Text != "done" || outcome.ToolCalls != 1 {
		t.Fatalf("an unobserved loop behaved differently: %+v", outcome)
	}
}

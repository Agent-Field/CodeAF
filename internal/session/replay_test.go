package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE BUG THIS FILE IS ABOUT: a resumed session drew its tool calls as lines
// with nothing behind them. The rows came from [Agent.Transcript], the entries
// carried a gloss and no payload, and a person who clicked a replayed write to
// see what had been written got an empty expansion — while the journal, three
// feet away, held every byte of the arguments and of the result.
//
// So the round trip is the test: write a real session to a real file, resume it
// in a second agent, and read the transcript the surface would draw.

// A resumed tool call carries what it did: the arguments the model sent and the
// result that answered them, for EVERY call in the file.
func TestResumedTranscriptCarriesArgumentsAndResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "write", `{"path":"notes.md","content":"the planner walks the graph\nand stops"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c2", "read", `{"path":"notes.md"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("written and read back"), nil
		},
	}}
	first, workspace := newTestAgent(t, writer, func(config *Config) { config.SessionFile = path })
	collect(t, mustSubmit(t, first, "write my notes down"))
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := newAgent(Config{
		Workspace:   workspace,
		Model:       "test/model",
		System:      "SYSTEM",
		SessionFile: path,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	entries := second.Transcript()
	calls := toolEntries(entries)
	if len(calls) != 2 {
		t.Fatalf("replayed tool entries = %d, want the write and the read: %+v", len(calls), entries)
	}
	for _, call := range calls {
		if call.Args == "" {
			t.Fatalf("%s replayed with no arguments: %+v", call.Tool, call)
		}
		if call.Output == "" {
			t.Fatalf("%s replayed with no result: %+v", call.Tool, call)
		}
	}

	// The write's arguments are the ONLY record of what was written — the tool's
	// own result is one sentence saying it worked — so this is the field a
	// clicked row draws its content preview from.
	if !strings.Contains(calls[0].Args, "the planner walks the graph") {
		t.Fatalf("the write's content did not survive the round trip: %q", calls[0].Args)
	}
	if !strings.Contains(calls[0].Args, `"path":"notes.md"`) {
		t.Fatalf("the write's path did not survive the round trip: %q", calls[0].Args)
	}
	// And the read's result is the file itself, which is what its expansion
	// shows.
	if !strings.Contains(calls[1].Output, "the planner walks the graph") {
		t.Fatalf("the read's output did not survive the round trip: %q", calls[1].Output)
	}
}

// The payload a resume replays is the payload the live turn streamed. Two
// renderings of one call is how a replayed screen starts lying about what
// happened, so the fields a surface draws from must match event for event.
func TestReplayedPayloadMatchesTheLiveEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "write", `{"path":"one.txt","content":"first"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	first, workspace := newTestAgent(t, writer, func(config *Config) { config.SessionFile = path })
	events := collect(t, mustSubmit(t, first, "write one.txt"))
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The gloss rides the BEGIN event and the result rides the END — a surface
	// draws one row from both — so the replayed row is checked against each
	// field where it was actually spoken.
	var began, ended Event
	for _, event := range events {
		switch event.Kind {
		case EventToolBegin:
			began = event
		case EventToolEnd:
			ended = event
		}
	}
	if began.Tool == "" || ended.Tool == "" {
		t.Fatalf("the turn did not stream a whole call: %v", kinds(events))
	}

	second, err := newAgent(Config{
		Workspace: workspace, Model: "test/model", System: "SYSTEM", SessionFile: path,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	calls := toolEntries(second.Transcript())
	if len(calls) != 1 {
		t.Fatalf("replayed tool entries = %d, want 1", len(calls))
	}
	if calls[0].Args != began.Args {
		t.Fatalf("replayed Args = %q, live Args = %q", calls[0].Args, began.Args)
	}
	if calls[0].Output != ended.Output {
		t.Fatalf("replayed Output = %q, live Output = %q", calls[0].Output, ended.Output)
	}
	if calls[0].Hint != began.Hint {
		t.Fatalf("replayed Hint = %q, live Hint = %q", calls[0].Hint, began.Hint)
	}
}

// A long result is capped for display exactly as the live event is: a resumed
// build log is a screen or two, not a megabyte, and the cut says how much was
// left behind rather than trailing off.
func TestAReplayedResultIsCappedForDisplay(t *testing.T) {
	huge := strings.Repeat("x", outputLimit*2)
	messages := []ai.Message{
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "run it"}}},
		{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: ""}}, ToolCalls: []ai.ToolCall{{
			ID: "c1", Type: "function",
			Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"go build ./..."}`},
		}}},
		{Role: "tool", ToolCallID: "c1", Content: []ai.ContentPart{{Type: "text", Text: huge}}},
	}
	calls := toolEntries(shapeEntries(messages, nil))
	if len(calls) != 1 {
		t.Fatalf("entries = %+v", calls)
	}
	if len(calls[0].Output) > outputLimit+64 {
		t.Fatalf("replayed output is %d bytes, want it capped near %d", len(calls[0].Output), outputLimit)
	}
	if !strings.Contains(calls[0].Output, "more bytes") {
		t.Fatalf("the cut is not marked: %q", tail(calls[0].Output))
	}
}

// A call whose result never reached the file — a session killed between the
// call and its answer — replays with NO payload rather than with an empty one
// that looks like a result. It is the honest floor, and it is what tells a
// surface the row has nothing to expand.
func TestACallWithNoJournaledResultReplaysWithNoOutput(t *testing.T) {
	messages := []ai.Message{
		{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: ""}}, ToolCalls: []ai.ToolCall{{
			ID: "c1", Type: "function",
			Function: ai.ToolCallFunction{Name: "ls", Arguments: `{"path":"."}`},
		}}},
	}
	calls := toolEntries(shapeEntries(messages, nil))
	if len(calls) != 1 {
		t.Fatalf("entries = %+v", calls)
	}
	if calls[0].Output != "" {
		t.Fatalf("Output = %q, want empty for a call nothing answered", calls[0].Output)
	}
	if calls[0].Args == "" {
		t.Fatalf("the arguments went missing too: %+v", calls[0])
	}
}

// A file written before the payload was journaled — a message line with no
// tool_calls — still replays as the conversation it is, with empty payload
// fields rather than a refusal or a panic.
func TestAnOlderJournalReplaysWithoutPayload(t *testing.T) {
	messages := []ai.Message{
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "what did you do?"}}},
		{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "I read the file"}}},
	}
	entries := shapeEntries(messages, nil)
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	for _, entry := range entries {
		if entry.Args != "" || entry.Output != "" || entry.ImageRefs != nil {
			t.Fatalf("a plain message replayed with a payload: %+v", entry)
		}
	}
}

// A resumed message says which pictures it carried. The bytes are not in the
// file and never will be — the journal writes a reference — so the transcript
// carries the PATH, which is what a surface marks the message with.
func TestAResumedMessageCarriesItsImageReferences(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "session.jsonl")
	first, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		withVision(config)
		config.SessionFile = path
	})
	photo := writeImage(t, workspace, "photo.png", "PHOTOBYTES")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	events, err := first.SubmitImage(ctx, "what is wrong with this", []Image{{Path: photo}})
	if err != nil {
		t.Fatalf("SubmitImage: %v", err)
	}
	collect(t, events)

	// Live first: the session that ATTACHED the picture can answer the same
	// question, because the index is written as the message is journaled.
	if refs := imageRefsOf(first.Transcript(), "what is wrong with this"); len(refs) != 1 || refs[0] != photo {
		t.Fatalf("live refs = %v, want [%s]", refs, photo)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := newAgent(Config{
		Workspace: workspace, Model: "test/model", System: "SYSTEM", SessionFile: path,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	refs := imageRefsOf(second.Transcript(), "what is wrong with this")
	if len(refs) != 1 || refs[0] != photo {
		t.Fatalf("resumed refs = %v, want [%s]", refs, photo)
	}
	// Every other message answers nothing, which is what keeps a marker off a
	// line nobody attached anything to. The person's own line is matched by its
	// opening words rather than whole: this session was reopened with no
	// SupportsImages, which is "nobody vouched for this model" and therefore NO
	// by Config's law, so the transcript guard has replaced the picture with its
	// placeholder and the line now carries that sentence too (image.go).
	for _, entry := range second.Transcript() {
		if !strings.HasPrefix(entry.Text, "what is wrong with this") && len(entry.ImageRefs) != 0 {
			t.Fatalf("%q replayed with pictures it never had: %v", entry.Text, entry.ImageRefs)
		}
	}
}

// A picture that was MOVED between the session and the resume is still named.
// The transcript the model reads gets a placeholder saying the file is gone
// (image_test.go proves that); the transcript the PERSON reads still says which
// picture it was, because that is the fact they can act on.
func TestAMovedPictureIsStillNamedInTheTranscript(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "session.jsonl")
	first, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		withVision(config)
		config.SessionFile = path
	})
	photo := writeImage(t, workspace, "chart.png", "CHARTBYTES")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	events, err := first.SubmitImage(ctx, "read this chart", []Image{{Path: photo}})
	if err != nil {
		t.Fatalf("SubmitImage: %v", err)
	}
	collect(t, events)
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := os.Remove(photo); err != nil {
		t.Fatalf("remove: %v", err)
	}

	second, err := newAgent(Config{
		Workspace: workspace, Model: "test/model", System: "SYSTEM", SessionFile: path,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	for _, entry := range second.Transcript() {
		if entry.Role != "user" || !strings.Contains(entry.Text, "read this chart") {
			continue
		}
		if len(entry.ImageRefs) != 1 || entry.ImageRefs[0] != photo {
			t.Fatalf("refs for a moved picture = %v, want [%s]", entry.ImageRefs, photo)
		}
		return
	}
	t.Fatal("the message with the picture is not in the replayed transcript")
}

// ── helpers ─────────────────────────────────────────────────────────────────

func toolEntries(entries []DisplayEntry) []DisplayEntry {
	var calls []DisplayEntry
	for _, entry := range entries {
		if entry.Role == "tool" && entry.Tool != "" {
			calls = append(calls, entry)
		}
	}
	return calls
}

func imageRefsOf(entries []DisplayEntry, text string) []string {
	for _, entry := range entries {
		if entry.Role == "user" && strings.Contains(entry.Text, text) {
			return entry.ImageRefs
		}
	}
	return nil
}

func tail(text string) string {
	if len(text) <= 80 {
		return text
	}
	return text[len(text)-80:]
}

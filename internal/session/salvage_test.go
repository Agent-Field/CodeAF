package session

// The salvage yard's laws (salvage.go), held by tests: a write the output
// limit cut mid-content lands its complete lines and tells the model to
// continue with append rather than pay for the file again; a path that did
// not arrive whole is never written to; anything that is not a write keeps
// its refusal but learns the cause; and the append flag itself (tools_write.go)
// adds to a file instead of replacing it.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestAnalyzeSeveredWriteRecoversWhatArrived(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		ok       bool
		path     string
		content  string
		appendOn bool
		complete bool
	}{
		{
			name:    "cut mid-content keeps the complete lines",
			raw:     `{"path":"page.html","content":"<!doctype html>\n<p>one</p>\n<p>tw`,
			ok:      true,
			path:    "page.html",
			content: "<!doctype html>\n<p>one</p>\n",
		},
		{
			name:    "cut mid-escape still closes cleanly",
			raw:     `{"path":"a.txt","content":"first\nsecond\nthird\u00`,
			ok:      true,
			path:    "a.txt",
			content: "first\nsecond\n",
		},
		{
			name:     "append flag that streamed before content survives",
			raw:      `{"path":"a.txt","append":true,"content":"more\nand th`,
			ok:       true,
			path:     "a.txt",
			content:  "more\n",
			appendOn: true,
		},
		{
			name:     "content complete with only the brace cut is whole",
			raw:      `{"path":"a.txt","content":"all here\n"`,
			ok:       true,
			path:     "a.txt",
			content:  "all here\n",
			complete: true,
		},
		{
			name: "a truncated path is never a target",
			raw:  `{"path":"/Users/some/lo`,
			ok:   false,
		},
		{
			name: "a cut inside a key saves nothing",
			raw:  `{"path":"a.txt","conte`,
			ok:   false,
		},
		{
			name: "a single unfinished line is not worth landing",
			raw:  `{"path":"a.txt","content":"no newline anywh`,
			ok:   false,
		},
		{
			name: "syntax garbage is not a truncation",
			raw:  `{"path":"a.txt" "content":"x\ny`,
			ok:   false,
		},
		{
			name: "arrays are not this schema",
			raw:  `["path","a.txt`,
			ok:   false,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recovered, ok := analyzeSeveredWrite(testCase.raw)
			if ok != testCase.ok {
				t.Fatalf("ok = %v, want %v (recovered %+v)", ok, testCase.ok, recovered)
			}
			if !ok {
				return
			}
			if recovered.path != testCase.path {
				t.Errorf("path = %q, want %q", recovered.path, testCase.path)
			}
			if recovered.content != testCase.content {
				t.Errorf("content = %q, want %q", recovered.content, testCase.content)
			}
			if recovered.appendMode != testCase.appendOn {
				t.Errorf("appendMode = %v, want %v", recovered.appendMode, testCase.appendOn)
			}
			if recovered.complete != testCase.complete {
				t.Errorf("complete = %v, want %v", recovered.complete, testCase.complete)
			}
		})
	}
}

// cutToolResponse is toolResponse with the provider saying "length": the
// answer did not fit, and the call it carries is the severed tail.
func cutToolResponse(id, name, arguments string) *ai.Response {
	response := toolResponse(id, name, arguments)
	response.Choices[0].FinishReason = "length"
	return response
}

// lastToolText digs the most recent tool result out of a scripted request's
// transcript.
func lastToolText(messages []ai.Message) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "tool" && len(messages[index].Content) > 0 {
			return messages[index].Content[0].Text
		}
	}
	return ""
}

func TestSeveredWriteLandsAndContinuesWithAppend(t *testing.T) {
	severed := `{"path":"page.html","content":"<!doctype html>\n<p>one</p>\n<p>tw`
	var noteSeen, appendResult string
	var recordedArguments string
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return cutToolResponse("c1", "write", severed), nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			noteSeen = lastToolText(messages)
			for _, message := range messages {
				for _, call := range message.ToolCalls {
					recordedArguments = call.Function.Arguments
				}
			}
			return toolResponse("c2", "write", `{"path":"page.html","append":true,"content":"<p>two</p>\n"}`), nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			appendResult = lastToolText(messages)
			return textResponse("done"), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "make the page"))

	// The complete lines landed; the severed tail did not.
	saved, err := os.ReadFile(filepath.Join(workspace, "page.html"))
	if err != nil {
		t.Fatalf("reading the salvaged file: %v", err)
	}
	after := "<!doctype html>\n<p>one</p>\n<p>two</p>\n"
	if string(saved) != after {
		t.Fatalf("file after continuation = %q, want %q", saved, after)
	}

	// The model was told what happened and how to continue, not "Invalid
	// arguments".
	if !strings.HasPrefix(noteSeen, "Saved what arrived") {
		t.Fatalf("salvage note = %q, want a 'Saved what arrived' note", noteSeen)
	}
	for _, must := range []string{"page.html", "append", "do not resend"} {
		if !strings.Contains(strings.ToLower(noteSeen), strings.ToLower(must)) {
			t.Errorf("salvage note %q does not mention %q", noteSeen, must)
		}
	}

	// The transcript carries the REPAIRED arguments: what ran is what was
	// recorded, and it parses.
	if !json.Valid([]byte(recordedArguments)) {
		t.Errorf("recorded arguments are not valid JSON: %q", recordedArguments)
	}

	// The continuation's own result speaks in lines.
	if !strings.HasPrefix(appendResult, "Appended 1 line") {
		t.Errorf("append result = %q, want an 'Appended 1 line' sentence", appendResult)
	}
}

func TestSeveredBashRefusesWithCauseAndRemedy(t *testing.T) {
	var refusal string
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return cutToolResponse("c1", "bash", `{"command":"echo hel`), nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			refusal = lastToolText(messages)
			return textResponse("understood"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "run it"))

	for _, must := range []string{"cut off at the output limit", "nothing was run"} {
		if !strings.Contains(refusal, must) {
			t.Errorf("severed bash result %q does not say %q", refusal, must)
		}
	}
	if strings.Contains(refusal, "unexpected end of JSON input") {
		t.Errorf("severed bash result still speaks parser: %q", refusal)
	}
}

func TestAppendWritesAfterWhatTheFileHolds(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return toolResponse("c1", "write", `{"path":"log.txt","content":"first\n"}`), nil
		},
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return toolResponse("c2", "write", `{"path":"log.txt","append":true,"content":"second\n"}`), nil
		},
		func(_ context.Context, _ []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "keep the log"))

	saved, err := os.ReadFile(filepath.Join(workspace, "log.txt"))
	if err != nil {
		t.Fatalf("reading the appended file: %v", err)
	}
	if string(saved) != "first\nsecond\n" {
		t.Fatalf("file = %q, want %q", saved, "first\nsecond\n")
	}
}

func TestAppendSchemaKeepsAppendBeforeContent(t *testing.T) {
	var writeTool bare.Tool
	for _, tool := range bare.AllTools(t.TempDir()) {
		if tool.Name == "write" {
			writeTool = tool
		}
	}
	schema := string(schemaWithAppend(writeTool.Schema))
	if !json.Valid([]byte(schema)) {
		t.Fatalf("extended schema is not valid JSON: %s", schema)
	}
	path := strings.Index(schema, `"path"`)
	appendAt := strings.Index(schema, `"append"`)
	content := strings.Index(schema, `"content"`)
	if path < 0 || appendAt < 0 || content < 0 {
		t.Fatalf("schema is missing a property: %s", schema)
	}
	// The order is the design: append must stream before the content it
	// qualifies, or a cut call cannot be salvaged with its intent intact.
	if !(path < appendAt && appendAt < content) {
		t.Fatalf("property order is path=%d append=%d content=%d; want path < append < content", path, appendAt, content)
	}
}

func TestLineTallyCountsWhatSomeoneWouldRead(t *testing.T) {
	for _, testCase := range []struct {
		text string
		want int
	}{
		{"", 0},
		{"one\n", 1},
		{"one\ntwo\n", 2},
		{"one\ntwo", 2},
	} {
		if got := lineTally(testCase.text); got != testCase.want {
			t.Errorf("lineTally(%q) = %d, want %d", testCase.text, got, testCase.want)
		}
	}
}

package session

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"reflect"
	"testing"
	"unsafe"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// THE MARK BELONGS TO THE OCCURRENCE, NOT TO THE PROVIDER'S CALL ID. Two turns
// reuse one id: the first call paints and has a record, the second is turned
// away at the door and has none. The live events, the session in memory, a
// reopened one and a detached read all keep the two apart.
func TestMediaAttemptStaysWithItsOccurrenceAcrossReusedIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	provider := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("same", "generate_image", `{"prompt":"a lighthouse"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("painted"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("same", "generate_image", `{"prompt":"x","n":11}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("refused"), nil },
	}}
	media := &scriptedMedia{base64: base64.StdEncoding.EncodeToString(pngOfSize(t, 2, 2))}
	agent, _ := newTestAgent(t, provider, func(c *Config) {
		c.SessionFile = path
		c.Media = media
		c.MediaModel = mediaModels(map[string]string{modalityImage: "paint/model"})
	})
	if _, err := agent.armFamily([]bare.Tool{beltTool(t, agent, "generate_image")}); err != nil {
		t.Fatal(err)
	}
	var live []bool
	for _, prompt := range []string{"first", "second"} {
		for _, event := range collect(t, mustSubmit(t, agent, prompt)) {
			if (event.Kind == EventToolEnd || event.Kind == EventToolFailed) && event.CallID == "same" {
				live = append(live, event.MediaAttempt)
			}
		}
	}
	if !reflect.DeepEqual(live, []bool{true, false}) {
		t.Fatalf("live events %v", live)
	}
	if page, _ := agent.Renders(10, ""); len(page.Renders) != 1 || page.Renders[0].Call != "same" {
		t.Fatalf("records = %+v", page.Renders)
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	for label, entries := range map[string][]DisplayEntry{"live": agent.Transcript(), "reopened": reopen(t, path).Transcript(), "detached": ReadTranscript(path).Entries} {
		var flags []bool
		for _, entry := range entries {
			if entry.Role == "tool" && entry.CallID == "same" {
				flags = append(flags, entry.ToolResult.MediaAttempt())
			}
		}
		if !reflect.DeepEqual(flags, []bool{true, false}) {
			t.Fatalf("%s flags %v", label, flags)
		}
	}
}

// The fifth producer fact rides in the byte DisplayEntry already had.
func TestMediaAttemptCostsDisplayEntryNoBytes(t *testing.T) {
	if unsafe.Sizeof(ToolResultFlags(0)) != 1 || ToolResultMediaAttempt != 1<<4 {
		t.Fatalf("flags grew: size %d bit %d", unsafe.Sizeof(ToolResultFlags(0)), ToolResultMediaAttempt)
	}
	if size := unsafe.Sizeof(DisplayEntry{}); size > 296 {
		t.Fatalf("DisplayEntry is %d bytes; budget is 296", size)
	}
}

// Two calls in ONE reply under one id cannot say which result is whose, so
// neither keeps the fact: unknown, never lent to the sibling.
func TestMediaAttemptIsUnknownForSiblingsSharingAnID(t *testing.T) {
	twin := func(context.Context, []ai.Message) (*ai.Response, error) {
		response := toolResponse("twin", "generate_image", `{"prompt":"a lighthouse"}`)
		calls := &response.Choices[0].Message.ToolCalls
		*calls = append(*calls, ai.ToolCall{ID: "twin", Type: "function", Function: ai.ToolCallFunction{Name: "generate_image", Arguments: `{"prompt":"x","n":11}`}})
		return response, nil
	}
	provider := &scriptedCompleter{steps: []step{twin, func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil }}}
	media := &scriptedMedia{base64: base64.StdEncoding.EncodeToString(pngOfSize(t, 2, 2))}
	agent, _ := newTestAgent(t, provider, func(c *Config) {
		c.Media = media
		c.MediaModel = mediaModels(map[string]string{modalityImage: "paint/model"})
	})
	if _, err := agent.armFamily([]bare.Tool{beltTool(t, agent, "generate_image")}); err != nil {
		t.Fatal(err)
	}
	collect(t, mustSubmit(t, agent, "go"))
	if page, _ := agent.Renders(10, ""); len(page.Renders) != 1 {
		t.Fatalf("records = %+v", page.Renders)
	}
	seen := 0
	for _, entry := range agent.Transcript() {
		if entry.Role == "tool" && entry.CallID == "twin" {
			seen++
			if entry.ToolResult.MediaAttempt() {
				t.Fatalf("a sibling sharing an id kept the fact: %+v", entry)
			}
		}
	}
	if seen != 2 {
		t.Fatalf("saw %d entries", seen)
	}
}

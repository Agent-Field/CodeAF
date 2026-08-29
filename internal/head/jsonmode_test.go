package head

import (
	"context"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// formatClient answers a scripted reply and remembers the response format each
// call asked for, read the way the adapter reads it: by applying the options
// to a request.
type formatClient struct {
	replies []string
	formats []string
}

func (c *formatClient) CompleteWithMessages(_ context.Context, _ []ai.Message, options ...ai.Option) (*ai.Response, error) {
	request := &ai.Request{}
	for _, option := range options {
		_ = option(request)
	}
	format := ""
	if request.ResponseFormat != nil {
		format = request.ResponseFormat.Type
	}
	c.formats = append(c.formats, format)
	reply := c.replies[len(c.formats)-1]
	return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: reply}}}, FinishReason: "stop"}}}, nil
}

// The compile asks for JSON on the wire, on the attempt and on the retry: a
// model handed an issue written in Markdown answered in Markdown twice and
// forfeited the job at three seconds.
func TestTheCompileAsksForJSONOnTheWireBothTimes(t *testing.T) {
	client := &formatClient{replies: []string{
		"## Summary\nThe normalizer should handle arithmetic.",
		`{"structure":"single_act","goal":"normalize arithmetic\nVerbatim request: do it","title":"Arithmetic normalization","scale":"task","contract":"","parts":[],"builds_on":[],"assumptions":[],"question":"","question_options":[],"trial_of":0}`,
	}}
	brief, err := NewCompiler(client).Compile(context.Background(), "do it", "")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if brief.Title != "Arithmetic normalization" {
		t.Fatalf("brief = %+v, want the retry's object", brief)
	}
	if len(client.formats) != 2 || client.formats[0] != "json_object" || client.formats[1] != "json_object" {
		t.Fatalf("response formats = %v, want json_object on both attempts", client.formats)
	}
}

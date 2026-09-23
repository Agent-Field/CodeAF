//go:build !windows

package orclient

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
)

// Long-enough payloads that a stringified copy is unmistakable in the output,
// and distinct enough from each other to pin the order images go out in.
const (
	firstPayload  = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	secondPayload = "R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	thirdPayload  = "Qk1GAAAAAAAAADYAAAAoAAAAAQAAAAEAAAABABgAAAAAABAAAAATCwAAEwsAAAAAAAAAAAAA////AAAAAAAAAAAAAAAAAAAA"
)

// The wordings this converter uses. They are pinned here because they are the
// only thing telling the model that an image it was promised is elsewhere.
const (
	imageNoteWording = "[attachment: image/png (sent as an image in the next user message)]"
	imageLeadWording = "The images below are attachments from the preceding tool results, " +
		"in the order those tools returned them."
)

type wireMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// convertPrompt converts a prompt and decodes each wire message's role and
// raw content.
func convertPrompt(t *testing.T, prompt ...msgmodel.ModelMessage) []wireMessage {
	t.Helper()
	messages, err := ConvertToOpenRouterChatMessages(prompt)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]wireMessage, 0, len(messages))
	for _, message := range messages {
		encoded, err := json.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		var decoded wireMessage
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("message is not an object: %s", encoded)
		}
		out = append(out, decoded)
	}
	return out
}

// roles is the message sequence a test asserts on.
func roles(messages []wireMessage) []string {
	out := make([]string, 0, len(messages))
	for _, message := range messages {
		out = append(out, message.Role)
	}
	return out
}

func assertRoles(t *testing.T, messages []wireMessage, want ...string) {
	t.Helper()
	got := roles(messages)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("roles = %v, want %v", got, want)
	}
}

// partsOf decodes a message's content as an array of typed parts.
func partsOf(t *testing.T, message wireMessage) []json.RawMessage {
	t.Helper()
	var parts []json.RawMessage
	if err := json.Unmarshal(message.Content, &parts); err != nil {
		t.Fatalf("%s content is not a part array: %s", message.Role, message.Content)
	}
	return parts
}

// toolResult is one `{type:"content"}` tool result carrying `items`.
func toolResult(callID string, items ...any) msgmodel.ToolResultContent {
	return msgmodel.ToolResultContent{
		Type:       "tool-result",
		ToolCallID: callID,
		ToolName:   "read",
		Output:     msgmodel.ToolOutput{Type: "content", Value: items},
	}
}

// toolMessage is a tool message carrying a single tool result.
func toolMessage(callID string, items ...any) msgmodel.ModelMessage {
	return msgmodel.ModelMessage{Role: "tool", Content: []any{toolResult(callID, items...)}}
}

func mediaPart(mediaType, data string) msgmodel.ToolOutputContentMedia {
	return msgmodel.ToolOutputContentMedia{Type: "media", MediaType: mediaType, Data: data}
}

func textPart(s string) msgmodel.ToolOutputContentText {
	return msgmodel.ToolOutputContentText{Type: "text", Text: s}
}

func assistantText(s string) msgmodel.ModelMessage {
	return msgmodel.ModelMessage{Role: "assistant", Content: []any{
		msgmodel.TextContent{Type: "text", Text: s},
	}}
}

// assertNoPayload fails when any base64 blob reached the given messages.
func assertNoPayload(t *testing.T, messages []wireMessage) {
	t.Helper()
	for _, message := range messages {
		for _, payload := range []string{firstPayload, secondPayload, thirdPayload} {
			if strings.Contains(string(message.Content), payload) {
				t.Fatalf("base64 payload reached the %s message: %s", message.Role, message.Content)
			}
		}
	}
}

// userImagePart is the `image_url` part the user-message path builds for a
// file, which the tool-result path has to match exactly.
func userImagePart(t *testing.T, mediaType, data string) json.RawMessage {
	t.Helper()
	messages := convertPrompt(t, msgmodel.ModelMessage{Role: "user", Content: []any{
		msgmodel.FileContent{Type: "file", MediaType: mediaType, Data: data},
	}})
	parts := partsOf(t, messages[0])
	if len(parts) != 1 {
		t.Fatalf("want one user part, got %d", len(parts))
	}
	return parts[0]
}

// An image is named in the tool result and delivered by the user message that
// follows it, because the wire accepts an image only on a user message.
func TestToolResultImageIsNamedAndSentInAFollowingUserMessage(t *testing.T) {
	messages := convertPrompt(t, toolMessage("call_1",
		textPart("Image read successfully"),
		mediaPart("image/png", firstPayload),
	))
	assertRoles(t, messages, "tool", "user")

	toolParts := partsOf(t, messages[0])
	if len(toolParts) != 2 {
		t.Fatalf("want two tool parts, got %d: %s", len(toolParts), messages[0].Content)
	}
	if want := `{"type":"text","text":"Image read successfully"}`; string(toolParts[0]) != want {
		t.Fatalf("text part = %s, want %s", toolParts[0], want)
	}
	wantNote := `{"type":"text","text":"` + imageNoteWording + `"}`
	if string(toolParts[1]) != wantNote {
		t.Fatalf("note part = %s\nwant %s", toolParts[1], wantNote)
	}
	assertNoPayload(t, messages[:1])

	userParts := partsOf(t, messages[1])
	if len(userParts) != 2 {
		t.Fatalf("want a lead text and one image, got %d: %s", len(userParts), messages[1].Content)
	}
	var lead struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(userParts[0], &lead); err != nil {
		t.Fatalf("lead part is not a text part: %s", userParts[0])
	}
	if lead.Type != "text" || lead.Text != imageLeadWording {
		t.Fatalf("lead part = %s, want text %q", userParts[0], imageLeadWording)
	}
	want := `{"type":"image_url","image_url":{"url":"data:image/png;base64,` + firstPayload + `"}}`
	if string(userParts[1]) != want {
		t.Fatalf("image part = %s\nwant %s", userParts[1], want)
	}
}

// The delivered image part must be byte-identical to the one the user-message
// path builds for the same file, so a tool result and an attached file look
// the same to the provider.
func TestToolResultImagePartMatchesUserImagePart(t *testing.T) {
	messages := convertPrompt(t, toolMessage("call_1", mediaPart("image/png", firstPayload)))
	assertRoles(t, messages, "tool", "user")
	got := partsOf(t, messages[1])[1]
	want := userImagePart(t, "image/png", firstPayload)
	if string(got) != string(want) {
		t.Fatalf("tool image part = %s\nuser image part = %s", got, want)
	}
}

// Every image of a run reaches one user message, in the order the tools
// returned them -- whether they came from one tool result or several.
func TestToolRunImagesGoOutTogetherInOrder(t *testing.T) {
	messages := convertPrompt(t,
		msgmodel.ModelMessage{Role: "tool", Content: []any{
			toolResult("call_1",
				textPart("Image read successfully"),
				mediaPart("image/png", firstPayload),
				mediaPart("image/gif", secondPayload),
			),
			toolResult("call_2",
				textPart("Image read successfully"),
				mediaPart("image/bmp", thirdPayload),
			),
		}},
	)
	assertRoles(t, messages, "tool", "tool", "user")
	assertNoPayload(t, messages[:2])

	userParts := partsOf(t, messages[2])
	if len(userParts) != 4 {
		t.Fatalf("want a lead text and three images, got %d: %s", len(userParts), messages[2].Content)
	}
	for i, want := range []string{
		`{"type":"image_url","image_url":{"url":"data:image/png;base64,` + firstPayload + `"}}`,
		`{"type":"image_url","image_url":{"url":"data:image/gif;base64,` + secondPayload + `"}}`,
		`{"type":"image_url","image_url":{"url":"data:image/bmp;base64,` + thirdPayload + `"}}`,
	} {
		if string(userParts[i+1]) != want {
			t.Fatalf("image %d = %s\nwant %s", i, userParts[i+1], want)
		}
	}
}

// The tool messages of one assistant turn stay contiguous: the images wait for
// the end of the run and go out in a single user message after the last of
// them, before whatever follows.
func TestToolRunFlushesAfterTheLastToolMessage(t *testing.T) {
	messages := convertPrompt(t,
		assistantText("reading both"),
		toolMessage("call_1", textPart("Image read successfully"), mediaPart("image/png", firstPayload)),
		toolMessage("call_2", textPart("Image read successfully"), mediaPart("image/gif", secondPayload)),
		assistantText("both read"),
	)
	assertRoles(t, messages, "assistant", "tool", "tool", "user", "assistant")

	userParts := partsOf(t, messages[3])
	if len(userParts) != 3 {
		t.Fatalf("want a lead text and two images, got %d: %s", len(userParts), messages[3].Content)
	}
	if !strings.Contains(string(userParts[1]), firstPayload) ||
		!strings.Contains(string(userParts[2]), secondPayload) {
		t.Fatalf("images out of order: %s", messages[3].Content)
	}
}

// A run that ends the prompt flushes at the end, so the images are the last
// thing the model sees.
func TestToolRunAtTheEndOfThePromptFlushesLast(t *testing.T) {
	messages := convertPrompt(t,
		msgmodel.ModelMessage{Role: "user", Content: "read this"},
		assistantText("reading"),
		toolMessage("call_1", textPart("Image read successfully"), mediaPart("image/png", firstPayload)),
	)
	assertRoles(t, messages, "user", "assistant", "tool", "user")
	if !strings.Contains(string(messages[3].Content), firstPayload) {
		t.Fatalf("trailing user message carries no image: %s", messages[3].Content)
	}
}

// A run with nothing to relocate emits no user message at all.
func TestToolRunWithoutImagesEmitsNoUserMessage(t *testing.T) {
	messages := convertPrompt(t,
		assistantText("reading"),
		toolMessage("call_1", textPart("     1\thello\n")),
		toolMessage("call_2", textPart("     1\tworld\n")),
		assistantText("done"),
	)
	assertRoles(t, messages, "assistant", "tool", "tool", "assistant")
}

// A PDF has no inline shape anywhere here: it is named, its payload never goes
// out, and nothing follows the tool message.
func TestToolResultPDFIsNamedAndSendsNoUserMessage(t *testing.T) {
	messages := convertPrompt(t, toolMessage("call_1",
		textPart("PDF read successfully"),
		mediaPart("application/pdf", firstPayload),
	))
	assertRoles(t, messages, "tool")
	parts := partsOf(t, messages[0])
	if len(parts) != 2 {
		t.Fatalf("want two parts, got %d: %s", len(parts), messages[0].Content)
	}
	want := `{"type":"text","text":"[attachment: application/pdf (content not sent)]"}`
	if string(parts[1]) != want {
		t.Fatalf("pdf part = %s\nwant %s", parts[1], want)
	}
	assertNoPayload(t, messages)
}

// A media part with no media type still gets named rather than stringified.
func TestToolResultMediaWithoutMediaTypeIsNamed(t *testing.T) {
	messages := convertPrompt(t, toolMessage("call_1", mediaPart("", firstPayload)))
	assertRoles(t, messages, "tool")
	parts := partsOf(t, messages[0])
	want := `{"type":"text","text":"[attachment: unknown (content not sent)]"}`
	if string(parts[0]) != want {
		t.Fatalf("part = %s\nwant %s", parts[0], want)
	}
	assertNoPayload(t, messages)
}

// A text-only tool result is untouched by the media handling: one tool
// message, exactly these bytes, and nothing after it.
func TestToolResultTextOnlyIsUnchanged(t *testing.T) {
	converted, err := ConvertToOpenRouterChatMessages([]msgmodel.ModelMessage{
		toolMessage("call_1", textPart("     1\thello\n")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(converted) != 1 {
		t.Fatalf("want one message, got %d", len(converted))
	}
	encoded, err := json.Marshal(converted[0])
	if err != nil {
		t.Fatal(err)
	}
	want := `{"role":"tool","tool_call_id":"call_1",` +
		`"content":[{"type":"text","text":"     1\thello\n"}],"name":"read"}`
	if string(encoded) != want {
		t.Fatalf("tool message = %s\nwant %s", encoded, want)
	}
}

// An element that is neither text nor media keeps the stringified fallback.
func TestToolResultUnknownElementIsStillStringified(t *testing.T) {
	messages := convertPrompt(t, toolMessage("call_1", map[string]any{"type": "widget", "n": 1}))
	assertRoles(t, messages, "tool")
	parts := partsOf(t, messages[0])
	var typed struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(parts[0], &typed); err != nil {
		t.Fatal(err)
	}
	if typed.Type != "text" || !strings.Contains(typed.Text, `"widget"`) {
		t.Fatalf("unknown element = %s", parts[0])
	}
}

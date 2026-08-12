package exec

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// toolCapture records what the model was actually offered on every turn, which
// is the only honest place to measure a prompt floor: the definitions the loop
// built, serialized exactly as they go on the wire.
type toolCapture struct {
	turns [][]ai.ToolCall
	tools [][]ai.ToolDefinition
}

func (c *toolCapture) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		if err := option(&request); err != nil {
			return nil, err
		}
	}
	c.tools = append(c.tools, request.Tools)
	index := len(c.tools) - 1
	message := ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "working"}}}
	if index < len(c.turns) {
		message.ToolCalls = c.turns[index]
	} else {
		message.Content = []ai.ContentPart{{Type: "text", Text: "done"}}
	}
	return &ai.Response{
		Choices: []ai.Choice{{Message: message, FinishReason: "stop"}},
		Usage:   &ai.Usage{PromptTokens: 10, CompletionTokens: 5},
	}, nil
}

func (c *toolCapture) names(turn int) []string {
	names := make([]string, 0, len(c.tools[turn]))
	for _, definition := range c.tools[turn] {
		names = append(names, definition.Function.Name)
	}
	return names
}

func offersEverything(t *testing.T) *MediaTools {
	t.Helper()
	wire := &documentWireCapture{}
	return &MediaTools{
		Provider: &fakeMediaProvider{}, Catalog: fakeModalities{}, ImageModel: "paint/model",
		SpeechModel: "voice/model", MusicModel: "music/model", VideoModel: "motion/model",
		DocumentClient: wire.client(t), WorkingModel: "work/model",
	}
}

func schemaBytes(t *testing.T, definitions []ai.ToolDefinition) int {
	t.Helper()
	encoded, err := json.Marshal(definitions)
	if err != nil {
		t.Fatal(err)
	}
	return len(encoded)
}

func carries(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

// The measurement, and the whole reason for the change.
//
// Every tool definition is re-sent on every turn of every leaf. Measured
// against a six-cell benchmark, the media and document schemas were ~1,112
// tokens of each turn's ~3,778-token fixed floor — 18% of the entire run's
// input — on six tasks not one of which could have generated a video or
// opened a PDF. A brain with everything configured now puts one small
// discovery tool in front of a plain code task instead of six schemas.
//
// The drop is asserted as a floor on the saving and a ceiling on what remains,
// never as an exact number: the schemas must stay free to be reworded.
func TestAPlainTaskCarriesNoSchemaItCannotUse(t *testing.T) {
	client := &toolCapture{}
	linear := NewLinear(client, workspace(t), nil, 10, 1_000_000, time.Minute).WithMedia(offersEverything(t))
	if _, err := linear.Run(context.Background(), Task{NodeID: 1, Brief: "fix the failing test in intervals.py"}); err != nil {
		t.Fatal(err)
	}
	if len(client.tools) == 0 {
		t.Fatal("the leaf never reached a model call")
	}
	names := client.names(0)
	for _, unusable := range []string{"generate_image", "generate_music", "generate_video", "speak", "view_image", "read_document"} {
		if carries(names, unusable) {
			t.Fatalf("turn 1 of a code task carried %s: %v", unusable, names)
		}
	}
	// The core loop is untouched — this is a trim, not an amputation — and the
	// door to the rest is in the prompt where the worker can find it.
	for _, core := range []string{"sh", "job", "write", "edit", "web", "capabilities"} {
		if !carries(names, core) {
			t.Fatalf("turn 1 lost %s: %v", core, names)
		}
	}

	// What turn 1 used to cost unconditionally, measured through the same
	// toolbox with every family in hand.
	full := newToolboxWithMedia(workspace(t), "1", nil, nil, offersEverything(t))
	full.Arm(FamilyMedia, FamilyDocument)
	before, after := schemaBytes(t, full.Definitions()), schemaBytes(t, client.tools[0])
	saved := before - after
	// ~3,782 characters of media and document schema were measured on the
	// benchmark; the discovery tool costs a few hundred back.
	if saved < 3200 {
		t.Fatalf("turn-1 schema bytes fell by only %d (%d → %d), want at least 3200", saved, before, after)
	}
	if after > 3800 {
		t.Fatalf("the trimmed turn-1 schema floor is %d bytes, ceiling is 3800", after)
	}
}

// Lazy is not gone: a worker whose assignment turns out to need a camera asks
// for one and is holding it on its next turn. This is the media journeys' path
// through the new seam, and it costs exactly one turn.
func TestAskingForACapabilityArmsItForTheNextTurn(t *testing.T) {
	client := &toolCapture{turns: [][]ai.ToolCall{
		{call("ask", "capabilities", `{"need":"media"}`)},
	}}
	media := &MediaTools{
		Provider: &fakeMediaProvider{}, Catalog: fakeModalities{}, ImageModel: "paint/model",
		SpeechModel: "voice/model", MusicModel: "music/model", VideoModel: "motion/model",
		WorkingModel: "work/model",
	}
	linear := NewLinear(client, workspace(t), nil, 10, 1_000_000, time.Minute).WithMedia(media)
	if _, err := linear.Run(context.Background(), Task{NodeID: 4, Brief: "make a picture of a harbor"}); err != nil {
		t.Fatal(err)
	}
	if len(client.tools) < 2 {
		t.Fatalf("the loop stopped after %d turns", len(client.tools))
	}
	if carries(client.names(0), "generate_image") {
		t.Fatalf("turn 1 already carried the generator: %v", client.names(0))
	}
	for _, want := range []string{"generate_image", "generate_music", "generate_video", "speak", "view_image"} {
		if !carries(client.names(1), want) {
			t.Fatalf("turn 2 did not carry %s after the worker asked: %v", want, client.names(1))
		}
	}
	// And the door stays where it was. Retiring it once everything on offer was
	// armed pulled a definition out of the MIDDLE of the tool list, so every
	// schema behind it shifted and the arming turn paid a second full-prompt
	// invalidation on top of the one arming already costs — a ~90-token tool
	// bought at the price of the whole transcript. It stays; asking twice is
	// answered in one cheap line instead.
	if !carries(client.names(1), "capabilities") {
		t.Fatalf("the discovery tool was pulled out of the middle of the tool list: %v", client.names(1))
	}
	if names := client.names(1); names[5] != "capabilities" {
		t.Fatalf("the discovery tool moved out of its fixed slot: %v", names)
	}
}

// Arming everything does not silence the door, and a worker that asks again is
// not left thinking its request failed.
func TestAskingForACapabilityAlreadyHeldIsACheapNoOp(t *testing.T) {
	toolbox := newToolboxWithMedia(workspace(t), "1", nil, nil, offersEverything(t))
	toolbox.Arm(FamilyMedia)
	result := toolbox.capabilities(map[string]any{"need": FamilyMedia})
	if result.IsError {
		t.Fatalf("a second ask was answered as a failure: %+v", result)
	}
	if !strings.Contains(result.Content, "Already loaded") || len(result.Content) > 200 {
		t.Fatalf("the repeat answer is not the cheap line: %q", result.Content)
	}
	// The other family is untouched by the no-op, so a real second ask still works.
	if armed := toolbox.capabilities(map[string]any{"need": FamilyDocument}); armed.IsError ||
		!strings.Contains(armed.Content, "Loaded for your next turn") {
		t.Fatalf("the unarmed family was refused: %+v", armed)
	}
}

// The structural pre-arm: what the assignment already contains is not a
// judgement about meaning and must not cost a turn. A leaf handed a document
// can read it on turn 1.
func TestAnAttachedDocumentArmsItsReaderBeforeTurnOne(t *testing.T) {
	wire := &documentWireCapture{}
	media := &MediaTools{Catalog: fakeModalities{}, DocumentClient: wire.client(t), WorkingModel: "work/model"}
	client := &toolCapture{}
	linear := NewLinear(client, workspace(t), nil, 10, 1_000_000, time.Minute).WithMedia(media)
	if _, err := linear.Run(context.Background(), Task{
		NodeID: 5, Brief: "summarise the filing", DocumentPaths: []string{"filing.pdf"},
	}); err != nil {
		t.Fatal(err)
	}
	if !carries(client.names(0), "read_document") {
		t.Fatalf("an attached document did not arm its reader: %v", client.names(0))
	}
	// The door is still in the prompt, in its fixed slot, and the armed schema
	// sits behind it. Presence follows the machine's wiring, never this leaf's
	// arm state — that is what keeps the block identical across every leaf of a
	// run and identical across every turn of this one.
	names := client.names(0)
	if len(names) != 7 || names[5] != "capabilities" || names[6] != "read_document" {
		t.Fatalf("the tool block is not fixed-head-then-armed-tail: %v", names)
	}
}

// Arming costs one invalidation, at the end of the tool list, and that is the
// whole price the lazy design accepted.
//
// It used to cost two. The discovery tool's description and its enum were built
// from whichever families were still unarmed, so the moment a worker armed
// media the sentence describing the families changed — and tool definitions
// ride at the very front of the request, ahead of the entire transcript, so the
// prefix diverged there and everything behind it was re-billed cold. That
// second invalidation was larger than the one it was riding on and nobody had
// costed it. The definition is frozen now: the families it names and the enum
// it offers are the same bytes on every turn of every leaf.
func TestTheCapabilitiesDefinitionDoesNotMoveWhenAFamilyIsArmed(t *testing.T) {
	space := workspace(t)
	unarmed := newToolboxWithMedia(space, "1", nil, nil, offersEverything(t))
	armed := newToolboxWithMedia(space, "1", nil, nil, offersEverything(t))
	armed.Arm(FamilyMedia)

	find := func(toolbox *Toolbox) (ai.ToolDefinition, bool) {
		for _, definition := range toolbox.Definitions() {
			if definition.Function.Name == "capabilities" {
				return definition, true
			}
		}
		return ai.ToolDefinition{}, false
	}
	before, ok := find(unarmed)
	if !ok {
		t.Fatal("a brain with everything configured offered no discovery tool")
	}
	after, ok := find(armed)
	if !ok {
		t.Fatal("the discovery tool vanished while a family was still unarmed")
	}
	first, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.Marshal(after)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("arming a family moved the discovery tool's own bytes:\n before %s\n after  %s", first, second)
	}
	// It names every family whatever is armed, so the sentence has nothing left
	// to move; a family this machine has not configured is answered by the tool
	// itself, in prose, rather than by a schema that changes shape.
	for _, family := range []string{FamilyMedia, FamilyDocument} {
		if !strings.Contains(before.Function.Description, family) {
			t.Errorf("the frozen description does not name %s: %q", family, before.Function.Description)
		}
	}
	bare := newToolboxWithMedia(space, "1", nil, nil,
		&MediaTools{Provider: &fakeMediaProvider{}, Catalog: fakeModalities{}, WorkingModel: "work/model"})
	unconfigured := bare.capabilities(map[string]any{"need": FamilyDocument})
	if !unconfigured.IsError || !strings.Contains(unconfigured.Content, "not configured") {
		t.Fatalf("asking for an unconfigured family was answered with %+v", unconfigured)
	}
}

// An unconfigured family is never advertised, so a worker is never told about
// a capability this machine cannot provide.
func TestNothingIsOfferedWhenNothingIsConfigured(t *testing.T) {
	client := &toolCapture{}
	linear := NewLinear(client, workspace(t), nil, 10, 1_000_000, time.Minute)
	if _, err := linear.Run(context.Background(), Task{NodeID: 6, Brief: "answer the question"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(client.names(0), ","); got != "sh,job,write,edit,web" {
		t.Fatalf("a bare brain offered %q", got)
	}
}

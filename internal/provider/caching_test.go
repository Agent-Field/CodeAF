package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// newCachingClient is newTestClient with the base URL left alone, because the
// dialect is a property of the endpoint AND the model and half the assertions
// here are about the gate rather than the markers.
func newCachingClient(t *testing.T, baseURL, model string) (*Client, *capture) {
	t.Helper()
	recorded := &capture{}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"m","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`))
	})
	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: baseURL, Model: model,
		HTTPClient: handlerClient(handler),
	})
	if err != nil {
		t.Fatal(err)
	}
	return client, recorded
}

// leafTranscript is the shape a tool loop actually sends on its second turn:
// system, brief, the assistant's tool call, and the result answering it. The
// last message being a tool result is the common case, not the exception, which
// is why the rolling breakpoint has to be placeable on one.
func leafTranscript() []ai.Message {
	return []ai.Message{
		{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: "the working method"}}},
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "your work: ship it"}}},
		{
			Role:      "assistant",
			Content:   []ai.ContentPart{{Type: "text", Text: "looking"}},
			ToolCalls: []ai.ToolCall{{ID: "c1", Type: "function", Function: ai.ToolCallFunction{Name: "sh", Arguments: `{"cmd":"ls"}`}}},
		},
		{Role: "tool", ToolCallID: "c1", Content: []ai.ContentPart{{Type: "text", Text: "a.go b.go"}}},
	}
}

func twoTools() []ai.ToolDefinition {
	return []ai.ToolDefinition{
		{Type: "function", Function: ai.ToolFunction{Name: "sh", Description: "run a command"}},
		{Type: "function", Function: ai.ToolFunction{Name: "write", Description: "write a file"}},
	}
}

// wireMessages decodes the messages array out of a recorded body.
func wireMessages(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, ok := body["messages"].([]any)
	if !ok {
		t.Fatalf("body has no messages array: %#v", body)
	}
	out := make([]map[string]any, len(raw))
	for index, entry := range raw {
		message, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("message %d is not an object: %#v", index, entry)
		}
		out[index] = message
	}
	return out
}

// markedPart reports whether a message carries an ephemeral breakpoint, either
// on its last content block or — for a tool result, whose content is a bare
// string on this wire — on the message itself.
func markedPart(message map[string]any) bool {
	if control, present := message["cache_control"].(map[string]any); present {
		return control["type"] == "ephemeral"
	}
	parts, ok := message["content"].([]any)
	if !ok || len(parts) == 0 {
		return false
	}
	last, ok := parts[len(parts)-1].(map[string]any)
	if !ok {
		return false
	}
	control, present := last["cache_control"].(map[string]any)
	return present && control["type"] == "ephemeral"
}

// TestBreakpointsLandOnPisThreePositions is the whole discipline in one
// assertion: the system block, the last tool definition, and the last content
// block of the final user-or-tool message. Anything else is either a wasted
// breakpoint or a cache entry nothing will ever read.
func TestBreakpointsLandOnPisThreePositions(t *testing.T) {
	client, recorded := newCachingClient(t, "https://openrouter.ai/api/v1", "anthropic/claude-opus-5")
	if _, err := client.CompleteWithMessages(context.Background(), leafTranscript(),
		ai.WithTools(twoTools())); err != nil {
		t.Fatal(err)
	}
	body := recorded.body(0)
	messages := wireMessages(t, body)
	if len(messages) != 4 {
		t.Fatalf("sent %d messages, want the fixture's 4", len(messages))
	}

	// (i) the system block.
	if !markedPart(messages[0]) {
		t.Fatalf("the system block carries no breakpoint: %#v", messages[0])
	}
	// (iii) the rolling one, on the final tool result.
	if !markedPart(messages[3]) {
		t.Fatalf("the transcript tail carries no breakpoint: %#v", messages[3])
	}
	// And nowhere else: four breakpoints is the provider's ceiling and the two
	// middle messages are re-sent unchanged, so a marker on either buys nothing
	// and spends one of the four.
	if markedPart(messages[1]) || markedPart(messages[2]) {
		t.Fatalf("a breakpoint landed mid-transcript: %#v", messages)
	}

	// (ii) the last tool definition, which caches the schema array before it.
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 2 {
		t.Fatalf("body has no two-tool array: %#v", body["tools"])
	}
	first, _ := tools[0].(map[string]any)
	last, _ := tools[1].(map[string]any)
	if _, present := first["cache_control"]; present {
		t.Fatalf("a breakpoint landed on a tool that is not the last: %#v", first)
	}
	control, present := last["cache_control"].(map[string]any)
	if !present || control["type"] != "ephemeral" {
		t.Fatalf("the last tool definition carries no breakpoint: %#v", last)
	}
}

// TestTheRollingBreakpointMovesWithTheTranscript is what makes the re-send
// cheap. A fixed pair of breakpoints on the head caches the head; only a marker
// that advances writes an entry over the whole transcript-so-far, which is the
// entry the NEXT turn reads.
func TestTheRollingBreakpointMovesWithTheTranscript(t *testing.T) {
	client, recorded := newCachingClient(t, "https://openrouter.ai/api/v1", "anthropic/claude-opus-5")
	first := leafTranscript()
	if _, err := client.CompleteWithMessages(context.Background(), first, ai.WithTools(twoTools())); err != nil {
		t.Fatal(err)
	}
	second := append(first,
		ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "next"}},
			ToolCalls: []ai.ToolCall{{ID: "c2", Type: "function", Function: ai.ToolCallFunction{Name: "sh"}}}},
		ai.Message{Role: "tool", ToolCallID: "c2", Content: []ai.ContentPart{{Type: "text", Text: "ok"}}},
	)
	if _, err := client.CompleteWithMessages(context.Background(), second, ai.WithTools(twoTools())); err != nil {
		t.Fatal(err)
	}
	turn := wireMessages(t, recorded.body(1))
	if len(turn) != 6 {
		t.Fatalf("second turn sent %d messages, want 6", len(turn))
	}
	if !markedPart(turn[5]) {
		t.Fatalf("the breakpoint did not move to the new tail: %#v", turn[5])
	}
	if markedPart(turn[3]) {
		t.Fatalf("the previous tail kept its breakpoint: %#v", turn[3])
	}
	if !markedPart(turn[0]) {
		t.Fatalf("the system breakpoint did not survive the turn: %#v", turn[0])
	}
}

// TestAModelWithNoBreakpointDialectSendsTheBytesItAlwaysDid is the safety half.
// Every provider that is not Anthropic-family caches automatically or not at
// all, and a marker it does not know is at best ignored and at worst a 400 —
// so the encoder must produce exactly what it produced before this existed.
func TestAModelWithNoBreakpointDialectSendsTheBytesItAlwaysDid(t *testing.T) {
	cases := []struct{ name, baseURL, model string }{
		{"deepseek through openrouter", "https://openrouter.ai/api/v1", "deepseek/deepseek-v4"},
		{"openai through openrouter", "https://openrouter.ai/api/v1", "openai/gpt-5"},
		{"claude through an unknown gateway", "https://gateway.example.test/v1", "anthropic/claude-opus-5"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			client, recorded := newCachingClient(t, test.baseURL, test.model)
			if _, err := client.CompleteWithMessages(context.Background(), leafTranscript(),
				ai.WithTools(twoTools())); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(recorded.raw[0]), "cache_control") {
				t.Fatalf("a breakpoint reached a model that cannot read one: %s", recorded.raw[0])
			}
			// The unmarked encoding must be the SDK's own, byte for byte: the
			// automatic prefix caches these providers do have are keyed on the
			// exact leading bytes, so a cosmetic reshaping here is a cold miss
			// on every request in flight at the moment it ships.
			messages, err := json.Marshal(sanitizeMessages(leafTranscript()))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(recorded.raw[0]), `"messages":`+string(messages)) {
				t.Fatalf("the unmarked encoding drifted from the SDK's:\nwire: %s\nsdk:  %s",
					recorded.raw[0], messages)
			}
		})
	}
}

// TestBreakpointPlacementSurvivesAnEmptyOrHeadlessTranscript pins the rule at
// its edges, where an off-by-one turns a breakpoint into a 400.
func TestBreakpointPlacementSurvivesAnEmptyOrHeadlessTranscript(t *testing.T) {
	tests := []struct {
		name     string
		messages []ai.Message
		want     breakpoints
	}{
		{name: "empty", messages: nil, want: breakpoints{system: -1, tail: -1}},
		{
			name:     "system only",
			messages: []ai.Message{{Role: "system"}},
			want:     breakpoints{system: 0, tail: -1},
		},
		{
			name:     "no system message",
			messages: []ai.Message{{Role: "user"}, {Role: "assistant"}, {Role: "tool"}},
			want:     breakpoints{system: -1, tail: 2},
		},
		{
			name:     "two leading system messages",
			messages: []ai.Message{{Role: "system"}, {Role: "system"}, {Role: "user"}},
			want:     breakpoints{system: 1, tail: 2},
		},
		{
			// A system nudge injected mid-transcript is not the shared prefix.
			// Marking it would write an entry no sibling leaf can read.
			name:     "a system message that is not the head",
			messages: []ai.Message{{Role: "system"}, {Role: "user"}, {Role: "system"}, {Role: "user"}},
			want:     breakpoints{system: 0, tail: 3},
		},
		{
			// A prefilled assistant turn is content the model continues, not a
			// boundary the next turn re-sends unchanged.
			name:     "assistant last",
			messages: []ai.Message{{Role: "system"}, {Role: "user"}, {Role: "assistant"}},
			want:     breakpoints{system: 0, tail: 1},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := breakpointsFor(test.messages); got != test.want {
				t.Fatalf("breakpointsFor = %+v, want %+v", got, test.want)
			}
		})
	}
}

// TestAMarkedMessageWithNowhereToPutTheMarkerIsLeftAlone: an image-only user
// turn has no text block, and inventing a position on an image part is how a
// breakpoint becomes a provider rejection.
func TestAMarkedMessageWithNowhereToPutTheMarkerIsLeftAlone(t *testing.T) {
	imageOnly := ai.Message{Role: "user", Content: []ai.ContentPart{
		{Type: "image_url", ImageURL: &ai.ImageURLData{URL: "data:image/png;base64,AA"}},
	}}
	marked, err := marshalMarked(imageOnly)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := json.Marshal(imageOnly)
	if err != nil {
		t.Fatal(err)
	}
	if string(marked) != string(plain) {
		t.Fatalf("a message with no text block was rewritten:\n%s\n%s", marked, plain)
	}
}

// TestALearnedCacheControlRefusalDowngradesTheModelForGood mirrors the reasoning
// quirk: one rejected call per model, then the automatic prefix cache every
// provider has anyway. A breakpoint that 400s on every turn would be far worse
// than never having sent one.
func TestALearnedCacheControlRefusalDowngradesTheModelForGood(t *testing.T) {
	model := "anthropic/claude-picky"
	quirksAt(t, model)
	recorded := &capture{}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		if strings.Contains(string(recorded.raw[len(recorded.raw)-1]), "cache_control") {
			writer.Header().Set("Content-Type", "application/json")
			writer.WriteHeader(http.StatusBadRequest)
			_, _ = writer.Write([]byte(`{"error":{"message":"Unrecognized request argument supplied: cache_control","code":400}}`))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"m","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`))
	})
	client, err := NewClient(Config{
		APIKey: "k", BaseURL: "https://openrouter.ai/api/v1", Model: model,
		HTTPClient: handlerClient(handler),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteWithMessages(context.Background(), leafTranscript()); err != nil {
		t.Fatalf("a refused breakpoint must be repaired, not returned: %v", err)
	}
	if recorded.count() != 2 {
		t.Fatalf("sent %d requests, want the rejected one and its repair", recorded.count())
	}
	if !strings.Contains(string(recorded.raw[0]), "cache_control") {
		t.Fatalf("the first request carried no breakpoint to be refused: %s", recorded.raw[0])
	}
	if strings.Contains(string(recorded.raw[1]), "cache_control") {
		t.Fatalf("the repair sent the breakpoint again: %s", recorded.raw[1])
	}
	if !CacheControlRejected(model) {
		t.Fatal("the refusal was repaired but not remembered")
	}
	// And no later call pays for the discovery again.
	if _, err := client.CompleteWithMessages(context.Background(), leafTranscript()); err != nil {
		t.Fatal(err)
	}
	if recorded.count() != 3 {
		t.Fatalf("sent %d requests, want one more", recorded.count())
	}
	if strings.Contains(string(recorded.raw[2]), "cache_control") {
		t.Fatalf("a later call re-sent a breakpoint the endpoint refused: %s", recorded.raw[2])
	}
}

// TestAnUnrelated400IsNotMistakenForACacheRefusal: the recovery re-sends, and a
// re-send of a request the provider rejected for its own reasons is a second
// bill for the same failure.
func TestAnUnrelated400IsNotMistakenForACacheRefusal(t *testing.T) {
	if refusesCacheControl([]byte(`{"error":{"message":"context length exceeded"}}`)) {
		t.Fatal("a context-length 400 was read as a cache refusal")
	}
	// A provider that echoes the offending request back is the trap: the body
	// contains the field without rejecting it.
	if refusesCacheControl([]byte(`{"error":{"message":"rate limited"},"request":{"cache_control":{"type":"ephemeral"}}}`)) {
		t.Fatal("an echoed request body was read as a cache refusal")
	}
	if !refusesCacheControl([]byte(`{"error":{"message":"Extra inputs are not permitted: cache_control"}}`)) {
		t.Fatal("a real refusal was not recognized")
	}
}

// TestALeafKeepsOneAffinityKeyAcrossItsTurnsAndSharesItWithNoSibling is the
// routing half of the discipline. A warm prefix on one replica is a cold miss on
// another, so every turn of one leaf must ask for the same destination — and two
// leaves, whose transcripts diverge from the first tool call, must not.
func TestALeafKeepsOneAffinityKeyAcrossItsTurnsAndSharesItWithNoSibling(t *testing.T) {
	run := RunCacheKey("ship the release", "anthropic/claude-opus-5")
	base := WithCacheKey(context.Background(), run)

	first := CacheKeyFrom(WithLeafCacheKey(base, "n1"))
	second := CacheKeyFrom(WithLeafCacheKey(base, "n2"))
	if first == second {
		t.Fatalf("two leaves of one run share the affinity key %q", first)
	}
	if first == run || second == run {
		t.Fatal("a leaf key must narrow the run key, not repeat it")
	}
	// Every turn of one leaf re-derives it, so it must be a pure function of the
	// two identities — no clock, no counter, no attempt number.
	for range 3 {
		if got := CacheKeyFrom(WithLeafCacheKey(base, "n1")); got != first {
			t.Fatalf("a later turn of one leaf keyed %q, want %q", got, first)
		}
	}
	// The lineage stays visible: an operator reading a provider's routing logs
	// can still tell which run a leaf belonged to.
	if !strings.HasPrefix(first, run+"-") {
		t.Fatalf("leaf key %q does not carry its run key %q", first, run)
	}
	// A leaf under no run is left unkeyed rather than inventing a lineage.
	if got := CacheKeyFrom(WithLeafCacheKey(context.Background(), "n1")); got != "" {
		t.Fatalf("an unkeyed run produced the leaf key %q", got)
	}
	if got := CacheKeyFrom(WithLeafCacheKey(base, "  ")); got != run {
		t.Fatalf("a nameless leaf changed the run key to %q", got)
	}
}

// TestTheCacheKeyStillReachesBothHalvesOfTheWire: the body field routes on
// providers that read it, the header on routers that do not, and a leaf key is
// only worth deriving if it arrives.
func TestTheCacheKeyStillReachesBothHalvesOfTheWire(t *testing.T) {
	client, recorded := newCachingClient(t, "https://openrouter.ai/api/v1", "anthropic/claude-opus-5")
	key := LeafCacheKey(RunCacheKey("goal", "anthropic/claude-opus-5"), "n7")
	ctx := WithCacheKey(context.Background(), key)
	if _, err := client.CompleteWithMessages(ctx, leafTranscript()); err != nil {
		t.Fatal(err)
	}
	if got, _ := recorded.body(0)["prompt_cache_key"].(string); got != key {
		t.Fatalf("prompt_cache_key = %q, want %q", got, key)
	}
	if got := recorded.headers[0].Get("X-Session-Affinity"); got != key {
		t.Fatalf("session affinity header = %q, want %q", got, key)
	}
}

// TestAMarkedRequestIsStillDeterministic: the encoder gained a branch, and a
// branch that could reorder or re-shape identical input would defeat the very
// cache the markers are placed for.
func TestAMarkedRequestIsStillDeterministic(t *testing.T) {
	client, recorded := newCachingClient(t, "https://openrouter.ai/api/v1", "anthropic/claude-opus-5")
	for range 2 {
		if _, err := client.CompleteWithMessages(context.Background(), leafTranscript(),
			ai.WithTools(twoTools())); err != nil {
			t.Fatal(err)
		}
	}
	if string(recorded.raw[0]) != string(recorded.raw[1]) {
		t.Fatalf("identical marked calls produced different bytes:\n%s\n%s", recorded.raw[0], recorded.raw[1])
	}
	// The shadowed fields must SUPPRESS the SDK's, not sit beside them. A body
	// with two `messages` keys is valid JSON that most decoders resolve to the
	// last one, so the failure would be a silently unmarked request rather than
	// an error — the worst shape a defect can take here.
	for _, field := range []string{`"messages":`, `"tools":`} {
		if count := strings.Count(string(recorded.raw[0]), field); count != 1 {
			t.Fatalf("body carries %s %d times, want once: %s", field, count, recorded.raw[0])
		}
	}
}

// TestTheDialectGateReadsBothTheEndpointAndTheModel documents, in the one place
// a reader will look, what each provider class lets this adapter express.
func TestTheDialectGateReadsBothTheEndpointAndTheModel(t *testing.T) {
	tests := []struct {
		baseURL, model string
		want           bool
	}{
		{"https://openrouter.ai/api/v1", "anthropic/claude-opus-5", true},
		{"https://openrouter.ai/api/v1", "~anthropic/claude-sonnet-5", true},
		{"https://api.anthropic.com/v1", "claude-opus-5", true},
		// Automatic caches, no expressible control: affinity key only.
		{"https://openrouter.ai/api/v1", "deepseek/deepseek-v4", false},
		{"https://openrouter.ai/api/v1", "openai/gpt-5", false},
		{"https://openrouter.ai/api/v1", "google/gemini-3-pro", false},
		// The right family behind a gateway that has not been vouched for.
		{"https://llm.internal.test/v1", "anthropic/claude-opus-5", false},
		// A host must be a host, not a mention of one in a path.
		{"https://evil.test/openrouter.ai/v1", "anthropic/claude-opus-5", false},
	}
	for _, test := range tests {
		if got := honoursCacheControl(test.baseURL, test.model); got != test.want {
			t.Fatalf("honoursCacheControl(%q, %q) = %t, want %t",
				test.baseURL, test.model, got, test.want)
		}
	}
}

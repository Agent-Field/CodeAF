package provider

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// leakedGrammar is what a deepseek endpoint actually served on 2026-08-31, as
// text content on a 200 with zero structured tool calls, while the request
// declared a belt. It is a FIXTURE AND A PATTERN NOWHERE: the detector reads
// shape — declared name fenced in delimiters, delimiter-dense text — so this
// stays true for the next provider's grammar too.
const leakedGrammar = `<｜DSML｜_web_search>{"query":"aforge harness token speed"}<｜/DSML｜_web_search><｜DSML｜tool_calls_end｜>`

func machineryTools(names ...string) []ai.ToolDefinition {
	tools := make([]ai.ToolDefinition, 0, len(names))
	for _, name := range names {
		tools = append(tools, ai.ToolDefinition{Type: "function", Function: ai.ToolFunction{Name: name}})
	}
	return tools
}

func machineryAnswer(text string) *ai.Response {
	return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{
		Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}},
	}}}}
}

// THE LEAK IS A SHAPE, AND THE SHAPE IS ENOUGH. The live grammar fires; the
// same grammar spelling a tool the request never declared does not, because a
// name the request did not send is a claim about nothing.
func TestMachineryLeakReadsTheShapeNotTheSyntax(t *testing.T) {
	request := &ai.Request{Tools: machineryTools("web_search", "read")}
	if !MachineryLeak(request, machineryAnswer(leakedGrammar)) {
		t.Fatal("the live leak was not read as machinery")
	}
	foreign := strings.ReplaceAll(leakedGrammar, "web_search", "somebody_elses_tool")
	if MachineryLeak(request, machineryAnswer(foreign)) {
		t.Fatal("grammar naming an undeclared tool was cut, but it makes no claim about this belt")
	}
	// And a different provider's fencing around the same declared name still
	// fires, which is the whole reason nothing here spells DSML.
	other := `[TOOL_REQUEST]{"name":"web_search","arguments":{"query":"x"}}[END_TOOL_REQUEST]`
	if !MachineryLeak(request, machineryAnswer(other)) {
		t.Fatal("the next provider's grammar was not read as machinery")
	}
}

// EVERY INNOCENT STAYS UNCUT. Prose about a tool, a fenced example, an answer
// on a request with no belt, and a reply that actually called its tool are the
// four neighbours of the leak, and cutting any of them is worse than the leak.
func TestMachineryLeakLeavesTheInnocentsAlone(t *testing.T) {
	request := &ai.Request{Tools: machineryTools("web_search")}
	innocents := map[string]struct {
		request  *ai.Request
		response *ai.Response
	}{
		"prose that names the tool": {request, machineryAnswer(
			"I could not reach the network, so web_search was not used. " +
				"Ask again and I will try web_search first.")},
		"a fenced example of the grammar": {request, machineryAnswer(
			"A tool call looks like this:\n```\n" + leakedGrammar + "\n```\nand the server parses it.")},
		"a request with no belt": {&ai.Request{}, machineryAnswer(leakedGrammar)},
		"an empty answer":        {request, machineryAnswer("")},
	}
	for name, tt := range innocents {
		if MachineryLeak(tt.request, tt.response) {
			t.Errorf("%s was cut", name)
		}
	}
	// A reply that CALLED the tool is clean whatever its text says: the
	// structured call is the proof the endpoint parsed its grammar.
	called := machineryAnswer(leakedGrammar)
	called.Choices[0].Message.ToolCalls = []ai.ToolCall{{ID: "one"}}
	if MachineryLeak(request, called) {
		t.Error("an answer with a structured tool call was cut")
	}
}

// THE WHOLE-BODY DOOR CUTS THE LEAK, AND THE CUT CARRIES EVERYTHING THE LADDER
// READS: the reason, the lane that served it, and whether the ledger struck it
// so the next ask lands elsewhere.
func TestALeakedCompletionEndsAsAMachineryCut(t *testing.T) {
	forgetLanes(t)
	recorded := &capture{}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		recorded.record(request)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"sim/model","provider":"leaky","choices":[{"index":0,` +
			`"finish_reason":"stop","message":{"role":"assistant","content":` +
			`"<｜DSML｜_web_search>{\"query\":\"x\"}<｜/DSML｜_web_search>"}}],` +
			`"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`))
	})
	// The base URL is the router's own, because the strike that [cut.Rerouted]
	// reports only exists where lanes do — see [Client.noteCutProvider].
	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: "https://openrouter.ai/api/v1", Model: "sim/model",
		Routing: StaticRouting(RoutingLatency), HTTPClient: handlerClient(handler),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.velocity = newVelocityLedger()
	_, err = client.CompleteWithMessages(context.Background(), userMessages("look this up"),
		ai.WithTools(machineryTools("web_search")))
	cut, ok := CutFrom(err)
	if !ok || cut.Reason != CutMachinery {
		t.Fatalf("err = %v, want a machinery cut", err)
	}
	if cut.Provider != "leaky" {
		t.Fatalf("cut.Provider = %q, want the lane that served the leak", cut.Provider)
	}
	if !cut.Rerouted {
		t.Fatal("the ledger did not strike the lane, so the retry would land on the same endpoint")
	}
}

// AND THE STREAMED DOOR CUTS IT TOO — both epilogues or neither, because the
// leak is the endpoint's and not the transport's.
func TestALeakedStreamEndsAsAMachineryCut(t *testing.T) {
	forgetLanes(t)
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(`data: {"id":"one","provider":"leaky","choices":[{"index":0,` +
			`"delta":{"content":"<｜DSML｜_web_search>{\"query\":\"x\"}"}}]}` + "\n\n" +
			`data: {"id":"one","choices":[{"index":0,"delta":{"content":"<｜/DSML｜_web_search>"},` +
			`"finish_reason":"stop"}]}` + "\n\n" + "data: [DONE]\n\n"))
	})
	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: "http://provider.test", Model: "sim/model",
		HTTPClient: handlerClient(handler),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithStreamObserver(context.Background(), func(StreamEvent) {})
	_, err = client.CompleteWithMessages(ctx, userMessages("look this up"),
		ai.WithTools(machineryTools("web_search")))
	cut, ok := CutFrom(err)
	if !ok || cut.Reason != CutMachinery {
		t.Fatalf("err = %v, want a machinery cut", err)
	}
	// A CLEAN STREAM ON THE SAME BELT STAYS AN ANSWER — the guard reads the
	// reply, never the request.
	clean := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte(`data: {"id":"two","choices":[{"index":0,` +
			`"delta":{"content":"I looked, and the answer is short."},"finish_reason":"stop"}]}` +
			"\n\n" + "data: [DONE]\n\n"))
	})
	client, err = NewClient(Config{
		APIKey: "test-key", BaseURL: "http://provider.test", Model: "sim/model",
		HTTPClient: handlerClient(clean),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CompleteWithMessages(ctx, userMessages("look this up"),
		ai.WithTools(machineryTools("web_search"))); err != nil {
		t.Fatal(err)
	}
}

// `reply guard off` PROMISES THE PERSON SEES WHATEVER ARRIVES, and this guard
// is a reading of the reply's shape exactly as the degeneration guard is — so
// the same switch takes both off.
func TestReplyGuardOffLetsTheLeakThrough(t *testing.T) {
	forgetLanes(t)
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"sim/model","choices":[{"index":0,` +
			`"finish_reason":"stop","message":{"role":"assistant","content":` +
			`"<｜DSML｜_web_search>{\"query\":\"x\"}<｜/DSML｜_web_search>"}}],` +
			`"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`))
	})
	client, err := NewClient(Config{
		APIKey: "test-key", BaseURL: "http://provider.test", Model: "sim/model",
		HTTPClient: handlerClient(handler),
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.CompleteWithMessages(WithoutBabbleGuard(context.Background()),
		userMessages("look this up"), ai.WithTools(machineryTools("web_search")))
	if err != nil {
		t.Fatalf("the guard cut with the switch off: %v", err)
	}
	if response == nil || len(response.Choices) == 0 {
		t.Fatal("no answer came back")
	}
}

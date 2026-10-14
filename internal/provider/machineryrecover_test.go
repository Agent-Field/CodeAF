package provider

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// These tests run against replies CAPTURED IN PRODUCTION, not written by hand
// here: testdata/machineryrecover_dsml.json holds six real replies this harness
// threw away — three where the model wrote a well-formed bash call as text, and
// three where it degenerated into prose after being told its call was rejected.
//
// The prose three are the reason this file exists as much as the leaks are. A
// repair that fires on a reply merely NAMING a tool destroys the only thing the
// person has left to read, and the shape that separates the two is not obvious
// from the markup alone: both spell `bash` in symbols, and both are dense enough
// for the guard to convict. What separates them is proportion — a leak is markup
// with a lead-in, a rant is markup inside a sentence.

type capturedReply struct {
	Content string   `json:"content"`
	Names   []string `json:"names"`
	Density float64  `json:"density"`
	Chars   int      `json:"chars"`
}

func loadCapturedReplies(t *testing.T) []capturedReply {
	t.Helper()
	raw, err := os.ReadFile("testdata/machineryrecover_dsml.json")
	if err != nil {
		t.Fatalf("read captured replies: %v", err)
	}
	var replies []capturedReply
	if err := json.Unmarshal(raw, &replies); err != nil {
		t.Fatalf("parse captured replies: %v", err)
	}
	if len(replies) == 0 {
		t.Fatal("no captured replies to test against")
	}
	return replies
}

func permittedNames(names ...string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

// The three captured leaks must come back as real calls, with the command the
// model actually wrote — not a cleaned-up paraphrase of it.
func TestRecoverMachineryReadsCapturedLeaksBackIntoCalls(t *testing.T) {
	recovered, refused := 0, 0
	for i, reply := range loadCapturedReplies(t) {
		calls, reason := ExplainMachinery(reply.Content, permittedNames("bash"))
		if !strings.Contains(reply.Content, "DSML") {
			// A captured degeneration. Nothing to recover, and recovering it
			// would be the bug this file exists to prevent.
			refused++
			if reason == recoverOK {
				t.Errorf("sample %d: narration was recovered as %d call(s); it must be refused", i, len(calls))
			}
			continue
		}
		recovered++
		if reason != recoverOK {
			t.Errorf("sample %d: want %q, got %q", i, recoverOK, reason)
			continue
		}
		if len(calls) != 1 {
			t.Errorf("sample %d: want exactly 1 call, got %d", i, len(calls))
			continue
		}
		if calls[0].Name != "bash" {
			t.Errorf("sample %d: want tool %q, got %q", i, "bash", calls[0].Name)
		}
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal([]byte(calls[0].Arguments), &args); err != nil {
			t.Errorf("sample %d: recovered arguments are not JSON: %v", i, err)
			continue
		}
		if args.Command == "" {
			t.Errorf("sample %d: recovered an empty command", i)
		}
		if !strings.Contains(args.Command, "/work") {
			t.Errorf("sample %d: recovered command lost the model's own text: %q", i, args.Command)
		}
	}
	if recovered < 2 {
		t.Fatalf("only %d captured leaks exercised; the corpus is not doing its job", recovered)
	}
	if refused < 2 {
		t.Fatalf("only %d captured narrations exercised; the refusal half is untested", refused)
	}
}

// The whole point of the change: a reply that carries a real call must stop
// being a cut and become an answered turn.
func TestRecoverMachineryTurnsTheReplyIntoAnAnsweredOne(t *testing.T) {
	var leak string
	for _, reply := range loadCapturedReplies(t) {
		if strings.Contains(reply.Content, "DSML") {
			leak = reply.Content
			break
		}
	}
	if leak == "" {
		t.Fatal("no captured leak to recover")
	}

	request := &ai.Request{Tools: []ai.ToolDefinition{{Function: ai.ToolFunction{Name: "bash"}}}}
	response := &ai.Response{Choices: []ai.Choice{{
		Message:      ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: leak}}},
		FinishReason: "stop",
	}}}

	if !MachineryLeak(request, response) {
		t.Fatal("this reply is no longer a leak; the fixture and the guard disagree")
	}
	calls, reason := RecoverMachinery(request, response)
	if reason != recoverOK {
		t.Fatalf("want %q, got %q", recoverOK, reason)
	}
	if len(calls) != 1 {
		t.Fatalf("want 1 recovered call, got %d", len(calls))
	}
	choice := response.Choices[0]
	if len(choice.Message.ToolCalls) != 1 {
		t.Fatalf("recovered call did not reach the response: %d tool calls", len(choice.Message.ToolCalls))
	}
	if choice.Message.ToolCalls[0].Function.Name != "bash" {
		t.Errorf("tool call carries %q, want bash", choice.Message.ToolCalls[0].Function.Name)
	}
	if choice.FinishReason != "tool_calls" {
		t.Errorf("finish reason is %q, want tool_calls", choice.FinishReason)
	}
	if len(choice.Message.Content) != 0 {
		t.Error("the markup is still in the content; it would be shown to the person as prose")
	}
	// A recovered reply is no longer a leak — which is what stops the cut.
	if MachineryLeak(request, response) {
		t.Error("a recovered reply still reads as a leak, so it would still be cut")
	}
}

// A refused recovery must leave the response EXACTLY as it found it, because the
// existing cut is still the fallback and it reads this same response.
//
// The sample is the one that matters most: a reply the guard convicts (it is
// symbol-dense and it spells a declared tool's name in symbol runes) that also
// carries a call to a tool nobody offered. Recovery must refuse it, and the
// refusal must leave the reply whole — otherwise the guard reads a reply whose
// content has already been edited on its way to being thrown away.
func TestRecoverMachineryLeavesTheResponseAloneWhenItRefuses(t *testing.T) {
	const text = `<｜DSML｜tool_calls>` +
		`<｜DSML｜invoke name="bash"><｜DSML｜parameter name="command">{"command":"ls"}</｜DSML｜parameter></｜DSML｜invoke>` +
		`<｜DSML｜invoke name="rm_rf"><｜DSML｜parameter name="path">{"path":"/"}</｜DSML｜parameter></｜DSML｜invoke>` +
		`</｜DSML｜tool_calls>`
	request := &ai.Request{Tools: []ai.ToolDefinition{{Function: ai.ToolFunction{Name: "bash"}}}}
	response := &ai.Response{Choices: []ai.Choice{{
		Message:      ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}}},
		FinishReason: "stop",
	}}}

	if !MachineryLeak(request, response) {
		t.Fatal("this reply is not a leak, so it is not the case this test is about")
	}
	calls, reason := RecoverMachinery(request, response)
	if reason == recoverOK {
		t.Fatalf("a call to a tool nobody offered was recovered as %d call(s)", len(calls))
	}
	if reason != recoverUnadvertised {
		t.Errorf("want %q for a smuggled tool name, got %q", recoverUnadvertised, reason)
	}
	if len(response.Choices[0].Message.ToolCalls) != 0 {
		t.Error("a refused recovery attached tool calls anyway")
	}
	if got := response.Choices[0].Message.Content[0].Text; got != text {
		t.Error("a refused recovery edited the content")
	}
	if response.Choices[0].FinishReason != "stop" {
		t.Error("a refused recovery changed the finish reason")
	}
}

// THE SAFETY ARGUMENT, pinned as a test.
//
// The parser alone will take a call out of a reply that is only QUOTING one: a
// person asked what a call looks like, and the honest answer spells the
// invocation. An earlier draft of this file assumed the guard behind recovery
// would keep such a reply out — that ordinary prose is not dense enough for
// [MachineryLeak] to convict.
//
// IT IS NOT. The markup itself carries the density, so a reply quoting an
// invocation IS convicted, and this test failing is what proved it. What
// actually keeps the quoting answer is the prose ceiling inside the parser
// (recoverProseCeiling): a leak is markup with a lead-in; a quotation is markup
// inside a sentence. Only one of the two is mostly markup.
func TestAQuotingReplyIsRefusedEvenThoughTheGuardConvictsIt(t *testing.T) {
	const quotation = "You asked what a bash tool call looks like. It is written like " +
		`<invoke name="bash"><parameter name="command">ls</parameter></invoke> in the model's own grammar.`
	request := &ai.Request{Tools: []ai.ToolDefinition{{Function: ai.ToolFunction{Name: "bash"}}}}
	response := &ai.Response{Choices: []ai.Choice{{
		Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: quotation}}},
	}}}

	if !MachineryLeak(request, response) {
		t.Log("the guard no longer convicts a quoting reply; the prose ceiling is now belt and braces")
	}
	calls, reason := ExplainMachinery(quotation, permittedNames("bash"))
	if reason == recoverOK {
		t.Fatalf("a reply QUOTING a call was recovered as %d call(s); it must be refused", len(calls))
	}
	if reason != recoverProse {
		t.Errorf("want %q for a quoting reply, got %q", recoverProse, reason)
	}
}

// The ceiling is a measured number, so the measurement is a test: every captured
// leak must sit under it and every quoting sample over it. A change that moved
// the bound without moving the corpus would pass the other tests and quietly
// stop recovering leaks, or start destroying answers.
func TestTheProseCeilingSeparatesCapturedLeaksFromQuotations(t *testing.T) {
	measure := func(text string) (float64, bool) {
		hay := recoverDSMLToken.ReplaceAllString(recoverFold.Replace(text), "")
		for _, envelope := range recoverEnvelopes {
			hay = strings.ReplaceAll(hay, envelope, "")
		}
		starts := recoverInvokeTag.FindAllStringIndex(hay, -1)
		if len(starts) == 0 {
			return 0, false
		}
		last := starts[len(starts)-1][1]
		for _, m := range starts {
			if at := strings.Index(hay[m[1]:], "</invoke>"); at >= 0 {
				if end := m[1] + at + len("</invoke>"); end > last {
					last = end
				}
			}
		}
		size := len([]rune(hay))
		if size == 0 {
			return 0, false
		}
		return float64(starts[0][0]+(size-last)) / float64(size), true
	}

	for i, reply := range loadCapturedReplies(t) {
		ratio, ok := measure(reply.Content)
		if !ok {
			continue
		}
		if strings.Contains(reply.Content, "DSML") && ratio > recoverProseCeiling {
			t.Errorf("captured leak %d measures prose ratio %.2f, over the %.2f ceiling: "+
				"the bound would refuse a reply this harness really threw away", i, ratio, recoverProseCeiling)
		}
	}

	for _, quotation := range []string{
		"You asked what a bash tool call looks like. It is written like " +
			`<invoke name="bash"><parameter name="command">ls</parameter></invoke> in the model's own grammar.`,
		"A tool call in this model's own grammar looks roughly like the following, and you can see " +
			`the name and the arguments. <invoke name="bash"><parameter name="command">{"command":"ls"}` +
			`</parameter></invoke> That is the whole shape of it.`,
	} {
		ratio, ok := measure(quotation)
		if !ok {
			t.Fatal("a quoting sample carried no invocation to measure")
		}
		if ratio <= recoverProseCeiling {
			t.Errorf("a quoting reply measures prose ratio %.2f, under the %.2f ceiling: "+
				"the bound would stop telling a leak from a quotation", ratio, recoverProseCeiling)
		}
	}
}

// Each refusal is load-bearing on its own, so each is pinned by name: a change
// that made any of them recoverable would dispatch work nobody asked for.
func TestExplainMachineryRefusesAndSaysWhy(t *testing.T) {
	cases := []struct {
		name  string
		text  string
		allow []string
		want  string
	}{
		{
			name:  "no text at all",
			text:  "   ",
			allow: []string{"bash"},
			want:  recoverNoText,
		},
		{
			name:  "prose with no markup",
			text:  "I will run the tests now and report what I find.",
			allow: []string{"bash"},
			want:  recoverNoTags,
		},
		{
			name:  "a tool the request never declared",
			text:  `<invoke name="bash"><parameter name="command">ls</parameter></invoke>`,
			allow: []string{"read_file"},
			want:  recoverUnadvertised,
		},
		{
			name: "a fenced code sample",
			text: "A call looks like this:\n```\n<invoke name=\"bash\">" +
				"<parameter name=\"command\">ls</parameter></invoke>\n```\n",
			allow: []string{"bash"},
			want:  recoverFenced,
		},
		{
			// A named-parameter call whose value is a bare word. This RECOVERS,
			// deliberately: "false", "17" and "ls" all arrive as text from
			// providers that do not JSON-encode parameter values, and a tool that
			// wanted the string is owed the string rather than a guess.
			name:  "a bare-word parameter is kept as text, not guessed at",
			text:  `<invoke name="bash"><parameter name="command">ls</parameter></invoke>`,
			allow: []string{"bash"},
			want:  recoverOK,
		},
		{
			// An arguments-parameter whose value is not JSON at all. There is
			// nothing to coerce here — the call claims one JSON object and does
			// not deliver one — so the whole repair is refused rather than
			// dispatched on a guess.
			name:  "an arguments-parameter that is not JSON is refused outright",
			text:  `<invoke name="bash"><parameter name="arguments">run the tests</parameter></invoke>`,
			allow: []string{"bash"},
			want:  recoverBadArguments,
		},
		{
			// Truncated mid-call. It recovers too, with the partial text kept as
			// the parameter's value, and that is the right answer for a TOOL: the
			// command it receives is malformed, fails, and comes back as an
			// ordinary tool error the model can retry. The alternative — refusing
			// — throws the turn away for a call that would have been harmless,
			// and this harness throws turns away for a living.
			name:  "a truncated parameter is passed through rather than invented",
			text:  `<invoke name="bash"><parameter name="command">{"command":"ls`,
			allow: []string{"bash"},
			want:  recoverOK,
		},
		{
			name: "markup with a long tail of talk after it",
			text: `<invoke name="bash"><parameter name="command">{"command":"ls"}</parameter></invoke>` +
				strings.Repeat(" and that is what a tool call looks like in the model's own grammar", 12),
			allow: []string{"bash"},
			want:  recoverProse,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls, reason := ExplainMachinery(tc.text, permittedNames(tc.allow...))
			if reason != tc.want {
				t.Fatalf("want %q, got %q", tc.want, reason)
			}
			if tc.want == recoverOK {
				if len(calls) == 0 {
					t.Fatalf("reason %q but no call came back", reason)
				}
				return
			}
			if calls != nil {
				t.Errorf("a refused parse still returned %d call(s)", len(calls))
			}
		})
	}
}

// An empty tool set means nothing was advertised, so nothing may be recovered —
// otherwise a reply could name a tool into existence.
func TestRecoverMachineryNeedsAnAdvertisedToolSet(t *testing.T) {
	request := &ai.Request{}
	response := &ai.Response{Choices: []ai.Choice{{
		Message: ai.Message{Content: []ai.ContentPart{{Type: "text", Text: `<invoke name="bash"></invoke>`}}},
	}}}
	if _, reason := RecoverMachinery(request, response); reason != recoverNoText {
		t.Fatalf("want %q with no advertised tools, got %q", recoverNoText, reason)
	}
}

// A call with a well-formed JSON object as its arguments recovers in the plain
// JSON-arguments form, which is what several providers emit.
func TestExplainMachineryReadsAPlainJSONArgumentObject(t *testing.T) {
	const reply = `Running it now. <invoke name="bash">{"command":"go test ./..."}</invoke>`
	calls, reason := ExplainMachinery(reply, permittedNames("bash"))
	if reason != recoverOK {
		t.Fatalf("want %q, got %q", recoverOK, reason)
	}
	if len(calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(calls))
	}
	if calls[0].Arguments != `{"command":"go test ./..."}` {
		t.Errorf("arguments came back as %q", calls[0].Arguments)
	}
}

// Two calls in one reply are both real work, and a reply that reads as a single
// envelope with both calls inside it must recover both.
func TestExplainMachineryReadsEveryCallInAMultiCallReply(t *testing.T) {
	const reply = `<invoke name="read_file"><parameter name="path">a.go</parameter></invoke>` +
		`<invoke name="read_file"><parameter name="path">b.go</parameter></invoke>`
	calls, reason := ExplainMachinery(reply, permittedNames("read_file"))
	if reason != recoverOK {
		t.Fatalf("want %q, got %q", recoverOK, reason)
	}
	if len(calls) != 2 {
		t.Fatalf("want 2 calls, got %d", len(calls))
	}
	if calls[0].Arguments != `{"path":"a.go"}` || calls[1].Arguments != `{"path":"b.go"}` {
		t.Errorf("arguments came back as %q and %q", calls[0].Arguments, calls[1].Arguments)
	}
}

// The id is derived, not random, so a repair repeated over an unchanged reply
// leaves the prompt prefix byte-identical and the cache still hits. A random id
// would re-key the prefix on every turn for the same work.
func TestRecoveredCallIDIsStableAndDistinct(t *testing.T) {
	first := recoveredCallID("bash", `{"command":"ls"}`)
	if again := recoveredCallID("bash", `{"command":"ls"}`); first != again {
		t.Errorf("the same call produced two ids: %q and %q", first, again)
	}
	if other := recoveredCallID("bash", `{"command":"pwd"}`); other == first {
		t.Error("two different calls produced the same id")
	}
	if !strings.HasPrefix(first, "call_") || len(first) != len("call_")+24 {
		t.Errorf("id %q is not the documented shape", first)
	}
}
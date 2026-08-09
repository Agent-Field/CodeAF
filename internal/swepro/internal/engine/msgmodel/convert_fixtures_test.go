package msgmodel

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// Replays testdata/convert-fixtures.json, produced by
// tools/fixtures/gen-convertmodelmessages.ts from the real
// `convertToModelMessages` in ai@6.0.168. This isolates half 2 of the
// ENGINE-DESIGN §5.4 pipeline — in particular R1, the rule that a `step-start`
// part flushes the assistant block into an assistant/tool ModelMessage pair.

// toolSpec mirrors the JSON-transportable toModelOutput spec in the generator.
type toolSpec struct {
	Kind   string `json:"kind"`
	Prefix string `json:"prefix"`
}

type convertArgs struct {
	Messages                  []UIMessage         `json:"messages"`
	Tools                     map[string]toolSpec `json:"tools"`
	IgnoreIncompleteToolCalls bool                `json:"ignoreIncompleteToolCalls"`
}

// convertOutcome is the {ok, value|message} envelope the generator records.
// Value is `any` so an EMPTY []ModelMessage still serialises as `[]` —
// omitempty on a slice would drop it, and the TS records `{"ok":true,"value":[]}`.
type convertOutcome struct {
	Ok      bool   `json:"ok"`
	Value   any    `json:"value,omitempty"`
	Message string `json:"message,omitempty"`
}

func buildToolMap(t *testing.T, specs map[string]toolSpec) map[string]ToolModelOutputFn {
	t.Helper()
	if specs == nil {
		return nil
	}
	out := make(map[string]ToolModelOutputFn, len(specs))
	for name, spec := range specs {
		spec := spec
		switch spec.Kind {
		case "passthrough":
			out[name] = func(_ string, _ RawValue, output RawValue) ToolOutput {
				return ToolOutput{Type: "json", Value: toJSONValue(output)}
			}
		case "text-prefix":
			out[name] = func(_ string, _ RawValue, output RawValue) ToolOutput {
				return ToolOutput{Type: "text", Value: spec.Prefix + jsString(output)}
			}
		case "content":
			out[name] = func(_ string, _ RawValue, output RawValue) ToolOutput {
				obj := RawObject(output)
				value := []any{}
				if text, ok := obj.StringField("text"); ok && text != "" {
					value = append(value, ToolOutputContentText{Type: "text", Text: text})
				}
				for _, att := range attachmentList(obj) {
					mime, _ := att.StringField("mime")
					url, _ := att.StringField("url")
					value = append(value, ToolOutputContentMedia{Type: "media", MediaType: mime, Data: url})
				}
				return ToolOutput{Type: "content", Value: value}
			}
		default:
			t.Fatalf("unknown tool spec kind %q", spec.Kind)
		}
	}
	return out
}

// jsString is `String(x)` over a JSON value — only the shapes the fixtures use.
func jsString(raw RawValue) string {
	if s, ok := asJSONString(raw); ok {
		return s
	}
	if len(raw) == 0 {
		return "undefined"
	}
	return string(compactJSON(raw))
}

func TestConvertToModelMessagesFixtures(t *testing.T) {
	fixtures := loadFixtures(t, "testdata/convert-fixtures.json")

	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.Name, func(t *testing.T) {
			if fixture.Fn != "convertToModelMessages" {
				t.Fatalf("unknown fn %q", fixture.Fn)
			}
			var args convertArgs
			decodeArgs(t, fixture.ArgsJSON, &args)

			var got convertOutcome
			out, err := ConvertToModelMessages(args.Messages, &ConvertOptions{
				IgnoreIncompleteToolCalls: args.IgnoreIncompleteToolCalls,
				Tools:                     buildToolMap(t, args.Tools),
			})
			if err != nil {
				got = convertOutcome{Ok: false, Message: err.Error()}
			} else {
				got = convertOutcome{Ok: true, Value: out}
			}

			raw, err := jscompat.Stringify(got)
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(raw) != fixture.OutJSON {
				t.Fatalf("mismatch\n  want %s\n  got  %s", fixture.OutJSON, string(raw))
			}
		})
	}

	if len(fixtures) < 30 {
		t.Fatalf("fixture corpus looks truncated: %d cases", len(fixtures))
	}
}

// UIPart carries `rawInput` presence, which a plain struct decode cannot
// distinguish from an explicit null. Guard the assumption the port relies on.
func TestUIPartRawInputPresence(t *testing.T) {
	var absent, present, null UIPart
	if err := json.Unmarshal([]byte(`{"type":"tool-a"}`), &absent); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"type":"tool-a","rawInput":"x"}`), &present); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"type":"tool-a","rawInput":null}`), &null); err != nil {
		t.Fatal(err)
	}
	if len(absent.RawInput) != 0 {
		t.Fatalf("absent rawInput should be empty, got %q", absent.RawInput)
	}
	if string(present.RawInput) != `"x"` || string(null.RawInput) != "null" {
		t.Fatalf("rawInput decode: present=%q null=%q", present.RawInput, null.RawInput)
	}
}

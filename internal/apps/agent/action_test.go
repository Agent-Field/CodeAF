package agent

import (
	"reflect"
	"testing"
)

func TestParseAction(t *testing.T) {
	query := action{Tool: "records.query", Args: map[string]any{"type": "contact"}}
	tests := []struct {
		name, reply string
		want        action
	}{
		{"plain answer", "  Ada works at Hooli.  ", action{Say: "Ada works at Hooli."}},
		{"two actions", `{"tool":"records.query","args":{"type":"contact"}} {"tool":"records.delete","args":{"id":"ct_1"}}`, action{Tool: query.Tool, Args: query.Args, dropped: 1}},
		{"JSON and prose", `Let me look. {"tool":"records.query","args":{"type":"contact"}} I'll report back.`, query},
		{"fenced", "Before\n```json\n{\"tool\":\"records.query\",\"args\":{\"type\":\"contact\"}}\n```\nAfter", query},
		{"unlabelled fence", "```\n{\"say\":\"Ada works at Hooli.\"}\n```", action{Say: "Ada works at Hooli."}},
		{"nested braces and escapes", `{"tool":"records.create","args":{"type":"contact","set":{"name":"A } { \\\" B"}}}`, action{Tool: "records.create", Args: map[string]any{"type": "contact", "set": map[string]any{"name": `A } { \" B`}}}},
		{"nonaction before action", `{"total":1} {"say":"Found Ada."}`, action{Say: "Found Ada."}},
		{"malformed wrapper", `{{"tool":"records.query","args":{"type":"contact"}}}`, query},
		{"malformed before valid", `{"tool":123} {"say":"Found Ada."}`, action{Say: "Found Ada."}},
		{"incomplete before valid", "{\"tool\":\n{\"say\":\"Found Ada.\"}", action{Say: "Found Ada."}},
		{"malformed fragment", `I could not finish. {"tool":"records.query","args": }`, action{Say: "I could not finish."}},
		{"unclosed fragment", `I could not finish. {"tool":"records.query","args":{"type":"contact"}`, action{Say: "I could not finish."}},
		{"truncated key", `I could not finish. {"tool`, action{Say: "I could not finish."}},
		{"only malformed fenced", "```json\n{\"tool\":\"records.query\",\"args\":}\n```", action{}},
		{"ordinary JSON", `The data is {"total":1}.`, action{Say: `The data is {"total":1}.`}},
		{"unknown object", `{"other":true}`, action{Say: `{"other":true}`}},
		{"empty answer", `{"say":""}`, action{}},
		{"shape", `Here: {"shape_this":true}`, action{ShapeThis: true}},
		{"embedded protocol in say", `{"say":"Found Ada. {\"tool\":\"records.delete\",\"args\":{\"id\":\"ct_1\"}}"}`, action{Say: "Found Ada."}},
		{"nested protocol in answer", `{"say":"Found Ada. {\"data\":{\"tool\":\"records.delete\"}}"}`, action{Say: "Found Ada."}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseAction(tc.reply); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseAction = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestSafeText(t *testing.T) {
	tests := []struct{ name, text, want string }{
		{"middle", `Before {"tool":"records.query","args":{}} after.`, "Before  after."},
		{"several", `{"tool":"records.query"} Done. {"say":"secret"}`, "Done."},
		{"malformed", `Done. {tool:"records.query"}`, "Done."},
		{"wrong types", `Done. {"say":false}`, "Done."},
		{"escaped protocol key", `Done. {"\u0074ool":"records.query"}`, "Done."},
		{"args fragment", `Done. {"args":{"type":"contact"}}`, "Done."},
		{"ordinary braces", "Set {a, b} is ready.", "Set {a, b} is ready."},
		{"ordinary code fence", "```go\nfmt.Println(\"Done\")\n```", "```go\nfmt.Println(\"Done\")\n```"},
		{"unmatched ordinary brace", "Set {a has {\"tool\":\"records.query\"}", "Set"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := safeText(tc.text); got != tc.want {
				t.Fatalf("safeText = %q, want %q", got, tc.want)
			}
		})
	}
}

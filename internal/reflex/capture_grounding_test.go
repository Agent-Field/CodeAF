package reflex

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// Worked examples are instructions too. Check the actual wire prompt rather
// than a parallel fixture: an assistant-only detail in a positive keep teaches
// the opposite of the source rule, even when that rule is stated correctly.
func TestNoticeWireExamplesDoNotAdoptUnacceptedAssistantDetails(t *testing.T) {
	client := &fake{replies: []string{`{"keep":""}`}}
	if _, err := Notice(context.Background(), client, "I like concise explanations", "I will add numbered footnotes too", nil, true); err != nil {
		t.Fatal(err)
	}
	wire := prompt(client.calls[0])
	pattern := regexp.MustCompile(`(?s)\nUSER:\n(.*?)\n\nASSISTANT:\n(.*?)\n\nKEPT:\n(.*?)\n\n(\{"keep":.*?\})`)
	examples := pattern.FindAllStringSubmatch(wire, -1)
	for _, tc := range []struct {
		person, assistantOnly string
		conditions            []string
	}{
		{"stop explaining", "say what I changed", []string{"not an explanation first"}},
		{"For Quartz reports", "rotate labels by 45 degrees", []string{"only when multiple series", "never smooth raw measurements"}},
	} {
		found := false
		for _, example := range examples {
			if !strings.Contains(example[1], tc.person) {
				continue
			}
			found = true
			if !strings.Contains(example[2], tc.assistantOnly) {
				t.Fatalf("example lost the assistant-only counterexample %q", tc.person)
			}
			var result struct {
				Keep string `json:"keep"`
			}
			if err := json.Unmarshal([]byte(example[4]), &result); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(result.Keep, tc.assistantOnly) || strings.Contains(result.Keep, "short note of what changed") || strings.Contains(result.Keep, "45 degrees") {
				t.Fatalf("positive capture teaches an unaccepted assistant addition: %q", result.Keep)
			}
			for _, condition := range tc.conditions {
				if !strings.Contains(result.Keep, condition) {
					t.Fatalf("example dropped a condition %q", condition)
				}
			}
		}
		if !found {
			t.Fatalf("actual wire lacks source-grounding example %q", tc.person)
		}
	}
}

func TestNoticeStillAcceptsNormalizedPreferencesAndConfirmedDecisions(t *testing.T) {
	for _, tc := range []struct{ name, person, assistant, keep, durability string }{
		{"normalized preference", "I like concise explanations", "Understood", "prefers concise explanations", "standing_rule"},
		{"accepted proposal", "Yes, use that as our project default", "Then Quartz exports will use UTF-8", "Quartz exports use UTF-8", "decision"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]string{"keep": tc.keep, "durability": tc.durability, "scope": "project"})
			client := &fake{replies: []string{string(raw)}}
			got, err := Notice(context.Background(), client, tc.person, tc.assistant, nil, true)
			if err != nil || got.Keep != tc.keep || got.Durability != tc.durability {
				t.Fatalf("ordinary capture was restricted: %+v %v", got, err)
			}
			if !strings.Contains(prompt(client.calls[0]), "An explicit acceptance") {
				t.Fatal("wire prompt lost confirmation guidance")
			}
		})
	}
}

package desktopbridge

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

func TestDoorMatchAcceptsABareKeyAndAJSONPick(t *testing.T) {
	options := []placeorg.Option{{Key: "month", Text: "organise this place by month"}, {Key: "none", Text: "none of these"}}
	for _, raw := range []string{"month", `{"pick":"month"}`, "organise this place by month"} {
		key, ok := doorMatch(raw, options)
		if !ok || key != "month" {
			t.Fatalf("%q -> %q ok=%v", raw, key, ok)
		}
	}
	if _, ok := doorMatch("sideways", options); ok {
		t.Fatal("an unknown answer matched")
	}
	prompt := doorPrompt(placeorg.Question{Purpose: "which view", Context: "organise this by month", Options: options})
	if !strings.Contains(prompt, "month") || !strings.Contains(prompt, `"pick"`) {
		t.Fatalf("prompt %q", prompt)
	}
}

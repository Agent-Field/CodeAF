package plan

import (
	"strings"
	"testing"
)

// TestFanoutPromptStatesTheSubjectTest pins the rule that decides whether the
// harness runs four agents over one procedure or one agent over four subjects.
// The judgment itself is the model's, so what is testable here is that the test
// it is judged against is actually stated: the distinction, an example of each
// side, and the instruction for what to do when a split comes out phase-shaped.
func TestFanoutPromptStatesTheSubjectTest(t *testing.T) {
	for _, want := range []struct {
		name   string
		phrase string
	}{
		{"ownable subject", "a subject someone can own"},
		{"independence test", "knowing nothing about what the other parts"},
		{"phases are never parts", "Phases of one procedure are never parts"},
		{"phase example", `"write it up" is one procedure`},
		{"subject example", "one part per venue, per component, per"},
		{"keep the procedure whole", "Keep the whole procedure inside one node"},
	} {
		t.Run(want.name, func(t *testing.T) {
			if !strings.Contains(fanoutPrompt, want.phrase) {
				t.Errorf("fan-out prompt no longer states %s: missing %q", want.name, want.phrase)
			}
		})
	}
}

// TestFanoutPromptStatesTheSharedSetupRule covers the other exception in the
// same prompt: a genuine mutating action is one early part everything else works
// behind, and it carries the orientation the siblings would otherwise each redo.
func TestFanoutPromptStatesTheSharedSetupRule(t *testing.T) {
	for _, want := range []struct {
		name   string
		phrase string
	}{
		{"mutating action is its own part", "changes the material every other part works from"},
		{"it comes first", "one part on its own, and it comes first"},
		{"orientation lives there", "shared orientation summary belongs"},
		{"no re-reading", "do not each re-read the same material"},
		{"not invented", "Do not invent one where nothing"},
	} {
		t.Run(want.name, func(t *testing.T) {
			if !strings.Contains(fanoutPrompt, want.phrase) {
				t.Errorf("fan-out prompt no longer states %s: missing %q", want.name, want.phrase)
			}
		})
	}
}

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

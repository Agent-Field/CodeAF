package manual

import (
	"strings"
	"testing"
)

// Reversal questions live beside their feature page so parallel desktop lanes
// do not need to edit the shared retrieval table.
func TestDecisionReversalManualAnswersEngineLimits(t *testing.T) {
	for _, probe := range []struct{ question, heading, needle string }{
		{"undo an automatic decision revoke consent or stop the task it started", "Undo an automatic decision", "This decision cannot be undone."},
		{"next time always ask me or keep deciding after an overturn", "Next time always ask me", "back to learning"},
		{"which tasks and decisions used this decision notify or pause without cascading", "Which tasks and decisions", "Nothing is automatically stopped"},
	} {
		found := false
		for _, section := range Chat().Search(probe.question, DefaultResults) {
			if section.Page == "desktop-decision-reversal" && strings.Contains(section.Title, probe.heading) {
				found = true
				if !strings.Contains(section.Body, probe.needle) {
					t.Errorf("%q omits %q", probe.question, probe.needle)
				}
			}
		}
		if !found {
			t.Errorf("%q does not reach desktop-decision-reversal · %s", probe.question, probe.heading)
		}
	}
}

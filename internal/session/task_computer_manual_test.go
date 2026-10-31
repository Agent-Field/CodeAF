package session

import (
	"strings"
	"testing"
)

// DESIGN 16 requires the rendered guidance and schema to agree for every
// belt shape, while the shared static prompt makes no unconditional promise.
func TestTheManualComputerGuidanceMatchesTheAvailableArgument(t *testing.T) {
	if strings.Contains(systemPrompt, "`computer`") {
		t.Fatal("the static system prompt promises a conditional argument")
	}
	for _, shape := range []struct {
		name                    string
		paired, inTask, offered bool
	}{
		{"paired conversation", true, false, true},
		{"unpaired conversation", false, false, false},
		{"worker without proposals", true, true, false},
	} {
		t.Run(shape.name, func(t *testing.T) {
			a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
				c.InTask = shape.inTask
				if shape.paired {
					c.TaskComputers = testTaskComputers{}
				}
			})
			argument := false
			for _, tool := range a.taskTools() {
				_, argument = schemaProperties(t, tool.Schema)["computer"]
			}
			guidance := strings.Contains(promptWithBeltFacts(a.config), "optional `computer` argument")
			if argument != shape.offered || guidance != shape.offered {
				t.Fatalf("computer argument=%v, guidance=%v, want both %v", argument, guidance, shape.offered)
			}
		})
	}
}

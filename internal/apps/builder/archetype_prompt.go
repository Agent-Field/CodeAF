package builder

import (
	"encoding/json"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

// The same instruction fragment is available to the Builder and its harness.
const archetypeInstructions = `Start each work screen with an archetype. Use recipe with kind "add-screen",
as the archetype, id and title for the job, record for the main type, fields for
record facts, and slots for the remaining inputs. Call parts with
{"for":"add-screen/<archetype>"} to read the nearest checked example.

Choose queue-detail for ranked triage, brief for a morning summary and what
needs attention, board-detail for work that moves through stages, dashboard
for metrics and exceptions, record for a full record workspace, inbox-thread
for customer conversations, agenda for a schedule, and canvas-visual for work
centered on a map, timeline, diagram, or canvas.

For a Brief, lead with one useful summary. Put a ranked queue beside the
agenda and what changed. Give short collections a compact section and a
specific name. Sections describe jobs such as Needs review or Handover. Use
one signature visual tied to the work, such as evidence, a schedule, or a
trend with its target. Keep the rows readable in Dark, Black, and Light at
both 800 and 1120 px.`

func archetypeExample(archetype string) (json.RawMessage, error) {
	var example, screen string
	switch archetype {
	case "queue-detail", "record":
		example, screen = "gallery-kyc", "queue"
	case "board-detail", "agenda":
		example, screen = "gallery-hiring", "candidates"
	case "brief", "dashboard", "canvas-visual":
		example, screen = "gallery-line", "lines"
	case "inbox-thread":
		example, screen = "gallery-support", "inbox"
	default:
		return nil, fmt.Errorf("unknown screen archetype %q", archetype)
	}
	raw, err := schema.BuilderExampleFile(example, "screens/"+screen+".json")
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Archetype string          `json:"archetype"`
		Gallery   json.RawMessage `json:"gallery"`
	}{archetype, raw})
}

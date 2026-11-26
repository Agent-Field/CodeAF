package builder

import (
	"encoding/json"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

// The same instruction fragment is available to the Builder and its harness.
const archetypeInstructions = `Start work screens with an add-screen recipe. Set as to queue-detail (triage),
brief (today's priorities), board-detail (stages), dashboard (metrics),
record (details), inbox-thread (messages), agenda (schedule), or canvas-visual
(visual workspace). Read parts with {"for":"add-screen/<archetype>"} for the
nearest gallery example. Use slots for each job's inputs.

Name sections for jobs. Give each app a visual fitted to its data. For a brief,
lead with a summary and pair a ranked attention list with the day agenda and
recent changes. Check Dark, Black, and Light at 800 and 1120 px.`

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

package appwire

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// A FACTORY RUNNER'S STEP LINE CARRIES ITS STEP AS A FACT beside its words, and
// every other entry carries none: the field is left out, never sent empty. The
// schema names it on Entry, optional, with the five events it may say.
func TestAStepLineCarriesItsStepThroughTheWire(t *testing.T) {
	line := Entry{ID: "a0.1", Agent: "job", Kind: EntryAnswer, Text: "review done · 4m · clean",
		Progress: &EntryProgress{Step: "review", Round: 2, Event: "done"}}
	raw, err := json.Marshal(line)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"progress":{"step":"review","round":2,"event":"done"}`) || !strings.Contains(string(raw), `"text":"review done · 4m · clean"`) {
		t.Fatalf("the step line went out as %s", raw)
	}
	var back Entry
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.Progress == nil || *back.Progress != *line.Progress || back.Text != line.Text {
		t.Fatalf("the step line came back as %+v", back)
	}
	plain, err := json.Marshal(Entry{ID: "a0.0", Agent: "job", Kind: EntryAnswer, Text: "landed"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "progress") {
		t.Fatalf("a line about the job carries a step: %s", plain)
	}

	out, err := SchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]struct {
			Properties map[string]map[string]any `json:"properties"`
			Required   []string                  `json:"required"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if entry := doc.Defs["Entry"]; entry.Properties["progress"] == nil || slices.Contains(entry.Required, "progress") {
		t.Fatalf("the schema's Entry says progress as %v (required %v)", entry.Properties["progress"], entry.Required)
	}
	event := doc.Defs["EntryProgress"].Properties["event"]
	words, _ := event["enum"].([]any)
	var said []string
	for _, w := range words {
		said = append(said, w.(string))
	}
	if strings.Join(said, ",") != "started,done,failed,stopped,waiting" {
		t.Fatalf("the schema's progress event may say %v", event)
	}
}

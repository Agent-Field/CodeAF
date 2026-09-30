package delegate

import (
	"encoding/json"
	"strings"
	"testing"
)

// THE MODELS A PROGRAM SAYS IT RUNS ON ARE KEPT ON ITS RECORD. A stage whose
// data names `models` and `effort` fills the record once, a repeat of the same
// pair changes nothing, and a stage that names none — or names only blanks —
// leaves what the record already holds.
func TestAStageThatNamesItsModelsIsKeptOnTheProgramRecord(t *testing.T) {
	stage := func(data string) StageRecord {
		return StageRecord{Stage: "bootstrap", Status: "ready", Data: json.RawMessage(data)}
	}
	var record ProgramRecord
	if record.Heard(stage(`{"recorder":"git"}`)) || record.Heard(StageRecord{Stage: "intake"}) || record.Heard(stage(`{"models":[" ",""]}`)) {
		t.Fatalf("a stage that names no model changed the record: %+v", record)
	}
	if !record.Heard(stage(`{"recorder":"git","models":["deepseek/deepseek-v4-pro","z-ai/glm-5.1","z-ai/glm-5.1"],"effort":"high"}`)) {
		t.Fatal("the models the program named were not kept")
	}
	if strings.Join(record.Models, ",") != "deepseek/deepseek-v4-pro,z-ai/glm-5.1" || record.Effort != "high" {
		t.Fatalf("record = %+v, want the two models once each and the effort", record)
	}
	if record.Heard(stage(`{"models":["deepseek/deepseek-v4-pro","z-ai/glm-5.1"],"effort":"high"}`)) {
		t.Fatal("the same pair said again asked for another write")
	}
	if record.Heard(stage(`{"attempt":2}`)) || len(record.Models) != 2 {
		t.Fatalf("a later stage that names no model took the models away: %+v", record)
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var back ProgramRecord
	if err := json.Unmarshal(data, &back); err != nil || strings.Join(back.Models, ",") != strings.Join(record.Models, ",") || back.Effort != "high" {
		t.Fatalf("the record did not survive the disk: %s", data)
	}
}

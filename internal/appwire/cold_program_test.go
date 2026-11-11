package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

// THE COLD PROGRAM FACTS ARE OPTIONAL ON THE WIRE: absent from a task that has
// none (absent is unknown) and round-tripped exactly when present.
func TestTaskColdProgramFactsAreOptional(t *testing.T) {
	raw, err := json.Marshal(Task{ID: "a/1", State: TaskFailed})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"program"`, `"ending"`, `"branch"`} {
		if strings.Contains(string(raw), key) {
			t.Fatalf("a task with none says %s: %s", key, raw)
		}
	}
	raw, _ = json.Marshal(Task{ID: "a/2", State: TaskFailed, Program: "senior-dev", Ending: "stopped", Branch: "task/x"})
	var back Task
	if err := json.Unmarshal(raw, &back); err != nil || back.Program != "senior-dev" || back.Ending != "stopped" || back.Branch != "task/x" {
		t.Fatalf("round trip = %+v, %v", back, err)
	}
	var old Task
	if err := json.Unmarshal([]byte(`{"id":"a/3","state":"failed"}`), &old); err != nil || old.Program != "" || old.Ending != "" || old.Branch != "" {
		t.Fatalf("legacy = %+v, %v", old, err)
	}
}

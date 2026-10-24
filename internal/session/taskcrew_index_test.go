package session

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/crewroute"
)

func TestTaskIndexKeepsCrewWithItsCanonicalResult(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = t.TempDir() + "/conversation.jsonl" })
	crew := &crewroute.Decision{EstUSD: .52, Stopped: "add credits", Crew: []crewroute.Pick{{Seat: crewroute.Worker, Model: "worker-model"}, {Seat: crewroute.Planner, Model: "planner-model"}, {Seat: crewroute.Checker, Model: "checker-model"}}}
	agent.indexRunRow(TaskNotice{ID: 7, Title: "Repair export", State: TaskFailed, Crew: crew, Model: "worker-model", CostUSD: .31})
	rows := ReadTaskIndex(agent.config.taskIndexFile())
	if len(rows) != 1 || rows[0].Crew == nil {
		t.Fatalf("crew missing from task result: %+v", rows)
	}
	got := rows[0]
	if got.Model != "worker-model" || got.Cost != .31 || got.Crew.EstUSD != .52 || got.Crew.Stopped != "add credits" || len(got.Crew.Crew) != 3 {
		t.Fatalf("result lost crew facts: %+v", got)
	}
	var legacy TaskIndexEntry
	if err := json.Unmarshal([]byte(`{"id":"7","title":"Old result","status":"done"}`), &legacy); err != nil || legacy.Crew != nil {
		t.Fatalf("legacy index cannot load: %+v %v", legacy, err)
	}
}

// Older installations already keep routed crews in the run checkpoint, even
// when the project index predates its crew field. Restore uses those exact facts.
func TestOlderRunCheckpointRestoresCrewWithoutTaskIndexMigration(t *testing.T) {
	raw := []byte(`{"id":7,"title":"Repair export","state":"failed","crewState":{"Version":1,"Routed":true,"Decision":{"EstUSD":0.52,"Stopped":"add credits","Crew":[{"Seat":"worker","Model":"worker-model"},{"Seat":"planner","Model":"planner-model"},{"Seat":"checker","Model":"checker-model"}]}}}`)
	var saved runRecord
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	notice := runRowNotice(saved)
	if notice.Crew == nil || notice.Crew.Stopped != "add credits" || notice.Crew.EstUSD != .52 || len(notice.Crew.Crew) != 3 {
		t.Fatalf("old checkpoint crew facts not restored: %+v", notice.Crew)
	}
}

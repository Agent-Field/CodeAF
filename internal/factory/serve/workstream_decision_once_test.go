package serve

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ONE ANSWER IS ONE DECISION (live2 E1 F6, E2 #5). Answer together on two
// tasks carrying the same Ask keeps one decision, and one request noted on
// three tasks keeps one too; a different answer is still its own.
func TestOneAnswerOrRequestIsOneDecision(t *testing.T) {
	o, path := taskOrg(t, "edit")
	ask := map[string]any{"id": "ask_group", "title": "Which TestFlight group first?", "options": []map[string]any{{"id": "internal", "label": "Internal team only"}, {"id": "everyone", "label": "Everyone"}}}
	var ids []string
	for _, title := range []string{"Ship the iPhone build", "Ship the iPad build", "Ship the watch build"} {
		task := o.task(t, "santosh", path, api.TaskCreate{Title: title, DoneBy: factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: "santosh", Name: "santosh"}})
		var asked api.WorkstreamTask
		o.do(t, "santosh", "PATCH", path+"/tasks/"+task.ID, task.Rev, map[string]any{"ask": ask}, 200, &asked)
		ids = append(ids, asked.ID)
	}
	answer := func(id string, option int) {
		t.Helper()
		got := o.one(t, path, id)
		o.do(t, "santosh", "POST", path+"/tasks/"+id+"/answer", -1, api.TaskAnswer{Option: got.Ask.Options[option].ID}, 200, nil)
	}
	// Answer together is one answer posted to each task.
	answer(ids[0], 0)
	answer(ids[1], 0)
	answer(ids[2], 1)
	for _, id := range ids {
		o.do(t, "santosh", "POST", path+"/tasks/"+id+"/comment", -1, api.TaskComment{TaskNote: factory.TaskNote{Kind: factory.TaskEventRequestNote, Text: "Marco asked: no Pantry Plus pricing", Decision: "No Pantry Plus pricing in 2.4 launch copy"}}, 201, nil)
	}
	var ws api.Workstream
	o.do(t, "santosh", "GET", path, -1, nil, 200, &ws)
	count := map[string]int{}
	for _, d := range ws.Decisions {
		count[d.Text]++
	}
	for text, want := range map[string]int{
		"Which TestFlight group first? Internal team only": 1,
		"Which TestFlight group first? Everyone":           1,
		"No Pantry Plus pricing in 2.4 launch copy":        1,
	} {
		if count[text] != want {
			t.Fatalf("%q is kept %d times, want %d: %+v", text, count[text], want, ws.Decisions)
		}
	}
}

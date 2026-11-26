package serve

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// AN ANSWER THAT NEEDS THE PERSON'S WORDS IS NEVER ONE CLICK (live test
// 2026-11-23 #37). The agent asked for dates, a budget and prices, marked the
// Ask reversible, and recommended "Paste venue options with prices": the Inbox
// filed it under Quick, where accepting it would have answered with nothing.
// ONE RULE, IN THE ANSWER: the Ask keeps the reversible the agent gave it and
// its answer keeps its words mark (so no door calls it quick); the words
// answer is refused without words and is never undoable once given, while a
// plain answer to the same Ask still is.
func TestAnAskThatNeedsThePersonsWordsIsNeverQuick(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Offsite"})
	d := &agentDoor{h: o.h, id: ws.ID, asker: "santosh"}
	ctx := context.Background()
	work := func(tool, args string) map[string]any {
		t.Helper()
		line, err := d.Work(ctx, tool, json.RawMessage(args))
		if err != nil {
			t.Fatalf("%s %s: %v", tool, args, err)
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(line), &out); err != nil {
			t.Fatalf("%s answered %q", tool, line)
		}
		return out
	}
	stored := func(id string) map[string]any {
		t.Helper()
		var got map[string]any
		o.do(t, "santosh", "GET", "/v1/workstreams/"+ws.ID+"/tasks/"+id, -1, nil, 200, &got)
		return got
	}
	asking := func(title string) (string, map[string]any) {
		t.Helper()
		id := work("ws_task_create", `{"title":"`+title+`","request":"Three venues with prices.","goal":"Pick a venue","deliverable":"A document in Files","acceptance":["Three venues","Prices for 12"]}`)["task"].(map[string]any)["id"].(string)
		work("ws_task_update", `{"task":"`+id+`","ask":{"title":"Need dates and prices","reversible":true,"options":[{"label":"Paste venue options with prices","recommended":true,"words":true},{"label":"Any venue will do"}]}}`)
		ask, _ := stored(id)["ask"].(map[string]any)
		if ask == nil {
			t.Fatalf("no Ask kept on %s", id)
		}
		return id, ask
	}
	answer := func(id, option, text string, status int) {
		t.Helper()
		o.do(t, "santosh", "POST", "/v1/workstreams/"+ws.ID+"/tasks/"+id+"/answer", -1, api.TaskAnswer{Option: option, Text: text}, status, nil)
	}

	id, ask := asking("Venue shortlist")
	if ask["reversible"] != true {
		t.Errorf("an Ask with a words answer lost the reversible the agent gave it: %v", ask)
	}
	opts, _ := ask["options"].([]any)
	if len(opts) != 2 || opts[0].(map[string]any)["words"] != true {
		t.Fatalf("the answer lost its words mark: %v", opts)
	}
	wordsOpt, plainOpt := opts[0].(map[string]any)["id"].(string), opts[1].(map[string]any)["id"].(string)
	answer(id, wordsOpt, "", 422)
	answer(id, wordsOpt, "June 3 to 5, under $4,000: Hall A $900, Hall B $1,200", 200)
	if undo, _ := stored(id)["undoable"].([]any); len(undo) != 0 {
		t.Errorf("a words answer can be undone: %v", undo)
	}

	id2, _ := asking("Second shortlist")
	answer(id2, plainOpt, "", 200)
	if undo, _ := stored(id2)["undoable"].([]any); len(undo) != 1 {
		t.Errorf("a plain answer to a reversible Ask with a words answer cannot be undone: %v", undo)
	}
}

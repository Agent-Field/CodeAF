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
// An Ask holding an answer that needs words is kept not reversible, whatever
// the agent said, and the answer keeps its mark for the app.
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
	id := work("ws_task_create", `{"title":"Venue shortlist","request":"Three venues with prices.","goal":"Pick a venue","deliverable":"A document in Files","acceptance":["Three venues","Prices for 12"]}`)["task"].(map[string]any)["id"].(string)
	work("ws_task_update", `{"task":"`+id+`","ask":{"title":"Need dates and prices","reversible":true,"options":[{"label":"Paste venue options with prices","recommended":true,"words":true},{"label":"Do it myself"}]}}`)
	stored := func(id string) map[string]any {
		t.Helper()
		var got map[string]any
		o.do(t, "santosh", "GET", "/v1/workstreams/"+ws.ID+"/tasks/"+id, -1, nil, 200, &got)
		return got
	}
	got := stored(id)
	ask, _ := got["ask"].(map[string]any)
	if ask == nil {
		t.Fatalf("no Ask kept on %v", got)
	}
	if ask["reversible"] == true {
		t.Errorf("an Ask whose answer needs words stayed reversible: %v", ask)
	}
	opts, _ := ask["options"].([]any)
	if len(opts) == 0 || opts[0].(map[string]any)["words"] != true {
		t.Errorf("the answer lost its words mark: %v", opts)
	}

	// The same Ask without the mark stays the agent's word: reversible.
	id2 := work("ws_task_create", `{"title":"Headline","request":"Pick one.","goal":"Ship it","deliverable":"A line","acceptance":["One headline","Plain words"]}`)["task"].(map[string]any)["id"].(string)
	work("ws_task_update", `{"task":"`+id2+`","ask":{"title":"Which headline?","reversible":true,"options":[{"label":"Short","recommended":true},{"label":"Long"}]}}`)
	plain := stored(id2)
	if pa, _ := plain["ask"].(map[string]any); pa == nil || pa["reversible"] != true {
		t.Errorf("a plain reversible Ask lost its mark: %v", plain["ask"])
	}
}

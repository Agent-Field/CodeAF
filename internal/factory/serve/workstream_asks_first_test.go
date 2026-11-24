package serve

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A PLAN SAYS WHICH TASKS WILL ASK FIRST, AND THE RUN KEEPS IT (lane L7, v2
// J6 and screen 7). The task list a plan card and a bulk offload draw from
// says asks_first on the task that posts, the run's opening words carry the
// rule, turning the rule off clears the prediction and is a decision, and the
// check a run writes says how many decisions it was checked against.
func TestAPlanSaysWhoAsksFirstAndTheRunAsks(t *testing.T) {
	o := newWSOrg(t)
	ws, _, _, _ := o.orgScene(t)
	ctx := context.Background()
	d := &agentDoor{h: o.h, id: ws.ID, asker: "mia"}
	work := func(tool, args string) map[string]any {
		t.Helper()
		line, err := d.Work(ctx, tool, json.RawMessage(args))
		if err != nil {
			t.Fatalf("%s %s: %v", tool, args, err)
		}
		var out map[string]any
		_ = json.Unmarshal([]byte(line), &out)
		return out
	}
	const brief = `"goal":"Tell the community","deliverable":"A post","acceptance":["Names both changes","Under 100 words"]`
	post := work("ws_task_create", `{"title":"Post the notes to Discord","request":"Post them in #announcements.",`+brief+`}`)["task"].(map[string]any)["id"].(string)
	draft := work("ws_task_create", `{"title":"Draft the release notes","request":"Plain words.",`+brief+`}`)["task"].(map[string]any)["id"].(string)

	asks := func() map[string]string {
		t.Helper()
		var list api.WorkstreamTasks
		o.do(t, "abir", "GET", "/v1/workstreams/"+ws.ID+"/tasks", -1, nil, 200, &list)
		out := map[string]string{}
		for _, x := range list.Tasks {
			out[x.ID] = x.AsksFirst
		}
		return out
	}
	if got := asks(); got[post] != "posting" || got[draft] != "" {
		t.Fatalf("asks first reads %v, want only the Discord post (posting)", got)
	}
	packet, _ := o.h.taskPacket(ctx, d, post, "Post the notes to Discord")
	if !strings.Contains(packet, "Rule: "+factory.AsksBeforePublishingRule+". This task involves posting") {
		t.Errorf("the run is not told to ask before posting:\n%s", packet)
	}

	// A check names the request and the decisions in force.
	work("ws_task_update", `{"task":"`+draft+`","state":"running","request_note":"Priya asked to keep them short","decision":"Release notes stay under 300 words"}`)
	work("ws_task_update", `{"task":"`+draft+`","state":"done","check":"Checked against the request: 240 words","handoff":"Notes are in Files."}`)
	got, _ := d.task(ctx, draft)
	if got.Check != "Checked against the request and 1 decision: 240 words" {
		t.Errorf("the check reads %q", got.Check)
	}

	// Turning the rule off is a decision, and nothing asks first any more.
	var cur api.Workstream
	o.do(t, "abir", "GET", "/v1/workstreams/"+ws.ID, -1, nil, 200, &cur)
	rules := cur.Rules
	rules.PublishesWithoutAsking = true
	o.do(t, "abir", "PATCH", "/v1/workstreams/"+ws.ID, cur.Rev, api.WorkstreamPatch{Rules: &rules}, 200, nil)
	if got := asks(); got[post] != "" {
		t.Errorf("with the rule off the post still asks first: %v", got)
	}
	o.do(t, "abir", "GET", "/v1/workstreams/"+ws.ID, -1, nil, 200, &cur)
	found := false
	for _, x := range cur.Decisions {
		found = found || x.Text == factory.PublishesWithoutAskingRule
	}
	if !found {
		t.Errorf("turning the rule off recorded no decision: %+v", cur.Decisions)
	}
}

package serve

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// THE SERVER'S AGENT IS CHECKED AGAINST THE DECISIONS (B2 on an
// organization). Its door answers the two verbs internal/session's check
// uses: ws_decisions reads what is kept now, and ws_task_conflict pauses a
// task on an Ask to the decision's maker — once however often it is raised,
// and never for a decision that is withdrawn.
func TestServerAgentDoorAnswersTheDecisionCheck(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Pantry",
		Decisions: []api.WorkstreamDecision{{Text: "No price change in 2.4", State: "kept"}, {Text: "Old rule", State: "withdrawn"}}})
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+ws.ID+"/shares", -1, sharesPut(factory.Share{Kind: "person", ID: "mia", Role: "edit"}), 200, nil)
	var task string
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		if turn.Task != nil {
			// An answered Ask puts Afi back to work on its task in a run of
			// its own; this scene is the person's turn alone.
			return AgentTurnEnd{}, nil
		}
		d := turn.Door.(workDoor)
		line, err := d.Work(ctx, "ws_decisions", json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("ws_decisions: %v", err)
		}
		var got struct {
			Decisions []struct{ ID, Text, By, ByName string } `json:"decisions"`
		}
		if json.Unmarshal([]byte(line), &got) != nil || len(got.Decisions) != 1 || got.Decisions[0].Text != "No price change in 2.4" || got.Decisions[0].By != "santosh" {
			t.Fatalf("ws_decisions = %s", line)
		}
		made, err := d.Work(ctx, "ws_task_create", json.RawMessage(`{"title":"Raise Plus to $5.99","request":"Change the price."}`))
		if err != nil {
			t.Fatal(err)
		}
		var m struct{ Task struct{ ID string } }
		_ = json.Unmarshal([]byte(made), &m)
		task = m.Task.ID
		for range 2 {
			if _, err := d.Work(ctx, "ws_task_conflict", json.RawMessage(`{"task":"`+task+`","decision":"`+got.Decisions[0].ID+`","why":"it changes the price"}`)); err != nil {
				t.Fatalf("ws_task_conflict: %v", err)
			}
		}
		if _, err := d.Work(ctx, "ws_task_conflict", json.RawMessage(`{"task":"`+task+`","decision":"nope","why":"x"}`)); err == nil {
			t.Fatal("a decision the workstream does not keep raised an Ask")
		}
		if _, err := d.Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+task+`","assumed":["Kept the old label"],"request_note":"mia asked for it","decision":"Plus stays one plan"}`)); err != nil {
			t.Fatalf("assumed and decision: %v", err)
		}
		return AgentTurnEnd{}, nil
	})
	o.do(t, "mia", "POST", "/v1/workstreams/"+ws.ID+"/agent/messages", -1, api.AgentMessageNew{Text: "Raise the price."}, 202, nil)
	o.settle()

	var got factory.WorkstreamTask
	o.do(t, "mia", "GET", "/v1/workstreams/"+ws.ID+"/tasks/"+task, -1, nil, 200, &got)
	if got.State != factory.TaskNeeds || len(got.Asks) != 1 {
		t.Fatalf("the task %s with Asks %+v", got.State, got.Asks)
	}
	ask := got.Asks[0]
	if ask.Decider == nil || ask.Decider.ID != "santosh" || ask.Against == nil || !strings.Contains(ask.Detail, "No price change in 2.4") || ask.Options[0].ID != "keep" {
		t.Fatalf("the Ask %+v", ask)
	}
	if len(got.Assumed) != 1 || got.Assumed[0].Text != "Kept the old label" {
		t.Fatalf("assumed %+v", got.Assumed)
	}
	// Only the maker answers it: mia may not, santosh may.
	o.do(t, "mia", "POST", "/v1/workstreams/"+ws.ID+"/tasks/"+task+"/answer", -1, api.TaskAnswer{Ask: ask.ID, Option: "keep"}, 403, nil)
	o.do(t, "santosh", "POST", "/v1/workstreams/"+ws.ID+"/tasks/"+task+"/answer", -1, api.TaskAnswer{Ask: ask.ID, Option: "keep"}, 200, nil)
	after := o.get(t, "santosh", ws.ID)
	texts := []string{}
	for _, d := range after.Decisions {
		texts = append(texts, d.Text)
	}
	if j := strings.Join(texts, " | "); !strings.Contains(j, "Plus stays one plan") || strings.Count(j, "No price change") != 1 {
		t.Fatalf("decisions after: %s", j)
	}
}

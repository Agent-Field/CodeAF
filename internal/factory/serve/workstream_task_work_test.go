package serve

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

// taskWorkHeard is every task.work event who would be sent, oldest first.
func (o *wsOrg) taskWorkHeard(t *testing.T, who string) []api.TaskWorkEvent {
	t.Helper()
	events, _, err := live.Read(o.st.Root())
	if err != nil {
		t.Fatal(err)
	}
	var out []api.TaskWorkEvent
	for _, e := range events {
		if e.Kind != api.EventTaskWork {
			continue
		}
		r := httptest.NewRequest("GET", "/v1/events", nil)
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("Authorization", "Bearer "+o.tokens[who])
		if sent, ok := o.h.liveForViewer(r, e); ok {
			var body api.TaskWorkEvent
			if err := json.Unmarshal(sent.Data, &body); err != nil {
				t.Fatal(err)
			}
			out = append(out, body)
		}
	}
	return out
}

// A TASK'S WORKING CONVERSATION IS SHARED, AND ITS OWNER OR STARTER STEERS IT
// (OWNER-DECISIONS 2026-11-17 §8). Before, a server task run "published no
// agent event at all": nobody could read what Afi was doing on a task. Now
// everyone who can see the workstream reads the run's lines live and later;
// the owner who started it steers it, the words reach the run and everyone
// reads them; someone who can see but neither owns nor started it has no
// steer; someone who cannot see the workstream hears nothing; and with no run
// going a steer is refused, not lost.
func TestATasksWorkingConversationIsSharedAndItsOwnerSteersIt(t *testing.T) {
	o := newWSOrg(t)
	_, _, pricing, _ := o.orgScene(t)
	ctx := context.Background()
	mia := &agentDoor{h: o.h, id: pricing.ID, asker: "mia"}
	line, err := mia.Work(ctx, "ws_task_create", json.RawMessage(`{"title":"Pricing copy","request":"Write the pricing page copy."}`))
	if err != nil {
		t.Fatal(err)
	}
	var made struct{ Task struct{ ID string } }
	_ = json.Unmarshal([]byte(line), &made)
	if _, err := mia.Work(ctx, "ws_task_update", json.RawMessage(`{"task":"`+made.Task.ID+`","state":"running"}`)); err != nil {
		t.Fatal(err)
	}
	task, _ := mia.task(ctx, made.Task.ID)
	ws, _, _ := o.st.Workstream(pricing.ID)
	path := "/v1/workstreams/" + pricing.ID + "/tasks/" + task.ID + "/work"

	steered := make(chan string, 1)
	working := make(chan struct{})
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		turn.Step(api.AgentEntryAgent, "Drafting the headline first.", "", false)
		turn.Step(api.AgentEntryStep, "read the files", "ws_files", false)
		close(working)
		select {
		case words := <-turn.Steers:
			steered <- words
			turn.Step(api.AgentEntryAgent, "Using the shorter title.", "", false)
		case <-ctx.Done():
		}
		return AgentTurnEnd{}, nil
	})
	if err := o.h.runTaskTurn(ws, task, "mia", "Write the pricing page copy."); err != nil {
		t.Fatal(err)
	}

	<-working
	// santosh can see the workstream but neither owns the task nor started it.
	var seen api.TaskWork
	o.do(t, "santosh", "GET", path, -1, nil, 200, &seen)
	if seen.State != api.AgentWorking || seen.For != "mia" || seen.CanSteer || seen.Steerer {
		t.Fatalf("santosh reads %+v", seen)
	}
	o.do(t, "santosh", "POST", path+"/messages", -1, api.TaskSteerNew{Text: "make it longer"}, 403, nil)
	// abir cannot see the workstream at all.
	o.do(t, "abir", "GET", path, -1, nil, 404, nil)

	var mine api.TaskWork
	o.do(t, "mia", "GET", path, -1, nil, 200, &mine)
	if !mine.CanSteer || !mine.Steerer {
		t.Fatalf("the owner who started it cannot steer: %+v", mine)
	}
	o.do(t, "mia", "POST", path+"/messages", -1, api.TaskSteerNew{Text: "use the shorter title"}, 202, nil)
	if got := <-steered; got != "use the shorter title" {
		t.Fatalf("the run was handed %q", got)
	}
	o.settle()

	var after api.TaskWork
	o.do(t, "santosh", "GET", path, -1, nil, 200, &after)
	var lines []string
	for _, e := range after.Entries {
		lines = append(lines, e.Kind+":"+e.By+":"+e.Text)
	}
	want := []string{"note:mia:Started by mia", "agent::Drafting the headline first.", "step::read the files", "person:mia:use the shorter title", "agent::Using the shorter title."}
	if len(lines) != len(want) {
		t.Fatalf("santosh reads %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("santosh reads %q, want %q", lines, want)
		}
	}
	if after.State != api.AgentIdle || after.CanSteer {
		t.Fatalf("after the run: %+v", after)
	}
	var refused map[string]string
	o.do(t, "mia", "POST", path+"/messages", -1, api.TaskSteerNew{Text: "one more thing"}, 409, &refused)
	if refused["reason"] != "idle" {
		t.Fatalf("a steer with nothing working = %v", refused)
	}

	// Everyone who can see it heard it live; whoever cannot heard nothing; and
	// nothing of it reached anyone's own conversation with Afi.
	if heard := o.taskWorkHeard(t, "santosh"); len(heard) < len(want)+2 {
		t.Fatalf("santosh heard %d task.work events", len(heard))
	}
	if heard := o.taskWorkHeard(t, "abir"); len(heard) != 0 {
		t.Fatalf("abir, who cannot see the workstream, heard %+v", heard)
	}
	var pane api.WorkstreamAgent
	o.do(t, "mia", "GET", "/v1/workstreams/"+pricing.ID+"/agent", -1, nil, 200, &pane)
	if len(pane.Entries) != 0 {
		t.Fatalf("the task run wrote into mia's own pane: %+v", pane.Entries)
	}
}

package serve

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// workDoor is session.WorkstreamWorkDoor, spelled here so this package does
// not import the engine's conversation machinery.
type workDoor interface {
	Work(ctx context.Context, tool string, args json.RawMessage) (string, error)
}

// THE SERVER'S AGENT KEEPS WORK AS TASKS (L1 F1). Its door answers the ws_*
// tools through the routes a person's client uses, as the person who asked:
// tasks it makes are owned by them and done by Afi, it changes only work an
// agent does, its lines on a thread are Afi's, and the workstream's rules
// (owner, rev, who changes what) are the routes' own.
func TestServerAgentKeepsWorkAsTasks(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	var page api.Workstream
	o.do(t, "abir", "POST", "/v1/workstreams/"+webhook.ID+"/pages", -1, api.PageNew{Title: "Plan"}, 201, &page)

	var steps []string
	work := func(ctx context.Context, d workDoor, tool, args string) map[string]any {
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
	refused := func(ctx context.Context, d workDoor, tool, args, want string) {
		t.Helper()
		if line, err := d.Work(ctx, tool, json.RawMessage(args)); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s %s = %q, %v; want a refusal saying %q", tool, args, line, err, want)
		}
	}
	o.serverMode(t, func(ctx context.Context, turn AgentTurn) (AgentTurnEnd, error) {
		d, ok := turn.Door.(workDoor)
		if !ok {
			t.Fatal("the server's door does not answer the ws_* tools")
		}
		created := work(ctx, d, "ws_task_create", `{"title":"Parser reads unicode fractions","request":"Make ½ parse."}`)
		made := created["task"].(map[string]any)
		id := made["id"].(string)
		// The sentence the model repeats names the task, never its id (live2 E6 F11).
		if said, _ := created["said"].(string); !strings.Contains(said, "Parser reads unicode fractions") || strings.Contains(said, id) {
			t.Errorf("ws_task_create said %q", said)
		}
		person := work(ctx, d, "ws_task_create", `{"title":"Release notes","request":"Write them.","owner":"santosh","done_by":"person"}`)["task"].(map[string]any)
		refused(ctx, d, "ws_task_create", `{"title":"x","request":"y","owner":"nobody-here"}`, "not someone in this workstream")
		refused(ctx, d, "ws_task_create", `{"title":"x","request":"y","done_by":"factory"}`, "Factory job")
		refused(ctx, d, "ws_task_update", `{"task":"`+person["id"].(string)+`","state":"done"}`, "not by you")
		refused(ctx, d, "ws_task_update", `{"task":"`+id+`","rev":99,"title":"z"}`, "changed since you read it")
		refused(ctx, d, "ws_task_comment", `{"task":"`+id+`","kind":"offer","text":"I can"}`, "for_event")
		asked := work(ctx, d, "ws_task_update", `{"task":"`+id+`","ask":{"title":"Which glyphs?","options":[{"label":"All"},{"label":"Common","recommended":true},{"label":"None","recommended":true}]},"request_note":"mia asked to keep it small"}`)
		if asked["task"].(map[string]any)["state"] != factory.TaskNeeds {
			t.Errorf("an Ask leaves the task %v", asked["task"])
		}
		work(ctx, d, "ws_task_comment", `{"task":"`+id+`","text":"Started on the tables."}`)
		list := work(ctx, d, "ws_tasks", `{"mine":true}`)["tasks"].([]any)
		if len(list) != 1 {
			t.Errorf("mine lists %v", list)
		}
		whole := work(ctx, d, "ws_tasks", `{"task":"`+id+`"}`)
		steps = append(steps, id)
		for _, e := range whole["events"].([]any) {
			ev := e.(map[string]any)
			if by := ev["by"].(map[string]any); by["kind"] != "afi" {
				t.Errorf("a line by %v on the thread", by)
			}
		}

		// Files: it makes a document, rewrites its own, suggests on abir's.
		doc := work(ctx, d, "ws_file_create", `{"name":"Fraction table","markdown":"½ ¼"}`)["file"].(map[string]any)
		if doc["you_may_rewrite"] != true {
			t.Errorf("its own document %v", doc)
		}
		work(ctx, d, "ws_file_create", `{"name":"x","file":"`+doc["id"].(string)+`","markdown":"½ ¼ ¾"}`)
		files := work(ctx, d, "ws_files", `{}`)["files"].([]any)
		var plan map[string]any
		for _, f := range files {
			if f.(map[string]any)["name"] == "Plan" {
				plan = f.(map[string]any)
			}
		}
		if plan == nil || plan["you_may_rewrite"] != false {
			t.Fatalf("abir's Plan in %v", files)
		}
		refused(ctx, d, "ws_file_create", `{"name":"x","file":"`+plan["id"].(string)+`","markdown":"mine now"}`, "only by suggestion")
		read := work(ctx, d, "ws_file_read", `{"file":"`+plan["id"].(string)+`"}`)
		refused(ctx, d, "ws_file_suggest", `{"file":"`+plan["id"].(string)+`","base_version":1,"why":"w","edits":[{"find":"nothing like this","replace":"x"}]}`, "not in the document")
		_ = read
		return AgentTurnEnd{}, nil
	})
	o.do(t, "mia", "POST", "/v1/workstreams/"+webhook.ID+"/agent/messages", -1, api.AgentMessageNew{Text: "Make tasks."}, 202, nil)
	o.settle()

	var tasks api.WorkstreamTasks
	o.do(t, "abir", "GET", "/v1/workstreams/"+webhook.ID+"/tasks", -1, nil, 200, &tasks)
	if len(tasks.Tasks) != 2 {
		t.Fatalf("abir sees %d tasks", len(tasks.Tasks))
	}
	afi := tasks.Tasks[0]
	if afi.Owner.ID != "mia" || afi.DoneBy.Kind != "afi" || afi.CreatedBy.ID != "mia" || afi.CameFrom.Name != "Afi" {
		t.Fatalf("Afi's task %+v", afi)
	}
	if afi.Ask == nil || afi.Ask.Options[0].Label != "Common" || !afi.Ask.Options[0].Recommended || afi.Ask.Options[1].Recommended || afi.Ask.Options[2].Recommended {
		t.Fatalf("the Ask %+v", afi.Ask)
	}
	if p := tasks.Tasks[1]; p.Owner.ID != "santosh" || p.DoneBy.Kind != "person" || p.DoneBy.ID != "santosh" {
		t.Fatalf("the person task %+v", p)
	}
	var events api.WorkstreamTaskEvents
	o.do(t, "abir", "GET", "/v1/workstreams/"+webhook.ID+"/tasks/"+afi.ID+"/events", -1, nil, 200, &events)
	kinds := []string{}
	for _, e := range events.Events {
		kinds = append(kinds, e.Kind)
	}
	if !strings.Contains(strings.Join(kinds, " "), "request_note") || !strings.Contains(strings.Join(kinds, " "), "comment") {
		t.Fatalf("the thread %v", kinds)
	}
	// Only the owner answers the Ask: abir may not, mia may.
	o.do(t, "abir", "POST", "/v1/workstreams/"+webhook.ID+"/tasks/"+afi.ID+"/answer", -1, api.TaskAnswer{Option: "o1"}, 403, nil)
	o.do(t, "mia", "POST", "/v1/workstreams/"+webhook.ID+"/tasks/"+afi.ID+"/answer", -1, api.TaskAnswer{Option: "o1"}, 200, nil)
}

// A PERSON WHO MAY NOT EDIT CANNOT WRITE THROUGH THE AGENT. Rights are the
// asker's, read again on every call.
func TestServerAgentWritesNoTaskForSomeoneWhoLostEdit(t *testing.T) {
	o := newWSOrg(t)
	_, launch, _, _ := o.orgScene(t)
	d := &agentDoor{h: o.h, id: launch.ID, asker: "abir"}
	if _, err := d.Work(context.Background(), "ws_task_create", json.RawMessage(`{"title":"x","request":"y"}`)); err == nil {
		t.Fatal("abir, who cannot see Launch plan, made a task in it through the agent")
	}
}

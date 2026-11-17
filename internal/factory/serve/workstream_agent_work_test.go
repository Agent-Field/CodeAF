package serve

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
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
		if turn.Task != nil {
			// An answered Ask puts Afi back to work on its task in a run of
			// its own; this scene is the person's turn alone.
			return AgentTurnEnd{}, nil
		}
		d, ok := turn.Door.(workDoor)
		if !ok {
			t.Fatal("the server's door does not answer the ws_* tools")
		}
		created := work(ctx, d, "ws_task_create", `{"title":"Parser reads unicode fractions","request":"Make ½ parse.","goal":"Ship it right","deliverable":"A document in Files","acceptance":["Covers the change","Plain words"]}`)
		made := created["task"].(map[string]any)
		id := made["id"].(string)
		// The sentence the model repeats names the task, never its id (live2 E6 F11).
		if said, _ := created["said"].(string); !strings.Contains(said, "Parser reads unicode fractions") || strings.Contains(said, id) {
			t.Errorf("ws_task_create said %q", said)
		}
		refused(ctx, d, "ws_task_create", `{"title":"x","request":"y","owner":"everyone"}`, "A task needs one person as owner")
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
	if _, err := d.Work(context.Background(), "ws_task_create", json.RawMessage(`{"title":"x","request":"y","goal":"Ship it right","deliverable":"A document in Files","acceptance":["Covers the change","Plain words"]}`)); err == nil {
		t.Fatal("abir, who cannot see Launch plan, made a task in it through the agent")
	}
}

// AN UPLOAD IS READ THROUGH ITS FILE (solo-sheets F1). ws_file_read answers a
// text upload's words, a long one in parts, and says a picture has no words,
// so the agent never goes looking for the bytes on disk.
func TestServerAgentReadsAnUploadedTextFile(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Sheets"})
	path := "/v1/workstreams/" + ws.ID + "/files"
	upload := func(name, mime string, data []byte) string {
		var f api.WorkstreamFile
		o.do(t, "santosh", "POST", path, -1, api.FileNew{Kind: "upload", Name: name, Mime: mime, Data: data}, 201, &f)
		return f.ID
	}
	d := &agentDoor{h: o.h, id: ws.ID, asker: "santosh"}
	read := func(args string) map[string]any {
		t.Helper()
		line, err := d.Work(context.Background(), "ws_file_read", json.RawMessage(args))
		if err != nil {
			t.Fatalf("ws_file_read %s: %v", args, err)
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(line), &out); err != nil {
			t.Fatalf("ws_file_read answered %q", line)
		}
		return out
	}

	contract := upload("CONTRACT.md", "text/markdown", []byte("# Contract\n\nOne row, one writer."))
	if got := read(`{"file":"` + contract + `"}`); got["text"] != "# Contract\n\nOne row, one writer." || got["more_from"] != nil {
		t.Fatalf("a text upload reads %v", got)
	}

	long := strings.Repeat("é", 60<<10)
	big := upload("notes.txt", "text/plain", []byte(long))
	first := read(`{"file":"` + big + `"}`)
	on, ok := first["more_from"].(float64)
	if !ok || !strings.Contains(first["said"].(string), "from") {
		t.Fatalf("a long upload's first read %v", first["said"])
	}
	rest := read(`{"file":"` + big + `","from":` + strconv.Itoa(int(on)) + `}`)
	if first["text"].(string)+rest["text"].(string) != long {
		t.Fatal("the two reads of a long upload do not make the whole")
	}

	pic := read(`{"file":"` + upload("look.png", "image/png", []byte{0x89, 'P', 'N', 'G', 0, 0, 0xff}) + `"}`)
	if pic["text"] != nil || !strings.Contains(pic["said"].(string), "Do not look for it on disk") {
		t.Fatalf("a picture reads %v", pic)
	}
}

// AN AGENT'S SUGGESTION IS ONE PEOPLE CAN SEE (live2 E3 F1). ws_file_suggest
// on a person's document lands in the workstream's suggestions, where every
// client draws it on the page with Keep and Drop, with each edit's exact
// words; it is never kept as a page version nobody is shown, and the page's
// words do not change until someone keeps it.
func TestServerAgentSuggestionIsInTheWorkstreamsSuggestions(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Pantry"})
	var doc api.WorkstreamFile
	o.do(t, "santosh", "POST", "/v1/workstreams/"+ws.ID+"/files", -1, api.FileNew{Kind: "document", Name: "Onboarding research", BodyMD: "Default servings to 4.\n\nAsk for allergies."}, 201, &doc)
	d := &agentDoor{h: o.h, id: ws.ID, asker: "santosh"}
	line, err := d.Work(context.Background(), "ws_file_suggest", json.RawMessage(`{"file":"`+doc.ID+`","base_version":`+strconv.Itoa(doc.Rev)+`,"why":"Smaller households","edits":[{"find":"servings to 4","replace":"servings to 2","why":"Most households are two"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(line, "Onboarding research") || strings.Contains(line, "version") {
		t.Errorf("ws_file_suggest said %s", line)
	}
	var list struct {
		Suggestions []struct {
			PageID string `json:"page_id"`
			Why    string `json:"why"`
			Agent  bool   `json:"agent"`
			By     string `json:"by"`
			Edits  []struct {
				ID, Find, Replace, Why, State string
			} `json:"edits"`
		} `json:"suggestions"`
	}
	o.do(t, "santosh", "GET", "/v1/workstreams/"+ws.ID+"/suggestions?page="+doc.PageID, -1, nil, 200, &list)
	if len(list.Suggestions) != 1 {
		t.Fatalf("the workstream's suggestions are %+v", list)
	}
	s := list.Suggestions[0]
	if s.PageID != doc.PageID || !s.Agent || s.By != "santosh" || s.Why != "Smaller households" || len(s.Edits) != 1 {
		t.Fatalf("the suggestion %+v", s)
	}
	if e := s.Edits[0]; e.ID != "e1" || e.Find != "servings to 4" || e.Replace != "servings to 2" || e.Why != "Most households are two" || e.State != "proposed" {
		t.Fatalf("its edit %+v", e)
	}
	var versions api.PageVersions
	o.do(t, "santosh", "GET", "/v1/workstreams/"+ws.ID+"/pages/"+doc.PageID+"/versions", -1, nil, 200, &versions)
	for _, v := range versions.Versions {
		if v.Reason == versionReasonKept {
			t.Fatalf("the suggestion was kept as a hidden version %+v", v)
		}
	}
	var after api.Workstream
	o.do(t, "santosh", "GET", "/v1/workstreams/"+ws.ID, -1, nil, 200, &after)
	if p := pageNamed(t, after, "Onboarding research"); !strings.Contains(p.BodyMD, "servings to 4") {
		t.Fatalf("the suggestion changed the page: %q", p.BodyMD)
	}
}

// A TASK IS A FULL WORK ITEM (org-space DESIGN §5, lane L2). A task's run
// stamps what it makes as made by the task and what it reads as used by it, and
// the agent makes a checklist on create and ticks it by id.
func TestServerAgentTaskRunLinksItsFilesAndTicksItsChecklist(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Pantry"})
	chat := &agentDoor{h: o.h, id: ws.ID, asker: "santosh"}
	work := func(d *agentDoor, tool, args string) map[string]any {
		t.Helper()
		line, err := d.Work(context.Background(), tool, json.RawMessage(args))
		if err != nil {
			t.Fatalf("%s %s: %v", tool, args, err)
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(line), &out); err != nil {
			t.Fatalf("%s answered %q", tool, line)
		}
		return out
	}
	made := work(chat, "ws_task_create", `{"title":"Draft the launch post","request":"A post for Friday","checklist":["Outline","First draft"]}`)
	task := made["task"].(map[string]any)
	id := task["id"].(string)
	list, _ := task["checklist"].([]any)
	if len(list) != 2 || list[0].(map[string]any)["id"] != "c1" {
		t.Fatalf("the task's checklist reads %v", task["checklist"])
	}
	var notes api.WorkstreamFile
	o.do(t, "santosh", "POST", "/v1/workstreams/"+ws.ID+"/files", -1, api.FileNew{Kind: "document", Name: "Interview notes", BodyMD: "Five people."}, 201, &notes)

	run := &agentDoor{h: o.h, id: ws.ID, asker: "santosh", runTask: id}
	work(run, "ws_file_read", `{"file":"`+notes.ID+`"}`)
	draft := work(run, "ws_file_create", `{"name":"Launch post","markdown":"Hello."}`)["file"].(map[string]any)
	ticked := work(run, "ws_task_update", `{"task":"`+id+`","checklist_done":["c1"]}`)["task"].(map[string]any)
	if c := ticked["checklist"].([]any); c[0].(map[string]any)["done"] != true || c[1].(map[string]any)["done"] != nil {
		t.Fatalf("ticked checklist %v", c)
	}
	if _, err := run.Work(context.Background(), "ws_task_update", json.RawMessage(`{"task":"`+id+`","checklist_done":["c7"]}`)); err == nil {
		t.Fatal("a line that is not on the checklist was ticked")
	}

	files, err := o.h.config.Store.WorkstreamFiles(ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := files[notes.ID].UsedBy; !slices.Equal(got, []string{id}) {
		t.Fatalf("the notes the run read are used by %v", got)
	}
	if got := files[draft["id"].(string)].FromTask; got != id {
		t.Fatalf("the post the run made is from %q", got)
	}
	// A chat that is not a task's run links nothing by itself.
	other := work(chat, "ws_file_create", `{"name":"Scratch","markdown":"x"}`)["file"].(map[string]any)
	work(chat, "ws_file_read", `{"file":"`+other["id"].(string)+`"}`)
	files, _ = o.h.config.Store.WorkstreamFiles(ws.ID)
	if f := files[other["id"].(string)]; f.FromTask != "" || len(f.UsedBy) != 0 {
		t.Fatalf("a chat's file was linked to a task: %+v", f)
	}
}

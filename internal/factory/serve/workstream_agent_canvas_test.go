package serve

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/session"
)

// canvasCmd runs one ws_canvas command through d and answers its JSON.
func canvasCmd(t *testing.T, d *agentDoor, input string, files map[string]string, words ...string) map[string]any {
	t.Helper()
	line, err := d.Canvas(context.Background(), session.CanvasCall{Args: words, Input: input, Files: files})
	if err != nil {
		t.Fatalf("canvas %s: %v", strings.Join(words, " "), err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(line), &out); err != nil {
		t.Fatalf("canvas %s answered %q", strings.Join(words, " "), line)
	}
	return out
}

// THE SERVER'S AFI DRAWS THE WORKSTREAM'S CANVASES (the org canvas gap). A
// task's run makes a canvas with the same `canvas` commands a laptop's agent
// types: it is the workstream's, in Files as made by Afi from the task, and
// what the run drew is written by the agent into the canvas a person opens.
func TestServerAfiDrawsACanvasInItsWorkstream(t *testing.T) {
	o := canvasOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Pantry launch"})
	chat := &agentDoor{h: o.h, id: ws.ID, asker: "santosh"}
	line, err := chat.Work(context.Background(), "ws_task_create", json.RawMessage(`{"title":"Social launch cards","request":"Three cards for Friday","goal":"Launch posts look like one set","deliverable":"A canvas of three cards","acceptance":["Three 1080 square cards","One shared palette"]}`))
	if err != nil {
		t.Fatal(err)
	}
	var made struct{ Task struct{ ID string } }
	_ = json.Unmarshal([]byte(line), &made)
	run := &agentDoor{h: o.h, id: ws.ID, asker: "santosh", runTask: made.Task.ID, wrote: &taskWrites{}}

	cv := canvasCmd(t, run, "", nil, "new", "--title", "Launch cards")
	id, _ := cv["id"].(string)
	c, ok, err := o.st.Canvas(id)
	if err != nil || !ok || c.Workstream != ws.ID || c.CreatedBy != "santosh" {
		t.Fatalf("the canvas Afi made = %+v %v %v", c, ok, err)
	}
	// THE AGENT IS THE CANVAS'S MANAGER, so the look the guide builds on, its
	// tokens, is Afi's to set (it was refused not_manager before).
	canvasCmd(t, run, "", nil, "token", "set", id, "$page", "color", "#1d3b2a")
	if toks, err := run.Canvas(context.Background(), session.CanvasCall{Args: []string{"token", "list", id}}); err != nil || !strings.Contains(toks, "#1d3b2a") {
		t.Fatalf("token list = %v %.300s", err, toks)
	}
	frame := canvasCmd(t, run, "", nil, "frame", "add", id, "--kind", "design", "--name", "Card one", "--json", `{"w":1080,"h":1080,"part":"Launch cards"}`)
	page := `<!doctype html><html><body style="background:#1d3b2a;color:#f4efe4"><h1>Pantry is live</h1></body></html>`
	canvasCmd(t, run, page, nil, "put", id, "--parent", frame["id"].(string), "--type", "html", "--json", `{"w":1080,"h":1080}`, "--html", "-")
	canvasCmd(t, run, "", map[string]string{"card2.html": page}, "put", id, "--parent", frame["id"].(string), "--type", "html", "--json", `{"w":1080,"h":1080}`, "--html", "card2.html")

	shown, err := run.Canvas(context.Background(), session.CanvasCall{Args: []string{"show", id}})
	if err != nil || !strings.Contains(shown, "Pantry is live") || !strings.Contains(shown, "Card one") {
		t.Fatalf("show = %v %.300s", err, shown)
	}
	if strings.Contains(shown, "workaf://") {
		t.Fatalf("show carried a laptop link: %.300s", shown)
	}

	// Files names it as Afi's, made by the task, and the run counts it.
	files, err := o.h.config.Store.WorkstreamFiles(ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	var file factory.WorkstreamFile
	for _, f := range files {
		if f.CanvasID == id {
			file = f
		}
	}
	if file.ID == "" || file.Name != "Launch cards" || file.MadeBy.Kind != factory.MakerAfi || file.MadeBy.For != "santosh" || file.FromTask != made.Task.ID {
		t.Fatalf("the canvas's file = %+v", file)
	}
	if !slices.Contains(run.wrote.list(), file.ID) {
		t.Fatalf("the run's writes %v miss the canvas %s", run.wrote.list(), file.ID)
	}

	// The writes are the agent's own in canvas-core's log.
	var log canvasOps
	o.do(t, "santosh", "GET", "/v1/canvases/"+id+"/ops?since=0", -1, nil, 200, &log)
	agent := 0
	for _, op := range log.Ops {
		if op.Actor == "afi" {
			agent++
		}
	}
	if agent < 3 {
		t.Fatalf("the agent wrote %d of %d ops", agent, len(log.Ops))
	}
	list := canvasList(t, run)
	if len(list) != 1 || list[0]["title"] != "Launch cards" {
		t.Fatalf("list = %v", list)
	}
}

func canvasList(t *testing.T, d *agentDoor) []map[string]any {
	t.Helper()
	line, err := d.Canvas(context.Background(), session.CanvasCall{Args: []string{"list"}})
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(line), &rows); err != nil {
		t.Fatalf("list answered %q", line)
	}
	return rows
}

// A CANVAS A PERSON MADE IN THE WORKSTREAM IS ONE AFI MAY DRAW ON: the agent
// joins it with edit on its first change, as a person adding it in the app.
func TestServerAfiDrawsOnACanvasAPersonMade(t *testing.T) {
	o := canvasOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Pantry launch"})
	var cv map[string]any
	o.do(t, "santosh", "POST", "/v1/canvases", -1, api.CanvasNew{Title: "Plan", Workstream: ws.ID}, 201, &cv)
	id := cv["id"].(string)
	d := &agentDoor{h: o.h, id: ws.ID, asker: "santosh"}
	canvasCmd(t, d, "", nil, "frame", "add", id, "--kind", "doc", "--name", "The plan")
	canvasCmd(t, d, "", nil, "title", id, "--title", "One-page plan")
	canvasCmd(t, d, "", nil, "token", "set", id, "$lead", "color", "#222222")
	if got := o.h.canvasTitle(id); got != "One-page plan" {
		t.Fatalf("title = %q", got)
	}
}

// NOTHING OUTSIDE THE WORKSTREAM'S CANVASES IS REACHABLE: another
// workstream's canvas, a verb that writes a file out or reaches the server's
// disk, a file the call did not carry, and a change by someone who may only
// view the workstream are all refused, and nothing changes.
func TestServerAfiCanvasReachesOnlyItsWorkstream(t *testing.T) {
	o := canvasOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Pantry launch"})
	other := o.create(t, "santosh", api.WorkstreamNew{Name: "Hiring"})
	var cv map[string]any
	o.do(t, "santosh", "POST", "/v1/canvases", -1, api.CanvasNew{Title: "Hiring board", Workstream: other.ID}, 201, &cv)
	elsewhere := cv["id"].(string)
	d := &agentDoor{h: o.h, id: ws.ID, asker: "santosh"}
	mine := canvasCmd(t, d, "", nil, "new", "--title", "Cards")["id"].(string)

	refused := map[string][]string{
		"another workstream's canvas": {"show", elsewhere},
		"export":                      {"export", mine, "--format", "html", "--out", "x.html"},
		"--root":                      {"show", mine, "--root", "/tmp"},
		"a file on the server's disk": {"put", mine, "--parent", "f_x", "--type", "html", "--html", "/etc/hostname"},
		"asset from the disk":         {"asset", "add", mine, "/etc/hostname"},
		"members":                     {"members", mine},
		"rpc":                         {"rpc"},
		"look":                        {"look", mine, "f_x"},
	}
	for why, words := range refused {
		if line, err := d.Canvas(context.Background(), session.CanvasCall{Args: words}); err == nil {
			t.Errorf("%s ran: %s", why, line)
		}
	}
	if rows := canvasList(t, d); len(rows) != 1 || rows[0]["id"] != mine {
		t.Fatalf("list reached past the workstream: %v", rows)
	}

	o.do(t, "santosh", "PUT", "/v1/workstreams/"+ws.ID+"/shares", -1, sharesPut(factory.Share{Kind: "person", ID: "abir", Role: "view"}), 200, nil)
	viewer := &agentDoor{h: o.h, id: ws.ID, asker: "abir"}
	if _, err := viewer.Canvas(context.Background(), session.CanvasCall{Args: []string{"show", mine}}); err != nil {
		t.Fatalf("a viewer's Afi could not read the canvas: %v", err)
	}
	if _, err := viewer.Canvas(context.Background(), session.CanvasCall{Args: []string{"frame", "add", mine, "--kind", "doc", "--name", "Mine"}}); err == nil {
		t.Fatal("a viewer's Afi changed the canvas")
	}
}

// A PERSON'S OWN SPACE CARRIES NO CANVAS: its agent is told of none there,
// so the verb is absent rather than present and untold.
func TestMySpaceAfiHasNoCanvas(t *testing.T) {
	o := canvasOrg(t)
	if !(&agentDoor{h: o.h}).CanvasesKept() {
		t.Fatal("a workstream's door keeps no canvases on a server that serves them")
	}
	if (&agentDoor{h: o.h, space: true}).CanvasesKept() {
		t.Fatal("a person's own space offered the canvas")
	}
}

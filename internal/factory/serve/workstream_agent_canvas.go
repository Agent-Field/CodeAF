package serve

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/canvasrpc"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE WORKSTREAM'S CANVASES, DRAWN BY THE SERVER'S AGENT ─────────────────
//
// session's ws_canvas (tools_workstream_canvas.go) hands this door one
// `canvas` command line, exactly as a laptop's agent would type it in its
// shell. The command is run by the same `canvas` program the server keeps
// its canvases with (canvasrpc), so a canvas the agent draws here is made
// the way a Personal one is. This file decides whether it may run:
//
//   - ONLY THIS WORKSTREAM'S CANVASES. A command names its canvas by id, and
//     the id must be a canvas made inside this workstream that the person
//     being answered can see; a change needs edit on it. One they cannot see
//     answers as one that does not exist.
//   - A NEW CANVAS IS MADE THE WAY A PERSON'S IS, through the same route
//     (POST /v1/canvases with the workstream), and named in Files as made by
//     Afi for the person; in a task's run it is the task's own Result.
//   - THE AGENT WRITES AS ITSELF, AS THE CANVAS'S MANAGER. canvas-core records
//     each change as [canvasrpc.AgentActor], seated through the canvas's
//     working folder exactly as the desktop app seats its manager, so it sets
//     the canvas's tokens and a person's hand on a frame still wins over it as
//     on a laptop; the organization's list says the person it answered
//     changed it.
//   - NOTHING ON THE SERVER'S DISK IS REACHABLE. A command reads only the
//     files the call carried, written into a folder of its own; a verb that
//     writes a file out, reaches another canvas, or waits is refused.

// canvasAgentVerbs are the commands the agent may run, and where each names
// its canvas: 1 for `show ID`, 2 for `frame add ID`. "new" and "list" name
// none.
var canvasAgentVerbs = map[string]int{
	"show": 1, "put": 1, "patch": 1, "move": 1, "del": 1, "title": 1, "link": 1, "marks": 1,
	"version": 1, "lease": 1, "writers": 1,
	"frame": 2, "token": 2, "asset": 2, "rows": 2, "table": 2,
}

// canvasAgentReads are the verbs that change nothing, so view is enough.
var canvasAgentReads = map[string]bool{"show": true, "marks": true, "writers": true}

// canvasAgentFlagsRefused are flags that would reach outside the canvas.
var canvasAgentFlagsRefused = []string{"--root", "--out", "--worker", "--wait"}

var errCanvasNotHere = errors.New("that is not one of this workstream's canvases; `list` lists them")

// CanvasesKept says the server keeps canvases, so its agent carries ws_canvas.
func (d *agentDoor) CanvasesKept() bool { return !d.space && d.h.CanvasesServed() }

// Canvas runs one of the agent's canvas commands.
func (d *agentDoor) Canvas(ctx context.Context, call session.CanvasCall) (string, error) {
	args := call.Args
	if len(args) == 0 {
		return "", errors.New("command is empty")
	}
	for _, a := range args {
		for _, refused := range canvasAgentFlagsRefused {
			if a == refused || strings.HasPrefix(a, refused+"=") {
				return "", errors.New(refused + " is not available on the organization's server")
			}
		}
	}
	verb := args[0]
	switch verb {
	case "help":
		return "Call ws_canvas with command guide for the whole guide.", nil
	case "list":
		return d.canvasList(ctx)
	case "new":
		return d.canvasNew(ctx, args)
	}
	at, ok := canvasAgentVerbs[verb]
	if !ok {
		return "", errors.New("`" + verb + "` is not available on the organization's server")
	}
	if len(args) <= at {
		return "", errors.New("`" + strings.Join(args, " ") + "` names no canvas; `list` lists this workstream's canvases")
	}
	if err := canvasAgentFiles(args, call.Files); err != nil {
		return "", err
	}
	read := canvasAgentReads[verb] || (at == 2 && args[1] == "list")
	c, ws, err := d.agentCanvas(args[at], read)
	if err != nil {
		return "", err
	}
	if !read {
		if err := d.joinCanvas(ctx, c.ID); err != nil {
			return "", err
		}
	}
	dir, done, err := d.canvasCallDir(c.ID, call.Files)
	if err != nil {
		return "", err
	}
	defer done()
	raw, err := d.h.config.Canvas.Command(ctx, c.ID, args, dir, []byte(call.Input))
	if err != nil {
		return "", canvasAgentError(err)
	}
	if f, ok := d.canvasFileOf(ws, c.ID); ok {
		if read {
			d.usedByTask(ctx, f, ws)
		} else {
			d.wrote.note(f.ID)
		}
	}
	if !read {
		d.h.touchCanvas(c, d.viewer())
		d.h.publishCanvas(ctx, api.EventCanvasChanged, nil, c, factory.NormalizeHandle(d.asker))
	}
	return string(canvasAgentAnswer(raw)), nil
}

// agentCanvas is canvas id when it is one of this workstream's that the
// person may view, or edit when read is false.
func (d *agentDoor) agentCanvas(id string, read bool) (factory.OrgCanvas, factory.OrgWorkstream, error) {
	ws, err := d.Workstream(context.Background())
	if err != nil {
		return factory.OrgCanvas{}, ws, err
	}
	if !canvasIDShape.MatchString(id) {
		return factory.OrgCanvas{}, ws, errCanvasNotHere
	}
	c, found, err := d.h.config.Store.Canvas(id)
	if err != nil {
		return c, ws, err
	}
	v := d.viewer()
	if !found || c.Workstream != d.id || !can(v, d.h.canvasItem(c), factory.RoleView) {
		return c, ws, errCanvasNotHere
	}
	if !read {
		if !can(v, d.h.canvasItem(c), factory.RoleEdit) {
			return c, ws, errAgentNoWrite
		}
		if ws.ArchivedAt != nil {
			return c, ws, errWorkstreamArchived
		}
	}
	return c, ws, nil
}

// joinCanvas seats the agent on canvas id as its manager, the way the
// desktop app seats a canvas's manager: the canvas's own working folder is a
// member with edit, and the agent, run inside that folder (canvasCallDir),
// joins through it on its first write. The manager is the one agent that
// sets a canvas's tokens, which the guide's whole look is built on; an agent
// added by name instead was refused every `token set`.
func (d *agentDoor) joinCanvas(ctx context.Context, id string) error {
	if _, done := d.canvasJoined.Load(id); done {
		return nil
	}
	work := canvasWorkDir(d.h.config.Canvas.Root, id)
	if err := os.MkdirAll(work, 0o700); err != nil {
		return err
	}
	_, err := d.h.config.Canvas.Command(ctx, id, []string{"members", "add", id, "cwd:" + work, "--role", "edit", "--kind", "agent", "--name", "Afi"}, work, nil)
	if err == nil {
		d.canvasJoined.Store(id, true)
	}
	return canvasAgentError(err)
}

// canvasWorkDir is canvas id's working folder, canvas-core's WORK_DIR.
func canvasWorkDir(root, id string) string {
	return filepath.Join(root, id, "work")
}

// canvasNew makes a canvas in the workstream: `new --title T`.
func (d *agentDoor) canvasNew(ctx context.Context, args []string) (string, error) {
	title := ""
	for i := 1; i < len(args); i++ {
		switch {
		case args[i] == "--title" && i+1 < len(args):
			title = args[i+1]
			i++
		default:
			return "", errors.New("make a canvas with `new --title \"A short name\"`")
		}
	}
	title, err := agentWords(title, "title", workstreamNameMost)
	if err != nil {
		return "", err
	}
	raw, err := d.call(ctx, d.h.createCanvas, http.MethodPost, "/v1/canvases", nil, api.CanvasNew{Title: title, Workstream: d.id}, -1)
	if err != nil {
		return "", err
	}
	var made struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &made); err != nil || made.ID == "" {
		return "", errors.New("the canvas was not made; try again")
	}
	// Name it in Files as Afi's, for the person, from this run's task.
	fileRaw, err := d.call(ctx, d.h.createWorkstreamFile, http.MethodPost, "/v1/workstreams/"+d.id+"/files", nil,
		api.FileNew{Kind: factory.FileCanvas, CanvasID: made.ID, Name: title, FromTask: d.runTask, MadeBy: &factory.FileMaker{Kind: factory.MakerAfi, Name: "Afi"}}, -1)
	if err != nil {
		return "", err
	}
	var file factory.WorkstreamFile
	_ = json.Unmarshal(fileRaw, &file)
	d.wrote.note(file.ID)
	if err := d.joinCanvas(ctx, made.ID); err != nil {
		return "", err
	}
	out := map[string]any{"id": made.ID, "title": title}
	if file.ID != "" {
		out["file"] = file.ID
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// canvasList is this workstream's canvases the person can see, by title.
func (d *agentDoor) canvasList(ctx context.Context) (string, error) {
	ws, err := d.Workstream(ctx)
	if err != nil {
		return "", err
	}
	all, err := d.h.config.Store.Canvases()
	if err != nil {
		return "", err
	}
	v := d.viewer()
	type row struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	var mine []factory.OrgCanvas
	for _, c := range all {
		if c.Workstream == ws.ID && can(v, d.h.canvasItem(c), factory.RoleView) {
			mine = append(mine, c)
		}
	}
	slices.SortFunc(mine, func(a, b factory.OrgCanvas) int { return a.CreatedAt.Compare(b.CreatedAt) })
	rows := make([]row, 0, len(mine))
	for _, c := range mine {
		rows = append(rows, row{ID: c.ID, Title: d.h.canvasTitle(c.ID)})
	}
	b, err := json.Marshal(rows)
	return string(b), err
}

// canvasFileOf is the file in ws's Files that canvas id is.
func (d *agentDoor) canvasFileOf(ws factory.OrgWorkstream, id string) (factory.WorkstreamFile, bool) {
	files, err := d.h.foldFiles(ws)
	if err != nil {
		return factory.WorkstreamFile{}, false
	}
	for _, f := range files {
		if f.CanvasID == id && f.DeletedAt == nil {
			return f, true
		}
	}
	return factory.WorkstreamFile{}, false
}

// canvasAgentFiles refuses a command that reads a file the call did not
// carry: --html, --csv and `asset add`'s FILE name one of call's files (or
// stdin, for --html).
func canvasAgentFiles(args []string, files map[string]string) error {
	named := func(name string, stdinOK bool) error {
		if stdinOK && name == "-" {
			return nil
		}
		if _, ok := files[name]; !ok {
			return errors.New("the command reads " + name + ", which this call did not carry: put it in files under that plain name")
		}
		return nil
	}
	for i, a := range args {
		if i+1 >= len(args) {
			break
		}
		switch a {
		case "--html":
			if err := named(args[i+1], true); err != nil {
				return err
			}
		case "--csv":
			if err := named(args[i+1], false); err != nil {
				return err
			}
		}
	}
	if len(args) >= 4 && args[0] == "asset" && args[1] == "add" {
		return named(args[3], false)
	}
	return nil
}

// canvasCallDir writes a call's files into a folder of its own inside
// canvas id's working folder, where the command runs: the agent is then the
// canvas's manager (joinCanvas), and the files it names are beside it.
func (d *agentDoor) canvasCallDir(id string, files map[string]string) (string, func(), error) {
	base := filepath.Join(canvasWorkDir(d.h.config.Canvas.Root, id), ".calls")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp(base, "agent-*")
	if err != nil {
		return "", nil, err
	}
	done := func() { _ = os.RemoveAll(dir) }
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(name)), []byte(text), 0o600); err != nil {
			done()
			return "", nil, err
		}
	}
	return dir, done, nil
}

// canvasAgentError is canvas-core's refusal in its own words, which say what
// to fix; anything else is said plainly.
func canvasAgentError(err error) error {
	if err == nil {
		return nil
	}
	var refused *canvasrpc.Error
	if errors.As(err, &refused) {
		return errors.New(refused.Message)
	}
	return err
}

// canvasAgentAnswer is the command's answer without what only a laptop can
// use: the workaf:// link, and a file's path on the server's disk.
func canvasAgentAnswer(raw json.RawMessage) json.RawMessage {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return raw
	}
	var strip func(any)
	strip = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			delete(t, "link")
			delete(t, "path")
			delete(t, "root")
			for _, y := range t {
				strip(y)
			}
		case []any:
			for _, y := range t {
				strip(y)
			}
		}
	}
	strip(v)
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return out
}

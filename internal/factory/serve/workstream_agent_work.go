package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── THE WORKSTREAM TOOLS, ANSWERED FOR THE SERVER'S AGENT ──────────────────
//
// internal/session spells the nine ws_* tools once (tools_workstream_work.go)
// and puts them on the belt of any conversation whose workstream door also
// answers Work. This file is that Work for the organization's server: the
// agent's calls onto the workstream's tasks and files.
//
//   - EVERY CALL GOES THROUGH THE ROUTE A PERSON'S CLIENT USES. A tool call is
//     turned into the same request the desk sends (POST .../tasks, PATCH
//     .../tasks/{tid} with If-Match, POST .../files, ...) and answered by the
//     same handler, as the person whose message the agent is answering. So
//     the owner, done-by, rev and who-may-change-what rules are the routes'
//     rules, checked again on every call, and a person whose edit was taken
//     away mid-answer cannot write through the agent.
//
//   - THE AGENT IS STAMPED AS AFI, FOR THE PERSON. What it adds to a thread is
//     by Afi; what it makes is made by Afi for the person who asked, and the
//     organization's record names that person. Rights are always the person's.
//
//   - THE RULES OF AN AGENT ARE ADDED HERE, AND MATCH THE DESK'S. These are
//     the ones codeaf-desk's crates/core/src/workstream_tools/work.rs applies
//     on a Personal workstream: the agent changes only the tasks an agent
//     does; it rewrites only a document it made that no person has edited;
//     every other document change is a suggestion that applies nothing.

// agentCallKey marks a request made by the workstream's agent for a person.
type agentCallKey struct{}

// agentCall is who an agent's request is made as: the person's handle and
// operator standing (their rights), and who it is stamped as on a thread.
type agentCall struct {
	handle   string
	operator bool
	by       factory.TaskEventBy
}

func agentCallOf(r *http.Request) (agentCall, bool) {
	call, ok := r.Context().Value(agentCallKey{}).(agentCall)
	return call, ok
}

// afiBy is how the server's agent signs a thread.
var afiBy = factory.TaskEventBy{Kind: factory.TaskByAfi, Name: "Afi"}

// The most one list answers the model, as on the desk.
const agentWorkListMost = 200

// agentRefusal is a route's refusal, in words the model can act on.
type agentRefusal struct {
	status int
	msg    string
}

func (e agentRefusal) Error() string { return e.msg }

// call answers one request as the person, through h's own handler for it.
func (d *agentDoor) call(ctx context.Context, handler http.HandlerFunc, method, path string, values map[string]string, body any, ifMatch int) ([]byte, error) {
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	ctx = context.WithValue(ctx, agentCallKey{}, agentCall{handle: factory.NormalizeHandle(d.asker), operator: d.operator, by: afiBy})
	r := httptest.NewRequestWithContext(ctx, method, path, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	if ifMatch >= 0 {
		r.Header.Set("If-Match", strconv.Itoa(ifMatch))
	}
	r.SetPathValue("id", d.id)
	for k, v := range values {
		r.SetPathValue(k, v)
	}
	w := httptest.NewRecorder()
	handler(w, r)
	if w.Code >= 300 {
		var refused struct {
			Error string `json:"error"`
		}
		msg := strings.TrimSpace(w.Body.String())
		if json.Unmarshal(w.Body.Bytes(), &refused) == nil && refused.Error != "" {
			msg = refused.Error
		}
		switch w.Code {
		case http.StatusPreconditionFailed:
			msg = "it changed since you read it; read it again"
		case http.StatusNotFound:
			msg = strings.TrimSuffix(msg, ".") + "; read the list again for the ids"
		case http.StatusForbidden:
			if strings.Contains(msg, "edit") && !strings.Contains(msg, "owner") {
				msg = errAgentNoWrite.Error()
			}
		}
		return nil, agentRefusal{status: w.Code, msg: msg}
	}
	return w.Body.Bytes(), nil
}

// Work answers one of session's ws_* tools.
func (d *agentDoor) Work(ctx context.Context, tool string, args json.RawMessage) (string, error) {
	var out any
	var err error
	switch tool {
	case "ws_tasks":
		out, err = d.wsTasks(ctx, args)
	case "ws_task_create":
		out, err = d.wsTaskCreate(ctx, args)
	case "ws_task_update":
		out, err = d.wsTaskUpdate(ctx, args)
	case "ws_task_comment":
		out, err = d.wsTaskComment(ctx, args)
	case "ws_files":
		out, err = d.wsFiles(ctx, args)
	case "ws_file_read":
		out, err = d.wsFileRead(ctx, args)
	case "ws_file_create":
		out, err = d.wsFileCreate(ctx, args)
	case "ws_file_suggest":
		out, err = d.wsFileSuggest(ctx, args)
	case "ws_decisions":
		out, err = d.wsDecisions(ctx, args)
	case "ws_task_conflict":
		out, err = d.wsTaskConflict(ctx, args)
	default:
		return "", errors.New("this workstream tool is not served here")
	}
	if err != nil {
		return "", err
	}
	if s, ok := out.(string); ok {
		return s, nil
	}
	raw, err := json.Marshal(out)
	return string(raw), err
}

func agentArgs(args json.RawMessage, into any) error {
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("invalid arguments: %v", err)
	}
	return nil
}

func agentWords(s, what string, most int) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("invalid arguments: %s needs words", what)
	}
	if len([]rune(s)) > most {
		return "", fmt.Errorf("invalid arguments: %s is longer than %d characters", what, most)
	}
	return s, nil
}

// ── tasks ──

// taskBrief is one task as the model reads it in a list.
func taskBrief(t factory.WorkstreamTask) map[string]any {
	o := map[string]any{"id": t.ID, "rev": t.Rev, "title": t.Title, "state": t.State, "owner": t.Owner, "done_by": t.DoneBy}
	if t.Group != nil {
		o["group"] = t.Group.Name
	}
	if len(t.WaitingOn) > 0 {
		o["waiting_on"] = t.WaitingOn
	}
	if t.Due != "" {
		o["due"] = t.Due
	}
	if t.Ask != nil {
		o["asks"] = t.Ask.Title
	}
	return o
}

// agentDoes is whether the agent may change t: work an agent does (Afi or a
// chat). A person's task and a Factory job are commented on, never changed.
func agentDoes(t factory.WorkstreamTask) bool {
	return t.DoneBy.Kind == factory.TaskByAfi || t.DoneBy.Kind == factory.TaskByChat
}

func (d *agentDoor) tasks(ctx context.Context) ([]factory.WorkstreamTask, error) {
	raw, err := d.call(ctx, d.h.listWorkstreamTasks, http.MethodGet, "/v1/workstreams/"+d.id+"/tasks", nil, nil, -1)
	if err != nil {
		return nil, err
	}
	var list api.WorkstreamTasks
	return list.Tasks, json.Unmarshal(raw, &list)
}

func (d *agentDoor) task(ctx context.Context, id string) (factory.WorkstreamTask, error) {
	raw, err := d.call(ctx, d.h.getWorkstreamTask, http.MethodGet, "/v1/workstreams/"+d.id+"/tasks/"+url.PathEscape(id), map[string]string{"tid": id}, nil, -1)
	if err != nil {
		return factory.WorkstreamTask{}, err
	}
	var t factory.WorkstreamTask
	return t, json.Unmarshal(raw, &t)
}

func (d *agentDoor) wsTasks(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		Task  string `json:"task"`
		State string `json:"state"`
		Group string `json:"group"`
		Mine  bool   `json:"mine"`
	}
	if err := agentArgs(args, &a); err != nil {
		return nil, err
	}
	if id := strings.TrimSpace(a.Task); id != "" {
		t, err := d.task(ctx, id)
		if err != nil {
			return nil, err
		}
		raw, err := d.call(ctx, d.h.workstreamTaskEvents, http.MethodGet, "/v1/workstreams/"+d.id+"/tasks/"+url.PathEscape(id)+"/events", map[string]string{"tid": id}, nil, -1)
		if err != nil {
			return nil, err
		}
		var events api.WorkstreamTaskEvents
		if err := json.Unmarshal(raw, &events); err != nil {
			return nil, err
		}
		return map[string]any{"task": t, "events": events.Events}, nil
	}
	all, err := d.tasks(ctx)
	if err != nil {
		return nil, err
	}
	picked := []map[string]any{}
	more := 0
	for _, t := range all {
		switch {
		case a.State != "" && t.State != a.State:
			continue
		case a.Group != "" && (t.Group == nil || !strings.EqualFold(t.Group.Name, strings.TrimSpace(a.Group))):
			continue
		case a.Mine && t.DoneBy.Kind != factory.TaskByAfi:
			continue
		}
		if len(picked) >= agentWorkListMost {
			more++
			continue
		}
		picked = append(picked, taskBrief(t))
	}
	out := map[string]any{"tasks": picked}
	if more > 0 {
		out["more"] = more
	}
	return out, nil
}

// ownerOf reads the model's owner word: "me" (the person it answers, the
// default), "everyone", or the handle of someone who can see the workstream.
func (d *agentDoor) ownerOf(ctx context.Context, word string) (*factory.TaskOwner, error) {
	word = strings.TrimPrefix(strings.TrimSpace(word), "@")
	switch strings.ToLower(word) {
	case "", "me", "you":
		return nil, nil
	case "everyone":
		return &factory.TaskOwner{Kind: factory.TaskOwnerEveryone}, nil
	}
	ws, err := d.Workstream(ctx)
	if err != nil {
		return nil, err
	}
	handle := factory.NormalizeHandle(word)
	if handle == factory.NormalizeHandle(d.asker) {
		return nil, nil
	}
	v := Viewer{Handle: handle, Teams: map[string]bool{}}
	for _, team := range d.h.Membership().TeamsOf(handle) {
		v.Teams[team] = true
	}
	if handle == "" || !v.canWrite(ws) {
		return nil, fmt.Errorf("%q is not someone in this workstream: owner is \"me\", \"everyone\", or the id of a person in it (a task's owner.id from ws_tasks)", word)
	}
	return &factory.TaskOwner{Kind: factory.TaskOwnerPerson, ID: handle, Name: handle}, nil
}

func (d *agentDoor) wsTaskCreate(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		Title     string   `json:"title"`
		Request   string   `json:"request"`
		Owner     string   `json:"owner"`
		DoneBy    string   `json:"done_by"`
		WaitingOn []string `json:"waiting_on"`
		Due       string   `json:"due"`
		Group     string   `json:"group"`
	}
	if err := agentArgs(args, &a); err != nil {
		return nil, err
	}
	title, err := agentWords(a.Title, "title", 200)
	if err != nil {
		return nil, err
	}
	request, err := agentWords(a.Request, "request", 4000)
	if err != nil {
		return nil, err
	}
	if a.Due != "" {
		if _, err := time.Parse(time.DateOnly, a.Due); err != nil {
			return nil, errors.New("invalid arguments: due is YYYY-MM-DD")
		}
	}
	if len(a.WaitingOn) > 20 {
		return nil, errors.New("invalid arguments: a task waits on at most 20 others")
	}
	owner, err := d.ownerOf(ctx, a.Owner)
	if err != nil {
		return nil, err
	}
	body := factory.TaskCreate{Title: title, Request: request, Owner: owner, WaitingOn: a.WaitingOn, Due: a.Due,
		CameFrom: &factory.TaskCameFrom{Kind: factory.TaskByChat, ChatID: "afi", Name: "Afi"}}
	switch a.DoneBy {
	case "", "this_chat":
		body.DoneBy = factory.TaskDoneBy{Kind: factory.TaskByAfi, Name: "Afi"}
	case "person":
		// The owner ticks a person task, so the owner does it: the person it
		// answers, or the person named.
		who := factory.NormalizeHandle(d.asker)
		if owner != nil && owner.Kind == factory.TaskOwnerPerson {
			who = owner.ID
		}
		body.DoneBy = factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: who, Name: who}
	case "factory":
		return nil, errors.New("a Factory task is a Factory job in this workstream: ask the person to start the job in the Factory, and it appears in Tasks by itself")
	default:
		return nil, fmt.Errorf("invalid arguments: done_by is this_chat, factory or person, not %q", a.DoneBy)
	}
	if g := strings.TrimSpace(a.Group); g != "" {
		if g, err = agentWords(g, "group", 200); err != nil {
			return nil, err
		}
		body.Group = &factory.TaskGroup{Name: g, Kind: "batch"}
	}
	raw, err := d.call(ctx, d.h.createWorkstreamTask, http.MethodPost, "/v1/workstreams/"+d.id+"/tasks", nil, body, -1)
	if err != nil {
		return nil, err
	}
	var t factory.WorkstreamTask
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, err
	}
	said := fmt.Sprintf("Made the task %q (%s) in Up next; everyone in the workstream sees it.", t.Title, t.ID)
	if t.Group != nil {
		n := 0
		if all, err := d.tasks(ctx); err == nil {
			for _, x := range all {
				if x.Group != nil && x.Group.ID == t.Group.ID {
					n++
				}
			}
		}
		said += fmt.Sprintf(" It is %d in the group %q.", n, t.Group.Name)
		if n >= 5 {
			said += " They wait in Up next; none of them starts by itself."
		}
	}
	return map[string]any{"said": said, "task": taskBrief(t)}, nil
}

func (d *agentDoor) wsTaskUpdate(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		Task        string  `json:"task"`
		Rev         *int    `json:"rev"`
		Title       *string `json:"title"`
		Request     *string `json:"request"`
		State       *string `json:"state"`
		StateReason *string `json:"state_reason"`
		Check       *string `json:"check"`
		Results     *[]struct {
			File  string `json:"file"`
			URL   string `json:"url"`
			Label string `json:"label"`
		} `json:"results"`
		Ask *struct {
			Title   string           `json:"title"`
			Detail  string           `json:"detail"`
			Options []agentAskOption `json:"options"`
		} `json:"ask"`
		WaitingOn   *[]string `json:"waiting_on"`
		Due         *string   `json:"due"`
		RequestNote string    `json:"request_note"`
		Decision    string    `json:"decision"`
		Assumed     []string  `json:"assumed"`
	}
	if err := agentArgs(args, &a); err != nil {
		return nil, err
	}
	id := strings.TrimSpace(a.Task)
	t, err := d.task(ctx, id)
	if err != nil {
		return nil, err
	}
	if !agentDoes(t) {
		who := firstNonEmpty(t.DoneBy.Name, t.DoneBy.Kind)
		return nil, fmt.Errorf("this task is done by %s, not by you; you change only the tasks an agent does. Comment on it with ws_task_comment instead", who)
	}
	rev := t.Rev
	if a.Rev != nil {
		rev = *a.Rev
	}
	p := map[string]any{}
	if a.Title != nil {
		s, err := agentWords(*a.Title, "title", 200)
		if err != nil {
			return nil, err
		}
		p["title"] = s
	}
	if a.Request != nil {
		s, err := agentWords(*a.Request, "request", 4000)
		if err != nil {
			return nil, err
		}
		p["request"] = s
	}
	if a.State != nil {
		if !slices.Contains([]string{factory.TaskRunning, factory.TaskDone, factory.TaskFailed, factory.TaskStopped}, *a.State) {
			return nil, errors.New("invalid arguments: state is running, done, failed or stopped")
		}
		p["state"] = *a.State
	}
	if a.StateReason != nil {
		p["state_reason"] = cutRunes(strings.TrimSpace(*a.StateReason), 500)
	}
	if a.Check != nil {
		p["check"] = cutRunes(strings.TrimSpace(*a.Check), 500)
	}
	if a.Results != nil {
		rs := []factory.TaskResult{}
		for _, r := range *a.Results {
			switch {
			case strings.TrimSpace(r.File) != "" && strings.TrimSpace(r.URL) == "":
				rs = append(rs, factory.TaskResult{Kind: "file", FileID: strings.TrimSpace(r.File)})
			case strings.TrimSpace(r.URL) != "" && strings.TrimSpace(r.File) == "":
				rs = append(rs, factory.TaskResult{Kind: "url", URL: strings.TrimSpace(r.URL), Label: strings.TrimSpace(r.Label)})
			default:
				return nil, errors.New("invalid arguments: a result is a file or a url, one of them")
			}
		}
		p["results"] = rs
	}
	if a.Ask != nil {
		if len(a.Ask.Options) == 0 || len(a.Ask.Options) > 4 {
			return nil, errors.New("invalid arguments: an Ask has one to four options")
		}
		title, err := agentWords(a.Ask.Title, "the Ask's title", 200)
		if err != nil {
			return nil, err
		}
		// The recommendation leads, as the Ask card draws it.
		opts := slices.Clone(a.Ask.Options)
		slices.SortStableFunc(opts, func(x, y agentAskOption) int {
			switch {
			case x.Recommended == y.Recommended:
				return 0
			case x.Recommended:
				return -1
			}
			return 1
		})
		// At most one option stays recommended: the first, which leads.
		options := []factory.TaskAskOption{}
		for i, o := range opts {
			o.Recommended = o.Recommended && i == 0
			label, err := agentWords(o.Label, "an option", 80)
			if err != nil {
				return nil, err
			}
			options = append(options, factory.TaskAskOption{ID: fmt.Sprintf("o%d", i+1), Label: label, Recommended: o.Recommended,
				Decides: cutRunes(strings.TrimSpace(o.Decides), 300)})
		}
		p["ask"] = factory.TaskAsk{Title: title, Detail: cutRunes(strings.TrimSpace(a.Ask.Detail), 1000), Options: options}
	}
	if a.WaitingOn != nil {
		if len(*a.WaitingOn) > 20 {
			return nil, errors.New("invalid arguments: a task waits on at most 20 others")
		}
		p["waiting_on"] = *a.WaitingOn
	}
	if a.Due != nil {
		if *a.Due != "" {
			if _, err := time.Parse(time.DateOnly, *a.Due); err != nil {
				return nil, errors.New("invalid arguments: due is YYYY-MM-DD")
			}
		}
		p["due"] = *a.Due
	}
	if len(a.Assumed) > 0 {
		if len(a.Assumed) > 10 {
			return nil, errors.New("invalid arguments: at most 10 assumed lines at once")
		}
		lines := []string{}
		for _, l := range a.Assumed {
			l, err := agentWords(l, "an assumed line", 300)
			if err != nil {
				return nil, err
			}
			lines = append(lines, l)
		}
		p["assumed"] = lines
	}
	if len(p) > 0 {
		raw, err := d.call(ctx, d.h.patchWorkstreamTask, http.MethodPatch, "/v1/workstreams/"+d.id+"/tasks/"+url.PathEscape(id), map[string]string{"tid": id}, p, rev)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, err
		}
	}
	if note := strings.TrimSpace(a.RequestNote); note != "" {
		body := api.TaskComment{TaskNote: factory.TaskNote{Kind: factory.TaskEventRequestNote, Text: cutRunes(note, 300), Decision: cutRunes(strings.TrimSpace(a.Decision), 300)}}
		if _, err := d.call(ctx, d.h.commentWorkstreamTask, http.MethodPost, "/v1/workstreams/"+d.id+"/tasks/"+url.PathEscape(id)+"/comment", map[string]string{"tid": id}, body, -1); err != nil {
			return nil, err
		}
		if t, err = d.task(ctx, id); err != nil {
			return nil, err
		}
	}
	return map[string]any{"said": fmt.Sprintf("Changed %q (%s).", t.Title, t.ID), "task": taskBrief(t)}, nil
}

func (d *agentDoor) wsTaskComment(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		Task     string `json:"task"`
		Kind     string `json:"kind"`
		Text     string `json:"text"`
		ForEvent string `json:"for_event"`
		Decision string `json:"decision"`
	}
	if err := agentArgs(args, &a); err != nil {
		return nil, err
	}
	kind := firstNonEmpty(a.Kind, factory.TaskEventComment)
	most := 2000
	if kind == factory.TaskEventRequestNote {
		most = 300
	}
	text, err := agentWords(a.Text, "text", most)
	if err != nil {
		return nil, err
	}
	note := factory.TaskNote{Kind: kind, Text: text}
	if kind == factory.TaskEventRequestNote {
		note.Decision = cutRunes(strings.TrimSpace(a.Decision), 300)
	}
	switch kind {
	case factory.TaskEventComment, factory.TaskEventRequestNote:
	case factory.TaskEventOffer:
		if note.OfferFor = strings.TrimSpace(a.ForEvent); note.OfferFor == "" {
			return nil, errors.New("invalid arguments: an offer names the comment it answers in for_event")
		}
	default:
		return nil, fmt.Errorf("invalid arguments: kind is comment, offer or request_note, not %q", kind)
	}
	id := strings.TrimSpace(a.Task)
	if _, err := d.task(ctx, id); err != nil {
		return nil, err
	}
	raw, err := d.call(ctx, d.h.commentWorkstreamTask, http.MethodPost, "/v1/workstreams/"+d.id+"/tasks/"+url.PathEscape(id)+"/comment", map[string]string{"tid": id}, api.TaskComment{TaskNote: note}, -1)
	if err != nil {
		return nil, err
	}
	var ev factory.WorkstreamTaskEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, err
	}
	said := "Wrote on the task's thread; everyone in the workstream reads it."
	switch kind {
	case factory.TaskEventOffer:
		said = "Offered under the comment. Nothing happens until someone in the workstream takes the offer."
	case factory.TaskEventRequestNote:
		said = "Left the one line on the task."
	}
	return map[string]any{"said": said, "event": ev.ID}, nil
}

// ── files ──

func (d *agentDoor) files(ctx context.Context, q string) (api.WorkstreamFiles, error) {
	path := "/v1/workstreams/" + d.id + "/files"
	if q != "" {
		path += "?q=" + url.QueryEscape(q)
	}
	raw, err := d.call(ctx, d.h.listWorkstreamFiles, http.MethodGet, path, nil, nil, -1)
	if err != nil {
		return api.WorkstreamFiles{}, err
	}
	var out api.WorkstreamFiles
	return out, json.Unmarshal(raw, &out)
}

func fileBrief(f factory.WorkstreamFile) map[string]any {
	o := map[string]any{"id": f.ID, "rev": f.Rev, "kind": f.Kind, "name": f.Name, "made_by": f.MadeBy.Name}
	if f.Folder != "" {
		o["folder"] = f.Folder
	}
	if f.PersonEdited {
		o["person_edited"] = true
	}
	if f.Kind == factory.FileDocument {
		o["you_may_rewrite"] = f.AgentMayEdit()
	}
	if f.URL != "" {
		o["url"] = f.URL
	}
	if f.Repo != "" {
		o["repo"] = f.Repo
	}
	return o
}

func (d *agentDoor) wsFiles(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		Folder string `json:"folder"`
		Query  string `json:"query"`
	}
	if err := agentArgs(args, &a); err != nil {
		return nil, err
	}
	all, err := d.files(ctx, strings.TrimSpace(a.Query))
	if err != nil {
		return nil, err
	}
	list := []map[string]any{}
	for _, f := range all.Files {
		if a.Folder != "" && f.Folder != a.Folder {
			continue
		}
		if len(list) < agentWorkListMost {
			list = append(list, fileBrief(f))
		}
	}
	out := map[string]any{"files": list}
	if all.From != nil && a.Folder == "" {
		from := []map[string]any{}
		for _, f := range all.From.Files {
			if len(from) < agentWorkListMost {
				from = append(from, fileBrief(f))
			}
		}
		out["from "+all.From.Name] = from
	}
	return out, nil
}

// fileOf is one file of this workstream (or, to read, of its parent), and
// the workstream whose pages hold its words.
func (d *agentDoor) fileOf(ctx context.Context, id string, parentToo bool) (factory.WorkstreamFile, factory.OrgWorkstream, error) {
	all, err := d.files(ctx, "")
	if err != nil {
		return factory.WorkstreamFile{}, factory.OrgWorkstream{}, err
	}
	if i := slices.IndexFunc(all.Files, func(f factory.WorkstreamFile) bool { return f.ID == id }); i >= 0 {
		ws, err := d.Workstream(ctx)
		return all.Files[i], ws, err
	}
	if parentToo && all.From != nil {
		if i := slices.IndexFunc(all.From.Files, func(f factory.WorkstreamFile) bool { return f.ID == id }); i >= 0 {
			ws, _, err := d.h.config.Store.Workstream(all.From.ID)
			return all.From.Files[i], ws, err
		}
	}
	return factory.WorkstreamFile{}, factory.OrgWorkstream{}, fmt.Errorf("this workstream has no file %q; ws_files lists them", id)
}

func pageOf(ws factory.OrgWorkstream, id string) (factory.WorkstreamPage, bool) {
	i := slices.IndexFunc(ws.Pages, func(p factory.WorkstreamPage) bool { return p.ID == id })
	if i < 0 {
		return factory.WorkstreamPage{}, false
	}
	return ws.Pages[i], true
}

func (d *agentDoor) wsFileRead(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		File string `json:"file"`
	}
	if err := agentArgs(args, &a); err != nil {
		return nil, err
	}
	f, ws, err := d.fileOf(ctx, strings.TrimSpace(a.File), true)
	if err != nil {
		return nil, err
	}
	out := fileBrief(f)
	out["version"] = f.Rev
	switch f.Kind {
	case factory.FileDocument:
		if p, ok := pageOf(ws, f.PageID); ok {
			out["markdown"] = p.BodyMD
		}
	case factory.FileUpload:
		if f.Upload != nil {
			out["size"], out["mime"] = f.Upload.Size, f.Upload.Mime
		}
	case factory.FileShortcut:
		if f.Target != nil {
			out["shows"] = f.Target.File
		}
	}
	return out, nil
}

func (d *agentDoor) wsFileCreate(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		Kind     string `json:"kind"`
		Name     string `json:"name"`
		Markdown string `json:"markdown"`
		URL      string `json:"url"`
		Folder   string `json:"folder"`
		FromTask string `json:"from_task"`
		File     string `json:"file"`
	}
	if err := agentArgs(args, &a); err != nil {
		return nil, err
	}
	if id := strings.TrimSpace(a.File); id != "" {
		return d.rewriteDocument(ctx, id, a.Markdown)
	}
	name, err := agentWords(a.Name, "name", 200)
	if err != nil {
		return nil, err
	}
	body := api.FileNew{Kind: firstNonEmpty(a.Kind, factory.FileDocument), Name: name, Folder: a.Folder, FromTask: a.FromTask,
		MadeBy: &factory.FileMaker{Kind: factory.MakerAfi, Name: "Afi"}}
	switch body.Kind {
	case factory.FileDocument:
		body.BodyMD = a.Markdown
	case factory.FileFolder:
	case factory.FileLink:
		body.URL = strings.TrimSpace(a.URL)
	default:
		return nil, errors.New("invalid arguments: kind is document, folder or link")
	}
	raw, err := d.call(ctx, d.h.createWorkstreamFile, http.MethodPost, "/v1/workstreams/"+d.id+"/files", nil, body, -1)
	if err != nil {
		return nil, err
	}
	var f factory.WorkstreamFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	return map[string]any{"said": fmt.Sprintf("Made the %s %q (%s); everyone in the workstream sees it.", f.Kind, f.Name, f.ID), "file": fileBrief(f)}, nil
}

// rewriteDocument rewrites a document the agent made that no person has
// edited. Any other document is changed by suggestion.
func (d *agentDoor) rewriteDocument(ctx context.Context, id, markdown string) (any, error) {
	f, _, err := d.fileOf(ctx, id, false)
	if err != nil {
		return nil, err
	}
	if f.Kind != factory.FileDocument || f.PageID == "" {
		return nil, errors.New("only a document's words are rewritten")
	}
	if !f.AgentMayEdit() {
		return nil, errors.New("a person made or has edited this document, so you change it only by suggestion: use ws_file_suggest")
	}
	line, err := d.WritePage(ctx, f.PageID, "", markdown)
	if err != nil {
		return nil, err
	}
	return map[string]any{"said": line}, nil
}

func (d *agentDoor) wsFileSuggest(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		File        string `json:"file"`
		BaseVersion int    `json:"base_version"`
		Why         string `json:"why"`
		Edits       []struct {
			Find    string `json:"find"`
			Replace string `json:"replace"`
			Why     string `json:"why"`
		} `json:"edits"`
	}
	if err := agentArgs(args, &a); err != nil {
		return nil, err
	}
	why, err := agentWords(a.Why, "why", 200)
	if err != nil {
		return nil, err
	}
	if len(a.Edits) == 0 || len(a.Edits) > 50 {
		return nil, errors.New("invalid arguments: a suggestion has 1 to 50 edits")
	}
	f, ws, err := d.fileOf(ctx, strings.TrimSpace(a.File), false)
	if err != nil {
		return nil, err
	}
	if f.Kind != factory.FileDocument || f.PageID == "" {
		return nil, errors.New("only a document takes suggestions")
	}
	if a.BaseVersion != f.Rev {
		return nil, errors.New("the document changed since you read it; read it again with ws_file_read")
	}
	page, ok := pageOf(ws, f.PageID)
	if !ok {
		return nil, errPageGone
	}
	text := page.BodyMD
	for _, e := range a.Edits {
		switch n := strings.Count(text, e.Find); {
		case e.Find == "" || n == 0:
			return nil, fmt.Errorf("%q is not in the document as it reads now; quote it exactly", cutRunes(e.Find, 80))
		case n > 1:
			return nil, fmt.Errorf("%q is in the document %d times; quote more of it so it is found once", cutRunes(e.Find, 80), n)
		}
		text = strings.Replace(text, e.Find, e.Replace, 1)
	}
	n, err := d.keepSuggestion(ctx, ws, page, text, why)
	if err != nil {
		return nil, err
	}
	return map[string]any{"said": "Suggested the change to " + strconv.Quote(page.Title) + "; nothing in it was applied: " + keptWords(n) + "."}, nil
}

// keepSuggestion keeps text as a version of page that is not applied, which
// anyone who can edit the document may take.
// It answers the version's number.
func (d *agentDoor) keepSuggestion(ctx context.Context, ws factory.OrgWorkstream, page factory.WorkstreamPage, text, why string) (int, error) {
	h := d.h
	v := d.viewer()
	if !v.canWrite(ws) {
		return 0, errAgentNoWrite
	}
	if ws.ArchivedAt != nil {
		return 0, errWorkstreamArchived
	}
	// The agent read the page's mirror, which holds the words of the version
	// the page holds now (keepVersion writes both): that version is what its
	// text changed, the base a Restore merges from (L1 D15).
	list, err := h.config.Store.PageVersions(ws.ID, page.ID)
	if err != nil {
		return 0, err
	}
	stamp := h.clock().UTC().Format(time.RFC3339Nano)
	saved, added, err := h.config.Store.AddPageVersion(ws.ID, page.ID, store.PageVersion{At: stamp, By: v.Handle, Title: page.Title, BodyMD: text,
		Reason: versionReasonKept, BasedOn: versionsWire(list).LiveVersion})
	if err != nil {
		return 0, err
	}
	if added {
		h.recordAs(ctx, v.Handle, record.WorkstreamChanged, "changed page", orgObjectWorkstream+ws.ID, page.ID+" version "+strconv.Itoa(saved.Version)+" by the agent: "+why)
		h.publishWorkstreamPart(ws, api.PartVersions, page.ID, v.Handle, saved.Version, false)
	}
	return saved.Version, nil
}

// keptWords is what the agent is told when its text was kept, not applied.
// NOTHING IN FILES MENTIONS VERSIONS (final spec, "No version history in this
// release"), and the agent must not claim the workstream keeps history: a
// model repeats what its tool answers. So the words say what the person sees
// and does, the same on Personal and on an organization, and never the
// version's number, History or restoring, though the server still keeps the
// text as a version underneath.
func keptWords(int) string {
	return "it waits as a suggestion for someone who can edit the page to take it"
}

func cutRunes(s string, most int) string {
	r := []rune(s)
	if len(r) <= most {
		return s
	}
	return string(r[:most])
}

// agentAskOption is one answer of an Ask the agent raises: its label, whether
// it leads, and the decision it records when chosen.
type agentAskOption struct {
	Label       string `json:"label"`
	Recommended bool   `json:"recommended"`
	Decides     string `json:"decides"`
}

// ── decisions ──

// wsDecisions answers what the workstream's people settled and keep now: a
// withdrawn or replaced decision is history, never something to keep to.
func (d *agentDoor) wsDecisions(ctx context.Context, args json.RawMessage) (any, error) {
	if err := agentArgs(args, &struct{}{}); err != nil {
		return nil, err
	}
	ws, err := d.Workstream(ctx)
	if err != nil {
		return nil, err
	}
	list := []map[string]any{}
	for _, x := range nameDecisions(factory.CurrentDecisions(ws.Decisions), d.h.personNames()) {
		list = append(list, map[string]any{"id": x.ID, "text": x.Text, "by": x.By, "by_name": x.ByName})
	}
	return map[string]any{"decisions": list}, nil
}

// wsTaskConflict is the decision check's own verb: it pauses a task on an Ask
// to the decision's maker. The model never holds it.
func (d *agentDoor) wsTaskConflict(ctx context.Context, args json.RawMessage) (any, error) {
	var a struct {
		Task     string `json:"task"`
		Decision string `json:"decision"`
		Why      string `json:"why"`
	}
	if err := agentArgs(args, &a); err != nil {
		return nil, err
	}
	id := strings.TrimSpace(a.Task)
	body := map[string]string{"decision": strings.TrimSpace(a.Decision), "why": cutRunes(strings.TrimSpace(a.Why), 500)}
	raw, err := d.call(ctx, d.h.conflictWorkstreamTask, http.MethodPost, "/v1/workstreams/"+d.id+"/tasks/"+url.PathEscape(id)+"/conflict", map[string]string{"tid": id}, body, -1)
	if err != nil {
		return nil, err
	}
	var t factory.WorkstreamTask
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, err
	}
	return map[string]any{"task": taskBrief(t)}, nil
}

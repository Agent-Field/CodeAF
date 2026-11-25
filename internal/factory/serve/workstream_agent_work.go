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
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── THE WORKSTREAM TOOLS, ANSWERED FOR THE SERVER'S AGENT ──────────────────
//
// internal/session spells the ws_* tools once (tools_workstream_work.go)
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
	agent    string
	bot      string
	only     []string
	operator bool
	by       factory.TaskEventBy
}

func agentCallOf(r *http.Request) (agentCall, bool) {
	call, ok := r.Context().Value(agentCallKey{}).(agentCall)
	return call, ok
}

// afiCallOf keeps exceptions made for the server's own agent off the bot door.
func afiCallOf(r *http.Request) (agentCall, bool) {
	call, ok := agentCallOf(r)
	return call, ok && call.bot == ""
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
	ctx = context.WithValue(ctx, agentCallKey{}, agentCall{handle: factory.NormalizeHandle(d.asker), operator: d.isCurrentOperator(), agent: d.agentID(), by: afiBy})
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
	case "ws_file_comment":
		out, err = d.wsFileComment(ctx, args)
	case "ws_decisions":
		out, err = d.wsDecisions(ctx, args)
	case "ws_task_propose":
		out, err = d.wsTaskPropose(ctx, args)
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
	if len(t.Checklist) > 0 {
		// The ids are what ws_task_update's checklist_done names.
		o["checklist"] = t.Checklist
	}
	// Whether it starts by itself once what it waits on is done, so Afi never
	// tells a person a task waits on their Start when it will not.
	if t.StartsWhenReady != nil && t.State == factory.TaskUpNext {
		if *t.StartsWhenReady {
			o["starts"] = "by itself when what it waits on is done"
		} else {
			o["starts"] = "when a person presses Start"
		}
	}
	return o
}

// agentDoes is whether the agent may change t: work an agent does (Afi, a
// chat's agent, a named agent or crew). A person's task and a Factory job are
// commented on, never changed.
func agentDoes(t factory.WorkstreamTask) bool {
	return factory.AgentDoes(t)
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
// default), or the handle of one person who can see the workstream.
func (d *agentDoor) ownerOf(ctx context.Context, word string) (*factory.TaskOwner, error) {
	word = strings.TrimPrefix(strings.TrimSpace(word), "@")
	switch strings.ToLower(word) {
	case "", "me", "you":
		return nil, nil
	case "everyone":
		return nil, fmt.Errorf("A task needs one person as owner: use \"me\" for the person you are talking to, or a person’s id from ws_tasks")
	}
	ws, err := d.Workstream(ctx)
	if err != nil {
		return nil, err
	}
	handle := factory.NormalizeHandle(word)
	if handle == factory.NormalizeHandle(d.asker) {
		return nil, nil
	}
	v := d.h.viewerOf(handle)
	if handle == "" || !v.canWrite(ws) {
		return nil, fmt.Errorf("%q is not someone in this workstream: owner is \"me\" or the id of a person in it (a task's owner.id from ws_tasks)", word)
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
		Checklist []string `json:"checklist"`
		agentBriefArgs
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
	if len(a.Checklist) > factory.TaskChecklistMost {
		return nil, fmt.Errorf("invalid arguments: a checklist has at most %d lines", factory.TaskChecklistMost)
	}
	body := factory.TaskCreate{Title: title, Request: request, Owner: owner, WaitingOn: a.WaitingOn, Due: a.Due, Checklist: a.Checklist,
		CameFrom: &factory.TaskCameFrom{Kind: factory.TaskByChat, ChatID: "afi", Name: "Afi"}}
	if body.DoneBy, err = d.doerOfWord(ctx, a.DoneBy, owner); err != nil {
		return nil, err
	}
	if g := strings.TrimSpace(a.Group); g != "" {
		if g, err = agentWords(g, "group", 200); err != nil {
			return nil, err
		}
		body.Group = &factory.TaskGroup{Name: g, Kind: "batch"}
	}
	if body.Brief, err = d.briefOf(ctx, a.agentBriefArgs, a.WaitingOn, body.DoneBy.Kind != factory.TaskByPerson); err != nil {
		return nil, err
	}
	raw, err := d.call(ctx, d.h.createWorkstreamTask, http.MethodPost, "/v1/workstreams/"+d.id+"/tasks", nil, body, -1)
	if err != nil {
		return nil, err
	}
	var t factory.WorkstreamTask
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, err
	}
	said := fmt.Sprintf("Made the task %q in Up next; everyone in the workstream sees it.", t.Title)
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
			said += " They wait in Up next as a plan until a person presses its Start; after that each starts by itself once what it waits on is done."
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
			// Reversible is the agent's own word that the answer can be
			// undone (session.WSAskReversibleRule).
			Reversible bool   `json:"reversible"`
			Kind       string `json:"kind"`
		} `json:"ask"`
		WaitingOn   *[]string `json:"waiting_on"`
		Due         *string   `json:"due"`
		RequestNote string    `json:"request_note"`
		Decision    string    `json:"decision"`
		Assumed     []string  `json:"assumed"`
		CheckDone   []string  `json:"checklist_done"`
		CheckUndone []string  `json:"checklist_undone"`
		Handoff     *string   `json:"handoff"`
	}
	if err := agentArgs(args, &a); err != nil {
		return nil, err
	}
	decidedLine := ""
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
	// CAN'T DO IT IS AN ASK, NOT FAILED (owner decisions §10). An agent that
	// sets its task failed is saying it cannot go on: what it lacks becomes a
	// question to the task's owner, with what to do about it as answers, and
	// the task waits amber instead of turning red. Failed stays for errors,
	// which only the run's own ending writes (finishTaskRun).
	if a.State != nil && *a.State == factory.TaskFailed && a.Ask == nil {
		delete(p, "state")
		delete(p, "state_reason")
		why := strings.TrimSpace(derefString(a.StateReason))
		if why == "" {
			why = "It did not say what it lacks."
		}
		p["ask"] = factory.CantDoAsk(firstNonEmpty(t.DoneBy.Name, "Afi"), why, strconv.FormatInt(time.Now().UnixNano(), 36))
	}
	if a.StateReason != nil {
		p["state_reason"] = cutRunes(strings.TrimSpace(*a.StateReason), 500)
	}
	if a.Check != nil {
		claims := cutRunes(strings.TrimSpace(*a.Check), 500)
		if claims != "" {
			ws, err := d.Workstream(ctx)
			if err != nil {
				return nil, err
			}
			claims = factory.CheckedAgainst(len(factory.CurrentDecisions(ws.Decisions)), claims)
		}
		p["check"] = claims
	}
	if a.Handoff != nil {
		p["handoff"] = cutRunes(strings.TrimSpace(*a.Handoff), factory.TaskHandoffMost)
	}
	// DONE CARRIES A HAND-OFF NOTE. The next task's run starts with it, so a
	// task an agent calls done without one is refused in words the model can
	// act on; the run's own clean ending (finishTaskRun) writes one for it.
	if a.State != nil && *a.State == factory.TaskDone && strings.TrimSpace(firstNonEmpty(derefString(a.Handoff), t.Handoff)) == "" {
		return nil, errors.New("set handoff with done: two to four lines on what you made, what you assumed, and what the next task should know")
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
				Decides: cutRunes(strings.TrimSpace(o.Decides), 300), Words: o.Words})
		}
		ask := factory.TaskAsk{Title: title, Detail: cutRunes(strings.TrimSpace(a.Ask.Detail), 1000), Options: options, Kind: factory.AskKind(a.Ask.Kind), Reversible: a.Ask.Reversible}
		// AN ANSWER THAT NEEDS THE PERSON'S WORDS IS NEVER ONE CLICK (live
		// test 2026-11-23 #37): an Ask holding one is not reversible, so it is
		// never accepted with the rest, and no rule decides it for them.
		words := factory.NeedsWords(ask)
		if words {
			ask.Reversible = false
		}
		// A KIND THE STEWARD LET GO IS NOT ASKED. The server decides it with
		// the answer the agent recommended and lists it under Assumed, where
		// Send back still overturns it (owner decisions §13).
		decided := false
		if ws, err := d.Workstream(ctx); err == nil && !words {
			if ex, ok := factory.ExceptionFor(ws.RulesOrDefault(), ask.Kind); ok {
				asked := ask
				asked.By = &factory.TaskEventBy{Kind: factory.TaskByAfi}
				if line, ok := factory.ExceptionAssumed(asked, ex); ok {
					a.Assumed = append(a.Assumed, line)
					decidedLine, decided = line, true
				}
			}
		}
		if !decided {
			p["ask"] = ask
		}
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
		if len(a.Assumed) > 10+len(nonEmptyStrings(decidedLine)) {
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
	// The agent ticks its own checklist lines as it works, by id; rewording
	// or adding lines is a person's change on the task page.
	if len(a.CheckDone) > 0 {
		p["check_done"] = a.CheckDone
	}
	if len(a.CheckUndone) > 0 {
		p["check_undone"] = a.CheckUndone
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
	said := fmt.Sprintf("Changed %q.", t.Title)
	if decidedLine != "" {
		said += " Your Ask was not raised: this workstream lets Afi decide that kind without asking, so it is listed under Assumed as " +
			strconv.Quote(decidedLine) + ". Carry on with your recommended answer."
	}
	return map[string]any{"said": said, "task": taskBrief(t)}, nil
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
	// A long comment is not refused: it is kept as a file (keepAsFile), so
	// the words are never lost to a limit the model did not see coming.
	most := factory.TaskCommentWordsMost
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
	t, err := d.task(ctx, id)
	if err != nil {
		return nil, err
	}
	if kind == factory.TaskEventComment && factory.TaskCommentTooLong(text) {
		return d.keepAsFile(ctx, t, a.Text)
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

// keepAsFile keeps a long comment as a document the task made, and leaves
// the one "made" line on its thread. WHAT AN AGENT MAKES IS A FILE (build
// spec v2, priority 1): a draft posted as a comment could not be opened,
// edited, suggested on or read by the next task, and buried every person's
// comment under it.
func (d *agentDoor) keepAsFile(ctx context.Context, t factory.WorkstreamTask, markdown string) (any, error) {
	name := factory.MadeFileName(markdown, t.Title)
	raw, err := d.call(ctx, d.h.createWorkstreamFile, http.MethodPost, "/v1/workstreams/"+d.id+"/files", nil,
		api.FileNew{Kind: factory.FileDocument, Name: name, FromTask: t.ID, BodyMD: markdown,
			MadeBy: &factory.FileMaker{Kind: factory.MakerAfi, Name: "Afi"}}, -1)
	if err != nil {
		return nil, err
	}
	var f factory.WorkstreamFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	d.wrote.note(f.ID)
	note := factory.TaskNote{Kind: factory.TaskEventMade, Text: f.Name, File: f.ID}
	raw, err = d.call(ctx, d.h.commentWorkstreamTask, http.MethodPost, "/v1/workstreams/"+d.id+"/tasks/"+url.PathEscape(t.ID)+"/comment", map[string]string{"tid": t.ID}, api.TaskComment{TaskNote: note}, -1)
	if err != nil {
		return nil, err
	}
	var ev factory.WorkstreamTaskEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, err
	}
	said := fmt.Sprintf("That was too long for a comment, so it is kept as the document %q in Files, made by this task; the thread has one line naming it. Write only a line or two as a comment.", f.Name)
	return map[string]any{"said": said, "event": ev.ID, "file": fileBrief(f)}, nil
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
	if f.FromTask != "" {
		o["from_task"] = f.FromTask
	}
	if len(f.UsedBy) > 0 {
		o["used_by"] = f.UsedBy
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

// earlierCopiesMost is how many of a document's earlier copies one
// ws_file_read names, newest first.
const earlierCopiesMost = 5

// earlierCopies is what a document's History holds before the words it has
// now, newest first, as the agent may speak of them: when, and who, never a
// number (owner decision 4, 2026-11-17). AN AGENT MAY SAY AN EARLIER COPY
// EXISTS ONLY WHEN THIS LISTS ONE, so an answer with none carries no field and
// the agent has nothing to claim. A text the agent kept that the page never
// held is not an earlier copy of the page, and is left out.
func earlierCopies(list []store.PageVersion, name func(string) string) []map[string]any {
	wire := versionsWire(list)
	out := []map[string]any{}
	for _, v := range wire.Versions {
		if v.Kept || v.Version == wire.LiveVersion || v.Reason == versionReasonKept {
			continue
		}
		out = append(out, map[string]any{"when": v.At, "by": name(v.By)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i]["when"].(string) > out[j]["when"].(string) })
	if len(out) > earlierCopiesMost {
		out = out[:earlierCopiesMost]
	}
	return out
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
		From int    `json:"from"`
	}
	if err := agentArgs(args, &a); err != nil {
		return nil, err
	}
	f, ws, err := d.fileOf(ctx, strings.TrimSpace(a.File), true)
	if err != nil {
		return nil, err
	}
	d.usedByTask(ctx, f, ws)
	out := fileBrief(f)
	out["version"] = f.Rev
	switch f.Kind {
	case factory.FileDocument:
		if p, ok := pageOf(ws, f.PageID); ok {
			out["markdown"] = p.BodyMD
			comments, err := d.passageComments(ws.ID, p.ID)
			if err != nil {
				return nil, err
			}
			if len(comments) > 0 {
				out["comments"] = comments
			}
			list, err := d.h.config.Store.PageVersions(ws.ID, p.ID)
			if err != nil {
				return nil, err
			}
			if earlier := earlierCopies(list, d.h.personNames()); len(earlier) > 0 {
				out["earlier_copies"] = earlier
			}
		}
	case factory.FileUpload:
		if f.Upload != nil {
			out["size"], out["mime"] = f.Upload.Size, f.Upload.Mime
			if f.Upload.Blob != "" {
				data, err := d.h.config.Store.Blob(f.Upload.Blob)
				if err != nil {
					return nil, err
				}
				uploadWords(out, data, a.From)
			}
		}
	case factory.FileShortcut:
		if f.Target != nil {
			out["shows"] = f.Target.File
		}
	}
	return out, nil
}

// usedByTask writes this run's task into the used_by of a file it just read,
// so the task page lists it under Uses and a later change to the file flags the
// task. Only this workstream's own files: a parent's file names only its own
// tasks. A file the task made is not also one it uses, a folder is never an
// input, and a write that loses a race is left alone; the read already
// happened, and the next read tries again.
func (d *agentDoor) usedByTask(ctx context.Context, f factory.WorkstreamFile, ws factory.OrgWorkstream) {
	if d.runTask == "" || ws.ID != d.id || f.Kind == factory.FileFolder || f.FromTask == d.runTask || slices.Contains(f.UsedBy, d.runTask) {
		return
	}
	used := append(slices.Clone(f.UsedBy), d.runTask)
	_, _ = d.call(ctx, d.h.patchWorkstreamFile, http.MethodPatch, "/v1/workstreams/"+d.id+"/files/"+url.PathEscape(f.ID), map[string]string{"fid": f.ID}, api.FilePatch{UsedBy: &used}, f.Rev)
}

// uploadTextMost is the most of an upload's words one ws_file_read answers;
// from reads on.
const uploadTextMost = 100 << 10

// uploadWords puts a text upload's words on a ws_file_read answer, from byte
// from on. AN UPLOAD IS READ THROUGH ITS FILE, NEVER FROM DISK (solo-sheets
// F1): its bytes live in the server's store, which no agent can reach, so an
// answer that gave only its size sent the agent hunting the disk for it. A
// file that is not text says plainly that it has no words to read.
func uploadWords(out map[string]any, data []byte, from int) {
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		out["said"] = "This upload is not text, so it has no words to read here. Do not look for it on disk or in the app's data folder; say what it is from its name and type, or ask the person about it."
		return
	}
	start := min(max(from, 0), len(data))
	for start > 0 && !utf8.RuneStart(data[start]) {
		start--
	}
	end := min(start+uploadTextMost, len(data))
	for end < len(data) && end > start && !utf8.RuneStart(data[end]) {
		end--
	}
	out["text"] = string(data[start:end])
	if start > 0 {
		out["from"] = start
	}
	if end < len(data) {
		out["more_from"] = end
		out["said"] = fmt.Sprintf("The upload is longer than one read: this is bytes %d to %d of %d. Call ws_file_read again with from %d for the rest.", start, end, len(data), end)
	}
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
	// What a task's run makes is made by that task, unless the agent named
	// another.
	fromTask := firstNonEmpty(strings.TrimSpace(a.FromTask), d.runTask)
	if a.Kind == factory.FileFolder {
		fromTask = strings.TrimSpace(a.FromTask)
	}
	body := api.FileNew{Kind: firstNonEmpty(a.Kind, factory.FileDocument), Name: name, Folder: a.Folder, FromTask: fromTask,
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
	if f.Kind != factory.FileFolder {
		d.wrote.note(f.ID)
	}
	return map[string]any{"said": fmt.Sprintf("Made the %s %q; everyone in the workstream sees it.", f.Kind, f.Name), "file": fileBrief(f)}, nil
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
	d.wrote.note(f.ID)
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
	edits := make([]suggestedEdit, 0, len(a.Edits))
	for i, e := range a.Edits {
		switch n := strings.Count(text, e.Find); {
		case e.Find == "" || n == 0:
			return nil, fmt.Errorf("%q is not in the document as it reads now; quote it exactly", cutRunes(e.Find, 80))
		case n > 1:
			return nil, fmt.Errorf("%q is in the document %d times; quote more of it so it is found once", cutRunes(e.Find, 80), n)
		}
		if e.Find == e.Replace {
			return nil, fmt.Errorf("edit %d changes nothing: its replace is the same as its find", i+1)
		}
		text = strings.Replace(text, e.Find, e.Replace, 1)
		edits = append(edits, suggestedEdit{ID: "e" + strconv.Itoa(i+1), Find: e.Find, Replace: e.Replace,
			Why: firstNonEmpty(strings.TrimSpace(e.Why), why), State: "proposed"})
	}
	// A SUGGESTION IS ONE THE PEOPLE IN THE WORKSTREAM CAN SEE AND DECIDE ON
	// (live2 E3 F1). It is kept in the workstream's suggestions, the list every
	// client draws on the document with Keep and Drop beside each edit; it was
	// once kept as a page version nobody was shown, while the agent said it
	// was waiting. Each edit keeps the exact words it changes, and the client
	// finds them again on the document as it reads when someone opens it.
	body := suggestionPart{PageID: page.ID, BaseVersion: f.Rev, Why: why, Agent: true,
		CreatedAt: d.h.clock().UTC().Format(time.RFC3339Nano), Edits: edits}
	if _, err := d.call(ctx, d.h.addPart(suggestionsPart), http.MethodPost, "/v1/workstreams/"+d.id+"/suggestions", nil, body, -1); err != nil {
		return nil, err
	}
	return map[string]any{"said": fmt.Sprintf("Suggested %d edit(s) on %s. They show on the document for everyone in the workstream, and nothing changes until someone keeps them.", len(edits), strconv.Quote(page.Title))}, nil
}

// suggestionPart is an agent's suggestion as the workstream's suggestions
// keep it: the desk's own object, which the server stamps with who and when.
type suggestionPart struct {
	PageID      string          `json:"page_id"`
	BaseVersion int             `json:"base_version"`
	Why         string          `json:"why"`
	Agent       bool            `json:"agent"`
	CreatedAt   string          `json:"created_at"`
	Edits       []suggestedEdit `json:"edits"`
}

// suggestedEdit is one exact quote and the words that would replace it.
type suggestedEdit struct {
	ID      string `json:"id"`
	Find    string `json:"find"`
	Replace string `json:"replace"`
	Why     string `json:"why"`
	State   string `json:"state"`
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
	Words       bool   `json:"words"`
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
	asked := ""
	for _, x := range t.Asks {
		if x.Against != nil && x.Against.Decision == strings.TrimSpace(a.Decision) && x.Decider != nil {
			asked = firstNonEmpty(x.Decider.Name, x.Decider.ID)
		}
	}
	return map[string]any{"task": taskBrief(t), "asked": asked}, nil
}

// ReadPages records the pages the workstream tool actually showed the model.
// Reading the workstream elsewhere must not mark its whole page list as inputs.
func (d *agentDoor) ReadPages(ctx context.Context, ids []string) {
	if d.runTask == "" {
		return
	}
	ws, err := d.Workstream(ctx)
	if err != nil {
		return
	}
	files, err := d.h.foldFiles(ws)
	if err != nil {
		return
	}
	for _, f := range files {
		if f.DeletedAt == nil && f.PageID != "" && slices.Contains(ids, f.PageID) {
			d.usedByTask(ctx, f, ws)
		}
	}
}

// nonEmptyStrings is the strings of s that are not empty.
func nonEmptyStrings(s ...string) []string {
	var out []string
	for _, x := range s {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

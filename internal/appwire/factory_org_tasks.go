package appwire

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// ── AN ORGANIZATION WORKSTREAM'S TASKS (Workstreams v5, SPEC §2.2) ────────
//
// A task is one piece of work with one owner. The desk sends a Personal
// workstream's tasks through its own Tauri commands (workstream_task_*) in
// exactly these shapes, so its screens never ask where a task lives.
//
//	factory.org_workstream_tasks           OrgTasksParams       -> OrgTasks      GET   /v1/workstreams/{id}/tasks
//	factory.org_workstream_task            OrgTaskParams        -> OrgTask       GET   /v1/workstreams/{id}/tasks/{tid}
//	factory.org_workstream_task_create     OrgTaskCreateParams  -> OrgTask       POST  /v1/workstreams/{id}/tasks
//	factory.org_workstream_task_update     OrgTaskUpdateParams  -> OrgTask       PATCH /v1/workstreams/{id}/tasks/{tid}  If-Match: rev
//	factory.org_workstream_task_answer     OrgTaskAnswerParams  -> OrgTask       POST  /v1/workstreams/{id}/tasks/{tid}/answer
//	factory.org_workstream_task_comment    OrgTaskCommentParams -> OrgTaskEvent  POST  /v1/workstreams/{id}/tasks/{tid}/comment
//	factory.org_workstream_task_events     OrgTaskParams        -> OrgTaskEvents GET   /v1/workstreams/{id}/tasks/{tid}/events[?seen=1]
//	factory.org_workstream_tasks_start_all OrgTasksStartAllParams -> OrgTasks    POST  /v1/workstreams/{id}/tasks/start_all
//	factory.org_workstream_task_view       OrgTaskViewParams    -> OrgTask       POST  /v1/workstreams/{id}/tasks/{tid}/view
//	factory.org_workstream_decision_withdraw OrgDecisionParams  -> workstream    POST  /v1/workstreams/{id}/decisions/{did}/withdraw
//	factory.org_workstream_decision_replace  OrgDecisionParams  -> workstream    POST  /v1/workstreams/{id}/decisions/{did}/replace
//
// With the hello word org_workstream_decisions, a task carries several Asks
// (ask, the owner's, and asks, each with its one decider), views on them,
// and Assumed; answering names the Ask (`ask`); and the workstream's
// decisions record themselves (an answered Ask, a request_note's `decision`,
// a rules change). Only the person who made a decision withdraws or replaces
// it.
//
// Every param names the workstream as `ws` and the task as `id`, the same
// words the desk's Tauri commands take. A Factory job of the workstream is a
// task with id "job:<n>" and done_by.kind "factory", derived from the job.
// Only the owner answers an Ask (with owner Everyone, anyone who edits; after
// a day, also the workstream's fallback) and ticks a person task (state
// "done"). A change is relayed as factory.org_workstream_changed with part
// "tasks".

// FeatureOrgWorkstreamTasks is the hello word for the methods above.
const FeatureOrgWorkstreamTasks = "org_workstream_tasks"

// FeatureOrgWorkstreamTaskStart says a person's Start (state "running") and
// Start all on an organization's Afi task run Afi on the server inside the
// same write, refusing with nothing kept when Afi cannot take it. A desk
// offers Start on an organization only where this word is heard, because an
// engine without it would mark the task running with nothing behind it.
const FeatureOrgWorkstreamTaskStart = "org_workstream_task_start"

// TASKS START BY THEMSELVES WHEN THEIR WAITS CLEAR (owner decisions
// 2026-11-17, 1). Where the engine says org_task_starts, the server starts an
// Afi task in the same write that finished the last thing it waited on, when
// its rule says so (factory.StartsBySelf); a task carries `starts`
// ("when_ready", "by_hand" or absent for the default), which its owner or
// whoever looks after the workstream sets with the ordinary task change, and
// reads back `starts_when_ready`, what that resolves to; and the signed-in
// person's own default is read and written here:
//
//	factory.org_work      OrgConn          -> OrgWork  GET /v1/me/work
//	factory.org_work_set  OrgWorkSetParams -> OrgWork  PUT /v1/me/work
const FeatureOrgTaskStarts = "org_task_starts"

// OrgWorkSetParams is the person's "Start tasks when what they wait on is
// done".
type OrgWorkSetParams struct {
	OrgConn
	StartWhenReady bool `json:"start_when_ready"`
}

// OrgWork is the server's answer, passed through: {start_when_ready}.
type OrgWork = json.RawMessage

// FeatureOrgWorkstreamTaskDoers says who does an organization's task can be
// changed after it is made (factory.org_workstream_task_update with
// done_by: the owner's or whoever looks after the workstream's, never while
// it runs), that a task may be done by a named agent or crew
// ({"kind":"agent","agent_id","name","crew"}) from the workstream's agent
// list, and that Start and Start all wake every agent doer, a chat's and a
// named agent's as well as Afi's, each in the task's own run on the server.
// A desk offers "Change who does it" on an organization only where it hears
// this word: an older server ignores done_by and changes nothing.
const FeatureOrgWorkstreamTaskDoers = "org_workstream_task_doers"

// FeatureOrgWorkstreamTaskDelete announces person-only Delete and short Undo for tasks.
const FeatureOrgWorkstreamTaskDelete = "org_workstream_task_delete"

// A REQUEST INTO ANOTHER WORKSTREAM (Workstreams final spec, flow 7): anyone
// in the organization asks a workstream that is not private, in it or not,
// and the request becomes a task there owned by its owner of incoming work.
//
//	factory.org_workstreams_requestable OrgConn               -> OrgRequestable GET  /v1/workstreams/requestable
//	factory.org_workstream_request      OrgTaskRequestParams  -> OrgRequestSent POST /v1/workstreams/{id}/requests
//
// FeatureOrgWorkstreamRequests is their hello word.
const FeatureOrgWorkstreamRequests = "org_workstream_requests"

// OrgTaskRequestParams is a request into ws: a title, the words, and the task
// of the caller's own it came from (from_ws, from_task), which gains one line.
type OrgTaskRequestParams struct {
	OrgConn
	WS       string `json:"ws"`
	Title    string `json:"title"`
	Request  string `json:"request,omitempty"`
	FromWS   string `json:"from_ws,omitempty"`
	FromTask string `json:"from_task,omitempty"`
	// WordsBy is "afi" when the desk's Afi wrote the title and request
	// rather than the person: it only ever lowers trust in them (the server's
	// factory.WordsAuthor), so the server agent's web_fetch never follows an
	// address in them. Leaving it out, or "person", changes nothing.
	WordsBy string `json:"words_by,omitempty"`
}

// OrgRequestable and OrgRequestSent are the server's shapes, passed through.
type (
	OrgRequestable = json.RawMessage
	OrgRequestSent = json.RawMessage
)

// FeatureOrgWorkstreamDecisions is the hello word for several Asks with
// deciders, views, Assumed, and the decisions that record themselves.
const FeatureOrgWorkstreamDecisions = "org_workstream_decisions"

// OrgTask and OrgTaskEvent are the server's own shapes, passed through whole:
// the desk's wsTasksModel.ts types them.
type (
	OrgTask      = json.RawMessage
	OrgTaskEvent = json.RawMessage
)

// OrgTasks is a list of tasks.
type OrgTasks struct {
	Tasks []json.RawMessage `json:"tasks"`
}

// OrgTaskEvents is a task's thread, oldest first.
type OrgTaskEvents struct {
	Events []json.RawMessage `json:"events"`
}

// OrgTasksParams names a workstream.
type OrgTasksParams struct {
	OrgConn
	WS string `json:"ws"`
}

// OrgTaskParams names one task of a workstream.
type OrgTaskParams struct {
	OrgConn
	WS string `json:"ws"`
	ID string `json:"id"`
	// Seen, on a thread read, marks it read for the caller: a person opened
	// the task, so its comments leave their Inbox's "Comments for you".
	Seen bool `json:"seen,omitempty"`
}

// OrgTaskCreateParams is a new task: SPEC §2.2's TaskCreate beside ws.
type OrgTaskCreateParams struct {
	OrgConn
	WS        string          `json:"ws"`
	Title     string          `json:"title"`
	Request   string          `json:"request,omitempty"`
	Owner     json.RawMessage `json:"owner,omitempty"`
	DoneBy    json.RawMessage `json:"done_by"`
	CameFrom  json.RawMessage `json:"came_from,omitempty"`
	WaitingOn []string        `json:"waiting_on,omitempty"`
	Due       string          `json:"due,omitempty"`
	Group     json.RawMessage `json:"group,omitempty"`
	State     string          `json:"state,omitempty"`
	// WordsBy is OrgTaskRequestParams' WordsBy: "afi" when the desk's Afi
	// wrote the title and request; it only ever lowers trust.
	WordsBy string `json:"words_by,omitempty"`
}

// OrgTaskUpdateParams is a change to one task at the rev it was read at.
// Every other field is the change itself (TaskPatch: title, request, owner,
// waiting_on, due, group, state, state_reason, results, check, ask, move_to,
// and words_by, "afi" when the desk's Afi wrote a new title or request, which
// only ever lowers trust in them), passed on as sent, so a null that clears
// reaches the server as a null.
type OrgTaskUpdateParams struct {
	OrgConn
	WS    string                     `json:"ws"`
	ID    string                     `json:"id"`
	Rev   int                        `json:"rev"`
	Patch map[string]json.RawMessage `json:"-"`
}

// UnmarshalJSON keeps every field that is not a param as the change.
func (p *OrgTaskUpdateParams) UnmarshalJSON(data []byte) error {
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	type plain OrgTaskUpdateParams
	var head plain
	if err := json.Unmarshal(data, &head); err != nil {
		return err
	}
	*p = OrgTaskUpdateParams(head)
	p.Patch = map[string]json.RawMessage{}
	for k, v := range all {
		switch k {
		case "connection", "factory", "ws", "id", "rev":
		default:
			p.Patch[k] = v
		}
	}
	return nil
}

// OrgTaskAnswerParams answers a task's Ask: an option's id, or words.
type OrgTaskAnswerParams struct {
	OrgConn
	WS string `json:"ws"`
	ID string `json:"id"`
	// Ask names which of the task's Asks; absent, the task's own.
	Ask    string `json:"ask,omitempty"`
	Option string `json:"option,omitempty"`
	Text   string `json:"text,omitempty"`
}

// OrgTaskCommentParams adds a line to a task's thread (kind comment, the
// default; posted; request_note; worked; offer with offer_for), or with Offer
// takes, dismisses or undoes Afi's offer.
type OrgTaskCommentParams struct {
	OrgConn
	WS        string          `json:"ws"`
	ID        string          `json:"id"`
	Kind      string          `json:"kind,omitempty"`
	Text      string          `json:"text,omitempty"`
	OfferFor  string          `json:"offer_for,omitempty"`
	Steps     int             `json:"steps,omitempty"`
	DurationS int             `json:"duration_s,omitempty"`
	Offer     json.RawMessage `json:"offer,omitempty"`
	// Decision is, on a request_note, what the request settles in one line.
	Decision string `json:"decision,omitempty"`
}

// OrgTaskViewParams puts the caller's view on one of a task's Asks; Text ""
// takes it away.
type OrgTaskViewParams struct {
	OrgConn
	WS   string `json:"ws"`
	ID   string `json:"id"`
	Ask  string `json:"ask"`
	Text string `json:"text"`
}

// OrgDecisionParams names one decision of a workstream (id), and for a
// replace the words that replace it.
type OrgDecisionParams struct {
	OrgConn
	ID       string `json:"id"`
	Decision string `json:"decision"`
	Text     string `json:"text,omitempty"`
}

// OrgTasksStartAllParams starts a group's up-next tasks.
type OrgTasksStartAllParams struct {
	OrgConn
	IDs   []string `json:"ids,omitempty"`
	WS    string   `json:"ws"`
	Group string   `json:"group"`
}

// orgTaskMost is the longest thing one task call carries, as the server takes it.
const orgTaskMost = 16 << 10

func orgTasksPath(ws string) (string, error) {
	path, err := orgWorkstreamPath(ws)
	if err != nil {
		return "", err
	}
	return path + "/tasks", nil
}

func orgTaskPath(ws, id string) (string, error) {
	path, err := orgTasksPath(ws)
	if err != nil {
		return "", err
	}
	tid, err := orgSegment("id", id)
	if err != nil {
		return "", err
	}
	return path + "/" + url.PathEscape(tid), nil
}

func withFactoryOrgTasksFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryOrg); ok {
		hello = withFeature(hello, FeatureOrgWorkstreamTasks)
		hello = withFeature(hello, FeatureOrgWorkstreamTaskStart)
		hello = withFeature(hello, FeatureOrgTaskStarts)
		hello = withFeature(hello, FeatureOrgWorkstreamTaskDoers)
		hello = withFeature(hello, FeatureOrgWorkstreamTaskDelete)
		hello = withFeature(hello, FeatureOrgWorkstreamRequests)
		hello = withFeature(hello, FeatureOrgWorkstreamDecisions)
	}
	return withFactoryOrgTaskWorkFeature(hello, e)
}

func (t *OrgTasks) fill()      { t.Tasks = nonNil(t.Tasks) }
func (e *OrgTaskEvents) fill() { e.Events = nonNil(e.Events) }

func init() {
	methods["factory.org_work"] = orgMethod[OrgConn, OrgWork]("factory.org_work", func(OrgConn) (OrgRequest, error) {
		return OrgRequest{Method: http.MethodGet, Path: "/v1/me/work"}, nil
	})
	methods["factory.org_work_set"] = orgMethod[OrgWorkSetParams, OrgWork]("factory.org_work_set", func(p OrgWorkSetParams) (OrgRequest, error) {
		return OrgRequest{Method: http.MethodPut, Path: "/v1/me/work", Body: map[string]bool{"start_when_ready": p.StartWhenReady}}, nil
	})
	methods["factory.org_workstreams_requestable"] = orgMethod[OrgConn, OrgRequestable]("factory.org_workstreams_requestable", func(OrgConn) (OrgRequest, error) {
		return OrgRequest{Method: http.MethodGet, Path: "/v1/workstreams/requestable"}, nil
	})
	methods["factory.org_workstream_request"] = orgMethod[OrgTaskRequestParams, OrgRequestSent]("factory.org_workstream_request", func(p OrgTaskRequestParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.WS)
		if err != nil {
			return OrgRequest{}, err
		}
		title := strings.TrimSpace(p.Title)
		if title == "" || !plainText(title, 300) {
			return OrgRequest{}, Errorf(CodeBadParams, "title is one line of at most 300 characters")
		}
		if len(p.Request) > orgTaskMost {
			return OrgRequest{}, Errorf(CodeBadParams, "request is at most 16384 bytes")
		}
		body := map[string]string{"title": title, "request": p.Request, "from_ws": p.FromWS, "from_task": p.FromTask}
		if w := strings.TrimSpace(p.WordsBy); w != "" {
			body["words_by"] = w
		}
		return OrgRequest{Method: http.MethodPost, Path: path + "/requests", Body: body}, nil
	})
	tasks := func(t *OrgTasks) { t.fill() }
	methods["factory.org_workstream_tasks"] = filled(orgMethod[OrgTasksParams, OrgTasks]("factory.org_workstream_tasks", func(p OrgTasksParams) (OrgRequest, error) {
		path, err := orgTasksPath(p.WS)
		return OrgRequest{Method: http.MethodGet, Path: path}, err
	}), tasks)
	methods["factory.org_workstream_task"] = orgMethod[OrgTaskParams, OrgTask]("factory.org_workstream_task", func(p OrgTaskParams) (OrgRequest, error) {
		path, err := orgTaskPath(p.WS, p.ID)
		return OrgRequest{Method: http.MethodGet, Path: path}, err
	})
	methods["factory.org_workstream_task_create"] = orgMethod[OrgTaskCreateParams, OrgTask]("factory.org_workstream_task_create", func(p OrgTaskCreateParams) (OrgRequest, error) {
		path, err := orgTasksPath(p.WS)
		if err != nil {
			return OrgRequest{}, err
		}
		title := strings.TrimSpace(p.Title)
		if title == "" || !plainText(title, 300) {
			return OrgRequest{}, Errorf(CodeBadParams, "title is one line of at most 300 characters")
		}
		if len(p.DoneBy) == 0 {
			return OrgRequest{}, Errorf(CodeBadParams, "done_by says who does the work")
		}
		body := p
		body.OrgConn, body.WS, body.Title = OrgConn{}, "", title
		return OrgRequest{Method: http.MethodPost, Path: path, Body: struct {
			Title     string          `json:"title"`
			Request   string          `json:"request,omitempty"`
			Owner     json.RawMessage `json:"owner,omitempty"`
			DoneBy    json.RawMessage `json:"done_by"`
			CameFrom  json.RawMessage `json:"came_from,omitempty"`
			WaitingOn []string        `json:"waiting_on,omitempty"`
			Due       string          `json:"due,omitempty"`
			Group     json.RawMessage `json:"group,omitempty"`
			State     string          `json:"state,omitempty"`
			WordsBy   string          `json:"words_by,omitempty"`
		}{body.Title, body.Request, body.Owner, body.DoneBy, body.CameFrom, body.WaitingOn, body.Due, body.Group, body.State, strings.TrimSpace(body.WordsBy)}}, nil
	})
	methods["factory.org_workstream_task_delete"] = orgMethod[OrgTaskUpdateParams, json.RawMessage]("factory.org_workstream_task_delete", func(p OrgTaskUpdateParams) (OrgRequest, error) {
		path, err := orgTaskPath(p.WS, p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		rev, err := orgRev(p.Rev, true)
		return OrgRequest{Method: http.MethodDelete, Path: path, IfMatch: rev}, err
	})
	methods["factory.org_workstream_task_restore"] = orgMethod[OrgTaskParams, OrgTask]("factory.org_workstream_task_restore", func(p OrgTaskParams) (OrgRequest, error) {
		path, err := orgTaskPath(p.WS, p.ID)
		return OrgRequest{Method: http.MethodPost, Path: path + "/restore"}, err
	})

	methods["factory.org_workstream_task_update"] = orgMethod[OrgTaskUpdateParams, OrgTask]("factory.org_workstream_task_update", func(p OrgTaskUpdateParams) (OrgRequest, error) {
		path, err := orgTaskPath(p.WS, p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		// A Factory job's task is first written at rev 0: its overlay is new.
		rev, err := orgRev(p.Rev, true)
		if err != nil {
			return OrgRequest{}, err
		}
		if len(p.Patch) == 0 {
			return OrgRequest{}, Errorf(CodeBadParams, "say what changes")
		}
		return OrgRequest{Method: http.MethodPatch, Path: path, Body: p.Patch, IfMatch: rev}, nil
	})
	methods["factory.org_workstream_task_answer"] = orgMethod[OrgTaskAnswerParams, OrgTask]("factory.org_workstream_task_answer", func(p OrgTaskAnswerParams) (OrgRequest, error) {
		path, err := orgTaskPath(p.WS, p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		if strings.TrimSpace(p.Option) == "" && strings.TrimSpace(p.Text) == "" {
			return OrgRequest{}, Errorf(CodeBadParams, "an answer is an option or words")
		}
		if len(p.Text) > orgTaskMost {
			return OrgRequest{}, Errorf(CodeBadParams, "text is at most 16384 bytes")
		}
		body := map[string]string{"option": p.Option, "text": p.Text}
		if p.Ask != "" {
			body["ask"] = p.Ask
		}
		return OrgRequest{Method: http.MethodPost, Path: path + "/answer", Body: body}, nil
	})
	methods["factory.org_workstream_task_comment"] = orgMethod[OrgTaskCommentParams, OrgTaskEvent]("factory.org_workstream_task_comment", func(p OrgTaskCommentParams) (OrgRequest, error) {
		path, err := orgTaskPath(p.WS, p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		if len(p.Offer) == 0 && strings.TrimSpace(p.Text) == "" {
			return OrgRequest{}, Errorf(CodeBadParams, "a comment needs words")
		}
		if len(p.Text) > orgTaskMost {
			return OrgRequest{}, Errorf(CodeBadParams, "text is at most 16384 bytes")
		}
		body := struct {
			Kind      string          `json:"kind,omitempty"`
			Text      string          `json:"text,omitempty"`
			OfferFor  string          `json:"offer_for,omitempty"`
			Steps     int             `json:"steps,omitempty"`
			DurationS int             `json:"duration_s,omitempty"`
			Offer     json.RawMessage `json:"offer,omitempty"`
			Decision  string          `json:"decision,omitempty"`
		}{p.Kind, p.Text, p.OfferFor, p.Steps, p.DurationS, p.Offer, strings.TrimSpace(p.Decision)}
		return OrgRequest{Method: http.MethodPost, Path: path + "/comment", Body: body}, nil
	})
	methods["factory.org_workstream_task_events"] = filled(orgMethod[OrgTaskParams, OrgTaskEvents]("factory.org_workstream_task_events", func(p OrgTaskParams) (OrgRequest, error) {
		path, err := orgTaskPath(p.WS, p.ID)
		if p.Seen {
			return OrgRequest{Method: http.MethodGet, Path: path + "/events?seen=1"}, err
		}
		return OrgRequest{Method: http.MethodGet, Path: path + "/events"}, err
	}), func(e *OrgTaskEvents) { e.fill() })
	methods["factory.org_workstream_tasks_start_all"] = filled(orgMethod[OrgTasksStartAllParams, OrgTasks]("factory.org_workstream_tasks_start_all", func(p OrgTasksStartAllParams) (OrgRequest, error) {
		path, err := orgTasksPath(p.WS)
		if err != nil {
			return OrgRequest{}, err
		}
		group, err := orgSegment("group", p.Group)
		if err != nil {
			return OrgRequest{}, err
		}
		body := map[string]any{"group": group}
		if p.IDs != nil {
			body["ids"] = p.IDs
		}
		return OrgRequest{Method: http.MethodPost, Path: path + "/start_all", Body: body}, nil
	}), tasks)
	methods["factory.org_workstream_task_view"] = orgMethod[OrgTaskViewParams, OrgTask]("factory.org_workstream_task_view", func(p OrgTaskViewParams) (OrgRequest, error) {
		path, err := orgTaskPath(p.WS, p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		if strings.TrimSpace(p.Ask) == "" {
			return OrgRequest{}, Errorf(CodeBadParams, "ask names the Ask the view is on")
		}
		if len(p.Text) > orgTaskMost {
			return OrgRequest{}, Errorf(CodeBadParams, "text is at most 16384 bytes")
		}
		return OrgRequest{Method: http.MethodPost, Path: path + "/view", Body: map[string]string{"ask": p.Ask, "text": p.Text}}, nil
	})
	decision := func(verb string) func(p OrgDecisionParams) (OrgRequest, error) {
		return func(p OrgDecisionParams) (OrgRequest, error) {
			path, err := orgWorkstreamPath(p.ID)
			if err != nil {
				return OrgRequest{}, err
			}
			did, err := orgSegment("decision", p.Decision)
			if err != nil {
				return OrgRequest{}, err
			}
			var body any = struct{}{}
			if verb == "replace" {
				text := strings.TrimSpace(p.Text)
				if text == "" || len(text) > orgTaskMost {
					return OrgRequest{}, Errorf(CodeBadParams, "a decision is replaced with its new words")
				}
				body = map[string]string{"text": text}
			}
			return OrgRequest{Method: http.MethodPost, Path: path + "/decisions/" + url.PathEscape(did) + "/" + verb, Body: body}, nil
		}
	}
	methods["factory.org_workstream_decision_withdraw"] = orgMethod[OrgDecisionParams, json.RawMessage]("factory.org_workstream_decision_withdraw", decision("withdraw"))
	methods["factory.org_workstream_decision_replace"] = orgMethod[OrgDecisionParams, json.RawMessage]("factory.org_workstream_decision_replace", decision("replace"))
}

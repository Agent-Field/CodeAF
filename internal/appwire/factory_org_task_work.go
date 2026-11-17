package appwire

import (
	"net/http"
	"reflect"
	"strconv"
	"strings"
)

// ── A TASK'S WORKING CONVERSATION ON THE ORGANIZATION'S SERVER ─────────────
//
// A task Afi works on the organization's server runs in a seat of its own,
// and what it says and does there is the task's Working conversation, which
// EVERYONE WHO CAN SEE THE WORKSTREAM READS (OWNER-DECISIONS 2026-11-17 §8).
// The task's owner and whoever started the run steer it there while it works;
// a steer is a line everyone reads too. Afi's pane stays private to each person.
//
//	factory.org_workstream_task_work        OrgTaskWorkParams   -> OrgTaskWork  GET  /v1/workstreams/{id}/tasks/{tid}/work?since=
//	factory.org_workstream_task_steer       OrgTaskSteerParams  -> OrgTaskWork  POST /v1/workstreams/{id}/tasks/{tid}/work/messages
//
// The server's `task.work` events, already filtered to those who can see the
// workstream, are relayed as factory.org_workstream_task_work {connection, id,
// task, entry?, state?}. A steer with no run going is conflict with detail
// {"reason":"idle"}; one from someone who neither owns the task nor started
// the run is forbidden.

// FeatureOrgTaskWork is the hello word for the methods above.
const FeatureOrgTaskWork = "org_task_work"

// EventFactoryOrgTaskWork is one line of a task's run, or whether it runs.
const EventFactoryOrgTaskWork = "factory.org_workstream_task_work"

// OrgTaskWork is a task's Working conversation as the caller reads it: State
// is "working" while a run goes (For is who started it) and "idle" otherwise;
// CanSteer is whether the caller may steer it now, and Steerer whether they
// could while a run goes (they own the task or started the run).
type OrgTaskWork struct {
	ID   string `json:"id"`
	Task string `json:"task"`
	OrgAgentState
	Entries  []OrgAgentEntry `json:"entries"`
	Seq      int             `json:"seq"`
	CanSteer bool            `json:"can_steer"`
	Steerer  bool            `json:"steerer,omitempty"`
	// CanRevise is whether the caller's words start a revision run now: a
	// done Afi task, nothing working, and they may steer it.
	CanRevise bool `json:"can_revise,omitempty"`
	// USD is what every run of the task has spent; absent while nothing has.
	USD float64 `json:"usd,omitempty"`
}

func (w *OrgTaskWork) fill() {
	if w.Entries == nil {
		w.Entries = []OrgAgentEntry{}
	}
	if w.State == "" {
		w.State = "idle"
	}
}

// OrgTaskWorkParams reads one task's Working conversation after Since.
type OrgTaskWorkParams struct {
	OrgConn
	WS    string `json:"ws"`
	ID    string `json:"id"`
	Since int    `json:"since,omitempty"`
}

// OrgTaskSteerParams is one steer into a task's run.
type OrgTaskSteerParams struct {
	OrgConn
	WS   string `json:"ws"`
	ID   string `json:"id"`
	Text string `json:"text"`
}

// FactoryOrgTaskWork is one task.work event on the connection it came from.
type FactoryOrgTaskWork struct {
	Connection string         `json:"connection"`
	ID         string         `json:"id"`
	Task       string         `json:"task"`
	Entry      *OrgAgentEntry `json:"entry,omitempty"`
	State      *OrgAgentState `json:"state,omitempty"`
}

func withFactoryOrgTaskWorkFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryOrg); ok {
		hello = withFeature(hello, FeatureOrgTaskWork)
	}
	return hello
}

func init() {
	eventShapes[EventFactoryOrgTaskWork] = reflect.TypeFor[FactoryOrgTaskWork]()
	work := func(w *OrgTaskWork) { w.fill() }
	methods["factory.org_workstream_task_work"] = filled(orgMethod[OrgTaskWorkParams, OrgTaskWork]("factory.org_workstream_task_work", func(p OrgTaskWorkParams) (OrgRequest, error) {
		path, err := orgTaskPath(p.WS, p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		if p.Since < 0 {
			return OrgRequest{}, Errorf(CodeBadParams, "since is the last seq you have")
		}
		path += "/work"
		if p.Since > 0 {
			path += "?since=" + strconv.Itoa(p.Since)
		}
		return OrgRequest{Method: http.MethodGet, Path: path}, nil
	}), work)
	methods["factory.org_workstream_task_steer"] = filled(orgMethod[OrgTaskSteerParams, OrgTaskWork]("factory.org_workstream_task_steer", func(p OrgTaskSteerParams) (OrgRequest, error) {
		path, err := orgTaskPath(p.WS, p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		text := strings.TrimSpace(p.Text)
		if text == "" || len(text) > orgAgentMessageMost {
			return OrgRequest{}, Errorf(CodeBadParams, "text is 1 to 16384 bytes of words")
		}
		return OrgRequest{Method: http.MethodPost, Path: path + "/work/messages", Body: map[string]string{"text": text}}, nil
	}), work)
}

package api

import "github.com/Agent-Field/codeaf/internal/factory"

// ForViewer adds facts that depend on the authenticated caller, never on UI state.
func ForViewer(job *Job, it factory.Item, viewer string) {
	job.Dismissed = factory.IsDismissed(it, viewer)
	if job.State == StateNeedsYou && it.Person() != "" && factory.NormalizeHandle(viewer) != it.Person() {
		job.WaitingFor = "@" + it.Person()
	}
}

// NewJob keeps hand-off metadata in the binary; the app sends no store shape.
type NewJob struct {
	Repo      string             `json:"repo"`
	Title     string             `json:"title"`
	Text      string             `json:"text"`
	Source    string             `json:"source,omitempty"`
	FromRef   *factory.SourceRef `json:"from_ref,omitempty"`
	Assignee  *string            `json:"assignee,omitempty"`
	Start     string             `json:"start,omitempty"`
	Pipeline  string             `json:"pipeline,omitempty"`
	Issue     bool               `json:"issue,omitempty"`
	Autostart bool               `json:"autostart,omitempty"`
}

// JobAction carries the read a person acted on so mutations can reject stale pages.
type JobAction struct {
	Expect *Expect `json:"expect,omitempty"`
}

type AssigneeRequest struct {
	Assignee string  `json:"assignee"`
	Expect   *Expect `json:"expect,omitempty"`
}

// StepEditsRequest uses the same bounded edit language as the job's manager.
type StepEditsRequest struct {
	Add      []StepAddition    `json:"add,omitempty"`
	Ask      map[string]string `json:"ask,omitempty"`
	Thinking map[string]string `json:"thinking,omitempty"`
	On       []string          `json:"on,omitempty"`
	Skip     []string          `json:"skip,omitempty"`
	Why      string            `json:"why,omitempty"`
	Expect   *Expect           `json:"expect,omitempty"`
}

type StepAddition struct {
	Step  StepDef `json:"step"`
	After string  `json:"after,omitempty"`
}

type StepEdits struct {
	Factory Factory  `json:"factory"`
	Steps   []Step   `json:"steps"`
	Version int      `json:"version,omitempty"`
	Lines   []string `json:"lines"`
}

type PipelinePRRequest struct {
	Text  string `json:"text"`
	Title string `json:"title,omitempty"`
}

type PipelinePRResponse struct {
	Factory Factory `json:"factory"`
	Note    string  `json:"note"`
}

type Issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Body   string `json:"body,omitempty"`
}

type Issues struct {
	Factory Factory `json:"factory"`
	Issues  []Issue `json:"issues"`
}

type JobRead struct {
	Factory Factory `json:"factory"`
	Job
}

type JobCreated struct {
	Factory Factory `json:"factory"`
	ID      int     `json:"id"`
}

type ActionResult struct {
	Factory Factory `json:"factory"`
}

// JobHandOff is the saved source of a job, never an app-owned store document.
type JobHandOff struct {
	Factory Factory            `json:"factory"`
	ID      string             `json:"id"`
	Title   string             `json:"title"`
	Brief   string             `json:"brief"`
	Repo    string             `json:"repo"`
	FromRef *factory.SourceRef `json:"from_ref,omitempty"`
	NotNow  bool               `json:"not_now"`
}

// StatePresentation contains the copy the binary derives from the run's facts.
type StatePresentation struct {
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

// JobControls contains only actions this caller can use on this host.
type JobControls struct {
	StopStep       bool `json:"stop_step"`
	RerunStep      bool `json:"rerun_step"`
	EditSteps      bool `json:"edit_steps"`
	ChangeAssignee bool `json:"change_assignee"`
	Sync           bool `json:"sync"`
	Timeline       bool `json:"timeline"`
}

// ServiceRoute publishes the person routes implemented by the factory service.
type ServiceRoute struct {
	Method   string
	Path     string
	Request  any
	Response any
}

var ServiceRoutes = []ServiceRoute{
	{"GET", "/v1/jobs/{id}", nil, JobRead{}},
	{"GET", "/v1/queue", nil, Queue{}},
	{"POST", "/v1/jobs/{id}/steps/{sid}/stop", JobAction{}, ActionResult{}},
	{"POST", "/v1/jobs/{id}/steps/{sid}/rerun", JobAction{}, ActionResult{}},
	{"POST", "/v1/jobs/{id}/assignee", AssigneeRequest{}, ActionResult{}},
	{"POST", "/v1/jobs/{id}/sync", JobAction{}, ActionResult{}},
	{"GET", "/v1/jobs/{id}/step-edits", nil, StepEdits{}},
	{"POST", "/v1/jobs/{id}/step-edits", StepEditsRequest{}, StepEdits{}},
	{"GET", "/v1/jobs/{id}/hand-off/{card}", nil, JobHandOff{}},
	{"POST", "/v1/jobs/{id}/hand-off/{card}/not-now", JobAction{}, ActionResult{}},
	{"POST", "/v1/repos/{owner}/{repo}/pipeline/open-pr", PipelinePRRequest{}, PipelinePRResponse{}},
	{"POST", "/v1/issues", NewJob{}, JobCreated{}},
	{"GET", "/v1/issues", nil, Issues{}},
	{"POST", "/v1/jobs", NewJob{}, JobCreated{}},
	{"DELETE", "/v1/auth/device", DevicePoll{}, ActionResult{}},
}

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
// Remove takes steps out of this job by name; Move puts one after another, or
// first. Neither touches the repository's pipeline.
type StepEditsRequest struct {
	Add      []StepAddition    `json:"add,omitempty"`
	Ask      map[string]string `json:"ask,omitempty"`
	Thinking map[string]string `json:"thinking,omitempty"`
	On       []string          `json:"on,omitempty"`
	Skip     []string          `json:"skip,omitempty"`
	Remove   []string          `json:"remove,omitempty"`
	Move     []StepMove        `json:"move,omitempty"`
	Why      string            `json:"why,omitempty"`
	Expect   *Expect           `json:"expect,omitempty"`
}

// StepMove is one step moved in one job: Step is its name, and After the step
// it now goes after. After is required, and "" puts it first, so a move that
// forgot its place is refused rather than read as first.
type StepMove struct {
	Step  string  `json:"step"`
	After *string `json:"after"`
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

// PipelinePRRequest asks the factory to open a pull request that changes only
// .codeaf/factory.yaml, as the signed-in caller, over GitHub's REST API.
// Reviewers and updating an existing pull request are NOT supported yet: a
// request naming them is refused as unknown JSON fields rather than ignored.
type PipelinePRRequest struct {
	Text  string `json:"text"`
	Title string `json:"title,omitempty"`
	// Body is the pull request description; empty gets a short default.
	Body string `json:"body,omitempty"`
	// Branch is the relative name of the NEW branch to create. An empty name
	// gets a codeaf/pipeline- suggestion. It is never silently renamed, and
	// an existing branch or the repository's default branch is refused.
	Branch string `json:"branch,omitempty"`
	// ExpectedBaseSHA is the 40-hex COMMIT the draft started from. When it is
	// not the default branch's tip right now, nothing is written.
	ExpectedBaseSHA string `json:"expected_base_sha,omitempty"`
}

// PipelinePR outcomes. Only "opened" carries a pull request; "unchanged"
// means the default branch already holds these bytes and nothing was written.
const (
	PipelinePROpened    = "opened"
	PipelinePRUnchanged = "unchanged"
	PipelinePRRefused   = "refused"
	PipelinePRNotOpened = "not_opened"
	PipelinePRUnknown   = "unknown"
)

// PipelinePRResult is the pull request GitHub confirmed: a positive number
// and the canonical https://github.com/<owner>/<repo>/pull/<number> URL.
type PipelinePRResult struct {
	Number  int    `json:"number"`
	URL     string `json:"url"`
	Branch  string `json:"branch"`
	Base    string `json:"base"`
	BaseSHA string `json:"base_sha"`
	HeadSHA string `json:"head_sha,omitempty"`
}

// PipelinePRResponse is a success: outcome "opened" (PR set) or "unchanged".
// Note is display text only.
type PipelinePRResponse struct {
	Factory Factory           `json:"factory"`
	Outcome string            `json:"outcome"`
	PR      *PipelinePRResult `json:"pr,omitempty"`
	BaseSHA string            `json:"base_sha,omitempty"`
	Note    string            `json:"note"`
}

// PipelinePRFailure is the flow's non-2xx body. Shared authentication,
// visibility and JSON validation may instead return the ordinary API error.
// Outcome is refused (GitHub or the
// factory said no before any write), not_opened (a definitive failure) or
// unknown (a write whose effect cannot be known; check GitHub, do not retry
// blindly). BranchLeft is true when this call created, or may have created,
// Branch on GitHub. The factory never deletes a remote branch.
type PipelinePRFailure struct {
	Factory      Factory `json:"factory"`
	Error        string  `json:"error"`
	Reason       string  `json:"reason,omitempty"`
	Outcome      string  `json:"outcome"`
	Stage        string  `json:"stage"`
	GitHubStatus int     `json:"github_status,omitempty"`
	Branch       string  `json:"branch,omitempty"`
	BranchLeft   bool    `json:"branch_left"`
	RequestID    string  `json:"request_id"`
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
	{"GET", "/v1/jobs/{id}/brief", nil, JobBrief{}},
	{"POST", "/v1/jobs/{id}/hand-off/{card}/not-now", JobAction{}, ActionResult{}},
	{"GET", "/v1/repos/{owner}/{repo}/pipeline/edit-source", nil, PipelineEditSource{}},
	{"POST", "/v1/repos/{owner}/{repo}/pipeline/open-pr", PipelinePRRequest{}, PipelinePRResponse{}},
	{"GET", "/v1/repos/{owner}/{repo}/pipeline/factory-copy", nil, PipelineCopyAnswer{}},
	{"PUT", "/v1/repos/{owner}/{repo}/pipeline/factory-copy", PipelineCopyRequest{}, PipelineCopyAnswer{}},
	{"DELETE", "/v1/repos/{owner}/{repo}/pipeline/factory-copy", PipelineCopyRequest{}, PipelineCopyAnswer{}},
	{"POST", "/v1/issues", NewJob{}, JobCreated{}},
	{"GET", "/v1/issues", nil, Issues{}},
	{"POST", "/v1/jobs", NewJob{}, JobCreated{}},
	{"DELETE", "/v1/auth/device", DevicePoll{}, ActionResult{}},
	{"POST", "/v1/github/poll", nil, ActionResult{}},
}

// EndpointNotFound is the factory's refusal of a route it does not serve, so
// a caller can tell a factory older than a route from a missing job.
const EndpointNotFound = "factory endpoint not found"

// JobBrief is GET /v1/jobs/{id}/brief: the job's newest brief version (its
// stub, made from engine data alone, before any is kept), the reader's status
// of it, and the keys that changed since the rev the caller saw (`?since=`),
// or since the version before when the caller names none. Failure and
// FailedAfter say why and after which step the last write failed, while
// Status is failed.
type JobBrief struct {
	Factory          Factory              `json:"factory"`
	Job              int                  `json:"job"`
	Status           string               `json:"status"`
	Failure          string               `json:"failure,omitempty"`
	FailedAfter      string               `json:"failed_after,omitempty"`
	Since            int                  `json:"since,omitempty"`
	Brief            factory.BriefVersion `json:"brief"`
	ChangedSinceSeen []string             `json:"changed_since_seen"`
}

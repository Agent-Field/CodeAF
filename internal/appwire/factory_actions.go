package appwire

import (
	"context"
	"encoding/json"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── JOB ACTIONS ON AN ATTACHED FACTORY ──────────────────────────────────────
//
// factory.job_* are the writes a person makes on one job of a factory this
// engine is attached to: start, stop, dismiss, drop, bring back a dropped job,
// assign, set priority or
// budget, approve, undo, ask for changes, retry publishing, make a new job,
// stop or rerun one step. Each is ONE method with its own typed params and one
// route behind it; there is no method that forwards a path, a verb name or a
// body, and none that acts on a list of jobs (an approval is one job, seen by
// one person, at one commit).
//
// THE FACTORY DECIDES; THIS WIRE ONLY CARRIES. Whether the caller may act (the
// assignee alone approves, publishes, starts, stops a step), whether the page
// is stale, and what the Undo window is are the factory's rules, read from its
// source. This layer checks the shape of a request and refuses to send one that
// cannot be a request, nothing else.
//
// EVERY JOB WRITE CARRIES THE VERSION THE PERSON READ. `expect.version` is
// required: a write with no version would run on a job the person never saw. A
// stale one is answered [CodeStale] with the factory's own account in
// [FactoryRefusal]; the caller reads the job again and the PERSON decides
// again. An action is never re-sent by the engine, and an answer that cannot
// say whether the factory acted is [CodeUncertain].
//
// ALL OF THIS IS OPTIONAL. An engine that is not a [FactoryActor] answers
// factory.job_* as unknown_method and does not announce
// [FeatureFactoryJobActions]; a factory that lacks a verb answers it as an
// engine refusal. [FactoryCapabilities.Writes] stays false until an app has
// bound these methods end to end.

// FeatureFactoryJobActions is announced in hello when the engine serves
// factory.job_*.
const FeatureFactoryJobActions = "factory.job_actions"

// FactoryBudgetSteps are the limits factory.job_set_budget accepts, in US
// dollars. The job owner can choose either direction, never below recorded spending.
var FactoryBudgetSteps = []float64{5, 10, 20, 30}

// The priorities factory.job_set_priority takes, as the factory spells them.
const (
	PriorityUrgent = "urgent"
	PriorityHigh   = "high"
	PriorityMedium = "medium"
	PriorityLow    = "low"
)

// The bounds on what a person types into a job action.
const (
	MaxFactoryJobWords = 4000
	MaxFactoryJobTitle = 200
	MaxFactoryJobText  = 20000
	MaxFactoryStepName = 128
)

// The actions a [FactoryJobActed] names.
const (
	JobActStart          = "start"
	JobActStop           = "stop"
	JobActDismiss        = "dismiss"
	JobActDrop           = "drop"
	JobActRestore        = "restore"
	JobActRunAgain       = "run_again"
	JobActUnassign       = "unassign"
	JobActAssignToMe     = "assign_to_me"
	JobActChangeAssignee = "change_assignee"
	JobActSetPriority    = "set_priority"
	JobActSetBudget      = "set_budget"
	JobActApprove        = "approve"
	JobActUndo           = "undo"
	JobActRequestChanges = "request_changes"
	JobActRetryPublish   = "retry_publish"
	JobActCreate         = "create"
	JobActStepStop       = "step_stop"
	JobActStepRerun      = "step_rerun"
	JobActMerge          = "merge"
	JobActSetModel       = "set_model"
)

// FactoryJobActionParams names the job and the read it was acted on from. It
// is the whole request of start, stop, dismiss, restore, run_again, unassign,
// assign_to_me, undo and retry_publish.
type FactoryJobActionParams struct {
	Factory string      `json:"factory"`
	Job     int         `json:"job"`
	Expect  *api.Expect `json:"expect"`
}

// FactoryJobDropParams drops a job, which deletes its local branch and folder
// and its conversations and crew on the factory's machine.
// Discard is the person's yes to deleting work they hold; without it a job with
// work is refused with [CodeHasWork]. A drop takes no words.
type FactoryJobDropParams struct {
	Factory string      `json:"factory"`
	Job     int         `json:"job"`
	Discard bool        `json:"discard,omitempty"`
	Expect  *api.Expect `json:"expect"`
}

// FactoryJobAssigneeParams hands a job to a GitHub login. Only the job's
// current assignee may; the factory says so with [CodeForbidden].
type FactoryJobAssigneeParams struct {
	Factory  string      `json:"factory"`
	Job      int         `json:"job"`
	Assignee string      `json:"assignee"`
	Expect   *api.Expect `json:"expect"`
}

// FactoryJobPriorityParams sets a job's priority.
type FactoryJobPriorityParams struct {
	Factory  string      `json:"factory"`
	Job      int         `json:"job"`
	Priority string      `json:"priority" enum:"urgent,high,medium,low"`
	Expect   *api.Expect `json:"expect"`
}

// FactoryJobBudgetParams changes a job's limit to one of [FactoryBudgetSteps], never below recorded spending.
type FactoryJobBudgetParams struct {
	Factory string      `json:"factory"`
	Job     int         `json:"job"`
	Budget  float64     `json:"budget"`
	Expect  *api.Expect `json:"expect"`
}

// FactoryJobMergeParams presses Merge on ONE job. Commit is the pull
// request's head the person saw (api.Job.PR.HeadSHA): the factory refuses a
// merge of any other head. The factory's merge gate decides; its refusal comes
// back whole, in the gate's words. A merge waits five seconds with Undo
// (factory.job_undo) before it is sent.
type FactoryJobMergeParams struct {
	Factory string      `json:"factory"`
	Job     int         `json:"job"`
	Commit  string      `json:"commit,omitempty"`
	Expect  *api.Expect `json:"expect"`
}

// FactoryJobSetModelParams chooses the model ONE job's steps run on. Model
// is a model id; "" goes back to the factory's default. The next step opens on
// it, and a step already running switches at its next turn. Expect is carried
// like every job write's, and the factory accepts it without checking it: a
// model is a preference that commutes with every other write, so a pick is
// never refused as stale. Only a Done job refuses one.
type FactoryJobSetModelParams struct {
	Factory   string      `json:"factory"`
	Job       int         `json:"job"`
	Model     string      `json:"model"`
	Automatic *bool       `json:"automatic,omitempty"`
	Expect    *api.Expect `json:"expect"`
}

// MaxFactoryJobModel is the longest model id a job keeps.
const MaxFactoryJobModel = 200

// FactoryJobApproveParams approves ONE job. Commit is the head commit the
// person saw (api.Job.HeadCommit): the factory refuses an approval of any
// other head with [CodeStale]. Edited says the person changed what was shown,
// which the factory holds to its proof rules. Words are optional.
type FactoryJobApproveParams struct {
	Factory string      `json:"factory"`
	Job     int         `json:"job"`
	Commit  string      `json:"commit,omitempty"`
	Words   string      `json:"words,omitempty"`
	Edited  bool        `json:"edited,omitempty"`
	Expect  *api.Expect `json:"expect"`
}

// FactoryJobChangesParams sends a job back with what to change.
type FactoryJobChangesParams struct {
	Factory string      `json:"factory"`
	Job     int         `json:"job"`
	Words   string      `json:"words"`
	Expect  *api.Expect `json:"expect"`
}

// FactoryJobStepParams names one step of a job, by the name the job's steps
// carry.
type FactoryJobStepParams struct {
	Factory string      `json:"factory"`
	Job     int         `json:"job"`
	Step    string      `json:"step"`
	Expect  *api.Expect `json:"expect"`
	// Words are the person's for a rerun (optional): what to do differently,
	// kept on the job's notes. A step stop takes none.
	Words string `json:"words,omitempty"`
	// Model is, on a rerun, the model the step runs on from now (run again
	// from here with another model), one of models.list's ids; "" keeps the
	// step's own. Sent only where [FeatureFactoryJobStepRerunModel] is
	// announced. A step stop takes none.
	Model string `json:"model,omitempty"`
}

// FeatureFactoryJobStepRerunModel is announced beside
// [FeatureFactoryJobActions] when factory.job_step_rerun carries `model`.
// factory.job_step_rerun is RUN AGAIN FROM HERE: the step and every step after
// it go back to pending, so the later steps run again and an approval after it
// asks again, while earlier steps and money spent are kept. With a model the
// step runs on it, checked against this engine's models.list; a check, an
// approval and a fixed step refuse one.
const FeatureFactoryJobStepRerunModel = "factory.job_step_rerun_model"

func withFactoryJobStepRerunModelFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryActor); ok {
		hello = withFeature(hello, FeatureFactoryJobStepRerunModel)
	}
	return hello
}

// FactoryJobCreateParams makes a job. WITHOUT Start THE JOB IS ONLY ADDED TO
// THE BACKLOG: nothing starts, no model is asked, and no GitHub issue is
// opened. Start "now" is the one explicit way to also start it, and it requires
// Assignee to be present ("" for no one; a team factory refuses that). Assignee
// left out leaves the factory's rule in charge; "" leaves the job unassigned.
type FactoryJobCreateParams struct {
	Factory  string  `json:"factory"`
	Repo     string  `json:"repo"`
	Title    string  `json:"title"`
	Text     string  `json:"text,omitempty"`
	Assignee *string `json:"assignee,omitempty"`
	Start    string  `json:"start,omitempty" enum:"backlog,now"`
}

// FactoryApprovalReceipt is the decision an approval left on the job. An
// approval waits UndoUntil before it is acted on: until then the same person's
// factory.job_undo withdraws it, and after it the factory publishes.
type FactoryApprovalReceipt struct {
	Automatic bool      `json:"automatic,omitempty"`
	Kind      string    `json:"kind,omitempty"`
	State     string    `json:"state"`
	DecidedBy string    `json:"decided_by,omitempty"`
	At        time.Time `json:"at"`
	UndoUntil time.Time `json:"undo_until"`
	Commit    string    `json:"commit,omitempty"`
	Edited    bool      `json:"edited,omitempty"`
}

// FactoryJobActed is what every job action answers: what was done to which
// job, and the job as the factory shows it afterwards. Current is a fresh read
// the engine makes once after the factory said yes; it is absent when that read
// failed, which says nothing about the action. The next write on this job must
// carry Current.Version, not the one before.
type FactoryJobActed struct {
	Factory api.Factory `json:"factory"`
	Action  string      `json:"action"`
	// Job is the job acted on, or the new job's number for create.
	Job int `json:"job"`
	// Approval and UndoUntil are set by approve when the factory holds the
	// decision for its Undo window.
	Approval  *FactoryApprovalReceipt `json:"approval,omitempty"`
	UndoUntil *time.Time              `json:"undo_until,omitempty"`
	// Event is the record id of an approval the factory acted on at once.
	Event   string   `json:"event,omitempty"`
	Current *api.Job `json:"current,omitempty"`
}

// FactoryRefusal is the Detail of a [CodeForbidden], [CodeStale] or
// [CodeConflict] a factory job action is refused with: the factory's own
// account of who and what the refusal is about. It holds handles, states,
// commit ids and times, never a credential.
type FactoryRefusal struct {
	// Reason is the factory's word: stale, not_job_person, not_allowed,
	// conflict, already_answered or changed_since_opened.
	Reason string `json:"reason,omitempty"`
	// By is who the refusal names (the assignee, the approver), with @.
	By string `json:"by,omitempty"`
	// JobPerson is who may do this, with @.
	JobPerson     string     `json:"job_person,omitempty"`
	At            *time.Time `json:"at,omitempty"`
	NowState      string     `json:"now_state,omitempty"`
	Taker         string     `json:"taker,omitempty"`
	Machine       string     `json:"machine,omitempty"`
	ApprovedBy    string     `json:"approved_by,omitempty"`
	Event         string     `json:"event,omitempty"`
	Commit        string     `json:"commit,omitempty"`
	CurrentCommit string     `json:"current_commit,omitempty"`
	HeadCommit    string     `json:"head_commit,omitempty"`
	RequestID     string     `json:"request_id,omitempty"`
}

// FactoryActor is the engine's half of factory.job_*. It is optional on top of
// [Factory]: an engine without it refuses every factory.job_* as
// unknown_method. Every method makes at most one write to the factory and
// never sends it twice; an outcome it cannot confirm is [CodeUncertain]. The
// params arrive checked ([FactoryJobActionParams] and the others), trimmed, and
// with a version.
type FactoryActor interface {
	FactoryJobStart(ctx context.Context, p FactoryJobActionParams) (FactoryJobActed, error)
	FactoryJobStop(ctx context.Context, p FactoryJobActionParams) (FactoryJobActed, error)
	FactoryJobDismiss(ctx context.Context, p FactoryJobActionParams) (FactoryJobActed, error)
	FactoryJobDrop(ctx context.Context, p FactoryJobDropParams) (FactoryJobActed, error)
	// FactoryJobRestore is Bring back: a dropped job returns to Backlog.
	FactoryJobRestore(ctx context.Context, p FactoryJobActionParams) (FactoryJobActed, error)
	// FactoryJobRunAgain is Run again: a job whose run ended on a failure,
	// a person's stop that cut a step among them, or whose pull request was
	// closed unmerged, starts over at its first step.
	FactoryJobRunAgain(ctx context.Context, p FactoryJobActionParams) (FactoryJobActed, error)
	FactoryJobUnassign(ctx context.Context, p FactoryJobActionParams) (FactoryJobActed, error)
	FactoryJobAssignToMe(ctx context.Context, p FactoryJobActionParams) (FactoryJobActed, error)
	FactoryJobChangeAssignee(ctx context.Context, p FactoryJobAssigneeParams) (FactoryJobActed, error)
	FactoryJobSetPriority(ctx context.Context, p FactoryJobPriorityParams) (FactoryJobActed, error)
	FactoryJobSetBudget(ctx context.Context, p FactoryJobBudgetParams) (FactoryJobActed, error)
	FactoryJobApprove(ctx context.Context, p FactoryJobApproveParams) (FactoryJobActed, error)
	FactoryJobUndo(ctx context.Context, p FactoryJobActionParams) (FactoryJobActed, error)
	FactoryJobRequestChanges(ctx context.Context, p FactoryJobChangesParams) (FactoryJobActed, error)
	FactoryJobRetryPublish(ctx context.Context, p FactoryJobActionParams) (FactoryJobActed, error)
	FactoryJobCreate(ctx context.Context, p FactoryJobCreateParams) (FactoryJobActed, error)
	FactoryJobStepStop(ctx context.Context, p FactoryJobStepParams) (FactoryJobActed, error)
	FactoryJobStepRerun(ctx context.Context, p FactoryJobStepParams) (FactoryJobActed, error)
	FactoryJobMerge(ctx context.Context, p FactoryJobMergeParams) (FactoryJobActed, error)
	FactoryJobSetModel(ctx context.Context, p FactoryJobSetModelParams) (FactoryJobActed, error)
}

func withFactoryActorFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryActor); ok {
		hello = withFeature(hello, FeatureFactoryJobActions)
		if _, ok := e.(FactoryCloser); ok {
			hello = withFeature(hello, FeatureFactoryJobClose)
		}
		if _, ok := e.(FactoryJobController); ok {
			hello = withFeature(hello, FeatureFactoryJobControl)
			hello = withFeature(hello, FeatureFactoryGiveBackChecks)
			hello = withFeature(hello, FeatureFactoryJobAutonomy)
		}
		if _, ok := e.(FactoryJobHolder); ok {
			hello = withFeature(hello, FeatureFactoryJobHold)
		}
	}
	return hello
}

func factoryActorOf(e Engine, name string) (FactoryActor, error) {
	a, ok := e.(FactoryActor)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return a, nil
}

// factoryLogin is a GitHub login with an optional leading @.
var factoryLogin = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)

var factoryCommit = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// checkJob trims the factory id and checks the job number and the read the
// action stands on.
func checkJob(factory *string, job int, expect *api.Expect) error {
	id, err := factoryID(strings.TrimSpace(*factory))
	if err != nil {
		return err
	}
	*factory = id
	if job < 1 {
		return Errorf(CodeBadParams, "job must be a whole number from 1")
	}
	if expect == nil || expect.Version == nil || *expect.Version < 1 {
		return Errorf(CodeBadParams, "expect.version is required: send the version of the job the person read")
	}
	if len(expect.State) > 64 || len(expect.Step) > MaxFactoryStepName {
		return Errorf(CodeBadParams, "expect.state and expect.step are too long")
	}
	return nil
}

// words is optional or required text of at most limit characters, trimmed.
func words(field, value string, limit int, need bool) (string, error) {
	value = strings.TrimSpace(value)
	if need && value == "" {
		return "", Errorf(CodeBadParams, "%s is required", field)
	}
	if len([]rune(value)) > limit {
		return "", Errorf(CodeBadParams, "%s is at most %d characters", field, limit)
	}
	return value, nil
}

// factoryAction builds one factory.job_* method. Params are read STRICTLY: a
// field this wire does not know (an issue flag, a path, a list of jobs) is
// refused as bad_params, because ignoring it would let a caller believe it had
// asked for something this wire cannot do.
func factoryAction[P any](name string, prep func(*P) error, call func(FactoryActor, context.Context, P) (FactoryJobActed, error)) method {
	return shaped[P, FactoryJobActed](func(e Engine, raw json.RawMessage) (*FactoryJobActed, error) {
		a, err := factoryActorOf(e, name)
		if err != nil {
			return nil, err
		}
		var p P
		if err := decodeStrict(name, raw, &p); err != nil {
			return nil, err
		}
		if err := prep(&p); err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := call(a, ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}

func prepAction(p *FactoryJobActionParams) error { return checkJob(&p.Factory, p.Job, p.Expect) }

func prepDrop(p *FactoryJobDropParams) error { return checkJob(&p.Factory, p.Job, p.Expect) }

func prepAssignee(p *FactoryJobAssigneeParams) error {
	if err := checkJob(&p.Factory, p.Job, p.Expect); err != nil {
		return err
	}
	login := strings.TrimPrefix(strings.TrimSpace(p.Assignee), "@")
	if !factoryLogin.MatchString(login) {
		return Errorf(CodeBadParams, "assignee must be a GitHub login")
	}
	p.Assignee = login
	return nil
}

func prepPriority(p *FactoryJobPriorityParams) error {
	if err := checkJob(&p.Factory, p.Job, p.Expect); err != nil {
		return err
	}
	switch p.Priority = strings.TrimSpace(p.Priority); p.Priority {
	case PriorityUrgent, PriorityHigh, PriorityMedium, PriorityLow:
		return nil
	}
	return Errorf(CodeBadParams, "priority must be urgent, high, medium or low")
}

func prepBudget(p *FactoryJobBudgetParams) error {
	if err := checkJob(&p.Factory, p.Job, p.Expect); err != nil {
		return err
	}
	for _, step := range FactoryBudgetSteps {
		if math.Abs(p.Budget-step) < 1e-9 {
			p.Budget = step
			return nil
		}
	}
	return Errorf(CodeBadParams, "budget must be one of 5, 10, 20 or 30 US dollars")
}

func prepApprove(p *FactoryJobApproveParams) error {
	if err := checkJob(&p.Factory, p.Job, p.Expect); err != nil {
		return err
	}
	p.Commit = strings.ToLower(strings.TrimSpace(p.Commit))
	if p.Commit != "" && !factoryCommit.MatchString(p.Commit) {
		return Errorf(CodeBadParams, "commit must be the head commit id the person saw")
	}
	var err error
	p.Words, err = words("words", p.Words, MaxFactoryJobWords, false)
	return err
}

func prepMerge(p *FactoryJobMergeParams) error {
	if err := checkJob(&p.Factory, p.Job, p.Expect); err != nil {
		return err
	}
	p.Commit = strings.ToLower(strings.TrimSpace(p.Commit))
	if p.Commit != "" && !factoryCommit.MatchString(p.Commit) {
		return Errorf(CodeBadParams, "commit must be the pull request's head commit id the person saw")
	}
	return nil
}

func prepSetModel(p *FactoryJobSetModelParams) error {
	if err := checkJob(&p.Factory, p.Job, p.Expect); err != nil {
		return err
	}
	p.Model = strings.TrimSpace(p.Model)
	if len(p.Model) > MaxFactoryJobModel || strings.ContainsFunc(p.Model, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return Errorf(CodeBadParams, "model must be a model id of at most 200 characters, with no spaces")
	}
	return nil
}

func prepChanges(p *FactoryJobChangesParams) error {
	if err := checkJob(&p.Factory, p.Job, p.Expect); err != nil {
		return err
	}
	var err error
	p.Words, err = words("words", p.Words, MaxFactoryJobWords, true)
	return err
}

// prepStepRerun is [prepStep] with the rerun's optional words.
func prepStepRerun(p *FactoryJobStepParams) error {
	words, err := words("words", p.Words, MaxFactoryJobWords, false)
	if err != nil {
		return err
	}
	model := strings.TrimSpace(p.Model)
	if !factory.ModelID(model) {
		return Errorf(CodeBadParams, "%s", factory.ErrModelID.Error())
	}
	p.Words, p.Model = "", ""
	if err := prepStep(p); err != nil {
		return err
	}
	p.Words, p.Model = words, model
	return nil
}

func prepStep(p *FactoryJobStepParams) error {
	if strings.TrimSpace(p.Words) != "" || strings.TrimSpace(p.Model) != "" {
		return Errorf(CodeBadParams, "only a rerun takes words or a model")
	}
	if err := checkJob(&p.Factory, p.Job, p.Expect); err != nil {
		return err
	}
	var err error
	if p.Step, err = words("step", p.Step, MaxFactoryStepName, true); err != nil {
		return err
	}
	if strings.ContainsAny(p.Step, "/\\?#%") || strings.ContainsFunc(p.Step, func(r rune) bool { return r < ' ' || r == 0x7f }) {
		return Errorf(CodeBadParams, "step must be a step name of the job")
	}
	return nil
}

func prepCreate(p *FactoryJobCreateParams) error {
	id, err := factoryID(strings.TrimSpace(p.Factory))
	if err != nil {
		return err
	}
	p.Factory = id
	if p.Repo, err = FactoryRepoName(p.Repo); err != nil {
		return err
	}
	if p.Title, err = words("title", p.Title, MaxFactoryJobTitle, true); err != nil {
		return err
	}
	if p.Text, err = words("text", p.Text, MaxFactoryJobText, false); err != nil {
		return err
	}
	if p.Assignee != nil {
		login := strings.TrimPrefix(strings.TrimSpace(*p.Assignee), "@")
		if login != "" && !factoryLogin.MatchString(login) {
			return Errorf(CodeBadParams, "assignee must be a GitHub login, or empty for none")
		}
		p.Assignee = &login
	}
	switch p.Start = strings.TrimSpace(p.Start); p.Start {
	case "", "backlog":
		return nil
	case "now":
		// STARTING IS DELIBERATE: the person says who the job is for, even if
		// that is no one, so a job is never started by leaving a field out.
		if p.Assignee == nil {
			return Errorf(CodeBadParams, "assignee is required to start a job now; send an empty assignee for none")
		}
		return nil
	}
	return Errorf(CodeBadParams, "start must be backlog or now")
}

func init() {
	for name, m := range map[string]method{
		"factory.job_start":           factoryAction("factory.job_start", prepAction, FactoryActor.FactoryJobStart),
		"factory.job_stop":            factoryAction("factory.job_stop", prepAction, FactoryActor.FactoryJobStop),
		"factory.job_dismiss":         factoryAction("factory.job_dismiss", prepAction, FactoryActor.FactoryJobDismiss),
		"factory.job_drop":            factoryAction("factory.job_drop", prepDrop, FactoryActor.FactoryJobDrop),
		"factory.job_restore":         factoryAction("factory.job_restore", prepAction, FactoryActor.FactoryJobRestore),
		"factory.job_run_again":       factoryAction("factory.job_run_again", prepAction, FactoryActor.FactoryJobRunAgain),
		"factory.job_unassign":        factoryAction("factory.job_unassign", prepAction, FactoryActor.FactoryJobUnassign),
		"factory.job_assign_to_me":    factoryAction("factory.job_assign_to_me", prepAction, FactoryActor.FactoryJobAssignToMe),
		"factory.job_change_assignee": factoryAction("factory.job_change_assignee", prepAssignee, FactoryActor.FactoryJobChangeAssignee),
		"factory.job_set_priority":    factoryAction("factory.job_set_priority", prepPriority, FactoryActor.FactoryJobSetPriority),
		"factory.job_set_budget":      factoryAction("factory.job_set_budget", prepBudget, FactoryActor.FactoryJobSetBudget),
		"factory.job_approve":         factoryAction("factory.job_approve", prepApprove, FactoryActor.FactoryJobApprove),
		"factory.job_undo":            factoryAction("factory.job_undo", prepAction, FactoryActor.FactoryJobUndo),
		"factory.job_request_changes": factoryAction("factory.job_request_changes", prepChanges, FactoryActor.FactoryJobRequestChanges),
		"factory.job_retry_publish":   factoryAction("factory.job_retry_publish", prepAction, FactoryActor.FactoryJobRetryPublish),
		"factory.job_create":          factoryAction("factory.job_create", prepCreate, FactoryActor.FactoryJobCreate),
		"factory.job_step_stop":       factoryAction("factory.job_step_stop", prepStep, FactoryActor.FactoryJobStepStop),
		"factory.job_step_rerun":      factoryAction("factory.job_step_rerun", prepStepRerun, FactoryActor.FactoryJobStepRerun),
		"factory.job_merge":           factoryAction("factory.job_merge", prepMerge, FactoryActor.FactoryJobMerge),
		"factory.job_set_model":       factoryAction("factory.job_set_model", prepSetModel, FactoryActor.FactoryJobSetModel),
	} {
		methods[name] = m
		factoryActionMethods[name] = m
	}
}

// factoryActionMethods is the table of factory.job_*, for the tests that walk it.
var factoryActionMethods = map[string]method{}

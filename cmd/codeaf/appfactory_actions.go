package main

// ── codeaf app: JOB WRITES ON AN ATTACHED FACTORY ───────────────────────────
//
// The engine's half of appwire.FactoryActor. Each method is one POST to one
// route of the factory's /v1 API, spelled here as a constant: the path, the
// verb and the body are never taken from the app, so the wire cannot be used as
// a proxy.
//
// THE FACTORY DECIDES. The version the person read travels as `expect`, and the
// factory refuses a stale one; the assignee-only rules for approving,
// publishing, starting and step controls are the factory's too. This file
// carries the refusal back whole and never works around it.
//
// A WRITE IS SENT ONCE. There is no retry, no replay and no second route tried
// when the first said no. When the request may have reached the factory and no
// answer came back (a dropped connection, a timeout, a 5xx), the answer is
// appwire.CodeUncertain: the person looks at the job again before anything is
// sent. Only a failure to connect at all, which proves nothing was sent, is
// "unreachable".
//
// NOTHING SECRET IS READ OR ANSWERED. The bearer is added by the transport as
// for every read; the answers are the factory's own job and approval, with
// handles, states, commits and times.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/redact"
)

var _ appwire.FactoryActor = (*appBackend)(nil)

// The factory's job verbs this file speaks, one per route.
const (
	factoryVerbStart         = "start_now"
	factoryVerbStop          = "stop"
	factoryVerbHold          = "pause"
	factoryVerbResume        = "resume"
	factoryVerbDismiss       = "dismiss"
	factoryVerbDrop          = "drop"
	factoryVerbRestore       = "restore"
	factoryVerbRunAgain      = "run_again"
	factoryVerbUnassign      = "unassign"
	factoryVerbAssignToMe    = "assign_to_me"
	factoryVerbAssignee      = "change_assignee"
	factoryVerbPriority      = "priority"
	factoryVerbBudget        = "raise_budget"
	factoryVerbApprove       = "approve"
	factoryVerbUndo          = "undo"
	factoryVerbRequestChange = "request_changes"
	factoryVerbRetryPublish  = "retry_publish"
	factoryVerbMerge         = "merge"
)

// appFactoryAnswer is the part of a factory's yes this layer reads. The
// factory's answers differ by verb (a bare factory, an approval with its Undo
// window, an event id, a new job's id), and every field is optional.
type appFactoryAnswer struct {
	Factory   api.Factory       `json:"factory"`
	ID        int               `json:"id"`
	Event     string            `json:"event"`
	Approval  *factory.Approval `json:"approval"`
	UndoUntil *time.Time        `json:"undo_until"`
}

// jobPath is the route of one verb on one job.
func jobPath(job int, rest string) string {
	return "/v1/jobs/" + strconv.Itoa(job) + "/" + rest
}

// ── the one write ──────────────────────────────────────────────────────────

// write sends body to path once and reads the factory's answer.
func (h *appFactoryHub) write(ctx context.Context, id, path string, body any) (appFactoryAnswer, error) {
	data, err := h.post(ctx, id, path, body)
	if err != nil {
		return appFactoryAnswer{}, err
	}
	var out appFactoryAnswer
	// A yes whose body cannot be read is still a yes: the action happened.
	_ = json.Unmarshal(data, &out)
	return out, nil
}

// post is the one send behind every job write: it returns the body of a
// factory's yes, and a refusal or an unconfirmed outcome as the wire error.
func (h *appFactoryHub) post(ctx context.Context, id, path string, body any) ([]byte, error) {
	row, token, err := h.credentialed(ctx, id)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, appwire.Errorf(appwire.CodeBadParams, "that is not a request a factory can take")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, row.URL+path, bytes.NewReader(raw))
	if err != nil {
		return nil, appwire.Errorf(appwire.CodeBadParams, "that is not a request a factory can take")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "codeaf-app")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, appFactoryWriteError(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, appFactorySmall+1))
	if err != nil {
		return nil, appwire.Errorf(appwire.CodeUncertain, "the factory stopped answering; the action may have happened, so read the job before trying again")
	}
	reply := appFactoryReply{status: resp.StatusCode, header: resp.Header, body: data}
	if int64(len(data)) > appFactorySmall {
		// An answer this size is not the factory's: whether it acted is unknown.
		return nil, appwire.Errorf(appwire.CodeUncertain, "the factory's answer is too large to read; read the job before trying again")
	}
	if reply.status < 200 || reply.status >= 300 {
		return nil, appFactoryActionFailure(reply)
	}
	h.touch(id)
	return data, nil
}

// appFactoryWriteError tells a request that never left from one that may have.
// Only a refused connection proves nothing was sent.
func appFactoryWriteError(err error) error {
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return appwire.Errorf(appwire.CodeUnreachable, "the factory is not answering")
	}
	return appwire.Errorf(appwire.CodeUncertain, "the factory did not answer; the action may have happened, so read the job before trying again")
}

// appFactoryActionFailure is the wire error for a factory's refusal of a write.
// The refusal is passed on whole as a FactoryRefusal, so the person is told who
// may act, who already did, or what the job is now. A 5xx is not a refusal: the
// factory may have acted before it failed.
func appFactoryActionFailure(r appFactoryReply) error {
	var body struct {
		Error         string     `json:"error"`
		Reason        string     `json:"reason"`
		By            string     `json:"by"`
		At            *time.Time `json:"at"`
		NowState      string     `json:"now_state"`
		Taker         string     `json:"taker"`
		Machine       string     `json:"machine"`
		ApprovedBy    string     `json:"approved_by"`
		Event         string     `json:"event"`
		Commit        string     `json:"commit"`
		CurrentCommit string     `json:"current_commit"`
		HeadCommit    string     `json:"head_commit"`
		JobPerson     string     `json:"job_person"`
		Outcome       string     `json:"outcome"`
		RequestID     string     `json:"request_id"`
	}
	_ = json.Unmarshal(bytes.TrimSpace(r.body), &body)
	clean := func(s string) string {
		s = strings.TrimSpace(redact.Secrets(s))
		if len(s) > 300 {
			s = s[:300]
		}
		return s
	}
	detail := &appwire.FactoryRefusal{
		Reason: clean(body.Reason), By: clean(body.By), JobPerson: clean(body.JobPerson), At: body.At,
		NowState: clean(body.NowState), Taker: clean(body.Taker), Machine: clean(body.Machine),
		ApprovedBy: clean(body.ApprovedBy), Event: clean(body.Event), Commit: clean(body.Commit),
		CurrentCommit: clean(body.CurrentCommit), HeadCommit: clean(body.HeadCommit), RequestID: clean(body.RequestID),
		Outcome: refusalOutcome(body.Outcome),
	}
	words := clean(body.Error)
	refused := func(code, fallback string) error {
		if words == "" {
			words = fallback
		}
		e := appwire.Errorf(code, "%s", words)
		e.Detail = detail
		return e
	}
	switch {
	case r.status == http.StatusForbidden:
		return refused(appwire.CodeForbidden, "the factory does not let you do that")
	case r.status == http.StatusConflict && (detail.Reason == "stale" || detail.Reason == "changed_since_opened"):
		return refused(appwire.CodeStale, "the job changed while you looked")
	case r.status == http.StatusConflict && detail.Reason == api.ReasonHasWork:
		// A drop that would delete work: the factory's sentence names it, and
		// the person's yes is the same drop with discard.
		return refused(appwire.CodeHasWork, "the job has work that dropping would delete")
	case r.status == http.StatusConflict:
		return refused(appwire.CodeConflict, "the job is not in a state for that")
	case r.status == http.StatusNotImplemented:
		return appwire.Errorf(appwire.CodeEngine, "this factory does not provide that action")
	case r.status >= 500:
		// STILL UNCERTAIN, BUT IN THE FACTORY'S OWN WORDS: a 5xx may have
		// half-happened, so the code stays uncertain, and the sentence the
		// factory gave for it (a clone that failed, a store that could not be
		// written) goes in front of the warning instead of being dropped.
		const maybe = "the action may have happened, so read the job before trying again"
		if words != "" {
			return withOutcome(appwire.Errorf(appwire.CodeUncertain, "%s · %s", words, maybe), detail)
		}
		return withOutcome(appwire.Errorf(appwire.CodeUncertain, "the factory failed while acting; %s", maybe), detail)
	}
	return withOutcome(appFactoryFailure(r), detail)
}

// refusalOutcome is the factory's word for whether a comment was posted, or
// "" for any other word (and from a factory older than the field).
func refusalOutcome(word string) string {
	if word == api.CommentRefused || word == api.CommentUnknown {
		return word
	}
	return ""
}

// withOutcome carries the typed detail on a refusal that says whether
// anything was posted, so the app can switch on it whatever the status was.
// A refusal without an outcome is left exactly as it was.
func withOutcome(err error, detail *appwire.FactoryRefusal) error {
	var e *appwire.Error
	if detail.Outcome != "" && errors.As(err, &e) && e.Detail == nil {
		e.Detail = detail
	}
	return err
}

// acted builds the answer to a successful write: what the factory said, and
// the job as it reads now. The re-read is one GET, never a retry of the write,
// and its failure leaves Current out.
func (h *appFactoryHub) acted(ctx context.Context, id, action string, job int, a appFactoryAnswer) appwire.FactoryJobActed {
	out := appwire.FactoryJobActed{Factory: a.Factory, Action: action, Job: job, Event: a.Event, UndoUntil: a.UndoUntil}
	if a.ID > 0 {
		out.Job = a.ID
	}
	if ap := a.Approval; ap != nil {
		out.Approval = &appwire.FactoryApprovalReceipt{Automatic: ap.Automatic, Kind: ap.Kind, State: ap.State, DecidedBy: ap.DecidedBy, At: ap.At, UndoUntil: ap.UndoUntil, Commit: ap.Commit, Edited: ap.Edited}
		if out.UndoUntil == nil && !ap.UndoUntil.IsZero() {
			u := ap.UndoUntil
			out.UndoUntil = &u
		}
	}
	var current api.Job
	if err := h.read(ctx, id, "/v1/jobs/"+strconv.Itoa(out.Job), appFactoryBody, &current); err == nil {
		out.Current = &current
	}
	return out
}

// jobWrite is the whole of a job verb: guard in, one write, answer out.
func (b *appBackend) jobWrite(ctx context.Context, id, action string, job int, rest string, body any) (appwire.FactoryJobActed, error) {
	h := b.factoryHub()
	a, err := h.write(ctx, id, jobPath(job, rest), body)
	if err != nil {
		return appwire.FactoryJobActed{}, err
	}
	return h.acted(ctx, id, action, job, a), nil
}

// guardBody is the body of a verb that takes nothing but the version read.
type guardBody struct {
	Expect *api.Expect `json:"expect,omitempty"`
}

func (b *appBackend) FactoryJobStart(ctx context.Context, p appwire.FactoryJobActionParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActStart, p.Job, factoryVerbStart, guardBody{p.Expect})
}

func (b *appBackend) FactoryJobStop(ctx context.Context, p appwire.FactoryJobActionParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActStop, p.Job, factoryVerbStop, guardBody{p.Expect})
}

// FactoryJobHold keeps a queued job from starting. The factory refuses a job
// that is not queued, and only the job's person may hold it.
func (b *appBackend) FactoryJobHold(ctx context.Context, p appwire.FactoryJobActionParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActHold, p.Job, factoryVerbHold, guardBody{p.Expect})
}

// FactoryJobResume lets a held job go on: it waits in the queue again.
func (b *appBackend) FactoryJobResume(ctx context.Context, p appwire.FactoryJobActionParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActResume, p.Job, factoryVerbResume, guardBody{p.Expect})
}

func (b *appBackend) FactoryJobDismiss(ctx context.Context, p appwire.FactoryJobActionParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActDismiss, p.Job, factoryVerbDismiss, guardBody{p.Expect})
}

// FactoryJobDrop is Drop. The factory deletes the job's local branch and
// folder, and refuses one whose work would be lost unless Discard says yes.
func (b *appBackend) FactoryJobDrop(ctx context.Context, p appwire.FactoryJobDropParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActDrop, p.Job, factoryVerbDrop, struct {
		Discard bool        `json:"discard,omitempty"`
		Expect  *api.Expect `json:"expect,omitempty"`
	}{p.Discard, p.Expect})
}

// FactoryJobRestore is Bring back. The factory refuses a job that was not
// dropped, and only the job's person may bring it back.
func (b *appBackend) FactoryJobRestore(ctx context.Context, p appwire.FactoryJobActionParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActRestore, p.Job, factoryVerbRestore, guardBody{p.Expect})
}

// FactoryJobRunAgain is Run again. The factory refuses a job that carries no
// failure (a person's stop that cut a step is one) and whose pull request was
// not closed unmerged, and only the job's person may run it again.
func (b *appBackend) FactoryJobRunAgain(ctx context.Context, p appwire.FactoryJobActionParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActRunAgain, p.Job, factoryVerbRunAgain, guardBody{p.Expect})
}

func (b *appBackend) FactoryJobUnassign(ctx context.Context, p appwire.FactoryJobActionParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActUnassign, p.Job, factoryVerbUnassign, guardBody{p.Expect})
}

func (b *appBackend) FactoryJobAssignToMe(ctx context.Context, p appwire.FactoryJobActionParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActAssignToMe, p.Job, factoryVerbAssignToMe, guardBody{p.Expect})
}

func (b *appBackend) FactoryJobChangeAssignee(ctx context.Context, p appwire.FactoryJobAssigneeParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActChangeAssignee, p.Job, factoryVerbAssignee, api.AssigneeRequest{Assignee: p.Assignee, Expect: p.Expect})
}

func (b *appBackend) FactoryJobSetPriority(ctx context.Context, p appwire.FactoryJobPriorityParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActSetPriority, p.Job, factoryVerbPriority, struct {
		Priority string      `json:"priority"`
		Expect   *api.Expect `json:"expect,omitempty"`
	}{p.Priority, p.Expect})
}

func (b *appBackend) FactoryJobSetBudget(ctx context.Context, p appwire.FactoryJobBudgetParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActSetBudget, p.Job, factoryVerbBudget, struct {
		Budget float64     `json:"budget"`
		Expect *api.Expect `json:"expect,omitempty"`
	}{p.Budget, p.Expect})
}

// FactoryJobApprove sends the person's decision for ONE job. The factory holds
// it for its Undo window before it acts; the answer carries that window, and
// FactoryJobUndo is how the same person withdraws it inside it.
func (b *appBackend) FactoryJobApprove(ctx context.Context, p appwire.FactoryJobApproveParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActApprove, p.Job, factoryVerbApprove, api.Approve{Words: p.Words, Commit: p.Commit, Edited: p.Edited, Expect: p.Expect})
}

func (b *appBackend) FactoryJobUndo(ctx context.Context, p appwire.FactoryJobActionParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActUndo, p.Job, factoryVerbUndo, guardBody{p.Expect})
}

func (b *appBackend) FactoryJobRequestChanges(ctx context.Context, p appwire.FactoryJobChangesParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActRequestChanges, p.Job, factoryVerbRequestChange, struct {
		Words  string      `json:"words"`
		Expect *api.Expect `json:"expect,omitempty"`
	}{p.Words, p.Expect})
}

func (b *appBackend) FactoryJobRetryPublish(ctx context.Context, p appwire.FactoryJobActionParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActRetryPublish, p.Job, factoryVerbRetryPublish, guardBody{p.Expect})
}

// FactoryJobMerge presses Merge. The factory's merge gate decides and its
// refusal comes back whole; a merge it takes waits with Undo, then rides the
// job's lease to the runner that carries it out.
func (b *appBackend) FactoryJobMerge(ctx context.Context, p appwire.FactoryJobMergeParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActMerge, p.Job, factoryVerbMerge, struct {
		Commit string      `json:"commit,omitempty"`
		Expect *api.Expect `json:"expect,omitempty"`
	}{p.Commit, p.Expect})
}

func (b *appBackend) FactoryJobSetModel(ctx context.Context, p appwire.FactoryJobSetModelParams) (appwire.FactoryJobActed, error) {
	acted, err := b.jobWrite(ctx, p.Factory, appwire.JobActSetModel, p.Job, "model", struct {
		Model     string      `json:"model"`
		Automatic *bool       `json:"automatic,omitempty"`
		Expect    *api.Expect `json:"expect,omitempty"`
	}{p.Model, p.Automatic, p.Expect})
	if err == nil {
		// The job's agent reads the job's model too (appfactory_chats.go).
		b.jobAgentModel(p.Factory, p.Job)
		b.briefEdited(p.Factory, p.Job)
	}
	return acted, err
}

func (b *appBackend) FactoryJobStepStop(ctx context.Context, p appwire.FactoryJobStepParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActStepStop, p.Job, "steps/"+stepSegment(p.Step)+"/stop", guardBody{p.Expect})
}

func (b *appBackend) FactoryJobStepRerun(ctx context.Context, p appwire.FactoryJobStepParams) (appwire.FactoryJobActed, error) {
	return b.jobWrite(ctx, p.Factory, appwire.JobActStepRerun, p.Job, "steps/"+stepSegment(p.Step)+"/rerun", struct {
		Expect *api.Expect `json:"expect,omitempty"`
		Words  string      `json:"words,omitempty"`
	}{p.Expect, p.Words})
}

// stepSegment is a step name as one path segment. The name was checked to
// carry no separator, and is escaped anyway so it can never be two segments.
func stepSegment(name string) string { return url.PathEscape(name) }

// FactoryJobCreate makes a job. It is a backlog job unless the person asked for
// "now", opens no GitHub issue, and asks no model for anything.
func (b *appBackend) FactoryJobCreate(ctx context.Context, p appwire.FactoryJobCreateParams) (appwire.FactoryJobActed, error) {
	h := b.factoryHub()
	body := api.NewJob{Repo: p.Repo, Title: p.Title, Text: p.Text, Source: factory.AskApp, Assignee: p.Assignee}
	if p.Start == "now" {
		body.Start = "now"
	}
	a, err := h.write(ctx, p.Factory, "/v1/jobs", body)
	if err != nil {
		return appwire.FactoryJobActed{}, err
	}
	if a.ID < 1 {
		// The factory said yes and named no job: a job may exist that this
		// layer cannot name, so it is uncertain rather than a success.
		return appwire.FactoryJobActed{}, appwire.Errorf(appwire.CodeUncertain, "the factory made a job but did not say which; read the queue before trying again")
	}
	return h.acted(ctx, p.Factory, appwire.JobActCreate, a.ID, a), nil
}

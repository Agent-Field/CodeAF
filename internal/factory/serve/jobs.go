package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

// jobVerbs keeps canvas actions additive to the existing /v1 doors.
func (h *Handler) jobVerbs() {
	stored := h.config.Store != nil
	h.postJob("start_now", h.seam.Launch != nil, h.guarded("start_now", h.onlyOwner(h.startJob)))
	h.postJob("run_again", stored && h.seam.Launch != nil, h.guarded("run_again", h.onlyOwner(h.runAgain)))
	h.postJob("restore", stored, h.guarded("restore", h.onlyOwner(h.restoreJob)))
	h.postJob("dismiss", stored, h.dismissJob)
	h.postJob("assign_to_me", stored, h.assignToMe)
	h.postJob("change_assignee", stored, h.changeAssignee)
	h.postJob("unassign", stored, h.onlyOwner(h.unassignJob))
	h.postJob("nudge", stored, h.nudgeJob)
	h.postJob("priority", stored, h.setPriority)
	h.postJob("raise_budget", stored, h.onlyOwner(h.raiseBudget))
	h.postJob("model", stored, h.onlyOwner(h.setModel))
	h.postJob("undo", stored, h.undoJob)
}

func (h *Handler) startJob(w http.ResponseWriter, r *http.Request, it factory.Item) {
	if it.QKind == factory.QKindAskFirst {
		h.refuse(w, 409, "Approval needed: start", "conflict", "", nil, factory.JobState(it))
		return
	}
	if h.endedOnGitHub(w, r, it) {
		return
	}
	if strings.HasSuffix(r.URL.Path, "/start_now") && h.config.Store != nil {
		if h.failed(w, h.config.Store.Update(it.ID, func(cur *factory.Item) error { cur.Marked = true; return nil })) {
			return
		}
	}
	if h.config.Store != nil {
		if h.failed(w, h.ensureBudget(r, it)) {
			return
		}
	}
	h.finish(w, h.recordAction(r, it, record.Started, "", func() error { return h.seam.Launch(it.ID) }))
}

// recheckWait bounds the one GitHub read a person's start makes. It is short
// because the person is waiting on the button, and a read that runs out
// starts the job anyway.
const recheckWait = 5 * time.Second

// endedOnGitHub is the check a person's start makes on the ONE item they chose
// (start_now, run, run_again): the poll's lists are of open items only, so a
// job never learns from them that its issue was closed. When GitHub says the
// issue or pull request was closed or merged, the job is finished the way a
// pull request that ended finishes one (done, DoneHow "closed" or "merged"),
// the factory records it, and the start is refused in one sentence.
//
// STARTING NEVER DEPENDS ON GITHUB BEING UP. A read that fails, times out, or
// has no token to read with answers nothing, and the job starts on what the
// floor already knows; the poll is still the source of every other fact.
func (h *Handler) endedOnGitHub(w http.ResponseWriter, r *http.Request, it factory.Item) bool {
	if h.config.Recheck == nil || h.config.Store == nil || it.Origin != factory.OriginForge || it.Num <= 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), recheckWait)
	how, err := h.config.Recheck(ctx, it)
	cancel()
	if err != nil || (how != factorygithub.StandingClosed && how != factorygithub.StandingMerged) {
		return false
	}
	now := h.clock().UTC()
	err = h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		cur.State = factory.StateShipped
		cur.DoneHow = how
		cur.DoneAt = now
		cur.Marked = false
		cur.Question = ""
		cur.QKind = ""
		cur.StopUndo = nil
		return nil
	})
	if h.failed(w, err) {
		return true
	}
	if h.config.Record != nil {
		if _, err := h.config.Record.Append(r.Context(), record.Event{Kind: record.SourceClosed, Verb: how, Job: it.ID, Detail: "on GitHub", Cost: jobCost(it)}); h.failed(w, err) {
			return true
		}
	}
	h.refuse(w, http.StatusConflict, fmt.Sprintf("#%d was %s on GitHub, so it wasn't started.", it.Num, how), "conflict", "", nil, api.StateDone)
	return true
}

// ensureBudget takes the pipeline's own default, not a global task spending cap.
func (h *Handler) ensureBudget(r *http.Request, it factory.Item) error {
	if it.Cap > 0 {
		return nil
	}
	budget := factory.DefaultJobBudget
	if h.config.Pipeline != nil {
		f, err := h.config.Pipeline(r.Context(), itemRepo(it), "")
		if err != nil {
			return err
		}
		budget = f.DefaultBudget
	} else if h.seam.RecipeAt != nil {
		recipe, _, _, err := h.seam.RecipeAt(itemRepo(it))
		if err != nil {
			return err
		}
		if recipe.DefaultBudget > 0 {
			budget = recipe.DefaultBudget
		}
	}
	if budget <= 0 {
		return errors.New("pipeline default_budget must be positive")
	}
	return h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		if cur.Cap <= 0 {
			cur.Cap = budget
		}
		return nil
	})
}

func (h *Handler) runAgain(w http.ResponseWriter, r *http.Request, it factory.Item) {
	if factory.JobDoneHow(it) != "closed" && it.Failure == nil {
		h.refuse(w, 409, "Run again requires failed work or a PR closed without merge", "conflict", "", nil, factory.JobState(it))
		return
	}
	err := h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		cur.State = factory.StateNew
		cur.DoneHow = ""
		cur.DoneAt = time.Time{}
		cur.PullRequest = nil
		cur.Approval = nil
		cur.Failure = nil
		return nil
	})
	if h.failed(w, err) {
		return
	}
	h.startJob(w, r, it)
}

// restoreJob is POST /v1/jobs/{id}/restore, Bring back: the one way out of a
// drop. The job goes back to Backlog AS IF IT HAD NEVER RUN, and nothing
// starts; starting it is the person's own start. The drop deleted its local
// branch and folder, so its next run begins on a fresh branch, from the first
// step, with nothing spent. What the job is stays: its identity, the issue's
// own words, who it is assigned to, its priority and budget, the person's notes
// and conversation, and its activity, the history of what happened to it.
//
// THE EARLIER PULL REQUEST IS FORGOTTEN, NOT CLOSED: nothing on GitHub is
// touched. Kept, the poll would go on writing that pull request's state onto
// a Backlog job and the job would be judged by a pull request its fresh run
// never made, while that run's branch no longer holds what the pull request
// shows. Forgotten, a fresh run cannot open a second pull request beside it by
// accident either: its push to the same branch name is refused while the old
// branch is still on GitHub, because a push never forces. The receipt names
// the earlier pull request, so it is not lost from the record.
func (h *Handler) restoreJob(w http.ResponseWriter, r *http.Request, it factory.Item) {
	if factory.JobDoneHow(it) != "dropped" {
		h.refuse(w, http.StatusConflict, "only a dropped job can be brought back", "conflict", "", nil, factory.JobState(it))
		return
	}
	detail := ""
	if pr := it.PullRequest; pr != nil && pr.URL != "" {
		detail = "earlier pr · " + pr.URL
	}
	h.finish(w, h.recordAction(r, it, record.Restored, detail, func() error {
		return h.config.Store.Update(it.ID, func(cur *factory.Item) error {
			forgetRun(cur)
			return nil
		})
	}))
}

// forgetRun takes every trace of a run off the job: its state and ending, its
// steps and what they spent, its question, failure, approval and stop, its
// proof, its change and the commits and machine it was made on, and what it
// published. Everything else on the job is the job's own and stays.
func forgetRun(it *factory.Item) {
	it.State = factory.StateNew
	it.DoneHow, it.DoneAt = "", time.Time{}
	it.Stream = nil
	it.Failure = nil
	it.Approval, it.StopUndo = nil, nil
	it.Question, it.QKind = "", ""
	it.Asking, it.AnsweredQuestions = nil, nil
	it.Proof, it.Policy = nil, nil
	it.PlannedFiles = nil
	it.Change, it.ChangeBase, it.HeadCommit = nil, "", ""
	it.WorktreeMachine = ""
	it.Publish, it.Publishing, it.PublishStatus, it.PublishError = "", false, nil, ""
	it.PullRequest, it.CommentURL = nil, ""
	it.MergePolicy, it.Merging = nil, nil
}

func (h *Handler) dismissJob(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, "dismiss", body.Expect) {
		return
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	if who == "" {
		h.fail(w, 422, "Dismiss requires a GitHub login")
		return
	}
	h.finish(w, h.recordAction(r, it, record.Dismissed, "until GitHub changes", func() error {
		return h.config.Store.Update(it.ID, func(cur *factory.Item) error {
			if !factory.IsDismissed(*cur, who) {
				cur.DismissedBy = append(cur.DismissedBy, who)
			}
			return nil
		})
	}))
}

func (h *Handler) assignToMe(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, "assign_to_me", body.Expect) {
		return
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	if it.Person() != "" {
		h.taken(w, r, it)
		return
	}
	h.assignJob(w, r, it, who)
}

func (h *Handler) changeAssignee(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Assignee string      `json:"assignee"`
		Expect   *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, false) || !h.fresh(w, r, it, "change_assignee", body.Expect) || !h.assigneeOnly(w, r, it) {
		return
	}
	h.assignJob(w, r, it, factory.NormalizeHandle(body.Assignee))
}

// Reassignment belongs to the current assignee even while a job is in backlog.
func (h *Handler) assigneeOnly(w http.ResponseWriter, r *http.Request, it factory.Item) bool {
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return false
	}
	if it.Person() != "" && factory.NormalizeHandle(who) == it.Person() {
		return true
	}
	h.notJobPerson(w, it, it.Person())
	return false
}

func (h *Handler) assignJob(w http.ResponseWriter, r *http.Request, it factory.Item, who string) {
	if who == "" {
		h.fail(w, 422, "assignment requires a GitHub login")
		return
	}
	if h.config.ChangeAssignee != nil {
		if h.failed(w, h.config.ChangeAssignee(r.Context(), it, who)) {
			return
		}
	}
	if factory.JobState(it) == "running" || factory.JobState(it) == "queued" || factory.JobState(it) == "needs_you" {
		if h.failed(w, h.stopForChange(it.ID)) {
			return
		}
	}
	h.finish(w, h.recordAction(r, it, record.Assigned, "@"+who, func() error {
		return h.config.Store.Update(it.ID, func(cur *factory.Item) error {
			cur.Assignee = who
			cur.Taker = ""
			cur.Starter = ""
			cur.StopUndo = nil
			cur.Approval = nil
			return nil
		})
	}))
}

func (h *Handler) unassignJob(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, "unassign", body.Expect) || !h.assigneeOnly(w, r, it) {
		return
	}
	if h.config.ChangeAssignee != nil {
		if h.failed(w, h.config.ChangeAssignee(r.Context(), it, "")) {
			return
		}
	}
	if factory.JobState(it) == "running" || factory.JobState(it) == "queued" || factory.JobState(it) == "needs_you" {
		if h.failed(w, h.stopForChange(it.ID)) {
			return
		}
	}
	h.finish(w, h.recordAction(r, it, record.Unassigned, "", func() error {
		return h.config.Store.Update(it.ID, func(cur *factory.Item) error {
			cur.Assignee = ""
			cur.Taker = ""
			cur.Starter = ""
			cur.State = factory.StateNew
			cur.Failure = nil
			cur.PublishError = ""
			cur.Approval = nil
			cur.StopUndo = nil
			return nil
		})
	}))
}

func (h *Handler) stopForChange(id int) error {
	if h.workers != nil && h.workers.desk != nil {
		return lease.NewDoors(h.workers.desk).Reset(id)
	}
	return h.seam.Stop(id)
}

// Nudge addresses the assignee in Activity; it never steers their AI run.
func (h *Handler) nudgeJob(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Words  string      `json:"words"`
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, "nudge", body.Expect) {
		return
	}
	if it.Person() == "" {
		h.fail(w, 422, "Nudge requires an assignee")
		return
	}
	h.finish(w, h.recordAction(r, it, record.Nudged, "@"+it.Person()+" "+strings.TrimSpace(body.Words), func() error { return nil }))
}

func (h *Handler) setPriority(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Priority string      `json:"priority"`
		Expect   *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, false) || !h.fresh(w, r, it, "priority", body.Expect) {
		return
	}
	if !factory.ValidPriority(body.Priority) {
		h.fail(w, 422, "priority must be urgent, high, medium or low")
		return
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	if who == "" {
		h.fail(w, 422, "priority requires a GitHub login")
		return
	}
	h.finish(w, h.recordAction(r, it, record.PriorityChanged, body.Priority, func() error {
		return h.config.Store.Update(it.ID, func(cur *factory.Item) error {
			cur.Priority = &factory.JobPriority{Value: body.Priority, SetBy: who}
			return nil
		})
	}))
}

func (h *Handler) raiseBudget(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Budget float64     `json:"budget"`
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, false) || !h.fresh(w, r, it, "raise_budget", body.Expect) {
		return
	}
	if !slices.Contains([]float64{5, 10, 20, 30}, body.Budget) || body.Budget <= it.Cap {
		h.fail(w, 422, "raise budget to a higher limit: 5, 10, 20 or 30 USD")
		return
	}
	_, held := h.holdOf(it.ID)
	err := h.recordAction(r, it, record.BudgetRaised, fmt.Sprintf("$%.2f", body.Budget), func() error {
		return h.config.Store.Update(it.ID, func(cur *factory.Item) error {
			cur.Cap = body.Budget
			if cur.QKind == "cap" {
				cur.State = factory.StateQueued
				if held {
					cur.State = factory.StateRunning
				}
				cur.Question = ""
				cur.QKind = ""
			}
			return nil
		})
	})
	if h.failed(w, err) {
		return
	}
	if h.workers != nil && h.workers.desk != nil {
		_, err = h.workers.desk.SignalIfHeld(it.ID, lease.Signal{Verb: lease.SignalBudget, Budget: body.Budget, Yes: it.QKind == "cap"})
	}
	h.finish(w, err)
}

// jobModelMost is the longest model id a job keeps.
const jobModelMost = 200

// setModel keeps the model a person chose for one job's steps ("" for the
// factory's default) and tells the worker running it, whose next step opens on
// it and whose running step switches at its next turn. A finished job has no
// step left to run on it.
func (h *Handler) setModel(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Model  string      `json:"model"`
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, false) || !h.fresh(w, r, it, "model", body.Expect) {
		return
	}
	model := strings.TrimSpace(body.Model)
	if len(model) > jobModelMost || strings.ContainsAny(model, " \t\r\n") {
		h.fail(w, http.StatusUnprocessableEntity, "model is a model id of at most 200 characters, with no spaces")
		return
	}
	if factory.JobState(it) == api.StateDone {
		h.refuse(w, http.StatusConflict, "job is already Done", "conflict", "", nil, api.StateDone)
		return
	}
	if err := h.config.Store.Update(it.ID, func(cur *factory.Item) error { cur.Model = model; return nil }); h.failed(w, err) {
		return
	}
	var err error
	if h.workers != nil && h.workers.desk != nil {
		_, err = h.workers.desk.SignalIfHeld(it.ID, lease.Signal{Verb: lease.SignalModel, Words: model})
	}
	h.finish(w, err)
}

// Undo withdraws only this caller's pending decision, never another person's work.
func (h *Handler) undoJob(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, "undo", body.Expect) {
		return
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	now := h.clock()
	err = h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		if a := cur.Approval; a != nil && a.State == "approving" && now.Before(a.UndoUntil) && a.DecidedBy == who {
			cur.Approval = nil
			return nil
		}
		if m := cur.Merging; m != nil && m.State == factory.MergeAsked && now.Before(m.UndoUntil) && m.By == who {
			cur.Merging = nil
			return nil
		}
		if u := cur.StopUndo; u != nil && now.Before(u.Until) && u.By == who && cur.State == factory.StateNew {
			cur.Stream = u.Stream
			cur.Failure = u.Failure
			cur.Question = u.Question
			cur.QKind = u.QKind
			cur.State = u.State
			if cur.State == factory.StateRunning {
				cur.State = factory.StateQueued
			}
			cur.StopUndo = nil
			return nil
		}
		return errors.New("job is not waiting for Undo")
	})
	if err == nil && h.config.Record != nil {
		_, err = h.config.Record.Append(r.Context(), record.Event{Kind: record.Undone, Job: it.ID, Handle: who})
	}
	h.finish(w, err)
}

// approve persists a five-second decision before any runner or publish door opens.
func (h *Handler) approve(w http.ResponseWriter, r *http.Request, it factory.Item) {
	if h.config.Store == nil {
		h.commitApproval(w, r, it)
		return
	}
	var body api.Approve
	if !h.decode(w, r, &body, true) || !h.commitFresh(w, it, body.Commit) || !h.fresh(w, r, it, "approve", body.Expect) || !h.owns(w, r, it) {
		return
	}
	a := factory.JobApproval(it)
	if a == nil && factory.NeedsKind(it) == factory.NeedsPublishFailed {
		data, _ := json.Marshal(body)
		r.Body = io.NopCloser(strings.NewReader(string(data)))
		h.commitApproval(w, r, it)
		return
	}
	if a == nil || a.State != "waiting" {
		h.notWaiting(w, r, it)
		return
	}
	if a.Kind == "proof" {
		if err := run.ApprovalRefused(&it, body.Edited); err != nil {
			h.refuse(w, 409, err.Error(), "conflict", "", nil, factory.JobState(it))
			return
		}
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	if who == "" && h.mode() == api.FactorySolo {
		h.fail(w, 422, "Sign in to GitHub before approving this job")
		return
	}
	if who == "" || (h.mode() != api.FactorySolo && it.Person() == "") {
		h.fail(w, 422, "approval requires an assignee")
		return
	}
	a.State = "approving"
	a.DecidedBy = who
	a.At = h.clock().UTC()
	a.UndoUntil = a.At.Add(factory.ApprovalUndo)
	a.Commit = it.HeadCommit
	a.Edited = body.Edited
	a.Words = body.Words
	var pinned *factory.MergePolicy
	if a.Kind == "proof" {
		pinned = h.pinnedMerge(r, it)
	}
	if h.failed(w, h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		cur.Approval = a
		if a.Kind == "proof" {
			// APPROVING THE PROOF PUBLISHES, and the pull request it opens may
			// merge on its own only if the pipeline said so at this moment.
			cur.MergePolicy = pinned
		}
		return nil
	})) {
		return
	}
	h.scheduleJobTimer()
	h.write(w, 200, struct {
		Factory   api.Factory       `json:"factory"`
		Approval  *factory.Approval `json:"approval"`
		UndoUntil time.Time         `json:"undo_until"`
	}{h.factory, a, a.UndoUntil})
}

// settleJobs is run at deadlines, before reads and after a restart. Consent is
// locked across processes so two servers cannot open the same decision twice.
func (h *Handler) settleJobs(approvalsOnly ...bool) {
	if h.config.Store == nil {
		return
	}
	// One listing serves every scan for due work. Each scan only picks
	// candidates from it and reads every candidate again under its lock
	// before acting, so sharing it changes nothing but the number of times
	// every document on the floor is read before each request.
	pending, err := h.config.Store.List()
	if err != nil {
		return
	}
	sweep := len(approvalsOnly) == 0 || !approvalsOnly[0]
	h.settleMerges(sweep, pending)
	due := false
	for _, it := range pending {
		if a := it.Approval; a != nil && a.State == "approving" && !h.clock().Before(a.UndoUntil) {
			due = true
			break
		}
	}
	if !due {
		return
	}
	unlock, err := h.lockConsent()
	if err != nil {
		return
	}
	defer unlock()
	items, err := h.config.Store.List()
	if err != nil {
		return
	}
	for _, it := range items {
		a := it.Approval
		if a == nil || a.State != "approving" || h.clock().Before(a.UndoUntil) {
			continue
		}
		raw, _ := json.Marshal(api.Approve{Commit: a.Commit, Edited: a.Edited, Words: a.Words})
		r, _ := http.NewRequestWithContext(context.WithValue(context.Background(), workerKey{}, lease.Worker{Handle: a.DecidedBy}), http.MethodPost, "/v1/jobs/approve", strings.NewReader(string(raw)))
		r.SetPathValue("id", strconv.Itoa(it.ID))
		w := &replayWriter{header: make(http.Header)}
		h.commitApproval(w, r, it)
		_ = h.config.Store.Update(it.ID, func(cur *factory.Item) error {
			if cur.Approval == nil || !cur.Approval.At.Equal(a.At) {
				return nil
			}
			cp := *cur.Approval
			if w.status() < 300 {
				cp.State = "approved"
			} else {
				cp.State = "waiting"
				var refusal refusalBody
				_ = json.Unmarshal(w.body.Bytes(), &refusal)
				cp.Error = refusal.Error
			}
			cur.Approval = &cp
			return nil
		})
	}
}

func (h *Handler) scheduleJobTimer() {
	if h.config.Store == nil {
		return
	}
	items, err := h.config.Store.List()
	if err != nil {
		return
	}
	var next time.Time
	for _, it := range items {
		if a := it.Approval; a != nil && a.State == "approving" && (next.IsZero() || a.UndoUntil.Before(next)) {
			next = a.UndoUntil
		}
		if m := it.Merging; m != nil && m.State == factory.MergeAsked && (next.IsZero() || m.UndoUntil.Before(next)) {
			next = m.UndoUntil
		}
	}
	h.jobsMu.Lock()
	defer h.jobsMu.Unlock()
	if h.jobsClosed {
		return
	}
	if h.jobsTimer != nil {
		h.jobsTimer.Stop()
	}
	if !next.IsZero() {
		h.jobsTimer = time.AfterFunc(max(next.Sub(h.clock()), time.Millisecond), func() { h.settleJobs(); h.scheduleJobTimer() })
	}
}

func approvalWords(it factory.Item) string {
	if it.Approval != nil && it.Approval.Words != "" {
		return it.Approval.Words
	}
	return "yes"
}

// A plain question is answered once. The second writer receives the exact
// collision copy, even when they did not include a document revision.
func (h *Handler) questionCollision(w http.ResponseWriter, r *http.Request, it factory.Item, id string, exp *api.Expect) bool {
	if len(it.AnsweredQuestions) == 0 {
		return false
	}
	q := it.AnsweredQuestions[len(it.AnsweredQuestions)-1]
	if q.Answer == nil || q.Kind == factory.QKindApprove || q.Kind == factory.QKindAskFirst {
		return false
	}
	if it.Question != "" && (id == "" || id != fmt.Sprintf("q%d", it.ID)) && (exp == nil || exp.Version == nil || *exp.Version == h.withVersion(api.FromItem(it)).Version) {
		return false
	}
	elapsed := max(0, int(h.clock().Sub(q.Answer.At).Seconds()))
	h.refuse(w, 409, fmt.Sprintf("@%s answered this %ds ago. Your answer wasn't sent.", q.Answer.By, elapsed), "already_answered", "@"+q.Answer.By, &q.Answer.At, factory.JobState(it))
	return true
}

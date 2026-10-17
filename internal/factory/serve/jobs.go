package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

// jobVerbs keeps canvas actions additive to the existing /v1 doors.
func (h *Handler) jobVerbs() {
	stored := h.config.Store != nil
	h.postJob("start_now", h.seam.Launch != nil, h.guarded("start_now", h.onlyOwner(h.startJob)))
	h.postJob("run_again", stored && h.seam.Launch != nil, h.guarded("run_again", h.onlyOwner(h.runAgain)))
	h.postJob("dismiss", stored, h.dismissJob)
	h.postJob("assign_to_me", stored, h.assignToMe)
	h.postJob("change_assignee", stored, h.changeAssignee)
	h.postJob("unassign", stored, h.onlyOwner(h.unassignJob))
	h.postJob("nudge", stored, h.nudgeJob)
	h.postJob("priority", stored, h.setPriority)
	h.postJob("raise_budget", stored, h.onlyOwner(h.raiseBudget))
	h.postJob("undo", stored, h.undoJob)
}

func (h *Handler) startJob(w http.ResponseWriter, r *http.Request, it factory.Item) {
	if it.QKind == factory.QKindAskFirst {
		h.refuse(w, 409, "Approval needed: start", "conflict", "", nil, factory.JobState(it))
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
	if len(approvalsOnly) == 0 || !approvalsOnly[0] {
		h.pruneDropped()
	}
	h.settleMerges(len(approvalsOnly) == 0 || !approvalsOnly[0])
	pending, err := h.config.Store.List()
	if err != nil {
		return
	}
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
		if h.config.DeleteBranch != nil && it.DoneHow == "dropped" && it.BranchDeletedAt.IsZero() && !it.BranchDeleteAt.IsZero() && (next.IsZero() || it.BranchDeleteAt.Before(next)) {
			next = it.BranchDeleteAt
			if !next.After(h.clock()) {
				next = h.clock().Add(time.Minute)
			}
		}
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

// pruneDropped uses its own per-job lock. A slow GitHub cleanup must never
// hold the consent lock or consume another person's five-second Undo window.
func (h *Handler) pruneDropped() {
	if h.config.DeleteBranch == nil || h.config.Record == nil {
		return
	}
	items, err := h.config.Store.List()
	if err != nil {
		return
	}
	for _, it := range items {
		if it.DoneHow != "dropped" || !it.BranchDeletedAt.IsZero() || it.BranchDeleteAt.IsZero() || h.clock().Before(it.BranchDeleteAt) {
			continue
		}
		h.pruneBranch(it.ID)
	}
}

func (h *Handler) pruneBranch(id int) {
	f, err := os.OpenFile(filepath.Join(h.config.Store.Root(), "retention-"+strconv.Itoa(id)+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	if err := filelock.Lock(f, true, true); err != nil {
		return
	}
	defer filelock.Unlock(f)
	it, err := h.config.Store.Get(id)
	if err != nil || it.DoneHow != "dropped" || !it.BranchDeletedAt.IsZero() {
		return
	}
	events, err := h.config.Record.Since(context.Background(), time.Time{})
	if err != nil {
		return
	}
	var deleted time.Time
	for _, e := range events {
		if e.Job == id && e.Kind == record.BranchDeleted {
			deleted = e.At
			break
		}
	}
	if deleted.IsZero() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := h.config.DeleteBranch(ctx, it)
		cancel()
		if err != nil {
			return
		}
		e, err := h.config.Record.Append(context.Background(), record.Event{Kind: record.BranchDeleted, Job: id, Detail: factory.JobRef(it)})
		if err != nil {
			return
		}
		deleted = e.At
	}
	_ = h.config.Store.Update(id, func(cur *factory.Item) error { cur.BranchDeletedAt = deleted; return nil })
}

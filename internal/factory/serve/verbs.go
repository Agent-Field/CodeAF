package serve

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func (h *Handler) verbs() {
	s := h.seam
	h.postJob("run", s.Launch != nil, h.guarded("run", h.onlyOwner(h.startJob)))
	h.postJob("stop", s.Stop != nil, h.guarded("stop", h.stop))
	h.postJob("pause", s.Hold != nil, h.guarded("pause", h.holdVerb(true)))
	h.postJob("resume", s.Hold != nil, h.guarded("resume", h.holdVerb(false)))
	h.postJob("drop", h.config.Store != nil || s.Dismiss != nil, h.onlyOwner(h.drop))
	h.postJob("answer", s.Answer != nil, h.answer)
	h.postJob("reject_plan", h.config.Store != nil && s.Answer != nil, h.rejectPlan)
	h.postJob("steer", s.Steer != nil, h.wordsVerb("steer", s.Steer))
	h.postJob("approve", true, h.approve)
	h.postJob("retry_publish", h.config.Store != nil && h.seam.SignOff != nil, h.guarded("retry_publish", h.onlyOwner(h.retryPublish)))
	h.postJob("merge", h.config.Store != nil, h.onlyOwner(h.mergeJob))
	h.postJob("request_changes", s.SendBack != nil, h.recordedWords(s.SendBack, record.RequestChanges))
	h.postJob("recheck", s.Reverify != nil, h.guarded("recheck", h.idVerb(s.Reverify)))
	// Take and release are the lease desk's own doors, not seam fields. The
	// desk exists only after the worker routes open it, so the handler checks then.
	h.postJob("take", true, h.take)
	h.postJob("release", true, h.release)
	h.postJob("prioritize", s.Mark != nil, h.prioritize)
	h.postJob("reply", h.config.Store != nil || s.Say != nil, h.reply)
	h.route("/v1/jobs", map[string]http.HandlerFunc{http.MethodPost: h.newJob})
	h.route("/v1/github/connect", map[string]http.HandlerFunc{http.MethodPost: h.connectGitHub})
}

type jobAction func(http.ResponseWriter, *http.Request, factory.Item)

func (h *Handler) postJob(verb string, exists bool, action jobAction) {
	h.route("/v1/jobs/{id}/"+verb, map[string]http.HandlerFunc{
		http.MethodPost: func(w http.ResponseWriter, r *http.Request) {
			if verb == "sync" || verb == "approve" || verb == "retry_publish" || verb == "merge" || verb == "answer" || verb == "reject_plan" || verb == "undo" || verb == "assign_to_me" || verb == "change_assignee" || verb == "assignee" || verb == "unassign" {
				unlock, err := h.lockConsent()
				if h.failed(w, err) {
					return
				}
				defer unlock()
			}
			if !h.requireVerb(w, exists) {
				return
			}
			it, ok := h.findJob(w, r)
			if !ok {
				return
			}
			if it.DoneHow == "closed_locally" && verb != "reopen" && verb != "close" {
				h.refuse(w, 409, "Job closed locally; Reopen keeps its work and spending.", "conflict", "", nil, "done")
				return
			}
			// THE ONE PLACE A PERSON'S DOOR MEETS A REPOSITORY THAT IS OFF:
			// every door that would work on the job is refused here, before
			// the door reads its body or writes anything (repo_off.go).
			if offVerbs[verb] && h.refuseOff(w, factory.RepoOf(it), factory.JobState(it)) {
				return
			}
			action(w, r, it)
		},
	})
}

// take is POST /v1/jobs/{id}/take. It names the connected person on the job
// and leaves the work queued. A job the person rule already gave to someone
// else is refused in a sentence, and the refusal is not recorded.
func (h *Handler) take(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, "take", body.Expect) || !h.workerReady(w) {
		return
	}
	me, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	if me == "" {
		h.fail(w, http.StatusUnprocessableEntity, "take requires a GitHub login")
		return
	}
	if err := h.workers.desk.Assign(it.ID, me); h.takeFailed(w, r, it, err) {
		return
	}
	if h.config.Record != nil {
		if _, err := h.config.Record.Append(r.Context(), record.Event{Kind: record.Took, Job: it.ID, Handle: me, Cost: jobCost(it)}); h.failed(w, err) {
			return
		}
	}
	updated, err := h.workers.st.Get(it.ID)
	if h.failed(w, err) {
		return
	}
	job := h.withVersion(api.FromItem(updated))
	job.Can = api.CanFor(updated, me, h.clock())
	if h.mode() != api.FactorySolo && h.isOperator(r) {
		api.AsOperator(&job.Can, updated, me)
	}
	h.presentJob(r, &job)
	h.write(w, http.StatusOK, struct {
		Factory api.Factory `json:"factory"`
		api.Job
	}{h.factory, job})
}

// release is POST /v1/jobs/{id}/release. The person who took the job lets it
// go. The branch stays, and a worker's hold is not cleared here.
func (h *Handler) release(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, "release", body.Expect) || !h.workerReady(w) {
		return
	}
	me, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	if me == "" {
		h.fail(w, http.StatusUnprocessableEntity, "release requires a GitHub login")
		return
	}
	if err := h.workers.desk.Release(it.ID, me); h.releaseFailed(w, it, err) {
		return
	}
	if h.config.Record != nil {
		if _, err := h.config.Record.Append(r.Context(), record.Event{Kind: record.Released, Job: it.ID, Handle: me, Cost: jobCost(it)}); h.failed(w, err) {
			return
		}
	}
	updated, err := h.workers.st.Get(it.ID)
	if h.failed(w, err) {
		return
	}
	job := h.withVersion(api.FromItem(updated))
	job.Can = api.CanFor(updated, me, h.clock())
	if h.mode() != api.FactorySolo && h.isOperator(r) {
		api.AsOperator(&job.Can, updated, me)
	}
	h.presentJob(r, &job)
	h.write(w, http.StatusOK, struct {
		Factory api.Factory `json:"factory"`
		api.Job
	}{h.factory, job})
}

// takeFailed maps the person rule and a live worker hold onto 409. The
// sentence is what the page shows; the lease package prefix stays off it.
func (h *Handler) takeFailed(w http.ResponseWriter, r *http.Request, it factory.Item, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, lease.ErrPerson) || errors.Is(err, lease.ErrLeased) {
		h.taken(w, r, it)
		return true
	}
	if errors.Is(err, store.ErrNotFound) {
		h.fail(w, http.StatusNotFound, "job not found")
		return true
	}
	return h.failed(w, err)
}

func (h *Handler) releaseFailed(w http.ResponseWriter, it factory.Item, err error) bool {
	if err == nil {
		return false
	}
	state := h.withVersion(api.FromItem(it)).State
	switch {
	case errors.Is(err, lease.ErrNotTaken):
		h.refuse(w, http.StatusConflict, "job is not taken", "conflict", "", nil, state)
		return true
	case errors.Is(err, lease.ErrPerson):
		who := it.Taker
		if who == "" {
			who = it.Person()
		}
		h.refuse(w, http.StatusForbidden, "only @"+who+" can release this job", "not_allowed", "@"+who, nil, state)
		return true
	case errors.Is(err, store.ErrNotFound):
		h.fail(w, http.StatusNotFound, "job not found")
		return true
	default:
		return h.failed(w, err)
	}
}

func (h *Handler) taken(w http.ResponseWriter, r *http.Request, it factory.Item) {
	state := h.withVersion(api.FromItem(it)).State
	who := it.Person()
	if who == "" {
		h.refuse(w, http.StatusConflict, "job is already taken", "conflict", "", nil, state)
		return
	}
	h.takenConflict(w, r, it, "already taken by @"+who, "conflict", state)
}

func (h *Handler) idVerb(door func(int) error) jobAction {
	return func(w http.ResponseWriter, r *http.Request, it factory.Item) {
		h.finish(w, door(it.ID))
	}
}

// holdVerb is pause (on) and resume. A hold of a queued job, or the end of
// one, is a decision on the queue that only the job's person makes, and it is
// recorded with who made it.
func (h *Handler) holdVerb(on bool) jobAction {
	return func(w http.ResponseWriter, r *http.Request, it factory.Item) {
		if it.State != factory.StateQueued {
			h.finish(w, h.seam.Hold(it.ID, on))
			return
		}
		if !h.owns(w, r, it) {
			return
		}
		kind := record.Held
		if !on {
			kind = record.Resumed
		}
		h.finish(w, h.recordAction(r, it, kind, "", func() error { return h.seam.Hold(it.ID, on) }))
	}
}

func (h *Handler) wordsVerb(verb string, door func(int, string) error) jobAction {
	return func(w http.ResponseWriter, r *http.Request, it factory.Item) {
		if words, ok := h.words(w, r, it, verb); ok {
			h.finish(w, door(it.ID, words))
		}
	}
}

func (h *Handler) answer(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		QuestionID string      `json:"question_id,omitempty"`
		Commit     string      `json:"commit"`
		Yes        *bool       `json:"yes"`
		Words      string      `json:"words"`
		Choice     string      `json:"choice"`
		Reason     string      `json:"reason"`
		Expect     *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, false) || h.questionCollision(w, r, it, body.QuestionID, body.Expect) || !h.fresh(w, r, it, "answer", body.Expect) {
		return
	}
	yes, words, ok := h.applyChoice(w, body.Yes, body.Words, body.Choice, body.Reason)
	if !ok {
		return
	}
	if yes && h.config.Store != nil && (it.QKind == factory.QKindApprove || it.QKind == factory.QKindAskFirst) {
		data, _ := json.Marshal(api.Approve{Commit: body.Commit, Words: body.Words, Expect: body.Expect})
		r.Body = io.NopCloser(bytes.NewReader(data))
		h.approve(w, r, it)
		return
	}
	if yes && it.QKind == factory.QKindApprove && !h.commitFresh(w, it, body.Commit) {
		return
	}
	if !h.mayAnswer(w, r, it) {
		return
	}
	if factory.IsContinue(yes, words) && it.QKind == "cap" && h.config.Store != nil {
		now := time.Now()
		if h.workers != nil {
			now = h.workers.now()
		}
		if err := h.config.Store.Update(it.ID, func(item *factory.Item) error {
			run.NoteContinue(item, now)
			return nil
		}); h.failed(w, err) {
			return
		}
	}
	err := h.recordAnswer(r, it, yes, words)
	if err != nil && factory.IsContinue(yes, words) && it.QKind == "cap" && h.config.Store != nil && strings.Contains(err.Error(), "is not waiting on you") {
		if qerr := h.releaseCapWait(it.ID); h.failed(w, qerr) {
			return
		}
		h.finish(w, h.recordContinued(r, it, words))
		return
	}
	h.finish(w, err)
}

// applyChoice maps the wire's choice onto yes and words. send_back requires
// a reason. An unknown choice is refused. yes or words alone still answer.
func (h *Handler) applyChoice(w http.ResponseWriter, yes *bool, words, choice, reason string) (bool, string, bool) {
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "":
	case factory.ChoiceContinue:
		t := true
		yes = &t
		if strings.TrimSpace(words) == "" {
			words = factory.ChoiceContinue
		}
	case factory.ChoiceTryOnceMore:
		t := true
		yes = &t
		if strings.TrimSpace(words) == "" {
			words = factory.ChoiceTryOnceMore
		}
	case factory.ChoiceCancel, "stop":
		f := false
		yes = &f
		if strings.TrimSpace(words) == "" {
			words = factory.ChoiceCancel
		}
	case factory.ChoiceSendBack:
		why := strings.TrimSpace(reason)
		if why == "" {
			why = factory.SendBackReason(words)
		}
		if why == "" {
			h.fail(w, http.StatusUnprocessableEntity, "send back requires a reason")
			return false, "", false
		}
		f := false
		yes = &f
		words = factory.ChoiceSendBack + ": " + why
	default:
		h.fail(w, http.StatusUnprocessableEntity, "choice must be continue, try_once_more, send_back or cancel_job")
		return false, "", false
	}
	if yes == nil && factory.IsContinue(false, words) {
		t := true
		yes = &t
	}
	if yes == nil && strings.TrimSpace(words) == "" {
		h.fail(w, http.StatusUnprocessableEntity, "answer requires yes or words")
		return false, "", false
	}
	return yes != nil && *yes, words, true
}

// mayAnswer refuses a job-person kind when the caller is not that person.
// A job with no person yet, and a free question, are not this check.
func (h *Handler) mayAnswer(w http.ResponseWriter, r *http.Request, it factory.Item) bool {
	if h.mode() == api.FactorySolo {
		return true
	}
	if factory.WaitingApprover(it) != "" {
		return h.approves(w, r, it)
	}
	if !factory.JobPersonOnly(factory.NeedsKind(it)) {
		return true
	}
	who := it.Person()
	if who == "" {
		return true
	}
	handle, err := h.callerHandle(r)
	if h.failed(w, err) {
		return false
	}
	if factory.NormalizeHandle(handle) == who {
		return true
	}
	h.writeRefusal(w, http.StatusForbidden, refusalBody{
		Error: "only @" + who + " can answer this", Reason: factory.ReasonNotJobPerson,
		By: "@" + who, JobPerson: "@" + who, NowState: h.withVersion(api.FromItem(it)).State,
	})
	return false
}

// approves is [Handler.owns] for an approval: when the approval names who it
// waits on (factory.WaitingApprover), that person, or any operator for the
// operator role, and nobody else, the job's person included; otherwise the
// job's person. A solo factory has one person, who answers everything.
func (h *Handler) approves(w http.ResponseWriter, r *http.Request, it factory.Item) bool {
	if h.mode() == api.FactorySolo {
		return true
	}
	who := factory.WaitingApprover(it)
	if who == "" {
		return h.owns(w, r, it)
	}
	handle, err := h.callerHandle(r)
	if h.failed(w, err) {
		return false
	}
	if factory.MayApprove(it, handle, h.isOperator(r)) {
		return true
	}
	words := "only " + who + " can approve this"
	if who == factory.ApproverOperator {
		words = "only an operator of this factory can approve this"
	}
	h.writeRefusal(w, http.StatusForbidden, refusalBody{
		Error: words, Reason: factory.ReasonNotApprover, By: who, NowState: h.withVersion(api.FromItem(it)).State,
	})
	return false
}

// releaseCapWait clears a spending question nobody is holding and queues the
// job, so continue still raises the cap when KeepAnswer would refuse it.
func (h *Handler) releaseCapWait(id int) error {
	return h.config.Store.Update(id, func(it *factory.Item) error {
		it.Question, it.QKind = "", ""
		it.State = factory.StateQueued
		if s := it.Stream; s != nil && s.Cur >= 0 && s.Cur < len(s.Phases) && s.Phases[s.Cur].State == factory.PhaseWaiting {
			s.Phases[s.Cur].State = factory.PhaseRunning
		}
		return nil
	})
}

func (h *Handler) recordContinued(r *http.Request, it factory.Item, words string) error {
	if h.config.Record == nil {
		return nil
	}
	handle, err := h.callerHandle(r)
	if err != nil {
		return err
	}
	_, err = h.config.Record.Append(r.Context(), record.Event{Kind: record.Answered, Job: it.ID, Handle: handle, Cost: jobCost(it), Detail: words})
	return err
}

func (h *Handler) commitApproval(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body api.Approve
	if !h.decode(w, r, &body, true) || !h.commitFresh(w, it, body.Commit) || !h.fresh(w, r, it, "approve", body.Expect) || h.failed(w, h.approvalIdentity(r)) {
		return
	}
	if !h.owns(w, r, it) {
		return
	}
	// A CLAIM THE DOOR WOULD REFUSE NEVER BECOMES A RECEIPT. The check runs
	// first. The publish door then reads that receipt as its trailer, so the
	// write sits between the check and the door. A plan approval is an
	// answer: the receipt is written only after that door accepts.
	if it.State == factory.StateLanded {
		if !h.requireVerb(w, h.seam.SignOff != nil) {
			return
		}
		if err := run.ApprovalRefused(&it, body.Edited); err != nil {
			h.refuse(w, http.StatusConflict, err.Error(), "conflict", "", nil, h.withVersion(api.FromItem(it)).State)
			return
		}
		// A PUBLISH THAT FAILED KEEPS THE APPROVAL ALREADY RECORDED. Retry
		// publishes that same commit and does not write a second receipt.
		if factory.NeedsKind(it) == factory.NeedsPublishFailed && h.config.Record != nil {
			trailer, err := h.config.Record.Trailer(r.Context(), it.ID)
			if h.failed(w, err) {
				return
			}
			if run.ValidTrailer(trailer) {
				if _, err = h.seam.SignOff(it.ID, body.Edited); h.failed(w, err) {
					return
				}
				h.writeApproval(w, approvalEventID(trailer))
				return
			}
		}
		event, err := h.recordApproval(r, it, body.Edited)
		if h.failed(w, err) {
			return
		}
		if _, err = h.seam.SignOff(it.ID, body.Edited); h.changedApproval(w, r, err) || h.failed(w, err) {
			return
		}
		h.wroteApproval(w, r, it, event)
		return
	}
	if it.State == factory.StateNeedsYou && (it.QKind == factory.QKindApprove || it.QKind == factory.QKindAskFirst) {
		if !h.requireVerb(w, h.seam.Answer != nil) {
			return
		}
		if h.failed(w, h.seam.Answer(it.ID, true, body.Words)) {
			return
		}
		event, err := h.recordApproval(r, it, body.Edited)
		if h.failed(w, err) {
			return
		}
		h.wroteApproval(w, r, it, event)
		return
	}
	h.notWaiting(w, r, it)
}

// approvalIdentity resolves the caller before any receipt is written.
func (h *Handler) approvalIdentity(r *http.Request) error {
	_, err := h.callerHandle(r)
	return err
}

func (h *Handler) wroteApproval(w http.ResponseWriter, r *http.Request, it factory.Item, event string) {
	if it.Question != "" {
		handle, err := h.callerHandle(r)
		if h.failed(w, err) {
			return
		}
		at := time.Now()
		if h.workers != nil {
			at = h.workers.now()
		}
		if h.failed(w, h.keepAnswered(it, factory.Answer{By: handle, Text: approvalWords(it), At: at, Event: event})) {
			return
		}
	}
	h.writeApproval(w, event)
}

func (h *Handler) writeApproval(w http.ResponseWriter, event string) {
	h.write(w, http.StatusOK, struct {
		Factory api.Factory `json:"factory"`
		Event   string      `json:"event"`
	}{h.factory, event})
}

func (h *Handler) notWaiting(w http.ResponseWriter, r *http.Request, it factory.Item) {
	job := h.withVersion(api.FromItem(it))
	if job.State == api.StateDone || it.Publishing || it.State == factory.StateShipped {
		sentence, by, at, eventID, commit := h.approvedSentence(r, it)
		h.writeRefusal(w, http.StatusConflict, refusalBody{
			Error: sentence, Reason: "conflict", By: by, At: at, NowState: job.State,
			ApprovedBy: by, Event: eventID, Commit: commit,
		})
		return
	}
	h.refuse(w, http.StatusConflict, "job is not waiting for approval", "conflict", "", nil, job.State)
}

// drop is POST /v1/jobs/{id}/drop: the job is stopped, taken off the list,
// and EVERYTHING OF ITS WORK ON THIS MACHINE IS THROWN AWAY: the worktree
// folder and the local branch the runner made for it ([JobFolders]), and its
// conversations, its agent's and every step's of every round, with the crew
// the factory made for it ([Config.DeleteChats]). NOTHING ON GITHUB IS
// TOUCHED: a pushed branch and an opened pull request stay where they are.
// Bring back is then simply the job in Backlog again, fresh.
//
// A DROP NEVER LOSES WORK UNASKED. When the branch has commits of its own, or
// the folder holds uncommitted changes, the drop is refused with 409 has_work
// and a sentence naming them; the same drop with `"discard": true` is the
// person's yes. Work that could not be read counts as work. What was said in
// the job's conversations is not asked about: dropping the job is dropping
// what was said about it.
//
// THE JOB IS DROPPED EVEN WHEN A DELETE FAILS: what was not deleted is said
// on the job's activity, and the drop answers as done. The answer waits for
// the deletes, which wait for a step still running here to stop.
func (h *Handler) drop(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Discard bool        `json:"discard,omitempty"`
		Expect  *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, "drop", body.Expect) {
		return
	}
	if h.config.Store != nil && factory.JobState(it) == "done" {
		h.refuse(w, 409, "job is already Done", "conflict", "", nil, "done")
		return
	}
	detail := ""
	if folders := h.config.Folders; folders != nil {
		work, err := folders.Work(it)
		lost := ""
		switch {
		case err != nil:
			lost = factory.JobRef(it) + "'s branch could not be read, so what it holds is unknown."
		case work.Any():
			lost = factory.JobRef(it) + "'s branch has " + work.String() + "."
		}
		if lost != "" && !body.Discard {
			h.refuse(w, http.StatusConflict, lost, api.ReasonHasWork, "", nil, factory.JobState(it))
			return
		}
		if lost != "" {
			detail = "discarded: " + lost
		}
	}
	err := h.dropJob(r, it, detail)
	if err == nil {
		h.throwAway(it, "dropped")
	}
	h.finish(w, err)
}

// dropJob stops the job where it runs and marks it dropped, with its receipt.
// A RUNNING JOB IS STOPPED FIRST, so the folder it works in and the
// conversations it runs in are let go of before [Handler.throwAway] deletes
// them.
func (h *Handler) dropJob(r *http.Request, it factory.Item, detail string) error {
	if h.config.Store == nil {
		if h.workers != nil && h.workers.desk != nil {
			if _, err := h.workers.desk.SignalIfHeld(it.ID, lease.Signal{Verb: lease.SignalStop}); err != nil {
				return err
			}
		}
		return h.recordAction(r, it, record.Dropped, detail, func() error { return h.seam.Dismiss(it.ID) })
	}
	now := h.clock().UTC()
	switch factory.JobState(it) {
	case "running", "queued", "needs_you":
		if err := h.stopForChange(it.ID); err != nil {
			return err
		}
	}
	return h.recordAction(r, it, record.Dropped, detail, func() error {
		return h.config.Store.Update(it.ID, func(cur *factory.Item) error {
			cur.State = factory.StateDismissed
			cur.DoneHow = "dropped"
			cur.DoneAt = now
			cur.Approval = nil
			cur.StopUndo = nil
			cur.Question = ""
			cur.QKind = ""
			cur.Failure = nil
			cur.PublishError = ""
			cur.Publishing = false
			return nil
		})
	})
}

// throwAway deletes a dropped job's work on this machine: first its local
// branch and folder, then its conversations and its crew. Drop calls it once
// the job is dropped, and Bring back again before the job is reset, for
// whatever came back since and for a job dropped before a drop deleted
// conversations. EXACTLY THE JOB'S OWN, BY WHAT THE FACTORY RECORDED FOR THIS
// ONE JOB: its branch and folder by its change and number, its conversations
// and crew by the links on it. Both deletes find nothing to do the second
// time, so calling it again is safe.
//
// THE FOLDER GOES FIRST because its delete waits for this machine's runner to
// let go of the job; once it has, no step records another conversation on
// the job, so the links read next are the last ones. Both are read from the
// job again, so what the run recorded since the drop was asked for is
// deleted too.
//
// THE LINKS GO BEFORE THE CONVERSATIONS, in the one write that reads them
// ([forgetChats]), and the conversations are deleted by the job as it stood.
// So a delete that fails leaves nothing pointing at a conversation half gone,
// and nothing of it reaches a fresh run; what was kept is said on the job's
// activity after lead ("dropped", "brought back"), and the drop or the Bring
// back stands.
func (h *Handler) throwAway(it factory.Item, lead string) {
	if h.config.Folders != nil {
		if h.config.Store != nil {
			if cur, err := h.config.Store.Get(it.ID); err == nil {
				it = cur
			}
		}
		if err := h.config.Folders.Discard(it); err != nil {
			h.noteKept(it.ID, lead+" · the local branch and folder were kept: "+err.Error())
		}
	}
	if h.config.DeleteChats == nil || h.config.Store == nil {
		return
	}
	var before factory.Item
	err := h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		before = *cur
		forgetChats(cur)
		return nil
	})
	if err != nil {
		// The job still names its conversations, so they are kept for a
		// later Bring back to delete rather than deleted under a link.
		h.noteKept(it.ID, lead+" · its conversations were not deleted: "+err.Error())
		return
	}
	if err := h.config.DeleteChats(before); err != nil {
		h.noteKept(it.ID, lead+" · its conversations were not all deleted: "+err.Error())
	}
}

// noteKept says on the job's activity what a delete kept. A factory with no
// store of its own has no activity to say it on.
func (h *Handler) noteKept(id int, what string) {
	if h.config.Store == nil {
		return
	}
	at := h.clock().UTC()
	_ = h.config.Store.Update(id, func(cur *factory.Item) error {
		cur.Note(at, what)
		return nil
	})
}

func (h *Handler) prioritize(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		To     string      `json:"to"`
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, false) || !h.fresh(w, r, it, "prioritize", body.Expect) {
		return
	}
	if body.To != "top" {
		h.fail(w, http.StatusUnprocessableEntity, "prioritize requires to: top")
		return
	}
	h.finish(w, h.seam.Mark([]int{it.ID}, true))
}

func (h *Handler) reply(w http.ResponseWriter, r *http.Request, it factory.Item) {
	if words, ok := h.words(w, r, it, "reply"); ok {
		if h.config.Store != nil {
			if !h.workerReady(w) {
				return
			}
			from, err := h.callerHandle(r)
			if h.failed(w, err) {
				return
			}
			if from == "" {
				from = "user"
			}
			h.finish(w, h.workers.desk.Reply(it.ID, from, words))
			return
		}
		h.finish(w, h.seam.Say(r.Context(), it.ID, words))
	}
}

func (h *Handler) newJob(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.seam.New != nil) {
		return
	}
	var body api.NewJob
	if !h.decode(w, r, &body, false) {
		return
	}
	if strings.TrimSpace(body.Repo) == "" || strings.TrimSpace(body.Title) == "" {
		h.fail(w, http.StatusUnprocessableEntity, "new job requires repo and title")
		return
	}
	switch body.Source {
	case "", factory.AskApp, factory.AskTUI, "hand-off":
	default:
		h.fail(w, 422, "source must be app, tui or hand-off")
		return
	}
	if body.Source == "hand-off" {
		if body.FromRef == nil || body.FromRef.Kind != "agent" || strings.TrimSpace(body.FromRef.ID) == "" || strings.TrimSpace(body.FromRef.Title) == "" {
			h.fail(w, 422, "hand-off requires from_ref with agent kind, id and title")
			return
		}
	} else if body.FromRef != nil {
		h.fail(w, 422, "from_ref requires source hand-off")
		return
	}
	if body.Start != "" && body.Autostart {
		h.fail(w, 422, "start and autostart cannot be combined")
		return
	}
	switch body.Start {
	case "", "backlog":
	case "now", "queued":
		body.Autostart = true
	default:
		h.fail(w, 422, "start must be now, queued or backlog")
		return
	}
	if (body.Source != "" || body.FromRef != nil || body.Assignee != nil || body.Start != "") && !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	if body.Assignee != nil {
		*body.Assignee = factory.NormalizeHandle(*body.Assignee)
		if body.Autostart && *body.Assignee == "" && h.mode() == api.FactoryTeam {
			h.fail(w, 422, "starting a team job requires an assignee")
			return
		}
	}
	visible := h.repoVisible(r, body.Repo)
	if h.authFailed(w, r) {
		return
	}
	if !visible {
		h.fail(w, http.StatusNotFound, "repository not found")
		return
	}
	// A JOB IS NEVER MADE ON A REPOSITORY THAT IS OFF: it would leave the
	// queue the moment it was made, and its start would be refused after
	// the job was already there (repo_off.go).
	if h.refuseOff(w, body.Repo, "") {
		return
	}
	if body.Autostart && !h.requireVerb(w, h.seam.Launch != nil) {
		return
	}
	// NOR IS ONE MADE TO START THAT COULD NOT: a new job started at once on a
	// repository with no checkout that cannot be cloned is refused before it
	// exists, as its own Start would be ([Handler.needsCheckout]).
	if body.Autostart && h.needsCheckout(w, r, body.Repo, "") {
		return
	}
	if body.Pipeline == "pr" || body.Pipeline == "ci" {
		h.fail(w, 422, "factory jobs support the issue pipeline only")
		return
	}
	var selected []factory.Stage
	// copied is the factory's copy the read of the selected steps used, nil
	// for main's file: the job's file is decided with them
	// ([factory.Item.DecidePipeline]), so its start runs the file its steps
	// came from.
	var copied *factory.PipelineCopy
	decided := false
	if body.Pipeline != "" && body.Pipeline != "default" {
		if !h.requireVerb(w, h.config.Store != nil && h.seam.RecipeAt != nil) {
			return
		}
		recipe, _, dir, err := h.seam.RecipeAt(body.Repo)
		if h.failed(w, err) {
			return
		}
		if h.config.Pipeline != nil {
			f, err := h.config.Pipeline(r.Context(), body.Repo, dir)
			if h.failed(w, err) {
				return
			}
			recipe = f.Recipe()
			copied, decided = f.Copy, true
		}
		selected = recipe.ByKind[factory.Kind(body.Pipeline)]
		if body.Pipeline == "issue" {
			selected = recipe.For(factory.KindIssue)
		}
		if len(selected) == 0 {
			h.fail(w, 422, "pipeline not found")
			return
		}
	}
	var issue api.Issue
	if body.Issue {
		if !h.requireVerb(w, h.config.Store != nil) {
			return
		}
		assignee := ""
		if body.Assignee != nil {
			assignee = *body.Assignee
		}
		var ok bool
		issue, ok = h.openIssue(w, r, body.Repo, body.Title, body.Text, assignee)
		if !ok {
			return
		}
	}
	// New has one words argument today. Join the title and brief here so a
	// wire client does not need to know the seam's terminal-input shape.
	words := body.Title
	if body.Text != "" {
		words += "\n\n" + body.Text
	}
	id, err := h.seam.New(body.Repo, words)
	if h.failed(w, err) {
		return
	}
	if h.failed(w, h.stampAsk(r, id, body)) {
		return
	}
	if selected != nil && h.failed(w, h.config.Store.Update(id, func(it *factory.Item) error {
		it.Stages = factory.CopyStages(selected)
		if decided {
			it.DecidePipeline(copied, h.clock())
		}
		return nil
	})) {
		return
	}
	if body.Issue && h.failed(w, h.keepIssue(id, issue)) {
		return
	}
	if body.Autostart && h.config.Store != nil {
		it, err := h.config.Store.Get(id)
		if h.failed(w, err) || h.failed(w, h.ensureBudget(r, it)) {
			return
		}
	}
	if body.Autostart && h.failed(w, h.recordAction(r, factory.Item{ID: id}, record.AutoStarted, "", func() error { return h.seam.Launch(id) })) {
		return
	}
	h.write(w, http.StatusCreated, struct {
		Factory api.Factory `json:"factory"`
		ID      int         `json:"id"`
	}{h.factory, id})
}

// stampAsk records the app's ask on a job this factory's store holds.
// The body carries the person's words, never a cap.
func (h *Handler) stampAsk(r *http.Request, id int, body api.NewJob) error {
	if h.config.Store == nil {
		return nil
	}
	by, err := h.callerHandle(r)
	if err != nil {
		return err
	}
	ask := strings.TrimSpace(body.Text)
	if ask == "" {
		ask = strings.TrimSpace(body.Title)
	}
	now := h.clock()
	source := body.Source
	if source == "" {
		source = factory.AskApp
	}
	return h.config.Store.Update(id, func(it *factory.Item) error {
		if body.Source == "hand-off" {
			it.Title = body.Title
		}
		if body.Start == "now" {
			it.Marked = true
		}
		it.Ask = &factory.JobAsk{Text: ask, By: by, At: now, Source: source, FromRef: body.FromRef}
		if it.Starter == "" && by != "" {
			it.Starter = by
		}
		if body.Assignee != nil {
			it.Assignee = *body.Assignee
			it.Starter = ""
			it.Taker = ""
		}
		if strings.TrimSpace(it.Body) == "" {
			it.Body = ask
		}
		return nil
	})
}

func approvalEventID(trailer string) string {
	fields := strings.Fields(trailer)
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}

func (h *Handler) connectGitHub(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.seam.ConnectGitHub != nil) {
		return
	}
	if !h.requireOperator(w, r) {
		return
	}
	var body struct {
		Token *string `json:"token"`
	}
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.Token == nil {
		h.fail(w, http.StatusUnprocessableEntity, "connect requires token; an empty token selects gh")
		return
	}
	h.finish(w, h.seam.ConnectGitHub(r.Context(), *body.Token))
}

func (h *Handler) words(w http.ResponseWriter, r *http.Request, it factory.Item, verb string) (string, bool) {
	var body struct {
		Words  string      `json:"words"`
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, false) || !h.fresh(w, r, it, verb, body.Expect) {
		return "", false
	}
	if strings.TrimSpace(body.Words) == "" {
		h.fail(w, http.StatusUnprocessableEntity, "words must not be empty")
		return "", false
	}
	return body.Words, true
}

// decode bounds input before touching the seam and rejects extra objects or
// misspelled fields, since silently ignoring intent would change the job.
func (h *Handler) decode(w http.ResponseWriter, r *http.Request, out any, optional bool) bool {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.fail(w, http.StatusRequestEntityTooLarge, "JSON body exceeds 1 MiB")
		} else {
			h.fail(w, http.StatusUnprocessableEntity, "could not read JSON body")
		}
		return false
	}
	data = bytes.TrimSpace(data)
	if optional && len(data) == 0 {
		return true
	}
	if len(data) == 0 || data[0] != '{' {
		h.fail(w, http.StatusUnprocessableEntity, "body must be a JSON object")
		return false
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		h.fail(w, http.StatusUnprocessableEntity, "invalid JSON body: "+err.Error())
		return false
	}
	if d.Decode(new(any)) != io.EOF {
		h.fail(w, http.StatusUnprocessableEntity, "body must contain one JSON object")
		return false
	}
	return true
}

func (h *Handler) requireVerb(w http.ResponseWriter, exists bool) bool {
	if !exists {
		h.fail(w, http.StatusNotImplemented, "this factory does not provide that action")
	}
	return exists
}

func (h *Handler) finish(w http.ResponseWriter, err error) {
	if !h.failed(w, err) {
		h.write(w, http.StatusOK, struct {
			Factory api.Factory `json:"factory"`
		}{h.factory})
	}
}

// stop supplies the caller before the lease door writes its durable stop receipt.
func (h *Handler) stop(w http.ResponseWriter, r *http.Request, it factory.Item) {
	handle, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	h.finish(w, h.stopAs(r, it, handle))
}

// stopAs is a person's Stop of one job, by handle: the run ends with its work
// kept, the stop is recorded, and Undo can put the job back. It is the job's
// own Stop and the stop a repository turned off with "stop" makes of each job
// under way on it (repos_v2.go), so the two are one stop.
func (h *Handler) stopAs(r *http.Request, it factory.Item, handle string) error {
	if h.config.Store != nil {
		raw, _ := json.Marshal(it.Stream)
		var prior *factory.Stream
		_ = json.Unmarshal(raw, &prior)
		var err error
		if h.workers != nil && h.workers.desk != nil {
			err = lease.NewDoors(h.workers.desk).StopAs(it.ID, handle)
		} else {
			err = h.stopForChange(it.ID)
		}
		if err != nil {
			return err
		}
		return h.config.Store.Update(it.ID, func(cur *factory.Item) error {
			// The stop's own write took down the approval and the publish
			// error and recorded the stop; this keeps only what Undo puts back.
			cur.StopUndo = &factory.StopUndo{By: handle, Until: h.clock().Add(factory.ApprovalUndo), Failure: it.Failure, State: it.State, Question: it.Question, QKind: it.QKind, Stream: prior}
			return nil
		})
	}
	if h.workers != nil && h.workers.desk != nil {
		return lease.NewDoors(h.workers.desk).StopAs(it.ID, handle)
	}
	return h.recordAction(r, it, record.Stopped, "", func() error { return h.seam.Stop(it.ID) })
}

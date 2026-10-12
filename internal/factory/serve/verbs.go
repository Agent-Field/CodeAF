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
	h.postJob("run", s.Launch != nil, h.guarded("run", h.onlyOwner(h.recordedID(s.Launch, record.Started))))
	h.postJob("stop", s.Stop != nil, h.guarded("stop", h.stop))
	h.postJob("pause", s.Hold != nil, h.guarded("pause", h.holdVerb(true)))
	h.postJob("resume", s.Hold != nil, h.guarded("resume", h.holdVerb(false)))
	h.postJob("drop", s.Dismiss != nil, h.onlyOwner(h.drop))
	h.postJob("answer", s.Answer != nil, h.answer)
	h.postJob("steer", s.Steer != nil, h.wordsVerb("steer", s.Steer))
	h.postJob("approve", true, h.approve)
	h.postJob("retry_publish", h.config.Store != nil && h.seam.SignOff != nil, h.guarded("retry_publish", h.onlyOwner(h.retryPublish)))
	h.postJob("request_changes", s.SendBack != nil, h.recordedWords(s.SendBack, record.RequestChanges))
	h.postJob("recheck", s.Reverify != nil, h.guarded("recheck", h.idVerb(s.Reverify)))
	// Shape is present even when the seam has no Shape door: a worker holding
	// the job is signaled, and only a job nobody holds needs the local turn.
	h.postJob("shape", true, h.guarded("shape", h.shape))
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
			if verb == "approve" || verb == "retry_publish" || verb == "answer" {
				unlock, err := h.lockConsent()
				if h.failed(w, err) {
					return
				}
				defer unlock()
			}
			if !h.requireVerb(w, exists) {
				return
			}
			if it, ok := h.findJob(w, r); ok {
				action(w, r, it)
			}
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
	if err := h.workers.desk.Assign(it.ID, me); h.takeFailed(w, it, err) {
		return
	}
	if h.config.Record != nil {
		if _, err := h.config.Record.Append(r.Context(), record.Event{Kind: record.Took, Job: it.ID, Handle: me, Cost: jobCost(it), Detail: h.machineFor(me)}); h.failed(w, err) {
			return
		}
	}
	updated, err := h.workers.st.Get(it.ID)
	if h.failed(w, err) {
		return
	}
	job := h.withVersion(api.FromItem(updated))
	job.Can = api.CanFor(updated, me)
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
	job.Can = api.CanFor(updated, me)
	h.write(w, http.StatusOK, struct {
		Factory api.Factory `json:"factory"`
		api.Job
	}{h.factory, job})
}

// takeFailed maps the person rule and a live worker hold onto 409. The
// sentence is what the page shows; the lease package prefix stays off it.
func (h *Handler) takeFailed(w http.ResponseWriter, it factory.Item, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, lease.ErrPerson) || errors.Is(err, lease.ErrLeased) {
		h.taken(w, it)
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

func (h *Handler) taken(w http.ResponseWriter, it factory.Item) {
	state := h.withVersion(api.FromItem(it)).State
	who := it.Person()
	if who == "" {
		h.refuse(w, http.StatusConflict, "job is already taken", "conflict", "", nil, state)
		return
	}
	h.takenConflict(w, it, "already taken by @"+who, "conflict", state)
}

func (h *Handler) idVerb(door func(int) error) jobAction {
	return func(w http.ResponseWriter, r *http.Request, it factory.Item) {
		h.finish(w, door(it.ID))
	}
}

func (h *Handler) holdVerb(on bool) jobAction {
	return func(w http.ResponseWriter, r *http.Request, it factory.Item) {
		h.finish(w, h.seam.Hold(it.ID, on))
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
		Commit string      `json:"commit"`
		Yes    *bool       `json:"yes"`
		Words  string      `json:"words"`
		Choice string      `json:"choice"`
		Reason string      `json:"reason"`
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, false) || !h.fresh(w, r, it, "answer", body.Expect) {
		return
	}
	yes, words, ok := h.applyChoice(w, body.Yes, body.Words, body.Choice, body.Reason)
	if !ok {
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

func (h *Handler) approve(w http.ResponseWriter, r *http.Request, it factory.Item) {
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
	if it.State == factory.StateNeedsYou && it.QKind == factory.QKindApprove {
		if !h.requireVerb(w, h.seam.Answer != nil) {
			return
		}
		if h.failed(w, h.seam.Answer(it.ID, true, "")) {
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
		if h.failed(w, h.keepAnswered(it, factory.Answer{By: handle, Text: "yes", At: at, Event: event})) {
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

// drop records optional words and tells a holding worker to stop. The branch
// is kept: nothing here deletes it.
func (h *Handler) drop(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Words  string      `json:"words"`
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, "drop", body.Expect) {
		return
	}
	words := strings.TrimSpace(body.Words)
	if h.workers != nil && h.workers.desk != nil {
		if _, err := h.workers.desk.SignalIfHeld(it.ID, lease.Signal{Verb: lease.SignalStop, Words: words}); h.failed(w, err) {
			return
		}
	}
	h.finish(w, h.recordAction(r, it, record.Dropped, words, func() error { return h.seam.Dismiss(it.ID) }))
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

// shape is POST /v1/jobs/{id}/shape. A worker holding the job hears it as a
// lease signal and gives the manager the turn on its own copy. A job nobody
// holds is shaped here, which is where that job's worker lives.
func (h *Handler) shape(w http.ResponseWriter, r *http.Request, it factory.Item) {
	if h.workers != nil && h.workers.desk != nil {
		held, err := h.workers.desk.SignalIfHeld(it.ID, lease.Signal{Verb: lease.SignalShape})
		if h.failed(w, err) {
			return
		}
		if held {
			h.finish(w, nil)
			return
		}
	}
	if !h.requireVerb(w, h.seam.Shape != nil) {
		return
	}
	line, err := h.seam.Shape(r.Context(), it.ID)
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, struct {
		Factory api.Factory `json:"factory"`
		Line    string      `json:"line,omitempty"`
	}{h.factory, line})
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
	var body struct {
		Repo      string `json:"repo"`
		Title     string `json:"title"`
		Text      string `json:"text"`
		Autostart bool   `json:"autostart"`
	}
	if !h.decode(w, r, &body, false) {
		return
	}
	if strings.TrimSpace(body.Repo) == "" || strings.TrimSpace(body.Title) == "" {
		h.fail(w, http.StatusUnprocessableEntity, "new job requires repo and title")
		return
	}
	visible := h.repoVisible(r, body.Repo)
	if h.authFailed(w, r) {
		return
	}
	if !visible {
		h.fail(w, http.StatusNotFound, "repository not found")
		return
	}
	if body.Autostart && !h.requireVerb(w, h.seam.Launch != nil) {
		return
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
	h.stampAsk(r, id, body.Title, body.Text)
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
func (h *Handler) stampAsk(r *http.Request, id int, title, text string) {
	if h.config.Store == nil {
		return
	}
	if _, err := h.config.Store.Get(id); err != nil {
		return
	}
	by, err := h.callerHandle(r)
	if err != nil {
		return
	}
	ask := strings.TrimSpace(text)
	if ask == "" {
		ask = strings.TrimSpace(title)
	}
	now := time.Now()
	if h.workers != nil {
		now = h.workers.now()
	}
	_ = h.config.Store.Update(id, func(it *factory.Item) error {
		it.Ask = &factory.JobAsk{Text: ask, By: by, At: now, Source: factory.AskApp}
		if it.Starter == "" && by != "" {
			it.Starter = by
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

func (h *Handler) setRepos(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.seam.SetRepos != nil) {
		return
	}
	if !h.requireOperator(w, r) {
		return
	}
	var body struct {
		Watched *[]string `json:"watched"`
	}
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.Watched == nil {
		h.fail(w, http.StatusUnprocessableEntity, "watched must be an array of repository names")
		return
	}
	if err := h.seam.SetRepos(*body.Watched); h.failed(w, err) {
		return
	}
	h.finish(w, h.operatorEvent(r, record.ReposChanged, "factory", strings.Join(*body.Watched, ", ")))
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
	if h.workers.desk != nil {
		h.finish(w, lease.NewDoors(h.workers.desk).StopAs(it.ID, handle))
		return
	}
	h.finish(w, h.recordAction(r, it, record.Stopped, "", func() error { return h.seam.Stop(it.ID) }))
}

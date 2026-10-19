package serve

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// guarded reads an optional expect and refuses a stale one before the verb runs.
// Verbs that read a body of their own decode expect themselves.
func (h *Handler) guarded(verb string, action jobAction) jobAction {
	return func(w http.ResponseWriter, r *http.Request, it factory.Item) {
		var body struct {
			Expect *api.Expect `json:"expect,omitempty"`
		}
		if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, verb, body.Expect) {
			return
		}
		action(w, r, it)
	}
}

// withVersion stamps the store's document revision onto the job the page
// reads. A handler with no store leaves it zero, and the field stays off the wire.
func (h *Handler) withVersion(job api.Job) api.Job {
	return h.withVersionKept(job, h.retention())
}

// retention is how long a finished job stays on the page. A response that
// lists many jobs reads it once and hands it to [Handler.withVersionKept].
func (h *Handler) retention() time.Duration {
	if h.config.ServedBy == "app" {
		return time.Duration(config.AppFactoryDoneDaysAt(h.config.ProfileDir)) * 24 * time.Hour
	}
	return api.RunningRetention
}

// withVersionKept is [Handler.withVersion] with the retention already read.
func (h *Handler) withVersionKept(job api.Job, retention time.Duration) api.Job {
	job.Display = api.DisplayForRetention(job, h.clock(), retention)
	if h.config.Store == nil || job.ID == 0 {
		return job
	}
	rev, err := h.config.Store.Revision(job.ID)
	if err != nil || rev == 0 {
		return job
	}
	job.Version = rev
	return job
}

// fresh is true when expect is absent or still matches the job. A mismatch
// is the page the person was looking at, not the job as it is now.
func (h *Handler) fresh(w http.ResponseWriter, r *http.Request, it factory.Item, verb string, exp *api.Expect) bool {
	if exp == nil {
		return true
	}
	job := h.withVersion(api.FromItem(it))
	step := ""
	if job.Step != nil {
		step = job.Step.Name
	}
	stale := (exp.State != "" && exp.State != job.State) ||
		(exp.Step != "" && exp.Step != step) ||
		(exp.Version != nil && *exp.Version != job.Version)
	if !stale {
		return true
	}
	h.moved(w, r, it, job, verb)
	return false
}

// moved answers a stale expect. The sentence is the one the page can show:
// already approved, already taken, or the job moved on while you looked.
func (h *Handler) moved(w http.ResponseWriter, r *http.Request, it factory.Item, job api.Job, verb string) {
	sentence := "job moved on while you looked"
	var by string
	var at *time.Time
	switch {
	case verb == "take" && it.Person() != "":
		sentence = "already taken by @" + it.Person()
		by = "@" + it.Person()
		h.takenConflict(w, r, it, sentence, "stale", job.State)
		return
	case job.State == api.StateDone || it.Publishing || it.State == factory.StateShipped:
		var eventID, commit string
		sentence, by, at, eventID, commit = h.approvedSentence(r, it)
		h.writeRefusal(w, http.StatusConflict, refusalBody{
			Error: sentence, Reason: "stale", By: by, At: at, NowState: job.State,
			ApprovedBy: by, Event: eventID, Commit: commit,
		})
		return
	default:
		if p := it.Person(); p != "" {
			by = "@" + p
		}
		if !it.Changed.IsZero() {
			stamp := it.Changed.UTC()
			at = &stamp
		}
	}
	h.refuse(w, http.StatusConflict, sentence, "stale", by, at, job.State)
}

func (h *Handler) approvedSentence(r *http.Request, it factory.Item) (string, string, *time.Time, string, string) {
	handle, eventID, commit, when, ok := h.latestApproval(r.Context(), it.ID)
	if !ok {
		handle = it.Person()
		when = it.Changed
		commit = it.HeadCommit
	}
	if handle == "" {
		handle = "someone"
	}
	by := "@" + handle
	if when.IsZero() {
		return "already approved by " + by, by, nil, eventID, commit
	}
	stamp := when.UTC()
	return fmt.Sprintf("already approved by %s at %s", by, stamp.Format("15:04")), by, &stamp, eventID, commit
}

func (h *Handler) latestApproval(ctx context.Context, id int) (handle, eventID, commit string, when time.Time, ok bool) {
	if h.config.Record == nil {
		return "", "", "", time.Time{}, false
	}
	events, err := h.config.Record.Since(ctx, time.Time{})
	if err != nil {
		return "", "", "", time.Time{}, false
	}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Job == id && e.Kind == record.Approved && e.Handle != "" {
			return e.Handle, e.ID, e.Commit, e.At, true
		}
	}
	return "", "", "", time.Time{}, false
}

type refusalBody struct {
	Factory       api.Factory `json:"factory"`
	Error         string      `json:"error"`
	Reason        string      `json:"reason,omitempty"`
	By            string      `json:"by,omitempty"`
	At            *time.Time  `json:"at,omitempty"`
	NowState      string      `json:"now_state,omitempty"`
	Taker         string      `json:"taker,omitempty"`
	Machine       string      `json:"machine,omitempty"`
	ApprovedBy    string      `json:"approved_by,omitempty"`
	Event         string      `json:"event,omitempty"`
	Commit        string      `json:"commit,omitempty"`
	CurrentCommit string      `json:"current_commit,omitempty"`
	HeadCommit    string      `json:"head_commit,omitempty"`
	JobPerson     string      `json:"job_person,omitempty"`
	RequestID     string      `json:"request_id"`
}

func (h *Handler) refuse(w http.ResponseWriter, status int, words, reason, by string, at *time.Time, nowState string) {
	h.writeRefusal(w, status, refusalBody{Error: words, Reason: reason, By: by, At: at, NowState: nowState})
}

func (h *Handler) writeRefusal(w http.ResponseWriter, status int, body refusalBody) {
	body.Factory = h.factory
	if body.RequestID == "" {
		body.RequestID = requestID()
	}
	h.write(w, status, body)
}

// notJobPerson is the who-may-act refusal. job_person is who may, with @.
func (h *Handler) notJobPerson(w http.ResponseWriter, it factory.Item, who string) {
	state := ""
	if it.ID != 0 {
		state = h.withVersion(api.FromItem(it)).State
	}
	h.writeRefusal(w, http.StatusForbidden, refusalBody{
		Error: "only @" + who + " can do this", Reason: factory.ReasonNotJobPerson,
		JobPerson: "@" + who, NowState: state,
	})
}

// takenConflict names who holds the job, when, and the machine when one is known.
func (h *Handler) takenConflict(w http.ResponseWriter, r *http.Request, it factory.Item, sentence, reason, state string) {
	who := it.Person()
	by := ""
	if who != "" {
		by = "@" + who
	}
	var at *time.Time
	// The machine is named only to the person it belongs to (FS0/FS15 privacy).
	caller := h.machineViewer(r)
	solo := h.mode() == api.FactorySolo
	machine := ""
	if solo || h.callersOnlyMachine(caller, it.WorktreeMachine) {
		machine = it.WorktreeMachine
	}
	if held, ok := h.holdOf(it.ID); ok {
		if !held.Since.IsZero() {
			stamp := held.Since.UTC()
			at = &stamp
		}
		if held.Machine != "" && (solo || (caller != "" && caller == factory.NormalizeHandle(held.Handle))) {
			machine = held.Machine
		}
	}
	if at == nil && !it.Changed.IsZero() {
		stamp := it.Changed.UTC()
		at = &stamp
	}
	h.writeRefusal(w, http.StatusConflict, refusalBody{
		Error: sentence, Reason: reason, By: by, At: at, NowState: state, Taker: by, Machine: machine,
	})
}

func (h *Handler) holdOf(id int) (lease.Lease, bool) {
	if h.workers == nil || h.workers.desk == nil {
		return lease.Lease{}, false
	}
	rows, err := h.workers.desk.Live()
	if err != nil {
		return lease.Lease{}, false
	}
	for _, row := range rows {
		if row.Job == id {
			return row, true
		}
	}
	return lease.Lease{}, false
}

// onlyOwner runs the verb when the caller is the job's person, or the job
// has no person yet. Otherwise the answer is 403 not_job_person.
func (h *Handler) onlyOwner(action jobAction) jobAction {
	return func(w http.ResponseWriter, r *http.Request, it factory.Item) {
		if !h.owns(w, r, it) {
			return
		}
		action(w, r, it)
	}
}

// owns is true when the job has no person yet, or the caller is that person.
func (h *Handler) owns(w http.ResponseWriter, r *http.Request, it factory.Item) bool {
	if h.mode() == api.FactorySolo {
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
	h.notJobPerson(w, it, who)
	return false
}

func reasonFor(status int) string {
	switch status {
	case http.StatusForbidden:
		return "not_allowed"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusUnprocessableEntity:
		return "bad_input"
	default:
		return ""
	}
}

// classify turns a door's error into a status a person can tell apart from a
// crash. Unknown errors stay 500 and carry no reason.
func classify(err error) (int, string) {
	var stepRefusal run.StepControlRefusal
	var editRefusal factory.EditRefusal
	var planRefusal run.PlanRevisionRefusal
	if errors.As(err, &stepRefusal) || errors.As(err, &editRefusal) || errors.As(err, &planRefusal) {
		return http.StatusConflict, "conflict"
	}
	var stale *store.StaleError
	if errors.As(err, &stale) {
		return http.StatusConflict, "stale"
	}
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, fs.ErrPermission):
		return http.StatusForbidden, "not_allowed"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, ""
	case errors.Is(err, context.Canceled):
		return http.StatusRequestTimeout, ""
	case errors.Is(err, run.ErrAlreadyRunning):
		return http.StatusConflict, "conflict"
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "a stranger's work"):
		return http.StatusForbidden, "not_allowed"
	case strings.Contains(msg, "say what to prove"):
		return http.StatusUnprocessableEntity, "bad_input"
	case conflictSentence(msg):
		return http.StatusConflict, "conflict"
	default:
		return http.StatusInternalServerError, ""
	}
}

func conflictSentence(msg string) bool {
	for _, part := range []string{
		"was dropped for good",
		"is not running",
		"is not paused",
		"is not waiting",
		"has not landed",
		"has landed",
		"has shipped",
		"claim not shown",
		"claims not shown",
		"not waiting for approval",
		"already running",
		"no longer running",
		"no longer asking",
	} {
		if strings.Contains(msg, part) {
			return true
		}
	}
	return false
}

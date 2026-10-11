package serve

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
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
	case job.State == api.StateDone || it.Publishing || it.State == factory.StateShipped:
		sentence, by, at = h.approvedSentence(r, it)
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

func (h *Handler) approvedSentence(r *http.Request, it factory.Item) (string, string, *time.Time) {
	handle, when, ok := h.latestApproval(r.Context(), it.ID)
	if !ok {
		handle = it.Person()
		when = it.Changed
	}
	if handle == "" {
		handle = "someone"
	}
	by := "@" + handle
	if when.IsZero() {
		return "already approved by " + by, by, nil
	}
	stamp := when.UTC()
	return fmt.Sprintf("already approved by %s at %s", by, stamp.Format("15:04")), by, &stamp
}

func (h *Handler) latestApproval(ctx context.Context, id int) (string, time.Time, bool) {
	if h.config.Record == nil {
		return "", time.Time{}, false
	}
	events, err := h.config.Record.Since(ctx, time.Time{})
	if err != nil {
		return "", time.Time{}, false
	}
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.Job == id && e.Kind == record.Approved && e.Handle != "" {
			return e.Handle, e.At, true
		}
	}
	return "", time.Time{}, false
}

type refusalBody struct {
	Factory   api.Factory `json:"factory"`
	Error     string      `json:"error"`
	Reason    string      `json:"reason,omitempty"`
	By        string      `json:"by,omitempty"`
	At        *time.Time  `json:"at,omitempty"`
	NowState  string      `json:"now_state,omitempty"`
	RequestID string      `json:"request_id"`
}

func (h *Handler) refuse(w http.ResponseWriter, status int, words, reason, by string, at *time.Time, nowState string) {
	h.write(w, status, refusalBody{
		Factory: h.factory, Error: words, Reason: reason, By: by, At: at, NowState: nowState, RequestID: requestID(),
	})
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
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, fs.ErrPermission), errors.Is(err, errSignInApproval):
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

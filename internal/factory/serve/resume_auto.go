package serve

import (
	"errors"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ResumeAutoVerb is the job door that tells Auto to carry on past the failing
// checks it paused on (factory's override.go). The job's manager presses it as
// the person, with their words, when they ask the job to resume.
const ResumeAutoVerb = "resume_auto"

// resumeAuto records every failing finding the pause shows as the caller's
// override and wakes the job timer, so Auto's own approval carries the job on.
//
// IT IS A PERSON'S DOOR. The caller must be signed in and allowed to approve
// the job, exactly as for Approve and Publish anyway; the record and the job's
// history both name who overrode what.
func (h *Handler) resumeAuto(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Words  string      `json:"words,omitempty"`
		Expect *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, ResumeAutoVerb, body.Expect) || !h.approves(w, r, it) {
		return
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	if who == "" {
		h.fail(w, 403, "Sign in before telling Auto to carry on")
		return
	}
	var found []factory.CheckOverride
	change := func(cur *factory.Item) error {
		var e error
		found, e = factory.OverrideFindings(cur, who, body.Words, h.clock())
		return e
	}
	if body.Expect != nil && body.Expect.Version != nil {
		err = h.config.Store.UpdateExpected(it.ID, *body.Expect.Version, change)
	} else {
		err = h.config.Store.Update(it.ID, change)
	}
	if err != nil {
		var stale *store.StaleError
		if errors.As(err, &stale) {
			h.refuse(w, 409, err.Error(), "stale", "", nil, factory.JobState(it))
			return
		}
		h.refuse(w, 409, err.Error(), "conflict", "", nil, factory.JobState(it))
		return
	}
	if h.config.Record != nil {
		if _, err := h.config.Record.Append(r.Context(), record.Event{Kind: record.Resumed, Job: it.ID, Handle: who, Commit: it.HeadCommit, Detail: "Auto pause overridden · " + factory.OverrideSummary(found)}); h.failed(w, err) {
			return
		}
	}
	h.scheduleJobTimer()
	h.finish(w, nil)
}

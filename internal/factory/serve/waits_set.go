package serve

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// waitsRoutes is separate from the job verbs: waits replace a list of parents.
func (h *Handler) waitsRoutes() {
	h.route("/v1/jobs/{id}/waits_on", map[string]http.HandlerFunc{http.MethodPut: func(w http.ResponseWriter, r *http.Request) {
		if !h.requireVerb(w, h.config.Store != nil) {
			return
		}
		it, ok := h.findJob(w, r)
		if ok {
			h.onlyOwner(h.setWaits)(w, r, it)
		}
	}})
}

type waitsRefusal string

func (e waitsRefusal) Error() string { return string(e) }

func (h *Handler) setWaits(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		WaitsOn *[]int      `json:"waits_on"`
		Expect  *api.Expect `json:"expect,omitempty"`
	}
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.WaitsOn == nil {
		h.fail(w, http.StatusUnprocessableEntity, "waits_on must be a list of job ids; use an empty list to clear it")
		return
	}
	if err := factory.CheckWaitsIDs(*body.WaitsOn); err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if !h.fresh(w, r, it, "waits_on", body.Expect) {
		return
	}
	caller, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	// The shared consent lock serializes graph edits across handlers too.
	// It is taken before listing parents and before taking the child's lock.
	unlock, err := h.lockConsent()
	if h.failed(w, err) {
		return
	}
	defer unlock()
	items, err := h.config.Store.List()
	if h.failed(w, err) {
		return
	}
	parents := make(map[int]factory.Item, len(items))
	for _, parent := range items {
		parents[parent.ID] = parent
	}
	if err := checkWaitsParents(it.ID, *body.WaitsOn, parents); err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	change := func(cur *factory.Item) error {
		if h.mode() != api.FactorySolo && cur.Person() != "" && cur.Person() != factory.NormalizeHandle(caller) {
			return notJobPersonError{who: cur.Person()}
		}
		if exp := body.Expect; exp != nil {
			job := api.FromItem(*cur)
			if exp.State != "" && exp.State != job.State || exp.Step != "" && (job.Step == nil || job.Step.Name != exp.Step) {
				return errPlanStepMoved
			}
		}
		if cur.WaitsReleased || factory.WaitsOverridden(*cur) {
			return waitsRefusal("waits can only be changed on a Backlog job that has never started or been released")
		}
		cur.WaitsOn = append([]int(nil), (*body.WaitsOn)...)
		cur.WaitingOn = factory.EvaluateWaits(*cur, parents).WaitingOn
		cur.Blocked = nil
		return nil
	}
	if exp := body.Expect; exp != nil && exp.Version != nil {
		err = h.config.Store.UpdateExpected(it.ID, *exp.Version, change)
	} else {
		err = h.config.Store.Update(it.ID, change)
	}
	var stale *store.StaleError
	var person notJobPersonError
	var refused waitsRefusal
	switch {
	case errors.As(err, &stale) || errors.Is(err, errPlanStepMoved):
		if cur, ok := h.findJob(w, r); ok {
			h.moved(w, r, cur, h.withVersion(api.FromItem(cur)), "waits_on")
		}
	case errors.As(err, &person):
		h.notJobPerson(w, it, person.who)
	case errors.As(err, &refused):
		h.fail(w, http.StatusUnprocessableEntity, refused.Error())
	case err != nil:
		h.failed(w, err)
	default:
		h.job(w, r)
	}
}

func checkWaitsParents(job int, ids []int, parents map[int]factory.Item) error {
	seen := make(map[int]bool)
	var reachesJob func(int) bool
	reachesJob = func(id int) bool {
		if id == job {
			return true
		}
		if seen[id] {
			return false
		}
		seen[id] = true
		for _, parent := range parents[id].WaitsOn {
			if reachesJob(parent) {
				return true
			}
		}
		return false
	}
	for _, id := range ids {
		if id == job {
			return fmt.Errorf("a job cannot wait on itself")
		}
		parent, ok := parents[id]
		if !ok || !factory.IsIssueJob(parent) {
			return fmt.Errorf("job %d does not exist in this factory", id)
		}
		if why := factory.WaitsEndedReason(parent); why != "" {
			return fmt.Errorf("%s %s; a job cannot wait on it", factory.JobRef(parent), why)
		}
		if reachesJob(id) {
			return fmt.Errorf("these waits would make a cycle between jobs")
		}
	}
	return nil
}

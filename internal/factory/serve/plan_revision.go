package serve

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── SENDING A PLAN BACK: POST /v1/jobs/{id}/reject_plan ────────────────────
//
// A plan revision is the approve question's no, typed: the person's words go
// back to the finished step before the plan's approve step, which plans again
// with them. It is a route of its own, and not a body of /answer, because the
// answer route takes a no for every question a job can wait on, and a no to a
// spending limit or to an exhausted round budget cancels the job, a no to a
// start declines it, and a no to a plan with no finished step before it stops
// it. A client that means "revise this plan" must never reach any of those.
//
// THE GUARD IS THE LOCKED DOCUMENT, NEVER THE CLOCK. The handler's own read of
// the job (findJob, fresh) only refuses early in the sentence a page can show.
// The decision is made again, whole, by lease.Doors.RejectPlan inside the
// desk's lock and the job's flock: the document must still be the revision
// the person reviewed, still a plan waiting with a finished step before it,
// and the caller must still be allowed to answer it. A worker or a second
// person who writes the job between this handler's read and that lock moves
// the revision, so the revision is refused as stale and nothing is written.
//
// IT IS AN ANSWER, NOT AN APPROVAL. It holds the consent lock the answer route
// holds, leaves the `answered` receipt and the kept answer the answer route
// leaves (recordAnswerBy), and never an approval, an Undo window or a publish.
// The item, lease signal and receipt are written separately, as for an answer:
// these locks prevent a stale decision, not a partial write after an I/O failure.

// errPlanStepMoved is a revision whose expect named a step the locked
// document is no longer at.
var errPlanStepMoved = errors.New("job moved on while you looked")

// notJobPersonError is the locked document's refusal of a caller who is not
// the job's person.
type notJobPersonError struct{ who string }

func (e notJobPersonError) Error() string { return "only @" + e.who + " can answer this" }

func (h *Handler) rejectPlan(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body api.RejectPlan
	if !h.decode(w, r, &body, false) {
		return
	}
	words, ok := h.planWords(w, body)
	if !ok || !h.fresh(w, r, it, "reject_plan", body.Expect) || !h.workerReady(w) {
		return
	}
	caller, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	var refused error
	err = h.recordAnswerBy(r, false, words, func() (factory.Item, error) {
		before, err := lease.NewDoors(h.workers.desk).RejectPlan(it.ID, *body.Expect.Version, words, h.mayRevise(caller, body.Expect.Step))
		refused = err
		return before, err
	})
	if refused != nil {
		h.planRefused(w, r, it, refused)
		return
	}
	h.finish(w, err)
}

// planWords refuses a body the route cannot act on, before anything is read
// under a lock: blank or too many words, or no exact page to act on.
func (h *Handler) planWords(w http.ResponseWriter, body api.RejectPlan) (string, bool) {
	words := strings.TrimSpace(body.Words)
	switch exp := body.Expect; {
	case words == "":
		h.fail(w, http.StatusUnprocessableEntity, "say what to change in the plan")
	case utf8.RuneCountInString(words) > api.MaxRejectPlanWords:
		h.fail(w, http.StatusUnprocessableEntity, "what to change in the plan is too long")
	case exp == nil || exp.Version == nil || exp.State != api.StateNeedsYou:
		h.fail(w, http.StatusUnprocessableEntity, "reject_plan requires expect.version and expect.state "+api.StateNeedsYou)
	default:
		return words, true
	}
	return "", false
}

// mayRevise is who may send this plan back, asked of the LOCKED document: the
// answer route's rule (mayAnswer) for an approve_plan wait, and the step the
// person reviewed when their expect named one.
func (h *Handler) mayRevise(caller, step string) func(factory.Item) error {
	solo := h.mode() == api.FactorySolo
	return func(cur factory.Item) error {
		if step != "" {
			if s := api.FromItem(cur).Step; s == nil || s.Name != step {
				return errPlanStepMoved
			}
		}
		who := cur.Person()
		if solo || who == "" || factory.NormalizeHandle(caller) == who {
			return nil
		}
		return notJobPersonError{who: who}
	}
}

// planRefused answers the door's refusal in the shapes the answer route uses:
// stale with the state the job is in now, not_job_person with who may, and a
// conflict naming why the job is not a plan to send back.
func (h *Handler) planRefused(w http.ResponseWriter, r *http.Request, it factory.Item, err error) {
	var stale *store.StaleError
	var person notJobPersonError
	var plan run.PlanRevisionRefusal
	switch {
	case errors.As(err, &stale) || errors.Is(err, errPlanStepMoved):
		if now, ok := h.findJob(w, r); ok {
			h.moved(w, r, now, h.withVersion(api.FromItem(now)), "reject_plan")
		}
	case errors.As(err, &person):
		h.notJobPerson(w, it, person.who)
	case errors.As(err, &plan):
		h.refuse(w, http.StatusConflict, plan.Error(), "conflict", "", nil, h.withVersion(api.FromItem(it)).State)
	default:
		h.failed(w, err)
	}
}

// servesPlanRevision is whether this factory can take a plan revision at all:
// the route is hung, and the lease desk it writes through is open.
func (h *Handler) servesPlanRevision() bool {
	if h.config.Store == nil || h.seam.Answer == nil || h.workers == nil {
		return false
	}
	h.workers.mu.RLock()
	defer h.workers.mu.RUnlock()
	return h.workers.err == nil && h.workers.desk != nil
}

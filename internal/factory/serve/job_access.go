package serve

import (
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── A JOB UNDER ITS WORKSTREAM ─────────────────────────────────────────────
//
// On an organization's server a job that belongs to a workstream answers to
// that workstream, not only to its GitHub repository. The rules, exactly:
//
//   - YOU SEE a job when you can read its repository AND, when it belongs to
//     a workstream, you can view that workstream. A job you cannot see is the
//     same 404 as one that does not exist, and none of its events reach you.
//   - YOU CHANGE a job (stop it, steer it, re-prioritise it, reply on it and
//     every other door that writes it) when you are its person, when you can
//     edit its workstream, or when you are the operator. A job with no person
//     yet needs edit on its workstream.
//   - A SIGN-IN THAT NAMES NO ONE sees nothing that belongs to a workstream:
//     a token with no profile used to read as the host's own login, which let
//     it see every workstream the host could.
//
// A job outside every workstream keeps the rules it always had, except that a
// job with a person is now changed only by that person or the operator; it
// used to be changeable by anyone who could read its repository. A solo
// factory has one person, who may do everything.

// jobWorkstreamID is the organization workstream the job belongs to, "" for
// none. THIS IS THE ONE PLACE that answers it: the workstream that took the job
// in by membership (PUT /v1/workstreams/{id}/jobs/{job}), or else the go-ahead
// group that started the job, which on the desk is the workstream.
func (h *Handler) jobWorkstreamID(it factory.Item) string { return h.jobWorkstreamOf(it, nil) }

// jobUnreadable is the workstream a job is held under when its membership
// cannot be read: no workstream has that id, so nobody but the operator gets
// through. A store that cannot answer must not open a job to every reader.
const jobUnreadable = workstreamIDPrefix + "unreadable"

// jobWorkstreamOf is jobWorkstreamID with every job's membership read once
// (member, from [store.Store.JobWorkstreams]) for a caller that asks about
// many jobs; nil looks this one job up.
func (h *Handler) jobWorkstreamOf(it factory.Item, member map[int]string) string {
	if h.config.Store != nil && it.ID > 0 {
		if member == nil {
			id, ok, err := h.config.Store.WorkstreamOfJob(it.ID)
			if err != nil {
				return jobUnreadable
			}
			if ok {
				return id
			}
		} else if id, ok := member[it.ID]; ok {
			return id
		}
	}
	if strings.HasPrefix(it.PolicyGroup, workstreamIDPrefix) {
		return it.PolicyGroup
	}
	return ""
}

// jobPersonalVerbs are doors that write only the caller's own facts, ask a
// question of the job without changing it, or speak as the caller somewhere
// that judges them itself, so seeing the job is enough: dismiss hides the job
// from the caller alone, and issue_comment posts on GitHub with the caller's
// own grant, which GitHub refuses in its own words when it will not have it.
var jobPersonalVerbs = map[string]bool{
	"follow":        true,
	"brief_seen":    true,
	"scout":         true,
	"dismiss":       true,
	"issue_comment": true,
}

// jobDeskVerbs are the doors that make the caller the job's person, or let it
// go. On a job that has a person each already refuses everyone else, naming
// who holds it (409 with the taker), so this gate steps aside; on a job with
// no person yet, taking it still needs edit on its workstream.
var jobDeskVerbs = map[string]bool{
	"take":         true,
	"release":      true,
	"assign_to_me": true,
}

// profileless is true for a sign-in that names no one: a counted link secret
// or a token row with no profile. The host's own secret is not one of them.
func (h *Handler) profileless(r *http.Request) bool {
	if r.Context().Value(workerKey{}) != nil || h.floorToken(bearer(r)) {
		return false
	}
	session, ok := h.credential(r)
	return ok && (session.profile == nil || session.profile.Handle == "")
}

// mayWorkstream is whether the caller holds verb on workstream id, for the
// job doors. A workstream the server no longer keeps puts nothing in the way.
func (h *Handler) mayWorkstream(r *http.Request, id, verb string) (bool, error) {
	if id == "" || h.mode() == api.FactorySolo || r.Context().Value(workerKey{}) != nil {
		return true, nil
	}
	if h.profileless(r) {
		return false, nil
	}
	known, ok, err := h.workstreamCan(r, id, verb)
	if err != nil {
		return false, err
	}
	return !known || ok, nil
}

// workstreamsMatter is whether any job on this factory can belong to an
// organization workstream: a team factory that keeps workstreams. The owner's
// unfiltered reads take the workstream filter only then.
func (h *Handler) workstreamsMatter() bool {
	return h.mode() != api.FactorySolo && h.config.Store != nil
}

// jobSight answers, per job, whether its workstream lets the caller see it.
// It reads each workstream once per request, so filtering a whole floor costs
// one read per workstream, not one per job. A failed read hides the job.
func (h *Handler) jobSight(r *http.Request) func(factory.Item) bool {
	seen := map[string]bool{}
	var member map[int]string
	if h.config.Store != nil {
		// Read once for every job this sight is asked about; a read that
		// fails leaves member nil, and each job is looked up (and held when
		// that fails too).
		member, _ = h.config.Store.JobWorkstreams()
	}
	return func(it factory.Item) bool {
		id := h.jobWorkstreamOf(it, member)
		if id == "" {
			return true
		}
		if ok, cached := seen[id]; cached {
			return ok
		}
		ok, err := h.mayWorkstream(r, id, "view")
		ok = ok && err == nil
		seen[id] = ok
		return ok
	}
}

// jobVisible is jobSight for a single job.
func (h *Handler) jobVisible(r *http.Request, it factory.Item) bool {
	return h.jobSight(r)(it)
}

// mayChangeJob writes the refusal and answers false when the caller may not
// use verb on the job; the rules are this file's header.
func (h *Handler) mayChangeJob(w http.ResponseWriter, r *http.Request, it factory.Item, verb string) bool {
	if jobPersonalVerbs[verb] || h.mode() == api.FactorySolo || r.Context().Value(workerKey{}) != nil {
		return true
	}
	if h.isOperator(r) {
		return true
	}
	person := it.Person()
	if jobDeskVerbs[verb] && person != "" {
		return true
	}
	if person != "" && !h.profileless(r) {
		me, err := h.callerHandle(r)
		if h.failed(w, err) {
			return false
		}
		if factory.NormalizeHandle(me) == person {
			return true
		}
	}
	ws := h.jobWorkstreamID(it)
	if ws == "" {
		if person == "" {
			return true
		}
		h.notJobPerson(w, it, person)
		return false
	}
	ok, err := h.mayWorkstream(r, ws, "edit")
	if h.failed(w, err) {
		return false
	}
	if ok {
		return true
	}
	h.notJobEditor(w, it, person)
	return false
}

// notJobEditor is the 403 for a job in a workstream the caller cannot edit.
func (h *Handler) notJobEditor(w http.ResponseWriter, it factory.Item, person string) {
	sentence := "only people who can edit this job's workstream can do this"
	who := ""
	if person != "" {
		who = "@" + person
		sentence = "only " + who + " and people who can edit this job's workstream can do this"
	}
	h.writeRefusal(w, http.StatusForbidden, refusalBody{
		Error: sentence, Reason: factory.ReasonNotJobPerson,
		JobPerson: who, NowState: h.withVersion(api.FromItem(it)).State,
	})
}

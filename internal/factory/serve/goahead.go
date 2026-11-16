package serve

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// goAheadRoutes are the doors of group policies and of waits-on
// (api/goahead.go). They keep the factory's side only: the go-ahead pass that
// starts what a policy covers runs after every poll (intake.GoAheads), so a
// policy is followed with no app attached.
func (h *Handler) goAheadRoutes() {
	h.read("/v1/features", h.features)
	h.route("/v1/policy", map[string]http.HandlerFunc{http.MethodGet: h.policy, http.MethodPut: h.policy})
	h.postJob("waits_on", h.config.Store != nil, h.onlyOwner(h.setWaits))
}

// features lists what this factory does beyond its contract. The go-ahead
// words need the factory's store, which is where policies and waits are kept.
// keyed_create does not: the rule that a keyed create answered before its
// first write is not kept lives in the request path (keepReplay), and an
// engine sends a create key only to a factory that lists it.
func (h *Handler) features(w http.ResponseWriter, r *http.Request) {
	out := api.Features{Features: []string{api.FeatureKeyedCreate}}
	if h.config.Store != nil {
		out.Features = append(out.Features, api.FeatureGroupPolicy, api.FeatureWaitsOn)
	}
	h.write(w, http.StatusOK, out)
}

// errPolicyNotYours refuses a change to a policy another person keeps.
var errPolicyNotYours = errors.New("this group's go-aheads are someone else's; only they or the operator can change them")

// policy reads (GET, ?group=) or replaces (PUT) one group's policy.
//
// A PUT REPLACES THE WHOLE POLICY and answers it as kept, so a client reads
// back exactly what it sent with the spend beside it. The caller becomes its
// owner: the jobs it starts become theirs when they have no person. On a team,
// only that owner or the operator may change a policy someone already keeps.
// The poll is nudged so the change is followed in seconds, not at the next
// minute.
func (h *Handler) policy(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	st := h.config.Store
	if r.Method == http.MethodGet {
		group, err := factory.CleanPolicyGroup(r.URL.Query().Get("group"))
		if err != nil {
			h.fail(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		p, ok, err := st.Policy(group)
		if h.failed(w, err) {
			return
		}
		if !ok {
			p = factory.GroupPolicy{Group: group}
		}
		h.writePolicy(w, p)
		return
	}
	var body api.PolicyPut
	if !h.decode(w, r, &body, false) {
		return
	}
	group, err := factory.CleanPolicyGroup(body.Group)
	if err == nil {
		err = factory.CheckAllowances(body.Allowances)
	}
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	who = factory.NormalizeHandle(who)
	operator := h.mode() == api.FactorySolo || h.isOperator(r)
	saved, err := st.ChangePolicy(group, func(cur *factory.GroupPolicy) error {
		if cur.Owner != "" && who != cur.Owner && !operator {
			return errPolicyNotYours
		}
		cur.Stopped, cur.Allowances, cur.UpdatedAt = body.Stopped, body.Allowances, h.clock().UTC()
		if who != "" {
			cur.Owner = who
		}
		return nil
	})
	if errors.Is(err, errPolicyNotYours) {
		h.fail(w, http.StatusForbidden, err.Error())
		return
	}
	if h.failed(w, err) {
		return
	}
	_ = st.NudgePoll()
	h.writePolicy(w, saved)
}

// writePolicy answers p with the spend of the jobs it started.
func (h *Handler) writePolicy(w http.ResponseWriter, p factory.GroupPolicy) {
	items, err := h.config.Store.List()
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, api.PolicyOf(p, items, h.clock()))
}

// setWaits replaces the jobs one job waits on. Every job named must be on this
// factory, and none may already wait, however far down, on this one: a loop
// would leave every job in it waiting for ever. The job's person (or anyone,
// on a job with none) may set it, as for Start; `expect` guards a page that
// read the job before it moved.
func (h *Handler) setWaits(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body api.WaitsPut
	if !h.decode(w, r, &body, false) || !h.fresh(w, r, it, "waits_on", body.Expect) {
		return
	}
	waits := append([]int{}, body.WaitsOn...)
	if err := factory.CheckWaitsOn(it.ID, waits); err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	items, err := h.config.Store.List()
	if h.failed(w, err) {
		return
	}
	byID := make(map[int]factory.Item, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	for _, id := range waits {
		if _, ok := byID[id]; !ok {
			h.fail(w, http.StatusUnprocessableEntity, fmt.Sprintf("job %d is not on this factory", id))
			return
		}
		if reaches(byID, id, it.ID) {
			h.fail(w, http.StatusUnprocessableEntity, fmt.Sprintf("job %d already waits on job %d, so this would wait in a circle", id, it.ID))
			return
		}
	}
	if h.failed(w, h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		cur.WaitsOn = waits
		if len(waits) == 0 {
			cur.WaitsOn = nil
		}
		return nil
	})) {
		return
	}
	_ = h.config.Store.NudgePoll()
	cur, err := h.config.Store.Get(it.ID)
	if h.failed(w, err) {
		return
	}
	job := h.withVersion(api.FromItem(cur))
	h.write(w, http.StatusOK, api.WaitsSet{Job: it.ID, WaitsOn: waits, Current: &job})
}

// reaches is whether job from waits, directly or through others, on job to.
func reaches(byID map[int]factory.Item, from, to int) bool {
	seen := map[int]bool{}
	next := []int{from}
	for len(next) > 0 {
		id := next[len(next)-1]
		next = next[:len(next)-1]
		if id == to {
			return true
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		next = append(next, byID[id].WaitsOn...)
	}
	return false
}

// waitingOn fills a job's waiting_on: the jobs it waits on that have not
// merged, read from byID when it is given and from the store when it is not.
// It replaces what was there, so a job answer filled twice says it once.
func (h *Handler) waitingOn(job *api.Job, byID map[int]factory.Item) {
	job.WaitingOn = nil
	if len(job.WaitsOn) == 0 || (byID == nil && h.config.Store == nil) {
		return
	}
	for _, id := range job.WaitsOn {
		dep, ok := byID[id]
		if !ok && h.config.Store != nil {
			got, err := h.config.Store.Get(id)
			dep, ok = got, err == nil
		}
		if !ok || !factory.Merged(dep) {
			job.WaitingOn = append(job.WaitingOn, id)
		}
	}
}

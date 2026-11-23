package serve

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// orgPolicyETag spells a policy revision as the ETag a client sends back to
// save.
func orgPolicyETag(rev int) string { return strconv.Quote(strconv.Itoa(rev)) }

// writeOrgPolicy writes the policy with its ETag.
func (h *Handler) writeOrgPolicy(w http.ResponseWriter, p acl.Policy) {
	w.Header().Set("ETag", orgPolicyETag(p.Rev))
	h.write(w, http.StatusOK, p)
}

// getOrgPolicy answers the policy to any member, which is how a client learns
// what the roles are called and what it may offer.
func (h *Handler) getOrgPolicy(w http.ResponseWriter, r *http.Request) {
	if v, ok := h.accessViewer(w, r); ok {
		h.writeOrgPolicy(w, v.acc.rules())
	}
}

// setOrgPolicy saves the policy; only the operator may. Because a policy change
// is an audience change, it re-evaluates every resource under the old and new
// policy and publishes one event per resource whose audience differs, to the
// union of both, then org.policy_changed to every member. Above the limit in
// live.PolicyChangeLimit only the last is sent, as a signal to refetch.
func (h *Handler) setOrgPolicy(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, h.config.Store != nil) || !h.requireOperator(w, r) {
		return
	}
	if call, ok := agentCallOf(r); ok && call.agent != "" {
		h.fail(w, http.StatusForbidden, "only the operator can change the organization policy")
		return
	}
	var next acl.Policy
	if !h.decode(w, r, &next, false) {
		return
	}
	rev := next.Rev
	if r.Header.Get("If-Match") != "" {
		var ok bool
		if rev, ok = h.ifMatch(w, r); !ok {
			return
		}
	}
	acc := h.access()
	old := acc.rules()
	if old.Rev != rev {
		h.accessFailed(w, &store.ErrPolicyStale{Rev: old.Rev})
		return
	}
	if h.accessFailed(w, next.Validate(old, nil)) {
		return
	}
	resources, err := h.allAccessResources("")
	if h.failed(w, err) {
		return
	}
	acc.prefetchResources(resources)
	next.UpdatedBy = h.orgActor(r)
	saved, err := h.config.Store.SavePolicyAt(next, rev, acc.time())
	if h.accessFailed(w, err) {
		return
	}

	clearPhase(w)
	changed := accessAudienceChanges(acc, saved, resources)
	events := make([]live.Event, 0, len(changed)+1)
	for _, change := range changed {
		events = append(events, h.accessEvent(change.ref, saved.UpdatedBy, change.audience))
	}
	events = append(events, live.New(eventOrgPolicyChanged, 0, api.OrgPolicyChanged{Rev: saved.Rev, By: saved.UpdatedBy}))
	h.recordOrg(r, record.OrgPolicyChanged, "org policy changed", orgObjectFactory, "")
	_ = live.AppendPolicyChange(h.eventRoot(), events...)
	h.writeOrgPolicy(w, saved)
}

// accessAudienceChange is one resource and the union of its audience before
// and after a policy change.
type accessAudienceChange struct {
	ref      Ref
	audience itemAudience
}

// accessAudienceChanges lists the resources whose audience differs between the
// old policy and the new one, or nil when there are too many to list.
func accessAudienceChanges(old *Access, policy acl.Policy, resources []accessResource) []accessAudienceChange {
	next := *old
	next.policy = policy
	var changed []accessAudienceChange
	for _, res := range resources {
		tuples := old.tuplesFor(res.item)
		before := old.rules().Audience(old.time(), old.policyItem(res.item, tuples))
		after := policy.Audience(next.time(), next.policyItem(res.item, tuples))
		if before.Equal(after) {
			continue
		}
		if len(changed) == live.PolicyChangeLimit {
			return nil
		}
		changed = append(changed, accessAudienceChange{ref: res.ref, audience: acl.Union(before, after)})
	}
	return changed
}

// resourceLive is a resource.changed event as it is stored, with the audience
// that filters it; the audience is stripped before a client sees it.
type resourceLive struct {
	api.ResourceChanged
	itemAudience
}

// publishAccessEvent appends one change event for a resource to the stream.
func (h *Handler) publishAccessEvent(ref Ref, by string, audience itemAudience) {
	_ = live.Append(h.eventRoot(), h.accessEvent(ref, by, audience))
}

// accessEvent builds the event for a resource of any kind: workstreams and
// canvases keep the events their clients already follow, and every other
// kind uses resource.changed.
func (h *Handler) accessEvent(ref Ref, by string, audience itemAudience) live.Event {
	switch ref.Kind {
	case ItemWorkstream, "space":
		e := workstreamLive{WorkstreamEvent: api.WorkstreamEvent{Kind: api.EventWorkstreamChanged, ID: ref.ID, By: by}, itemAudience: audience}
		return live.New(api.EventWorkstreamChanged, 0, e)
	case ItemCanvas:
		h.canvasFrames.forget(ref.ID)
		e := canvasLive{CanvasEvent: api.CanvasEvent{Kind: api.EventCanvasChanged, ID: ref.ID, By: by}, itemAudience: audience}
		return live.New(api.EventCanvasChanged, 0, e)
	default:
		e := resourceLive{ResourceChanged: ref, itemAudience: audience}
		return live.New(eventResourceChanged, 0, e)
	}
}

// accessEventForViewer filters an access event for one subscriber: a policy
// change reaches every member, and a resource event reaches only someone in the
// stored audience, stripped of that audience before it is sent.
func (h *Handler) accessEventForViewer(r *http.Request, e live.Event) (live.Event, bool) {
	v, err := h.itemViewer(r)
	if err != nil {
		return e, false
	}
	if e.Kind == eventOrgPolicyChanged {
		return e, v.Member
	}
	var stored resourceLive
	if json.Unmarshal(e.Data, &stored) != nil || stored.ID == "" || !v.hears(stored.itemAudience) {
		return e, false
	}
	data, err := json.Marshal(stored.ResourceChanged)
	if err != nil {
		return e, false
	}
	e.Data = data
	return e, true
}

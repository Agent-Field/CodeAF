package serve

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// listAccessGrants lists the live tuples on a resource for anyone who may view
// it: the owner, the legacy shares (source "share"), the stored grants and the
// grants the policy wrote for itself (source "policy"). A private resource
// lists only its owner, and, once grants on itself have opened it, those grants.
func (h *Handler) listAccessGrants(w http.ResponseWriter, r *http.Request) {
	res, v, ok := h.findAccessResource(w, r)
	if !ok {
		return
	}
	now, p := v.acc.time(), v.acc.rules()
	data := v.acc.item(res.item)
	out := api.ResourceGrants{Resource: res.ref, Grants: []api.GrantTuple{
		{AccessGrant: acl.Grant{ID: "owner:" + data.OwnerKind + ":" + data.OwnerID,
			Subject: acl.Subject{Kind: data.OwnerKind, ID: data.OwnerID}, Resource: res.ref, Role: p.OwnerRole}, Source: "owner", Live: true},
	}}
	if !data.Private || data.Closed {
		for _, share := range data.Shares {
			out.Grants = append(out.Grants, api.GrantTuple{AccessGrant: acl.Grant{ID: "share:" + share.Kind + ":" + share.ID,
				Subject: acl.Subject{Kind: share.Kind, ID: share.ID}, Resource: res.ref, Role: share.Role}, Source: "share", Live: true})
		}
		for _, g := range data.Grants {
			if !g.Live(now) {
				continue
			}
			source := "grant"
			if g.GrantedBy.Kind == "system" {
				source = "policy"
			}
			out.Grants = append(out.Grants, api.GrantTuple{AccessGrant: g, Source: source, Live: true})
		}
	}
	h.write(w, http.StatusOK, out)
}

// grantSubject checks that the subject of a new grant names a real person,
// team, service or the whole organization, and refuses an agent, whose
// permissions are policy data.
func (h *Handler) grantSubject(subject acl.Subject) (acl.Subject, error) {
	subject.ID = strings.TrimSpace(subject.ID)
	switch subject.Kind {
	case "person":
		subject.ID = factory.NormalizeHandle(subject.ID)
		if !personHandle.MatchString(subject.ID) || (!h.isMember(subject.ID) && !slices.ContainsFunc(h.config.Team, func(handle string) bool {
			return factory.NormalizeHandle(handle) == subject.ID
		})) {
			return subject, &acl.ValidationError{Message: "choose a person in this organization by their handle"}
		}
	case "team":
		if !h.teamKnown(subject.ID) {
			return subject, &acl.ValidationError{Message: "choose a team in this organization"}
		}
	case "org":
		if subject.ID != "*" {
			return subject, &acl.ValidationError{Message: "use * to share with every member of the organization"}
		}
	case "service":
		if subject.ID == "" {
			return subject, &acl.ValidationError{Message: "a service needs an id"}
		}
	case "agent":
		return subject, &acl.ValidationError{Message: "give an agent its permissions in the organization policy"}
	default:
		return subject, &acl.ValidationError{Message: "choose a person, team, service, or the organization"}
	}
	return subject, nil
}

// requestedGrant checks add and preview against the same rights without
// writing anything. The expiry words preserve preview's existing response.
func (h *Handler) requestedGrant(w http.ResponseWriter, r *http.Request, v Viewer, res accessResource, before acl.Item, expiryWords string) (acl.Grant, bool) {
	var body api.GrantNew
	if !h.decode(w, r, &body, false) {
		return acl.Grant{}, false
	}
	subject, err := h.grantSubject(body.Subject)
	if h.accessFailed(w, err) {
		return acl.Grant{}, false
	}
	now := v.acc.time()
	if body.ExpiresAt != nil && !now.Before(*body.ExpiresAt) {
		h.fail(w, http.StatusUnprocessableEntity, expiryWords)
		return acl.Grant{}, false
	}
	if v.Handle == "" {
		h.fail(w, http.StatusForbidden, "sign in to share this item")
		return acl.Grant{}, false
	}
	// A document's private flag is authoritative until it is changed in its own route.
	if before.Private && !res.item.registered {
		h.fail(w, http.StatusForbidden, "change this item's privacy before sharing it")
		return acl.Grant{}, false
	}
	if h.accessFailed(w, acl.MayGrant(v.acc.rules(), v.caller(), before, subject, body.Role, now)) {
		return acl.Grant{}, false
	}
	return acl.Grant{Subject: subject, Resource: Ref{Kind: res.item.Kind, ID: res.item.ID},
		Role: body.Role, GrantedBy: accessPrincipal(v), CreatedAt: now, ExpiresAt: body.ExpiresAt, Note: body.Note}, true
}

// addAccessGrant adds a grant, records who made it, and tells everyone whose
// access changed.
func (h *Handler) addAccessGrant(w http.ResponseWriter, r *http.Request) {
	res, v, ok := h.findAccessResource(w, r)
	if !ok {
		return
	}
	raw := v.acc.tuplesFor(res.item)
	before := v.acc.policyItem(res.item, raw)
	g, ok := h.requestedGrant(w, r, v, res, before, store.ErrGrantExpiry.Error())
	if !ok {
		return
	}
	var parent *Ref
	if res.item.registered {
		parent = &res.item.Parent
	}
	g, err := h.config.Store.AddGrantAtPolicy(g, v.acc.rules().Rev, h.clock, parent)
	if h.accessFailed(w, err) {
		return
	}
	clearPhase(w)
	raw.Grants = append(slices.Clone(raw.Grants), g)
	after := v.acc.policyItem(res.item, raw)
	h.grantChanged(r, res, v, before, after, "shared")
	if g.ExpiresAt != nil {
		h.scheduleGrantSweep()
	}
	h.write(w, http.StatusCreated, g)
}

// removeAccessGrant removes a stored grant. A legacy share is not one: the old
// share route still owns those, and the refusal names it. NOT GENERIC YET.
func (h *Handler) removeAccessGrant(w http.ResponseWriter, r *http.Request) {
	res, v, ok := h.findAccessResource(w, r)
	if !ok {
		return
	}
	raw := v.acc.tuplesFor(res.item)
	before := v.acc.policyItem(res.item, raw)
	p, now := v.acc.rules(), v.acc.time()
	gid := r.PathValue("grant")
	if strings.HasPrefix(gid, "share:") {
		// NOT GENERIC YET: legacy shares.
		if !p.MayShare(v.caller(), before, now) {
			h.fail(w, http.StatusForbidden, "you cannot share this item")
			return
		}
		route := "PUT /v1/workstreams/{id}/shares"
		if res.item.Kind == ItemCanvas && res.item.Parent.ID == "" {
			route = "PUT /v1/canvases/{id}/shares"
		}
		h.fail(w, http.StatusConflict, "change existing shares with "+route)
		return
	}
	index := slices.IndexFunc(raw.Grants, func(g acl.Grant) bool {
		return g.ID == gid && g.Resource.Kind == res.item.Kind && g.Resource.ID == res.item.ID
	})
	if index < 0 {
		h.fail(w, http.StatusNotFound, "grant not found on this item")
		return
	}
	if raw.Grants[index].GrantedBy != accessPrincipal(v) && !p.MayShare(v.caller(), before, now) {
		h.fail(w, http.StatusForbidden, "only someone who can share this item or the person who made the grant may remove it")
		return
	}
	if h.accessFailed(w, h.config.Store.RemoveGrantAtPolicy(gid, p.Rev)) {
		return
	}
	clearPhase(w)
	rearm := raw.Grants[index].ExpiresAt != nil
	raw.Grants = slices.Delete(slices.Clone(raw.Grants), index, index+1)
	h.grantChanged(r, res, v, before, v.acc.policyItem(res.item, raw), "removed access")
	if rearm {
		h.scheduleGrantSweep()
	}
	w.WriteHeader(http.StatusNoContent)
}

// grantChanged publishes the event for a grant added or removed, to the union
// of the audience before and after, and records the change like a share.
func (h *Handler) grantChanged(r *http.Request, res accessResource, v Viewer, before, after acl.Item, verb string) {
	p, now := v.acc.rules(), v.acc.time()
	h.publishAccessEvent(res.ref, v.Handle, p.Audience(now, before, after))
	object := orgObjectResource + res.ref.Kind + "/" + res.ref.ID
	if res.item.Kind == ItemWorkstream {
		object = orgObjectWorkstream + res.ref.ID
	}
	h.recordOrg(r, record.ResourceAccessChanged, verb, object, "")
}

// previewAccessGrant says who would gain access if the grant were added, and
// writes nothing.
func (h *Handler) previewAccessGrant(w http.ResponseWriter, r *http.Request) {
	res, v, ok := h.findAccessResource(w, r)
	if !ok {
		return
	}
	raw := v.acc.tuplesFor(res.item)
	before := v.acc.policyItem(res.item, raw)
	grant, ok := h.requestedGrant(w, r, v, res, before, "the expiration must be in the future")
	if !ok {
		return
	}
	raw.Grants = append(slices.Clone(raw.Grants), grant)
	after := v.acc.policyItem(res.item, raw)
	p, now := v.acc.rules(), v.acc.time()
	out := api.GrantPreview{AudienceBefore: p.Audience(now, before), AudienceAfter: p.Audience(now, after)}
	out.Gains, out.Loses = h.accessAudienceDiff(r, out.AudienceBefore, out.AudienceAfter)
	h.write(w, http.StatusOK, out)
}

// accessAudienceDiff names the members who hear an item in one audience and
// not the other.
func (h *Handler) accessAudienceDiff(r *http.Request, before, after acl.Audience) (gains, loses []string) {
	gains, loses = []string{}, []string{}
	nameOf := h.personNames()
	membership := h.Membership()
	members := h.memberList()
	operator := factory.NormalizeHandle(h.operatorLogin(r))
	for _, m := range members {
		handle := factory.NormalizeHandle(m.Handle)
		c := acl.Caller{Handle: handle, Member: handle != "", Operator: handle != "" && handle == operator, Teams: map[string]bool{}}
		for _, team := range membership.TeamsOf(handle) {
			c.Teams[team] = true
		}
		had, has := before.Hears(c), after.Hears(c)
		if !had && has {
			gains = append(gains, nameOf(handle))
		}
		if had && !has {
			loses = append(loses, nameOf(handle))
		}
	}
	slices.Sort(gains)
	slices.Sort(loses)
	return slices.Compact(gains), slices.Compact(loses)
}

// orgDefaultGrant is the grant a registration writes for itself when the policy
// makes a kind visible to the organization: every member at the policy's
// organization role, recorded as the policy's own doing rather than a person's.
func orgDefaultGrant(p acl.Policy, ref Ref, now time.Time) *acl.Grant {
	return &acl.Grant{Resource: ref, Subject: acl.Subject{Kind: "org", ID: "*"}, Role: p.OrgRole,
		GrantedBy: acl.Principal{Kind: "system", ID: "organization"}, CreatedAt: now}
}

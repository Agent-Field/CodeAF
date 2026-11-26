package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// The events the access model adds. resource.changed says a registered
// resource's audience changed and carries only its kind and id, so no event
// reveals anything a client could not already fetch; org.policy_changed asks
// every member to refetch because the policy itself moved.
const (
	eventResourceChanged  = "resource.changed"
	eventOrgPolicyChanged = "org.policy_changed"
)

// accessResource is one thing the access API can name: its Item (what the
// decider reads), the Item of the space it sits in, its reference, a label and,
// for a registered resource, who created it and when.
type accessResource struct {
	item      Item
	parent    *Item
	ref       Ref
	label     string
	createdBy *acl.Principal
	createdAt *time.Time
}

// accessRoutes mounts the policy, resources and grants routes.
func (h *Handler) accessRoutes() {
	h.route("/v1/org/policy", map[string]http.HandlerFunc{http.MethodGet: h.getOrgPolicy, http.MethodPut: h.setOrgPolicy})
	h.route("/v1/resources", map[string]http.HandlerFunc{http.MethodGet: h.listAccessResources, http.MethodPost: h.registerAccessResource})
	h.read("/v1/resources/{kind}", h.listAccessResources)
	h.read("/v1/resources/{kind}/{id}", h.getAccessResource)
	h.read("/v1/resources/{kind}/{id}/transcript", h.accessTranscript)
	h.route("/v1/resources/{kind}/{id}/grants", map[string]http.HandlerFunc{http.MethodGet: h.listAccessGrants, http.MethodPost: h.addAccessGrant})
	h.route("/v1/resources/{kind}/{id}/grants/preview", map[string]http.HandlerFunc{http.MethodPost: h.previewAccessGrant})
	h.route("/v1/resources/{kind}/{id}/grants/{grant}", map[string]http.HandlerFunc{http.MethodDelete: h.removeAccessGrant})
	h.route("/v1/resources/{kind}/{id}/move", map[string]http.HandlerFunc{http.MethodPost: h.moveAccessResource})
}

// accessViewer answers who is asking and refuses an anonymous caller, so the
// access routes never reveal that a resource exists to someone signed out.
func (h *Handler) accessViewer(w http.ResponseWriter, r *http.Request) (Viewer, bool) {
	if !h.require(w, h.config.Store != nil) {
		return Viewer{}, false
	}
	v, err := h.itemViewer(r)
	if h.failed(w, err) {
		return v, false
	}
	if !v.Member && !v.Operator {
		h.fail(w, http.StatusForbidden, "sign in as an organization member first")
		return v, false
	}
	return v, true
}

// workstreamResource is a workstream or space seen as a resource.
func workstreamResource(ws factory.OrgWorkstream) accessResource {
	kind := ItemWorkstream
	if ws.Space {
		kind = "space"
	}
	creator := acl.Principal{Kind: "person", ID: ws.CreatedBy}
	return accessResource{item: workstreamItem(ws), ref: Ref{Kind: kind, ID: ws.ID}, label: ws.Name,
		createdBy: &creator, createdAt: &ws.CreatedAt}
}

// canvasResource is a canvas seen as a resource, inheriting what its
// workstream gives it.
func (h *Handler) canvasResource(c factory.OrgCanvas, ws factory.OrgWorkstream, row store.Resource) accessResource {
	item := canvasItemIn(c, ws)
	res := accessResource{item: item, ref: Ref{Kind: ItemCanvas, ID: c.ID}, createdAt: &c.CreatedAt}
	if ws.ID != "" {
		parent := workstreamItem(ws)
		res.parent = &parent
	}
	if row.CreatedBy.Kind != "" {
		res.label, res.createdBy = row.Label, &row.CreatedBy
	} else if c.CreatedBy != "" {
		res.createdBy = &acl.Principal{Kind: "person", ID: c.CreatedBy}
	}
	return res
}

// resourceItem builds the Item of a registered resource from its registry row
// and the space it sits in. THE LAW OF A PRIVATE CHAT: a registration that is
// private is its owner's alone, so the space's people do not reach it.
func (h *Handler) resourceItem(row store.Resource, parent factory.OrgWorkstream) Item {
	it := Item{Kind: row.Kind, ID: row.ID, Parent: row.Parent, Visibility: row.Visibility, registered: true,
		Owner: factory.WorkstreamOwner{Kind: row.Owner.Kind, ID: row.Owner.ID}, Private: row.Visibility == acl.Private}
	if parent.ID != "" && !parent.Private {
		it.Shares, it.parentOwner = parent.Shares, parent.Owner
	}
	it.parentPrivate = parent.Private
	if parent.Private && taskSession(it) {
		// NOT GENERIC YET: A task conversation follows a private workstream's audience.
		it.Private, it.parentOwner = true, parent.Owner
	}
	return it
}

// registeredResource is a registry row seen as a resource.
func (h *Handler) registeredResource(row store.Resource, parent factory.OrgWorkstream) accessResource {
	res := accessResource{item: h.resourceItem(row, parent), ref: row.Resource, label: row.Label,
		createdBy: &row.CreatedBy, createdAt: &row.CreatedAt}
	if parent.ID != "" {
		item := workstreamItem(parent)
		res.parent = &item
	}
	return res
}

// lookupAccessResource finds a resource by kind and id wherever it lives
// (a workstream, a canvas, or the registry) and says false when none is kept.
func (h *Handler) lookupAccessResource(kind, id string) (accessResource, bool, error) {
	st := h.config.Store
	switch kind {
	case ItemWorkstream, "space":
		ws, ok, err := st.Workstream(id)
		if err != nil || !ok || (kind == "space" && !ws.Space) {
			return accessResource{}, false, err
		}
		res := workstreamResource(ws)
		res.ref.Kind = kind
		return res, true, nil
	case ItemCanvas:
		c, ok, err := st.Canvas(id)
		if err != nil || !ok {
			return accessResource{}, false, err
		}
		var ws factory.OrgWorkstream
		if c.Workstream != "" {
			ws, _, err = st.Workstream(c.Workstream)
			if err != nil {
				return accessResource{}, false, err
			}
		}
		row, _, err := st.Resource(kind, id)
		return h.canvasResource(c, ws, row), err == nil, err
	default:
		if !acl.ValidResourceKind(kind) {
			return accessResource{}, false, nil
		}
		row, ok, err := st.Resource(kind, id)
		if err != nil || !ok {
			return accessResource{}, false, err
		}
		var parent factory.OrgWorkstream
		if row.Parent.ID != "" {
			parent, _, err = st.Workstream(row.Parent.ID)
			if err != nil {
				return accessResource{}, false, err
			}
		}
		return h.registeredResource(row, parent), true, nil
	}
}

// allAccessResources lists every resource of a kind, or of every kind when
// kind is empty, unfiltered; callers keep only what the viewer may see.
func (h *Handler) allAccessResources(kind string) ([]accessResource, error) {
	st := h.config.Store
	workstreams, err := st.Workstreams()
	if err != nil {
		return nil, err
	}
	out := []accessResource{}
	for _, ws := range workstreams {
		if kind == "" || kind == ItemWorkstream && !ws.Space || kind == "space" && ws.Space {
			out = append(out, workstreamResource(ws))
		}
	}
	canvases := factory.OrgCanvases{}
	if kind == "" || kind == ItemCanvas {
		canvases, err = st.Canvases()
		if err != nil {
			return nil, err
		}
	}
	rows, err := st.Resources(kind)
	if err != nil {
		return nil, err
	}
	canvasRows := make(map[string]store.Resource)
	for _, row := range rows {
		switch row.Kind {
		case ItemCanvas:
			canvasRows[row.ID] = row
		case ItemWorkstream, "space":
			// The workstream document supplies its own current relationships.
		default:
			out = append(out, h.registeredResource(row, workstreams[row.Parent.ID]))
		}
	}
	for _, c := range canvases {
		out = append(out, h.canvasResource(c, workstreams[c.Workstream], canvasRows[c.ID]))
	}
	slices.SortFunc(out, func(a, b accessResource) int {
		if n := strings.Compare(a.ref.Kind, b.ref.Kind); n != 0 {
			return n
		}
		return strings.Compare(a.ref.ID, b.ref.ID)
	})
	return out, nil
}

// findAccessResource resolves the path's resource for a viewer who may view
// it. Anyone who may not gets 404, never 403, so an id is not a way to learn
// what exists.
func (h *Handler) findAccessResource(w http.ResponseWriter, r *http.Request) (accessResource, Viewer, bool) {
	v, ok := h.accessViewer(w, r)
	if !ok {
		return accessResource{}, v, false
	}
	res, found, err := h.lookupAccessResource(r.PathValue("kind"), r.PathValue("id"))
	if h.failed(w, err) {
		return accessResource{}, v, false
	}
	if !found || !can(v, res.item, acl.VerbView) {
		h.fail(w, http.StatusNotFound, "resource not found")
		return accessResource{}, v, false
	}
	return res, v, true
}

// accessResourceWire is the wire form of a resource for one viewer, with the
// role and verbs that viewer holds on it.
func accessResourceWire(res accessResource, v Viewer) api.OrgResource {
	it, data := res.item, v.acc.item(res.item)
	p, now := v.acc.rules(), v.acc.time()
	visibility := acl.Owner
	if data.Private {
		visibility = acl.Private
	} else if p.Audience(now, data).Org {
		visibility = acl.Org
	}
	owner := accessSubject(it.Owner.Kind, it.Owner.ID)
	out := api.OrgResource{ResourceRef: res.ref, Label: res.label, Owner: owner,
		Visibility: visibility, MyRole: p.RoleOf(v.caller(), data, now), MyVerbs: p.Verbs(v.caller(), data, now).List(),
	}
	if res.createdBy != nil && res.createdBy.ID != "" {
		out.CreatedBy = res.createdBy
	}
	if res.createdAt != nil && !res.createdAt.IsZero() {
		out.CreatedAt = res.createdAt
	}
	// A shared child must not identify a private parent the caller cannot see.
	if it.Parent.ID != "" && (res.parent == nil || !res.parent.Private || can(v, *res.parent, acl.VerbView)) {
		out.Parent = &it.Parent
	}
	return out
}

func (h *Handler) getAccessResource(w http.ResponseWriter, r *http.Request) {
	if res, v, ok := h.findAccessResource(w, r); ok {
		h.write(w, http.StatusOK, accessResourceWire(res, v))
	}
}

func (h *Handler) listAccessResources(w http.ResponseWriter, r *http.Request) {
	v, ok := h.accessViewer(w, r)
	if !ok {
		return
	}
	kind := r.PathValue("kind")
	if kind == "" {
		kind = r.URL.Query().Get("kind")
	}
	if kind != "" && !acl.ValidResourceKind(kind) {
		h.fail(w, http.StatusNotFound, "resource kind not found")
		return
	}
	resources, err := h.allAccessResources(kind)
	if h.failed(w, err) {
		return
	}
	v.acc.prefetchResources(resources)
	out := api.ResourceList{Resources: []api.OrgResource{}}
	for _, res := range resources {
		if can(v, res.item, acl.VerbView) {
			out.Resources = append(out.Resources, accessResourceWire(res, v))
		}
	}
	h.write(w, http.StatusOK, out)
}

// accessPrincipal is who a change is recorded against: the agent and its
// sponsor when an agent acted, else the person.
func accessPrincipal(v Viewer) acl.Principal {
	if v.Agent != nil {
		if id, bot := strings.CutPrefix(v.Agent.ID, botAgentPrefix); bot {
			return acl.Principal{Kind: "bot", ID: id, Sponsor: v.Agent.Sponsor}
		}
		return acl.Principal{Kind: "agent", ID: v.Agent.ID, Sponsor: v.Agent.Sponsor}
	}
	return acl.Principal{Kind: "person", ID: v.Handle}
}

// mayCreateResource refuses an agent when the policy does not let agents create.
func (h *Handler) mayCreateResource(w http.ResponseWriter, v Viewer) bool {
	if v.Agent != nil && !v.acc.rules().Agents.MayCreate {
		h.fail(w, http.StatusForbidden, "agents cannot create resources under the organization policy")
		return false
	}
	return true
}

// registerAccessResource creates a chat as a registered resource in a
// workstream the caller may edit, owned by the caller (the sponsor, when an
// agent acts) and starting with the visibility the policy sets for chats.
func (h *Handler) registerAccessResource(w http.ResponseWriter, r *http.Request) {
	v, ok := h.accessViewer(w, r)
	if !ok || !h.mayCreateResource(w, v) {
		return
	}
	var body api.ResourceNew
	if !h.decode(w, r, &body, false) {
		return
	}
	// NOT GENERIC YET: Afi panes and task conversations register through their own routes.
	if body.Kind != "chat" {
		h.fail(w, http.StatusUnprocessableEntity, "only chats can be registered here")
		return
	}
	if v.Handle == "" {
		h.fail(w, http.StatusForbidden, "a chat is someone's; sign in first")
		return
	}
	parent, found, err := h.config.Store.Workstream(strings.TrimSpace(body.Parent))
	if h.failed(w, err) {
		return
	}
	if !found || !v.canSee(parent) {
		h.fail(w, http.StatusNotFound, "workstream not found")
		return
	}
	if !v.canWrite(parent) {
		h.fail(w, http.StatusForbidden, "you need permission to edit the workstream to create a chat")
		return
	}
	if parent.ArchivedAt != nil {
		h.fail(w, http.StatusConflict, workstreamArchivedWords)
		return
	}
	title := strings.Join(strings.Fields(body.Label), " ")
	if len(title) > api.AgentChatTitleMost {
		h.fail(w, http.StatusUnprocessableEntity, "a chat's title is at most 120 bytes")
		return
	}
	if len(h.myChats(parent.ID, v.Handle)) >= api.AgentChatsMost {
		h.fail(w, http.StatusConflict, "you keep 500 chats here already; delete one first")
		return
	}
	row, err := h.keepNewChat(v, parent, v.Handle, title, "")
	if h.accessFailed(w, err) {
		return
	}
	clearPhase(w)
	v.acc = h.access()
	res := h.registeredResource(row, parent)
	h.publishAccessEvent(res.ref, v.Handle, v.acc.audienceOf(res.item))
	h.write(w, http.StatusCreated, accessResourceWire(res, v))
}

// accessFailed writes the right refusal for an access error: 422 for a
// malformed request, 403 for one that is not allowed, 409 when the target
// changed under the caller, 412 for a stale policy. It returns false when err
// is nil or is an ordinary failure another door handles.
func (h *Handler) accessFailed(w http.ResponseWriter, err error) bool {
	var invalid *acl.ValidationError
	var forbidden *acl.PermissionError
	var stale *store.ErrPolicyStale
	switch {
	case errors.As(err, &invalid):
		h.fail(w, http.StatusUnprocessableEntity, invalid.Error())
	case errors.Is(err, store.ErrGrantExpiry):
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
	case errors.As(err, &forbidden):
		h.fail(w, http.StatusForbidden, forbidden.Error())
	case errors.Is(err, store.ErrGrantTargetChanged):
		h.fail(w, http.StatusConflict, err.Error())
	case errors.As(err, &stale):
		w.Header().Set("ETag", orgPolicyETag(stale.Rev))
		h.fail(w, http.StatusPreconditionFailed, stale.Error())
	default:
		return h.failed(w, err)
	}
	return true
}

// readCreatePrivacy reads the legacy private key of a create body when it is
// present. NOT GENERIC YET: a policy default for the kind applies only when
// that key is absent, because the key is older than the policy and a caller who
// sends it has said what they want.
func (h *Handler) readCreatePrivacy(w http.ResponseWriter, raw json.RawMessage, private *bool, status int) bool {
	if raw == nil {
		return true
	}
	if err := json.Unmarshal(raw, private); err != nil {
		words := "the body is not the JSON this route takes"
		if status == http.StatusUnprocessableEntity {
			words = "invalid JSON body: " + err.Error()
		}
		h.fail(w, status, words)
		return false
	}
	return true
}

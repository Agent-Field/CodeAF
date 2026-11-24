package serve

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

const hostedOwnerManage = "Only the owner of this chat can stop or delete it."

type hostedWireClass uint8

const (
	hostedWireNone hostedWireClass = iota
	hostedWireOwner
	hostedWireGuest
)

// hostedWireClassFor strips operator access because the wire opens the owner's live conversation.
func hostedWireClassFor(v Viewer, item Item, owner string) hostedWireClass {
	v = hostedWithoutOperator(v)
	if !can(v, item, acl.VerbView) {
		return hostedWireNone
	}
	if v.Handle == owner && can(v, item, acl.VerbRun) {
		return hostedWireOwner
	}
	return hostedWireGuest
}

func hostedWithoutOperator(v Viewer) Viewer {
	v.Operator = false
	return v
}

func (h *Handler) hostedViewer(w http.ResponseWriter, r *http.Request) (Viewer, bool) {
	if h.config.Store == nil {
		// With no store there are no teams and no grants, so the viewer is the person under the default policy.
		person, ok := h.hostedPerson(w, r)
		return h.viewerOf(person), ok
	}
	v, err := h.itemViewer(r)
	return v, h.hostedSignedIn(w, v.Handle, err)
}

// hostedRegistration only accepts parentless person-owned hosted chats because other session resources live elsewhere.
func hostedRegistration(row store.Resource) bool {
	return row.Parent == (acl.Resource{}) && row.Owner.Kind == "person" && row.Owner.ID != "" && hostedIDForm.MatchString(row.ID)
}

func hostedResourceRow(id, owner string, creator acl.Principal, visibility string, at time.Time) store.Resource {
	return store.Resource{
		Resource:   Ref{Kind: store.SessionKind, ID: id},
		Owner:      acl.Subject{Kind: "person", ID: owner},
		CreatedBy:  creator,
		Visibility: visibility,
		CreatedAt:  at.UTC(),
	}
}

func (h *Handler) registerHostedChat(v Viewer, id string) (store.Resource, error) {
	p := v.acc.rules()
	row := hostedResourceRow(id, v.Handle, accessPrincipal(v), p.Visibility(store.SessionKind), h.clock())
	var grant *acl.Grant
	if row.Visibility == acl.Org {
		grant = orgDefaultGrant(p, row.Resource, row.CreatedAt)
	}
	return row, h.config.Store.RegisterResource(row, grant, p.Rev, nil)
}

func (h *Handler) backfillHostedChat(id, owner, created string) (store.Resource, error) {
	at, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		at = h.clock()
	}
	row := hostedResourceRow(id, owner, acl.Principal{Kind: "person", ID: owner}, acl.Private, at)
	return row, h.config.Store.BackfillResource(row)
}

type hostedChatLookup struct {
	item         Item
	owner        string
	legacyHeader http.Header
	legacyBody   []byte
}

func (h *Handler) resolveHostedChat(w http.ResponseWriter, r *http.Request, id string, v Viewer) (hostedChatLookup, bool) {
	hs := h.config.Hosted.state
	hs.registry.RLock()
	defer hs.registry.RUnlock()
	return h.lookupHostedChat(w, r, id, v)
}

// lookupHostedChat resolves the registry before asking the host. Only a successful read as
// the caller proves ownership of a chat from before registration was introduced.
func (h *Handler) lookupHostedChat(w http.ResponseWriter, r *http.Request, id string, v Viewer) (hostedChatLookup, bool) {
	st := h.config.Store
	row, found, err := st.Resource(store.SessionKind, id)
	if h.failed(w, err) {
		return hostedChatLookup{}, false
	}
	var chat hostedChatLookup
	if !found {
		hs := h.config.Hosted.state
		status, header, body, err := hs.call(r.Context(), http.MethodGet, "/v1/sessions/"+id, v.Handle, nil)
		if h.hostedFailed(w, status, err) {
			return chat, false
		}
		if status == http.StatusNotFound {
			hostedNoSuchChatWrite(w)
			return chat, false
		}
		if status != http.StatusOK {
			hostedRelay(w, status, header, body)
			return chat, false
		}
		var data hostedStatusFields
		if json.Unmarshal(body, &data) != nil || data.ID != id {
			h.fail(w, http.StatusBadGateway, hostedUnreachable)
			return chat, false
		}
		if _, err := h.backfillHostedChat(id, v.Handle, data.Created); h.failed(w, err) {
			return chat, false
		}
		// Another server may have registered this chat after the first read.
		row, found, err = st.Resource(store.SessionKind, id)
		if h.failed(w, err) {
			return chat, false
		}
		if row.Owner.ID == v.Handle {
			chat.legacyHeader, chat.legacyBody = header, body
		}
	}
	if !found || !hostedRegistration(row) {
		hostedNoSuchChatWrite(w)
		return hostedChatLookup{}, false
	}
	chat.item, chat.owner = h.resourceItem(row, factory.OrgWorkstream{}), row.Owner.ID
	return chat, true
}

type hostedStatusFields struct {
	ID         string `json:"id"`
	State      string `json:"state"`
	Created    string `json:"created"`
	LastActive string `json:"last_active"`
}

type hostedSharedRow struct {
	hostedStatusFields
	Owner  string  `json:"owner"`
	Role   *string `json:"role,omitempty"`
	Access string  `json:"access"`
}

// hostedMetadata shows an operator-only row without a role because an operator's metadata access does not open the chat.
func hostedMetadata(v Viewer, item Item) hostedSharedRow {
	v = hostedWithoutOperator(v)
	row := hostedSharedRow{Owner: item.Owner.ID, Access: "operator"}
	if can(v, item, acl.VerbView) {
		role := roleOf(v, item)
		row.Role, row.Access = &role, "shared"
	}
	return row
}

type hostedSessionsList struct {
	Sessions json.RawMessage `json:"sessions"`
}

func (h *Handler) hostedOwnList(w http.ResponseWriter, r *http.Request, v Viewer) (hostedSessionsList, []accessResource, bool) {
	hs := h.config.Hosted.state
	hs.registry.RLock()
	defer hs.registry.RUnlock()
	status, header, body, err := hs.call(r.Context(), http.MethodGet, "/v1/sessions", v.Handle, nil)
	var own hostedSessionsList
	if h.hostedFailed(w, status, err) {
		return own, nil, false
	}
	st := h.config.Store
	if st == nil || status != http.StatusOK {
		hostedRelay(w, status, header, body)
		return own, nil, false
	}
	var rows []json.RawMessage
	if json.Unmarshal(body, &own) != nil || json.Unmarshal(own.Sessions, &rows) != nil {
		h.fail(w, http.StatusBadGateway, hostedUnreachable)
		return own, nil, false
	}
	registered, err := st.Resources(store.SessionKind)
	if h.failed(w, err) {
		return own, nil, false
	}
	byID := make(map[string]store.Resource, len(registered))
	var others []accessResource
	for _, row := range registered {
		byID[row.ID] = row
		if hostedRegistration(row) && row.Owner.ID != v.Handle {
			others = append(others, h.registeredResource(row, factory.OrgWorkstream{}))
		}
	}
	kept := make([]json.RawMessage, 0, len(rows))
	for _, raw := range rows {
		var status hostedStatusFields
		if json.Unmarshal(raw, &status) != nil || !hostedIDForm.MatchString(status.ID) {
			h.fail(w, http.StatusBadGateway, hostedUnreachable)
			return own, nil, false
		}
		row, found := byID[status.ID]
		if !found {
			row, err = h.backfillHostedChat(status.ID, v.Handle, status.Created)
			if h.failed(w, err) {
				return own, nil, false
			}
		}
		if hostedRegistration(row) && row.Owner.ID == v.Handle && can(v, h.resourceItem(row, factory.OrgWorkstream{}), acl.VerbView) {
			kept = append(kept, raw)
		}
	}
	if len(kept) != len(rows) {
		own.Sessions, _ = json.Marshal(kept)
	}
	return own, others, true
}

func (h *Handler) hostedSharedList(w http.ResponseWriter, r *http.Request, v Viewer, others []accessResource) ([]hostedSharedRow, bool) {
	v.acc.prefetchResources(others)
	owners := map[string]map[string]hostedSharedRow{}
	for _, res := range others {
		if !can(v, res.item, acl.VerbView) {
			continue
		}
		row := hostedMetadata(v, res.item)
		if owners[row.Owner] == nil {
			owners[row.Owner] = map[string]hostedSharedRow{}
		}
		owners[row.Owner][res.ref.ID] = row
	}
	shared := []hostedSharedRow{}
	hs := h.config.Hosted.state
	for _, owner := range slices.Sorted(maps.Keys(owners)) {
		status, _, body, err := hs.call(r.Context(), http.MethodGet, "/v1/sessions", owner, nil)
		var listing hostedSessionsList
		var rows []hostedStatusFields
		if err != nil || status != http.StatusOK || json.Unmarshal(body, &listing) != nil || json.Unmarshal(listing.Sessions, &rows) != nil {
			h.fail(w, http.StatusBadGateway, hostedUnreachable)
			return nil, false
		}
		for _, status := range rows {
			if row, ok := owners[owner][status.ID]; ok {
				row.hostedStatusFields = status
				shared = append(shared, row)
			}
		}
	}
	return shared, true
}

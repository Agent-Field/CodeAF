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

const hostedOwnerManage = "Only this chat's owner, or someone it is shared with in full, can stop or delete it."

// hostedBotNeedsRun is what a bot reads when its scope does not let it run the
// chat it would start, which is every scope below `run` (view and comment).
const hostedBotNeedsRun = "This bot's scope does not let it run chats, so it cannot start one. Ask the person who made it."

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

// hostedStartItem is the chat a person is about to start, as the access core
// sees it: theirs, private, and with no grant yet, so what a caller holds on it
// is what they would hold the moment it exists.
func (h *Handler) hostedStartItem(id, owner string) Item {
	row := hostedResourceRow(id, owner, acl.Principal{Kind: "person", ID: owner}, acl.Private, h.clock())
	return h.resourceItem(row, factory.OrgWorkstream{})
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

// hostedStateRunning is the state the chats' host reports for a chat with a live process. Anything else it says (it says
// suspended) means opening the chat starts it.
const hostedStateRunning = "running"

type hostedStatusFields struct {
	ID         string `json:"id"`
	State      string `json:"state"`
	Created    string `json:"created"`
	LastActive string `json:"last_active"`
	// Title is the name the chat gave itself, as the chats' host last saw it, and it is left out of the row until the chat
	// has one (a client that ignores the field reads the row as before). The host keeps it from the chat's own wire
	// (internal/sessionhost's title_linux.go), so listing never starts a chat that is not running to ask.
	Title string `json:"title,omitempty"`
}

type hostedSharedRow struct {
	hostedStatusFields
	Owner  string  `json:"owner"`
	Role   *string `json:"role,omitempty"`
	Access string  `json:"access"`
	// Verbs is what the asker may do in the chat under the policy as it stands now.
	Verbs []string `json:"verbs"`
}

// hostedVerbsOn lists in the order of acl.Verbs what v may do in a hosted chat now. It is the
// guest wire's own decision (hostedGuestVerbs, which the wire re-reads every heartbeat), so a
// list row and the wire cannot come from two readings of the policy, and a client that shows
// controls from the row never shows one the wire's table would refuse. An owner whose policy
// still holds run is attached by the byte-for-byte proxy, which has no table; the row still
// names only what the policy gives that owner, so a row never claims more than the policy does.
func hostedVerbsOn(v Viewer, item Item) []string {
	return hostedGuestVerbs(v, item).List()
}

// hostedWithVerbs sets the verbs of one chat row the chats' host wrote and leaves every other
// field as it was. The host never decides access, so a verbs field of its own is overwritten.
func hostedWithVerbs(row []byte, verbs []string) []byte {
	var fields map[string]json.RawMessage
	if json.Unmarshal(row, &fields) != nil || fields == nil {
		return row
	}
	fields["verbs"], _ = json.Marshal(verbs)
	out, err := json.Marshal(fields)
	if err != nil {
		return row
	}
	return out
}

// hideUnreadTitle takes the title off a row whose reader does not hold the chat itself. A title is the chat's own words,
// drawn from what was said in it, so it travels only with a row that opens the chat (access "shared"): an operator's
// metadata row says who owns a chat and when it was used, and never what it is about.
func (r *hostedSharedRow) hideUnreadTitle() {
	if r.Access != "shared" {
		r.Title = ""
	}
}

// hostedMetadata shows an operator-only row without a role because an operator's metadata access does not open the chat.
func hostedMetadata(v Viewer, item Item) hostedSharedRow {
	v = hostedWithoutOperator(v)
	row := hostedSharedRow{Owner: item.Owner.ID, Access: "operator", Verbs: []string{}}
	if verbs := hostedGuestVerbs(v, item); verbs.Has(acl.VerbView) {
		role := roleOf(v, item)
		row.Role, row.Access, row.Verbs = &role, "shared", verbs.List()
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
	// The server that runs chats is always started with its store, which holds the policy. A
	// handler built without one has no policy to read and relays the host's rows as they came,
	// and a client reads a row with no verbs as view only.
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
		if !hostedRegistration(row) || row.Owner.ID != v.Handle {
			continue
		}
		if item := h.resourceItem(row, factory.OrgWorkstream{}); can(v, item, acl.VerbView) {
			kept = append(kept, hostedWithVerbs(raw, hostedVerbsOn(v, item)))
		}
	}
	own.Sessions, _ = json.Marshal(kept)
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
				row.hideUnreadTitle()
				shared = append(shared, row)
			}
		}
	}
	return shared, true
}

package serve

import (
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── ONE ACCESS CORE FOR EVERY ORGANIZATION ITEM ────────────────────────────
//
// Every organization item (a workstream, a canvas, a shared document, a chat,
// the jobs of a workstream) is decided here and nowhere else. A route turns
// its item into an [Item], asks [can] with the verb it needs, and answers 404
// when the caller may not even view it, so an id is never a way to learn
// what someone else is working on. The rules, exactly, under the DEFAULT
// policy (internal/factory/acl: what an organization's policy may change is
// there, and nothing but the policy changes it):
//
//   - THE OWNER HOLDS FULL: the person, or anyone in the owning team.
//   - A SHARE GIVES ITS ROLE to the person it names or to everyone in the team
//     it names; several shares reaching one person give the highest.
//   - A PRIVATE ITEM IS ITS OWNER'S ALONE. Its shares, if any were ever kept,
//     grant nothing, and neither do stored grants.
//   - THE OPERATOR VIEWS every item that is not private, and of a private item
//     sees NOTHING: not its name, its owner or when it changed, because a
//     name alone can say what the work is ("Layoff plan"). What the operator
//     learns of private work is totals: what each person's space spent this
//     month and how many things it holds (myspace.go, OWNER-DECISIONS §8).
//     Seeing is not writing: the operator edits
//     an item only through a role like anyone else. What the operator may do
//     beyond that (purge, reassign an offboarded person's items) is checked
//     by the route that does it, against [Viewer.Operator].
//   - ON A SOLO FACTORY there is one person, and they hold full on everything.
//
// WHERE THE RELATIONSHIPS LIVE. A workstream's or canvas's owner, shares and
// private flag stay on its document (the wire shape clients read); chats and
// anything else added later keep theirs as rows of the grants and resources
// tables (store/grants.go). [Access.tuplesFor] is the ONE reader of both, and
// [roleOf], [can] and [audienceOf] ask only it, so a route never learns which
// side a relationship came from. An agent acts for the person who sponsors it
// and holds what that person holds right now, narrowed by the policy's scope
// for it; nothing is ever copied to the agent.

// Item is the access shape of one organization item, whatever its kind.
type Item struct {
	// Kind names what the item is ("workstream", "canvas", "doc"); the rules
	// do not read it, but a refusal or an event can name it.
	Kind          string
	ID            string
	Owner         factory.WorkstreamOwner
	Shares        []factory.Share
	Private       bool
	Parent        Ref
	Visibility    string
	registered    bool
	parentOwner   factory.WorkstreamOwner
	parentPrivate bool
}

// Ref names one resource: its kind and id.
type Ref = acl.Resource

// The item kinds the core is asked about today.
const (
	ItemWorkstream = "workstream"
	ItemCanvas     = "canvas"
	ItemDoc        = "doc"
)

// Viewer is the caller as the access core sees them.
type Viewer struct {
	Handle   string
	Operator bool
	Solo     bool
	Teams    map[string]bool
	Member   bool
	Agent    *acl.Agent
	acc      *Access
}

// itemViewer is the caller of r as a [Viewer]: their handle, whether they are
// the operator, and the teams they are in, read fresh from the store.
func (h *Handler) itemViewer(r *http.Request) (Viewer, error) {
	who, err := h.callerHandle(r)
	if err != nil {
		return Viewer{}, err
	}
	v := h.viewerOf(who)
	v.Solo = h.mode() == api.FactorySolo
	v.Operator = v.Solo || h.isOperator(r)
	v.Member = v.Solo || v.Member && !h.profileless(r)
	if call, ok := agentCallOf(r); ok && call.agent != "" {
		v.Agent = &acl.Agent{ID: call.agent, Sponsor: v.Handle, Only: call.only}
		if call.bot == "" {
			v.Operator = v.Solo || v.Handle != "" && v.Handle == factory.NormalizeHandle(h.operatorLogin(r))
		} else {
			v.Operator = false
		}
	}
	return v, nil
}

// viewerOf is the person with this handle as the access core sees them, with
// the teams they are in read fresh and the organization's policy attached. It
// is how code that is not answering a request (an agent naming a doer, a card
// naming a person) asks what someone may do.
func (h *Handler) viewerOf(handle string) Viewer {
	v := Viewer{Handle: factory.NormalizeHandle(handle), Teams: map[string]bool{}, acc: h.access()}
	v.Member = v.Handle != ""
	if v.Member {
		for _, team := range h.Membership().TeamsOf(v.Handle) {
			v.Teams[team] = true
		}
	}
	return v
}

// Access is what one request needs to decide anything: the organization's
// policy as it stands now and a reader for stored grants that remembers what it
// read, so one request sees one answer. A nil *Access is the default policy and
// no stored grants, which only the tests' literals use; the one production
// constructor is [Handler.accessAt] (access_constructor_test.go says so).
type Access struct {
	policy   acl.Policy
	grants   func(kind, id string) []acl.Grant
	prefetch func([]Ref)
	now      func() time.Time
}

// access reads the policy and opens a grant reader for one request.
func (h *Handler) access() *Access {
	return h.accessAt(h.clock())
}

// accessAt fixes the check time so the same rules can read an expiry audience.
func (h *Handler) accessAt(now time.Time) *Access {
	policy := acl.DefaultPolicy()
	if h.config.Store != nil {
		var err error
		policy, err = h.config.Store.OrgPolicy()
		if err != nil {
			slog.Error("could not read the organization policy", "error", err)
		}
	}
	cache := map[Ref][]acl.Grant{}
	read := func(kind, id string) []acl.Grant {
		if h.config.Store == nil || id == "" {
			return nil
		}
		key := canonicalGrantRef(Ref{Kind: kind, ID: id})
		if rows, found := cache[key]; found {
			return rows
		}
		rows, err := h.config.Store.GrantsOn(key.Kind, key.ID)
		if err != nil {
			slog.Error("could not read the item's grants", "error", err)
		}
		cache[key] = rows
		return rows
	}
	prefetch := func(keys []Ref) {
		if h.config.Store == nil || len(keys) == 0 {
			return
		}
		missing := make([]Ref, 0, len(keys))
		for _, key := range keys {
			if _, ok := cache[key]; !ok {
				missing = append(missing, key)
			}
		}
		rows, err := h.config.Store.GrantsOnMany(missing)
		if err != nil {
			slog.Error("could not read grants together", "error", err)
			return
		}
		for _, key := range missing {
			cache[key] = rows[key]
		}
	}
	return &Access{policy: policy, now: func() time.Time { return now }, grants: read, prefetch: prefetch}
}

// at reuses one policy and grant snapshot when comparing two moments.
func (acc *Access) at(now time.Time) *Access {
	fixed := *acc
	fixed.now = func() time.Time { return now }
	return &fixed
}

// canonicalGrantRef maps the space kind onto workstream, because a space is a
// workstream with a flag and its grants are stored under the workstream's id.
func canonicalGrantRef(ref Ref) Ref {
	if ref.Kind == "space" {
		ref.Kind = ItemWorkstream
	}
	return ref
}

// prefetchResources reads the grants of a whole list of resources, and of the
// spaces they sit in, in a few queries, so listing items does not cost a query
// per item.
func (acc *Access) prefetchResources(resources []accessResource) {
	keys := make([]Ref, 0, len(resources))
	seen := make(map[Ref]bool, len(resources))
	for _, res := range resources {
		for _, ref := range []Ref{res.ref, res.item.Parent} {
			ref = canonicalGrantRef(ref)
			if ref.ID != "" && !seen[ref] {
				seen[ref] = true
				keys = append(keys, ref)
			}
		}
	}
	acc.prefetch(keys)
}

// rules is the policy this request decides by; a nil Access is the default.
func (acc *Access) rules() acl.Policy {
	if acc == nil {
		return acl.DefaultPolicy()
	}
	return acc.policy
}

// time is the one moment every check in this request is made at, so a grant
// that lapses mid-request cannot make two answers disagree.
func (acc *Access) time() time.Time {
	if acc == nil {
		return time.Time{}
	}
	return acc.now()
}

// caller is v as the evaluator sees them. The Agent field is what makes a
// request an agent's: the sponsor's current verbs, narrowed to the agent's scope.
func (v Viewer) caller() acl.Caller {
	return acl.Caller{Handle: v.Handle, Teams: v.Teams, Operator: v.Operator, Solo: v.Solo, Member: v.Member, Agent: v.Agent}
}

// owns is whether v owns the item in person or through the owning team.
func (v Viewer) owns(it Item) bool {
	switch it.Owner.Kind {
	case api.OwnerPerson:
		return v.Handle != "" && factory.NormalizeHandle(it.Owner.ID) == v.Handle
	case api.OwnerTeam:
		return v.Teams[it.Owner.ID]
	}
	return false
}

// accessSubject spells an owner or a share's target the way grants are
// compared, with a person's handle normalized.
func accessSubject(kind, id string) acl.Subject {
	if kind == api.OwnerPerson {
		id = factory.NormalizeHandle(id)
	}
	return acl.Subject{Kind: kind, ID: id}
}

// NOT GENERIC YET: legacy owner, shares and privacy.
// tuplesFor reads the document's relationships and the stored grants in one place.
func (acc *Access) tuplesFor(it Item) acl.Item {
	owner := accessSubject(it.Owner.Kind, it.Owner.ID)
	if it.parentPrivate && taskSession(it) {
		owner = accessSubject(it.parentOwner.Kind, it.parentOwner.ID)
	}
	out := acl.Item{OwnerKind: owner.Kind, OwnerID: owner.ID, Private: it.Private || it.Visibility == acl.Private}
	if !it.registered {
		out.Visibility = it.Visibility
	}
	for _, s := range it.Shares {
		subject := accessSubject(s.Kind, s.ID)
		out.Shares = append(out.Shares, acl.Share{Kind: subject.Kind, ID: subject.ID, Role: s.Role})
	}
	if acc != nil && acc.grants != nil {
		out.Grants = acc.grants(it.Kind, it.ID)
		if !it.parentPrivate && it.Parent.ID != "" {
			out.Grants = append(slices.Clone(out.Grants), acc.grants(it.Parent.Kind, it.Parent.ID)...)
		}
	}
	return out
}

// policyItem requires a private registration to have its own grant before sharing it.
// The parent's relationships alone cannot open it, and neither can its own first
// grant: a private registration that has been shared stays private and is its
// owner's and its grantees' alone (Closed), so the space's shares, the space's
// grants, the space's owner and the operator stay out of it. Otherwise sharing a
// chat with one person would open it, title and all, to everyone who can see the
// workstream.
func (acc *Access) policyItem(it Item, tuples acl.Item) acl.Item {
	if it.registered && it.Visibility == acl.Private && tuples.Private {
		now, p := acc.time(), acc.rules()
		own := func(g acl.Grant) bool { return g.Resource.Kind == it.Kind && g.Resource.ID == it.ID }
		shared := slices.ContainsFunc(tuples.Grants, func(g acl.Grant) bool {
			_, exists := p.Role(g.Role)
			return own(g) && exists && g.Live(now) &&
				g.Subject.Kind != "agent" && (g.Subject.Kind != tuples.OwnerKind || g.Subject.ID != tuples.OwnerID)
		})
		if shared {
			tuples.Closed = true
			tuples.Shares = nil
			tuples.Grants = slices.DeleteFunc(slices.Clone(tuples.Grants), func(g acl.Grant) bool { return !own(g) })
			return tuples
		}
	}
	if !tuples.Private && it.parentOwner.Kind != "" {
		parent := accessSubject(it.parentOwner.Kind, it.parentOwner.ID)
		tuples.Shares = append(slices.Clone(tuples.Shares), acl.Share{Kind: parent.Kind, ID: parent.ID, Role: acc.rules().OwnerRole})
	}
	return tuples
}

// item is the item as the evaluator sees it: the document's own relationships,
// the stored grants, and what it inherits from its space.
func (acc *Access) item(it Item) acl.Item {
	return acc.policyItem(it, acc.tuplesFor(it))
}

// roleOf is the highest role v holds on it, "" for none.
func roleOf(v Viewer, it Item) string {
	return v.acc.rules().RoleOf(v.caller(), v.acc.item(it), v.acc.time())
}

// can is whether v may do verb to it: one of the policy's verbs (view,
// comment, edit, run, steer, answer, share, manage), or the old role name "full"
// for share and manage together. An unknown verb is never allowed.
func can(v Viewer, it Item, verb string) bool {
	return v.acc.rules().Allowed(v.caller(), v.acc.item(it), verb, v.acc.time())
}

// itemAudience is who may hear that an item changed: its owner, everyone it
// is shared with, and the operator unless it is private. An event built from
// the item before and after a change joins both audiences, so someone it was
// just taken from still hears to drop their row.
type itemAudience = acl.Audience

// audienceOf joins the audiences of every item given, under the default policy
// and no stored grants; a handler uses [Handler.audienceOf].
func audienceOf(items ...Item) itemAudience {
	var acc *Access
	return acc.audienceOf(items...)
}

// audienceOf is [audienceOf] under the organization's policy and its stored
// grants, which is what an event's audience must be.
func (h *Handler) audienceOf(items ...Item) itemAudience {
	return h.access().audienceOf(items...)
}

// audienceOf computes the audience of the items from this request's policy and
// grants.
func (acc *Access) audienceOf(items ...Item) itemAudience {
	tuples := make([]acl.Item, 0, len(items))
	for _, it := range items {
		tuples = append(tuples, acc.item(it))
	}
	return acc.rules().Audience(acc.time(), tuples...)
}

// hears is whether v is in a. On a solo factory the one person hears all.
func (v Viewer) hears(a itemAudience) bool {
	return a.Hears(v.caller())
}

// ── checking shares a caller sends ──

// cleanShares checks shares a caller sent: every one names a person by a
// GitHub handle or a team this organization keeps, with a role. One person
// or team named twice keeps the higher role, so a list never says two things
// about one person. The answer is ordered teams first, then people, each by
// id, so equal lists compare equal. An owner named as a share is left out:
// they hold full already.
func (h *Handler) cleanShares(shares []factory.Share, owner factory.WorkstreamOwner) ([]factory.Share, error) {
	byKey := map[string]factory.Share{}
	for _, s := range shares {
		if factory.RoleRank(s.Role) == 0 {
			return nil, invalidf("a share's role must be one of %s", strings.Join(factory.Roles, ", "))
		}
		switch s.Kind {
		case factory.ShareToPerson:
			s.ID = factory.NormalizeHandle(s.ID)
			if !personHandle.MatchString(s.ID) {
				return nil, invalidf("a person is shared with by their handle")
			}
		case factory.ShareToTeam:
			s.ID = strings.TrimSpace(s.ID)
			if !h.teamKnown(s.ID) {
				return nil, invalidf("team %q is not in this organization", s.ID)
			}
		default:
			return nil, invalidf(`a share's kind must be "person" or "team"`)
		}
		if s.Kind == owner.Kind && s.ID == owner.ID {
			continue
		}
		key := s.Kind + ":" + s.ID
		if cur, ok := byKey[key]; ok {
			s.Role = factory.HigherRole(cur.Role, s.Role)
		}
		byKey[key] = s
	}
	out := make([]factory.Share, 0, len(byKey))
	for _, s := range byKey {
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b factory.Share) int {
		if a.Kind != b.Kind {
			return strings.Compare(b.Kind, a.Kind) // "team" before "person"
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

// ── workstreams as items ──

// workstreamItem is an organization workstream's access shape.
func workstreamItem(ws factory.OrgWorkstream) Item {
	it := Item{Kind: ItemWorkstream, ID: ws.ID, Owner: ws.Owner, Shares: ws.Shares, Private: ws.Private}
	if ws.Parent != "" {
		it.Parent = Ref{Kind: ItemWorkstream, ID: ws.Parent}
	}
	return it
}

// workstreamCan is whether the caller of r may do verb to the organization
// workstream wsID. known is false, with no error, when wsID names none, so a
// go-ahead group or a job's group that is not an organization workstream
// keeps the rules it always had. Factory jobs (E5) and go-aheads ask this.
func (h *Handler) workstreamCan(r *http.Request, wsID string, verb string) (known, ok bool, err error) {
	if h.config.Store == nil || !strings.HasPrefix(wsID, workstreamIDPrefix) {
		return false, false, nil
	}
	ws, found, err := h.config.Store.Workstream(wsID)
	if err != nil || !found {
		return false, false, err
	}
	v, err := h.itemViewer(r)
	if err != nil {
		return true, false, err
	}
	return true, can(v, workstreamItem(ws), verb), nil
}

package acl

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

// The verbs are a CLOSED enum in code: a policy composes roles out of them and
// can never invent one, so every door's check is a question the code already
// knows how to ask. "admin" is deliberately absent, because it is the app
// operator's word and an existing test pins it as never allowed on an item.
const (
	VerbView    = "view"
	VerbComment = "comment"
	VerbEdit    = "edit"
	VerbRun     = "run"
	VerbSteer   = "steer"
	VerbAnswer  = "answer"
	VerbShare   = "share"
	VerbManage  = "manage"
)

// Verbs lists every verb in its fixed order.
var Verbs = []string{VerbView, VerbComment, VerbEdit, VerbRun, VerbSteer, VerbAnswer, VerbShare, VerbManage}

// ValidVerb reports whether verb is in the closed enum.
func ValidVerb(verb string) bool {
	switch verb {
	case VerbView, VerbComment, VerbEdit, VerbRun, VerbSteer, VerbAnswer, VerbShare, VerbManage:
		return true
	}
	return false
}

// The values a policy may use for a kind's default visibility (Private, Owner
// or Org) and for who may share (ShareVerb or OwnerOnly).
const (
	Private   = "private"
	Owner     = "owner"
	Org       = "org"
	ShareVerb = "share_verb"
	OwnerOnly = "owner_only"
)

// kinds lists the resource kinds a policy may set defaults for.
var kinds = []string{"workstream", "canvas", "doc", "task", "page", "file", "job", "chat", "session", "space"}

// ValidResourceKind reports whether kind is one a policy may name.
func ValidResourceKind(kind string) bool { return slices.Contains(kinds, kind) }

// Role is a named bundle of verbs. Its position in Policy.Roles is its rank,
// lowest first, and every role must include view.
type Role struct {
	Name  string   `json:"name"`
	Verbs []string `json:"verbs"`
}

// KindDefault is what a newly created resource of a kind starts as. It never
// changes a resource that already exists.
type KindDefault struct {
	Visibility string `json:"visibility"`
}

// Sharing says who may add grants: ShareVerb (anyone holding the share verb)
// or OwnerOnly.
type Sharing struct {
	Who string `json:"who"`
}

// Operator is what the app operator may do: the role it holds on items it can
// see, whether that includes private items, and whether it may change who owns
// and shares a workstream that is not private.
type Operator struct {
	Role        string `json:"role"`
	SeesPrivate bool   `json:"sees_private"`
	MayGovern   bool   `json:"may_govern"`
}

// Agents holds the org-wide switches for server-run agents and the role bundle
// that bounds each one. Scopes maps an agent id to a role name; an agent
// without an entry gets DefaultScope.
type Agents struct {
	MayCreate    bool              `json:"may_create"`
	MayShare     bool              `json:"may_share"`
	MayGrant     bool              `json:"may_grant"`
	DefaultScope string            `json:"default_scope"`
	Scopes       map[string]string `json:"scopes,omitempty"`
}

// Hosting holds optional organization settings for where chats run and how much they may spend.
type Hosting struct {
	LocalSessions     string  `json:"local_sessions,omitempty"`
	MaxSessionCostUSD float64 `json:"max_session_cost_usd,omitempty"`
}

const (
	HostingRefuseWords = "This organization runs your chats on its own server, so they do not start on this computer. To work here instead, switch to your personal account, which does not see the organization's chats or files."
	HostingWarnWords   = "This organization keeps your chats on its own server. A chat started on this computer is not part of the organization's account."
)

// LocalSessionsFor keeps the existing local-chat behavior when the organization has no preference.
func (h Hosting) LocalSessionsFor(hasChatHost bool) string {
	if h.LocalSessions != "" {
		return h.LocalSessions
	}
	if hasChatHost {
		return "refuse"
	}
	return "allow"
}

// Words explains the effective local-chat policy where it affects the caller.
func (h Hosting) Words(hasChatHost bool) string {
	switch h.LocalSessionsFor(hasChatHost) {
	case "refuse":
		return HostingRefuseWords
	case "warn":
		return HostingWarnWords
	default:
		return ""
	}
}

// Chats holds the organization's rules for the chats its people keep: who may move a chat to another workstream or into
// My space, and, for a chat that runs on the organization's own server and is shared with other people, who may approve
// one of its tool calls and who may start it when it is not running. Every field is optional, and an empty one means the
// rule the server had before the field existed, so a policy that never mentions a rule is unchanged by it.
type Chats struct {
	// MoveBy is MoveByOwner or MoveByManage. Empty means MoveByOwner, so a policy stored before this field existed, and
	// a policy that never mentions it, keep the rule the server always had.
	MoveBy string `json:"move_by,omitempty"`
	// ApprovalsBy is ApprovalsByOwner or ApprovalsByAnswer. Empty means ApprovalsByOwner: a tool call a shared chat wants
	// to make waits for its owner, whatever role anyone else holds.
	ApprovalsBy string `json:"approvals_by,omitempty"`
	// WakeBy is WakeByView, WakeByRun or WakeByOwner. Empty means WakeByView: anyone who can see a shared chat opens it
	// when it is not running, and it starts for its owner.
	WakeBy string `json:"wake_by,omitempty"`
}

const (
	// MoveByOwner lets only the person who owns a chat move it, as long as they still hold manage on it. It is the
	// default, and what the server did before the rule was a setting.
	MoveByOwner = "owner"
	// MoveByManage lets anyone who holds the manage verb on a chat move it, its owner included. Under the starting
	// roles that is whoever the chat is shared with at `full`.
	MoveByManage = "manage"
)

const (
	// ApprovalsByOwner keeps every approval of a tool call with the chat's owner. It is the default.
	ApprovalsByOwner = "owner"
	// ApprovalsByAnswer lets anyone who holds the answer verb on a shared chat allow one tool call once, or refuse it.
	// Allowing a tool for the rest of the chat, and every other setting of the chat, stays with the owner. Under the
	// starting roles that is whoever the chat is shared with at `edit` or `full`.
	ApprovalsByAnswer = "answer"
)

const (
	// WakeByView lets anyone who can see a shared chat open it when it is not running, which starts it for its owner.
	// It is the default, and what the server did before the rule was a setting.
	WakeByView = "view"
	// WakeByRun lets only a person who may send messages in the chat (the run verb) open it when it is not running.
	// Someone who can only watch it sees a running chat, and is told to ask its owner for one that is not.
	WakeByRun = "run"
	// WakeByOwner lets only the chat's owner start it. Everyone else sees it while it is running.
	WakeByOwner = "owner"
)

// MoveByFor is the rule that applies, with an empty setting read as the default.
func (c Chats) MoveByFor() string {
	if c.MoveBy == "" {
		return MoveByOwner
	}
	return c.MoveBy
}

// ApprovalsByFor is the rule that applies, with an empty setting read as the default.
func (c Chats) ApprovalsByFor() string {
	if c.ApprovalsBy == "" {
		return ApprovalsByOwner
	}
	return c.ApprovalsBy
}

// WakeByFor is the rule that applies, with an empty setting read as the default.
func (c Chats) WakeByFor() string {
	if c.WakeBy == "" {
		return WakeByView
	}
	return c.WakeBy
}

// Policy is the organization's access document: the roles, what each kind of
// resource starts as, who may share, what the operator and agents may do. It
// is stored as one row with a revision and changes only through a PUT by the
// operator, because a policy change is an audience change and must emit events.
type Policy struct {
	Rev       int                    `json:"rev"`
	UpdatedAt string                 `json:"updated_at,omitempty"`
	UpdatedBy string                 `json:"updated_by,omitempty"`
	Roles     []Role                 `json:"roles"`
	OwnerRole string                 `json:"owner_role"`
	OrgRole   string                 `json:"org_role"`
	Defaults  map[string]KindDefault `json:"defaults"`
	Sharing   Sharing                `json:"sharing"`
	Operator  Operator               `json:"operator"`
	Agents    Agents                 `json:"agents"`
	Hosting   Hosting                `json:"hosting,omitzero"`
	Chats     Chats                  `json:"chats,omitzero"`
}

// legacyRoles are the four names old shares and clients already use, so a
// policy may edit their bundles but never remove or rename them.
var legacyRoles = []string{"view", "comment", "edit", "full"}

// DefaultPolicy reproduces what the server did before policies existed:
// nested view, comment, edit and full roles, the owner holding full, the
// operator viewing what is not private, chats and sessions private, and
// agents acting as their person. FIXES KEEP DEFAULTS: a server with no stored
// policy must behave exactly as it always has.
func DefaultPolicy() Policy {
	defaults := make(map[string]KindDefault, len(kinds))
	for _, kind := range kinds {
		defaults[kind] = KindDefault{Visibility: defaultVisibility(kind)}
	}
	return Policy{
		Roles: []Role{
			{Name: "view", Verbs: []string{VerbView}},
			{Name: "comment", Verbs: []string{VerbView, VerbComment}},
			{Name: "edit", Verbs: []string{VerbView, VerbComment, VerbEdit, VerbRun, VerbSteer, VerbAnswer}},
			{Name: "full", Verbs: slices.Clone(Verbs)},
		},
		OwnerRole: "full", OrgRole: "view", Defaults: defaults,
		Sharing:  Sharing{Who: ShareVerb},
		Operator: Operator{Role: "view", MayGovern: true},
		Agents:   Agents{MayCreate: true, DefaultScope: "full"},
	}
}

// defaultVisibility is the kind's starting visibility under the default policy.
func defaultVisibility(kind string) string {
	switch kind {
	case "chat", "session", "space":
		return Private
	default:
		return Owner
	}
}

// Visibility returns what a new resource of kind starts as under the policy.
func (p Policy) Visibility(kind string) string {
	if d, ok := p.Defaults[kind]; ok {
		return d.Visibility
	}
	return defaultVisibility(kind)
}

// ValidationError says in words why a policy or a request is malformed; a door
// shows it as a 422.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

// invalid builds a ValidationError from a format string.
func invalid(format string, args ...any) error {
	return &ValidationError{fmt.Sprintf(format, args...)}
}

// Validate refuses a policy that names an unknown verb or role, holds an empty
// or duplicate role, a role without view, drops one of the four legacy roles,
// or removes a role from prev that inUse says a live grant or default still
// uses. inUse may be nil when nothing is stored yet.
func (p Policy) Validate(prev Policy, inUse func(role string) bool) error {
	roles := make(map[string]bool, len(p.Roles))
	for _, r := range p.Roles {
		switch {
		case strings.TrimSpace(r.Name) == "":
			return invalid("a role needs a name")
		case roles[r.Name]:
			return invalid("role %q appears more than once", r.Name)
		case !slices.Contains(r.Verbs, VerbView):
			return invalid("role %q must include view", r.Name)
		}
		for _, verb := range r.Verbs {
			if !ValidVerb(verb) {
				return invalid("role %q uses an unknown permission %q", r.Name, verb)
			}
		}
		roles[r.Name] = true
	}
	for _, name := range legacyRoles {
		if !roles[name] {
			return invalid("the %q role must be kept", name)
		}
	}
	for _, use := range []struct{ field, role string }{
		{"the owner role", p.OwnerRole}, {"the organization role", p.OrgRole},
		{"the operator role", p.Operator.Role}, {"the default agent scope", p.Agents.DefaultScope},
	} {
		if !roles[use.role] {
			return invalid("%s must name an existing role", use.field)
		}
	}
	for agent, name := range p.Agents.Scopes {
		if !roles[name] {
			return invalid("the scope for agent %q must name an existing role", agent)
		}
	}
	for kind, d := range p.Defaults {
		if !ValidResourceKind(kind) {
			return invalid("%q is not a resource kind", kind)
		}
		switch d.Visibility {
		case Private, Owner, Org:
		default:
			return invalid("the default visibility for %s must be private, owner, or org", kind)
		}
	}
	if p.Sharing.Who != ShareVerb && p.Sharing.Who != OwnerOnly {
		return invalid("sharing must be set to people with sharing permission or to owners only")
	}
	switch p.Hosting.LocalSessions {
	case "", "allow", "warn", "refuse":
	default:
		return invalid("hosting local sessions must be allow, warn or refuse")
	}
	switch p.Chats.MoveBy {
	case "", MoveByOwner, MoveByManage:
	default:
		return invalid("who may move a chat must be the owner or people with manage permission")
	}
	switch p.Chats.ApprovalsBy {
	case "", ApprovalsByOwner, ApprovalsByAnswer:
	default:
		return invalid("who may approve a tool call in a shared chat must be owner (only its owner) or answer (anyone who may answer its questions)")
	}
	switch p.Chats.WakeBy {
	case "", WakeByView, WakeByRun, WakeByOwner:
	default:
		return invalid("who may start a chat that is not running must be view (anyone who can see it), run (anyone who may send messages in it) or owner (only its owner)")
	}
	if cost := p.Hosting.MaxSessionCostUSD; math.IsNaN(cost) || cost < 0 || cost > 1_000_000 {
		return invalid("hosting chat cost must be between 0 and 1,000,000 dollars")
	}
	for _, r := range prev.Roles {
		if !roles[r.Name] && inUse != nil && inUse(r.Name) {
			return invalid("role %q is still in use", r.Name)
		}
	}
	return nil
}

// Role finds a role by name.
func (p Policy) Role(name string) (Role, bool) {
	for _, r := range p.Roles {
		if r.Name == name {
			return r, true
		}
	}
	return Role{}, false
}

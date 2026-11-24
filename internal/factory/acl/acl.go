// Package acl decides organization access from a policy and the relationships on an item.
package acl

import (
	"fmt"
	"slices"
	"time"
)

// Subject is who a grant reaches: a person (by handle), a team (by id, its
// members read at the moment of the check), the whole organization (id "*",
// signed-in members only) or a service (a non-person caller with its own id).
// An agent is NEVER a subject: an agent's rights are its sponsor's current
// rights narrowed by policy, so a grant that named one could outlive the
// sponsor's access.
type Subject struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// Resource names the thing a grant is on, by kind and id.
type Resource struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// Principal is who made a grant. An agent's grant also records the person who
// sponsors it, so the audit trail always ends at a person.
type Principal struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Sponsor string `json:"sponsor,omitempty"`
}

// Grant is one relationship tuple: Subject holds Role on Resource, given by
// GrantedBy at CreatedAt, until ExpiresAt when that is set. Rows are kept after
// they lapse so the audit trail survives; Live says whether one still counts.
type Grant struct {
	ID        string     `json:"id"`
	Subject   Subject    `json:"subject"`
	Resource  Resource   `json:"resource"`
	Role      string     `json:"role"`
	GrantedBy Principal  `json:"granted_by,omitzero"`
	CreatedAt time.Time  `json:"created_at,omitzero"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Note      string     `json:"note,omitempty"`
}

// Live reports whether the grant still grants anything at now.
func (g Grant) Live(now time.Time) bool {
	return g.ExpiresAt == nil || now.Before(*g.ExpiresAt)
}

// Caller is the already-authenticated party a check is made for. Operator,
// Solo and Member mirror the server's modes (the operator sees what the policy
// lets it see, solo is a one-person server where the caller holds every verb,
// and a member is any signed-in person). Agent is set when a server-run agent
// acts for Handle, who is then its sponsor: the caller's verbs are narrowed to
// the agent's scope. ServiceID is set for a non-person caller that holds
// grants of its own.
type Caller struct {
	Handle                 string
	Teams                  map[string]bool
	Operator, Solo, Member bool
	Agent                  *Agent
	ServiceID              string
}

// Agent names a server-run agent and the person who sponsors it.
type Agent struct {
	ID, Sponsor string
	// Only is a further limit. An empty non-nil slice grants no verbs.
	Only []string
}

// Share is a legacy share, as it sits on a workstream or canvas document: a
// person or team (Kind), its id and the role it was given. It is read as a
// tuple, exactly as a stored Grant is.
type Share struct {
	Kind, ID, Role string
}

// Item is everything the evaluator needs to know about one resource: its
// owner, whether it is private, the visibility its space or kind gives it,
// the legacy shares on it and the stored grants on it. Callers fill it in
// from the item and from the space it sits in (one level of inheritance), so
// the evaluator itself never reads a store.
type Item struct {
	OwnerKind, OwnerID string
	Private            bool
	// Closed is a private item that grants on the item itself have opened: it
	// stays private, so shares, the organization's visibility and the operator
	// (unless the policy lets it see private things) are still kept out, and
	// only its owner and the people those grants name reach it.
	Closed     bool
	Visibility string
	Shares     []Share
	Grants     []Grant
}

// VerbSet is a set of verbs from the closed enum.
type VerbSet map[string]bool

// Has reports whether verb is in the set.
func (s VerbSet) Has(verb string) bool { return s[verb] }

// List returns the verbs in the set in the fixed order of Verbs.
func (s VerbSet) List() []string {
	out := make([]string, 0, len(s))
	for _, verb := range Verbs {
		if s.Has(verb) {
			out = append(out, verb)
		}
	}
	return out
}

func (s VerbSet) containsAll(verbs []string) bool {
	for _, verb := range verbs {
		if !s.Has(verb) {
			return false
		}
	}
	return true
}

func (s Subject) matches(c Caller) bool {
	switch s.Kind {
	case "person":
		return c.Handle != "" && c.Handle == s.ID
	case "team":
		return c.Teams[s.ID]
	case "org":
		return s.ID == "*" && c.Member
	case "service":
		return c.Agent == nil && c.ServiceID != "" && c.ServiceID == s.ID
	}
	return false
}

func (it Item) ownedBy(c Caller) bool {
	return (it.OwnerKind == "person" || it.OwnerKind == "team") && (Subject{Kind: it.OwnerKind, ID: it.OwnerID}).matches(c)
}

// held returns the names of every role the caller holds on the item. THE LAW
// OF A PRIVATE ITEM: it is its owner's alone, so shares, grants and the
// organization's visibility are not consulted, and the operator is let in only
// when the policy says it sees private items. A private item that has been
// opened by grants on itself (Closed) adds those grants and nothing else.
func (p Policy) held(c Caller, it Item, now time.Time) map[string]bool {
	roles := map[string]bool{}
	if it.ownedBy(c) {
		roles[p.OwnerRole] = true
	}
	if c.Operator && (!it.Private || p.Operator.SeesPrivate) {
		roles[p.Operator.Role] = true
	}
	if it.Private && !it.Closed {
		return roles
	}
	if !it.Private {
		if it.Visibility == Org && c.Member {
			roles[p.OrgRole] = true
		}
		for _, s := range it.Shares {
			if (s.Kind == "person" || s.Kind == "team") && (Subject{Kind: s.Kind, ID: s.ID}).matches(c) {
				roles[s.Role] = true
			}
		}
	}
	for _, g := range it.Grants {
		if g.Live(now) && g.Subject.matches(c) {
			roles[g.Role] = true
		}
	}
	return roles
}

// scope is the role bundle that bounds an agent: the one the policy names for
// that agent, or the default scope. It is policy data, never a per-item grant.
func (p Policy) scope(c Caller) Role {
	name := p.Agents.DefaultScope
	if scope, ok := p.Agents.Scopes[c.Agent.ID]; ok {
		name = scope
	}
	role, _ := p.Role(name)
	return role
}

// verbs answers from the UNION of the verbs of every role held. For an agent
// it then keeps only the verbs inside the agent's scope, so an agent can never
// do more than its sponsor can do right now, whatever its scope says.
func (p Policy) verbs(c Caller, held map[string]bool) VerbSet {
	verbs := VerbSet{}
	if c.Solo {
		for _, verb := range Verbs {
			verbs[verb] = true
		}
	} else {
		for _, r := range p.Roles {
			if held[r.Name] {
				for _, verb := range r.Verbs {
					verbs[verb] = true
				}
			}
		}
	}
	if c.Agent != nil {
		scope := p.scope(c)
		for verb := range verbs {
			if !slices.Contains(scope.Verbs, verb) || (c.Agent.Only != nil && !slices.Contains(c.Agent.Only, verb)) {
				delete(verbs, verb)
			}
		}
	}
	return verbs
}

// AgentAllows is whether the scope the policy gives agent holds verb, whatever
// its sponsor holds: the narrowing half of an agent's rights, for doors that
// check the sponsor themselves.
func (p Policy) AgentAllows(agent, verb string) bool {
	if verb != "full" && !ValidVerb(verb) {
		return false
	}
	scope := p.scope(Caller{Agent: &Agent{ID: agent}})
	if verb == "full" {
		return slices.Contains(scope.Verbs, VerbShare) && slices.Contains(scope.Verbs, VerbManage)
	}
	return slices.Contains(scope.Verbs, verb)
}

// Verbs returns the verbs the caller holds on the item at now. Nothing is
// cached: a sponsor who loses access loses the agent's access on the next call.
func (p Policy) Verbs(c Caller, it Item, now time.Time) VerbSet {
	return p.verbs(c, p.held(c, it, now))
}

// RoleOf names the highest-ranked role the caller holds on the item, for
// display, and is empty when the caller holds none. It is not what decides
// anything: Allowed answers from the union of verbs, so a custom bundle that
// ranks low can still allow a verb a higher role lacks.
func (p Policy) RoleOf(c Caller, it Item, now time.Time) string {
	if c.Solo && c.Agent == nil {
		return "full"
	}
	held := p.held(c, it, now)
	if c.Agent == nil {
		for _, r := range slices.Backward(p.Roles) {
			if held[r.Name] {
				return r.Name
			}
		}
		return ""
	}
	verbs, scope := p.verbs(c, held), p.scope(c)
	for _, r := range slices.Backward(p.Roles) {
		if (held[r.Name] || r.Name == scope.Name) && verbs.containsAll(r.Verbs) {
			return r.Name
		}
	}
	// When the intersection is only part of both bundles, show view if it survived.
	if verbs.Has(VerbView) {
		return "view"
	}
	return ""
}

// Allowed reports whether the caller may do verb to the item. The legacy verb
// "full" still means the strongest need, share and manage together, and every
// name outside the closed enum, "admin" and "" included, is never allowed.
func (p Policy) Allowed(c Caller, it Item, verb string, now time.Time) bool {
	if verb != "full" && !ValidVerb(verb) {
		return false
	}
	verbs := p.Verbs(c, it, now)
	if verb == "full" {
		return verbs.Has(VerbShare) && verbs.Has(VerbManage)
	}
	return verbs.Has(verb)
}

// PermissionError says in words why a caller may not do something that is
// well formed; a door shows it as a refusal (403), never as a bad request.
type PermissionError struct{ Message string }

func (e *PermissionError) Error() string { return e.Message }

// MayShare reports whether the caller may add or remove grants on the item
// under the policy's rule for who shares.
func (p Policy) MayShare(c Caller, it Item, now time.Time) bool {
	return p.sharingAllowed(c, it, p.Verbs(c, it, now))
}

// sharingAllowed applies the policy's rule for who shares. Solo and the
// governing operator always may; under owner_only only an owner may, and an
// agent only if it also holds the share verb.
func (p Policy) sharingAllowed(c Caller, it Item, verbs VerbSet) bool {
	if c.Solo && c.Agent == nil || p.mayGovern(c, it) {
		return true
	}
	if p.Sharing.Who == OwnerOnly {
		return it.ownedBy(c) && (c.Agent == nil || verbs.Has(VerbShare))
	}
	return verbs.Has(VerbShare)
}

// mayGovern is the operator's standing to change who holds a workstream that
// is not private. It is a policy switch and never reaches a private item.
func (p Policy) mayGovern(c Caller, it Item) bool {
	return c.Agent == nil && c.Operator && p.Operator.MayGovern && !it.Private
}

// MayGrant says whether caller may give subject the named role on the item. It
// refuses an agent subject, an unknown role, an agent caller the policy does
// not let grant, and a role holding any verb the caller does not itself hold on
// the item: a grant can hand on only what its giver has.
func MayGrant(p Policy, c Caller, it Item, subject Subject, roleName string, now time.Time) error {
	if subject.Kind == "agent" {
		return invalid("give an agent its permissions in the organization policy")
	}
	role, ok := p.Role(roleName)
	if !ok {
		return invalid("role %q does not exist", roleName)
	}
	if c.Agent != nil {
		if !p.Agents.MayGrant {
			return &PermissionError{"agents cannot grant access under the organization policy"}
		}
		if slices.Contains(role.Verbs, VerbShare) && !p.Agents.MayShare {
			return &PermissionError{"agents cannot give sharing permission under the organization policy"}
		}
	}
	verbs := p.Verbs(c, it, now)
	if !p.sharingAllowed(c, it, verbs) {
		return &PermissionError{"you cannot share this item"}
	}
	if p.mayGovern(c, it) {
		verbs[VerbShare], verbs[VerbManage] = true, true
	}
	if !verbs.containsAll(role.Verbs) {
		return &PermissionError{fmt.Sprintf("you cannot give the %q role because it includes permissions you do not have", roleName)}
	}
	return nil
}

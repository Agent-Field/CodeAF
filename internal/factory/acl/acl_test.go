package acl

import (
	"errors"
	"slices"
	"testing"
	"time"
)

// Contract A1, A3. The default bundles and creation rules agree with the existing server.
func TestDefaultPolicy(t *testing.T) {
	p := DefaultPolicy()
	if err := p.Validate(Policy{}, nil); err != nil {
		t.Fatal(err)
	}
	for i, name := range legacyRoles {
		if p.Roles[i].Name != name || !slices.Equal(p.Roles[i].Verbs, Verbs[:[]int{1, 2, 6, 8}[i]]) {
			t.Errorf("role %d = %+v", i, p.Roles[i])
		}
	}
	for _, kind := range kinds {
		want := Owner
		if slices.Contains([]string{"chat", "session", "space"}, kind) {
			want = Private
		}
		if p.Visibility(kind) != want {
			t.Errorf("%s visibility = %s, want %s", kind, p.Visibility(kind), want)
		}
	}
	if p.OwnerRole != "full" || p.OrgRole != "view" || p.Operator != (Operator{Role: "view", MayGovern: true}) ||
		p.Agents.MayShare || p.Agents.MayGrant || !p.Agents.MayCreate || p.Agents.DefaultScope != "full" {
		t.Fatalf("default rules = %+v", p)
	}
}

// Contract A2. Ownership, sharing and privacy are evaluated for each caller.
func TestDefaultAccess(t *testing.T) {
	p, now := DefaultPolicy(), time.Now()
	owner := Caller{Handle: "writer", Teams: map[string]bool{"makers": true}}
	editor := Caller{Handle: "editor", Teams: map[string]bool{"readers": true}}
	commenter := Caller{Handle: "commenter"}
	operator := Caller{Operator: true}
	shared := Item{OwnerKind: "person", OwnerID: "writer", Shares: []Share{
		{Kind: "team", ID: "readers", Role: "view"},
		{Kind: "person", ID: "editor", Role: "edit"},
		{Kind: "person", ID: "commenter", Role: "comment"},
	}}
	private := Item{OwnerKind: "person", OwnerID: "writer", Private: true, Shares: []Share{{Kind: "person", ID: "editor", Role: "full"}}}
	team := Item{OwnerKind: "team", OwnerID: "makers"}
	for _, tc := range []struct {
		name string
		c    Caller
		it   Item
		want string
	}{
		{"owner", owner, shared, "full"},
		{"person and team shares", editor, shared, "edit"},
		{"one share", commenter, shared, "comment"},
		{"operator", operator, shared, "view"},
		{"private owner", owner, private, "full"},
		{"private share", editor, private, ""},
		{"private operator", operator, private, ""},
		{"owning team", owner, team, "full"},
		{"unrelated person", commenter, team, ""},
		{"solo", Caller{Solo: true}, private, "full"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := p.RoleOf(tc.c, tc.it, now); got != tc.want {
				t.Fatalf("role = %q, want %q", got, tc.want)
			}
			for _, verb := range []string{"admin", ""} {
				if p.Allowed(tc.c, tc.it, verb, now) {
					t.Errorf("allowed %q", verb)
				}
			}
			if got := p.Allowed(tc.c, tc.it, "full", now); got != (tc.want == "full") {
				t.Errorf("full allowed = %v for %s", got, tc.want)
			}
		})
	}
}

// Contract B2, E4. Membership and expiry are checked at use, and services hold direct grants.
func TestGrantSubjectsAndExpiry(t *testing.T) {
	p, now := DefaultPolicy(), time.Now()
	it := Item{Grants: []Grant{
		{Subject: Subject{Kind: "org", ID: "*"}, Role: "view"},
		{Subject: Subject{Kind: "team", ID: "makers"}, Role: "edit", ExpiresAt: &now},
		{Subject: Subject{Kind: "service", ID: "builder"}, Role: "comment"},
		{Subject: Subject{Kind: "agent", ID: "builder"}, Role: "full"},
	}}
	team := Caller{Member: true, Teams: map[string]bool{"makers": true}}
	for _, tc := range []struct {
		name string
		c    Caller
		at   time.Time
		want string
	}{
		{"org member", Caller{Member: true}, now, "view"},
		{"anonymous", Caller{}, now, ""},
		{"team before expiration", team, now.Add(-time.Second), "edit"},
		{"team removed", Caller{Member: true}, now.Add(-time.Second), "view"},
		{"team at expiration", team, now, "view"},
		{"service", Caller{ServiceID: "builder"}, now, "comment"},
		{"agent has no direct grant", Caller{Member: true, Agent: &Agent{ID: "builder"}}, now, "view"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := p.RoleOf(tc.c, it, tc.at); got != tc.want {
				t.Fatalf("role = %q, want %q", got, tc.want)
			}
		})
	}
}

// Contract C2. Invalid bundles and dangling references must be refused.
func TestPolicyValidation(t *testing.T) {
	runner := Role{Name: "runner", Verbs: []string{VerbView, VerbRun}}
	prev := DefaultPolicy()
	prev.Roles = append(prev.Roles, runner)
	for _, tc := range []struct {
		name string
		edit func(*Policy)
	}{
		{"unknown verb", func(p *Policy) { p.Roles[0].Verbs = append(p.Roles[0].Verbs, "admin") }},
		{"empty role", func(p *Policy) { p.Roles = append(p.Roles, Role{}) }},
		{"duplicate", func(p *Policy) { p.Roles = append(p.Roles, p.Roles[0]) }},
		{"no view", func(p *Policy) { p.Roles[1].Verbs = []string{VerbComment} }},
		{"legacy removed", func(p *Policy) { p.Roles = p.Roles[1:] }},
		{"owner role", func(p *Policy) { p.OwnerRole = "other" }},
		{"org role", func(p *Policy) { p.OrgRole = "other" }},
		{"operator role", func(p *Policy) { p.Operator.Role = "other" }},
		{"default scope", func(p *Policy) { p.Agents.DefaultScope = "other" }},
		{"agent scope", func(p *Policy) { p.Agents.Scopes = map[string]string{"assistant": "other"} }},
		{"used role", func(p *Policy) { p.Roles = p.Roles[:4] }},
		{"visibility", func(p *Policy) { p.Defaults["chat"] = KindDefault{Visibility: "public"} }},
		{"sharing", func(p *Policy) { p.Sharing.Who = "anyone" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next := DefaultPolicy()
			next.Roles = append(next.Roles, runner)
			tc.edit(&next)
			var invalid *ValidationError
			if err := next.Validate(prev, func(role string) bool { return role == "runner" }); !errors.As(err, &invalid) {
				t.Fatalf("validation = %v", err)
			}
		})
	}
}

// Contract C2, C3. Rank is for display, while permissions accumulate.
func TestRoleBundlesAreCombined(t *testing.T) {
	p, now := DefaultPolicy(), time.Now()
	p.Roles = append(p.Roles, Role{Name: "runner", Verbs: []string{VerbView, VerbRun}})
	c := Caller{Handle: "runner"}
	it := Item{Grants: []Grant{{Subject: Subject{Kind: "person", ID: c.Handle}, Role: "runner"}}}
	if p.RoleOf(c, it, now) != "runner" || !p.Allowed(c, it, VerbRun, now) || p.Allowed(c, it, VerbEdit, now) {
		t.Fatal("runner should be able to run and view")
	}
	it.Shares = []Share{{Kind: "person", ID: c.Handle, Role: "comment"}}
	if !p.Allowed(c, it, VerbComment, now) || p.RoleOf(c, it, now) != "runner" {
		t.Fatal("runner should keep permissions from its lower-ranked role")
	}
	it.Grants = nil
	if p.Allowed(c, it, VerbRun, now) {
		t.Fatal("the caller retained a removed run permission")
	}
}

// Contract E1, E2, E3, E4, E5. The sponsor and the scope bound each check independently.
func TestAgentScopeAndGranting(t *testing.T) {
	p, now := DefaultPolicy(), time.Now()
	p.Roles = append(p.Roles, Role{Name: "sharer", Verbs: []string{VerbView, VerbShare}})
	c := Caller{Handle: "sponsor", Agent: &Agent{ID: "assistant", Sponsor: "sponsor"}}
	it := Item{Shares: []Share{{Kind: "person", ID: c.Handle, Role: "view"}}}
	p.Agents.DefaultScope = "edit"
	if !p.Allowed(c, it, VerbView, now) || p.Allowed(c, it, VerbEdit, now) {
		t.Fatal("the agent exceeded its sponsor")
	}
	it.Shares = nil
	if p.Allowed(c, it, VerbView, now) {
		t.Fatal("the agent retained the sponsor's removed share")
	}
	it.OwnerKind, it.OwnerID = "person", c.Handle
	p.Agents.Scopes = map[string]string{c.Agent.ID: "sharer"}
	if !p.Allowed(c, it, VerbShare, now) || p.Allowed(c, it, VerbEdit, now) {
		t.Fatal("the agent exceeded its configured scope")
	}
	person := Subject{Kind: "person", ID: "reader"}
	for _, tc := range []struct {
		name, role                  string
		subject                     Subject
		mayGrant, mayShare, allowed bool
	}{
		{"granting disabled", "view", person, false, false, false},
		{"view allowed", "view", person, true, false, true},
		{"missing edit", "edit", person, true, true, false},
		{"sharing disabled", "sharer", person, true, false, false},
		{"sharing allowed", "sharer", person, true, true, true},
		{"self", "view", Subject{Kind: "agent", ID: c.Agent.ID}, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p.Agents.MayGrant, p.Agents.MayShare = tc.mayGrant, tc.mayShare
			if err := MayGrant(p, c, it, tc.subject, tc.role, now); (err == nil) != tc.allowed {
				t.Fatalf("grant decision = %v", err)
			}
		})
	}
}

// Contract A4, C5, C7, D1. The transition reaches both audiences.
func TestAudienceAndSharingPolicy(t *testing.T) {
	p, now := DefaultPolicy(), time.Now()
	it := Item{OwnerKind: "person", OwnerID: "writer", Private: true}
	before := p.Audience(now, it)
	p.Operator.SeesPrivate = true
	after := p.Audience(now, it)
	operator := Caller{Operator: true}
	if before.Equal(after) || before.Hears(operator) || !Union(before, after).Hears(operator) {
		t.Fatalf("audiences before = %+v, after = %+v", before, after)
	}
	if !p.Allowed(operator, it, VerbView, now) {
		t.Fatal("the operator cannot view private items with the policy enabled")
	}
	it.Private = false
	it.Shares = []Share{{Kind: "person", ID: "delegate", Role: "full"}}
	p.Sharing.Who = OwnerOnly
	if err := MayGrant(p, Caller{Handle: "delegate"}, it, Subject{Kind: "person", ID: "reader"}, "view", now); err == nil {
		t.Fatal("sharing exceeded the owner-only rule")
	}
}

// A private item that grants on itself have opened (Closed) is reached by its
// owner and by the people those grants name, and by nobody else: not by the
// people its space is shared with, not by the organization, not by the operator.
// This is the same contract as org-follow item A4.
func TestAClosedPrivateItemIsReachedByItsGranteesAlone(t *testing.T) {
	p, now := DefaultPolicy(), time.Now()
	it := Item{
		OwnerKind: "person", OwnerID: "writer", Private: true, Closed: true, Visibility: Org,
		Shares: []Share{{Kind: "person", ID: "sharer", Role: "full"}},
		Grants: []Grant{{Subject: Subject{Kind: "person", ID: "grantee"}, Role: "view"}},
	}
	for who, want := range map[string]bool{"writer": true, "grantee": true, "sharer": false, "member": false} {
		if got := p.Allowed(Caller{Handle: who, Member: true}, it, VerbView, now); got != want {
			t.Errorf("%s may view = %v, want %v", who, got, want)
		}
	}
	if p.Allowed(Caller{Operator: true}, it, VerbView, now) {
		t.Error("the operator reaches a private item its grants opened")
	}
	if got := p.Audience(now, it); got.Operator || got.Org || !slices.Equal(got.Persons, []string{"grantee", "writer"}) {
		t.Errorf("the audience of a closed private item = %+v", got)
	}
	p.Operator.SeesPrivate = true
	if !p.Allowed(Caller{Operator: true}, it, VerbView, now) {
		t.Error("the operator cannot see a private item the policy lets it see")
	}
}

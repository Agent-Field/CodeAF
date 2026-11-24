package serve

import (
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func legacyRoleOf(v Viewer, it Item) string {
	if v.Solo || v.owns(it) {
		return factory.RoleFull
	}
	if it.Private {
		return ""
	}
	role := ""
	for _, s := range it.Shares {
		switch s.Kind {
		case factory.ShareToPerson:
			if v.Handle != "" && factory.NormalizeHandle(s.ID) == v.Handle {
				role = factory.HigherRole(role, s.Role)
			}
		case factory.ShareToTeam:
			if v.Teams[s.ID] {
				role = factory.HigherRole(role, s.Role)
			}
		}
	}
	if role == "" && v.Operator {
		role = factory.RoleView
	}
	return role
}

// can is whether v may do verb (a role: view, comment, edit or full) to it.
// An unknown verb is never allowed.
func legacyCan(v Viewer, it Item, verb string) bool {
	need := factory.RoleRank(verb)
	return need > 0 && factory.RoleRank(legacyRoleOf(v, it)) >= need
}

// ── who hears an item's events ──

// itemAudience is who may hear that an item changed: its owner, everyone it
// is shared with, and the operator unless it is private. An event built from
// the item before and after a change joins both audiences, so someone it was
// just taken from still hears to drop their row.
// audienceOf joins the audiences of every item given.
func legacyAudienceOf(items ...Item) itemAudience {
	var a itemAudience
	for _, it := range items {
		switch it.Owner.Kind {
		case api.OwnerPerson:
			a.Persons = append(a.Persons, factory.NormalizeHandle(it.Owner.ID))
		case api.OwnerTeam:
			a.Teams = append(a.Teams, it.Owner.ID)
		}
		if it.Private {
			continue
		}
		a.Operator = true
		for _, s := range it.Shares {
			if s.Kind == factory.ShareToPerson {
				a.Persons = append(a.Persons, factory.NormalizeHandle(s.ID))
			} else if s.Kind == factory.ShareToTeam {
				a.Teams = append(a.Teams, s.ID)
			}
		}
	}
	a.Persons, a.Teams = sortedTeams(a.Persons), sortedTeams(a.Teams)
	return a
}

// hears is whether v is in a. On a solo factory the one person hears all.
func legacyHears(v Viewer, a itemAudience) bool {
	if v.Solo || (v.Operator && a.Operator) {
		return true
	}
	if v.Handle != "" && slices.Contains(a.Persons, v.Handle) {
		return true
	}
	return slices.ContainsFunc(a.Teams, func(t string) bool { return v.Teams[t] })
}

// Contract P1, A2, A4. The default keeps every legacy decision and audience.
func TestAccessMatchesLegacy(t *testing.T) {
	viewers := []Viewer{
		{Solo: true, Operator: true},
		{Handle: "operator", Operator: true},
		{Handle: "writer"},
		{Handle: "team-owner", Teams: map[string]bool{"writing": true}},
		{Handle: "shared"},
		{Handle: "team-reader", Teams: map[string]bool{"reading": true}},
		{Handle: "shared", Teams: map[string]bool{"reading": true}},
		{Handle: "unrelated", Member: true},
		{},
	}
	owners := []factory.WorkstreamOwner{{Kind: "person", ID: "@Writer"}, {Kind: "team", ID: "writing"}, {}}
	shareRoles := append([]string{""}, factory.Roles...)
	var items []Item
	for _, owner := range owners {
		for _, private := range []bool{false, true} {
			for _, personRole := range shareRoles {
				for _, teamRole := range shareRoles {
					var shares []factory.Share
					if personRole != "" {
						shares = append(shares, factory.Share{Kind: "person", ID: "@Shared", Role: personRole})
					}
					if teamRole != "" {
						shares = append(shares, factory.Share{Kind: "team", ID: "reading", Role: teamRole})
					}
					// Old documents can contain an unrecognized share kind.
					shares = append(shares, factory.Share{Kind: "org", ID: "*", Role: "full"})
					items = append(items, Item{Kind: ItemWorkstream, Owner: owner, Private: private, Shares: shares})
					ws := factory.OrgWorkstream{ID: "ws_fixture", Owner: owner, Private: private, Shares: shares}
					canvas := factory.OrgCanvas{ID: "cv_fixture", Workstream: ws.ID, Shares: []factory.Share{
						{Kind: "person", ID: "shared", Role: "comment"},
					}}
					items = append(items, canvasItemIn(canvas, ws))
				}
			}
		}
	}
	for i, it := range items {
		a := audienceOf(it)
		old := legacyAudienceOf(it)
		if !a.Equal(old) {
			t.Errorf("item %d audience = %+v, want %+v", i, a, old)
		}
		next := items[(i+1)%len(items)]
		both, oldBoth := audienceOf(it, next), legacyAudienceOf(it, next)
		if !both.Equal(oldBoth) {
			t.Errorf("item %d union = %+v, want %+v", i, both, oldBoth)
		}
		for j, viewer := range viewers {
			if got, want := roleOf(viewer, it), legacyRoleOf(viewer, it); got != want {
				t.Errorf("item %d viewer %d role = %q, want %q", i, j, got, want)
			}
			if viewer.hears(a) != legacyHears(viewer, old) || viewer.hears(both) != legacyHears(viewer, oldBoth) {
				t.Errorf("item %d viewer %d heard a different audience", i, j)
			}
			for _, verb := range []string{"view", "comment", "edit", "full", "", "admin"} {
				if can(viewer, it, verb) != legacyCan(viewer, it, verb) {
					t.Errorf("item %d viewer %d permission %q changed", i, j, verb)
				}
			}
		}
	}
}

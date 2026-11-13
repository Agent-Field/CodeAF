package serve

import (
	"net/http"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── ONE ACCESS CORE FOR EVERY ORGANIZATION ITEM ────────────────────────────
//
// Every organization item (a workstream, a canvas, a shared document, the
// jobs of a workstream) is decided here and nowhere else. A route turns its
// item into an [Item], asks [can] with the verb it needs, and answers 404
// when the caller may not even view it, so an id is never a way to learn
// what someone else is working on. The rules, exactly:
//
//   - THE OWNER HOLDS FULL: the person, or anyone in the owning team.
//   - A SHARE GIVES ITS ROLE to the person it names or to everyone in the team
//     it names; several shares reaching one person give the highest.
//   - A PRIVATE ITEM IS ITS OWNER'S ALONE. Its shares, if any were ever kept,
//     grant nothing.
//   - THE OPERATOR VIEWS every item that is not private, and of a private item
//     sees only that it exists ([nameOnly]): its name, owner and when it
//     changed, never what it says. Seeing is not writing: the operator edits
//     an item only through a role like anyone else. What the operator may do
//     beyond that (purge, reassign an offboarded person's items) is checked
//     by the route that does it, against [Viewer.Operator].
//   - ON A SOLO FACTORY there is one person, and they hold full on everything.

// Item is the access shape of one organization item, whatever its kind.
type Item struct {
	// Kind names what the item is ("workstream", "canvas", "doc"); the rules
	// do not read it, but a refusal or an event can name it.
	Kind    string
	ID      string
	Owner   factory.WorkstreamOwner
	Shares  []factory.Share
	Private bool
}

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
}

// itemViewer is the caller of r as a [Viewer]: their handle, whether they are
// the operator, and the teams they are in, read fresh from the store.
func (h *Handler) itemViewer(r *http.Request) (Viewer, error) {
	who, err := h.callerHandle(r)
	if err != nil {
		return Viewer{}, err
	}
	v := Viewer{Handle: factory.NormalizeHandle(who), Solo: h.mode() == api.FactorySolo, Teams: map[string]bool{}}
	v.Operator = v.Solo || h.isOperator(r)
	if v.Handle != "" {
		for _, team := range h.Membership().TeamsOf(v.Handle) {
			v.Teams[team] = true
		}
	}
	return v, nil
}

// owns is whether v is it's owner, in person or through the owning team.
func (v Viewer) owns(it Item) bool {
	switch it.Owner.Kind {
	case api.OwnerPerson:
		return v.Handle != "" && factory.NormalizeHandle(it.Owner.ID) == v.Handle
	case api.OwnerTeam:
		return v.Teams[it.Owner.ID]
	}
	return false
}

// roleOf is the highest role v holds on it, "" for none.
func roleOf(v Viewer, it Item) string {
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
func can(v Viewer, it Item, verb string) bool {
	need := factory.RoleRank(verb)
	return need > 0 && factory.RoleRank(roleOf(v, it)) >= need
}

// nameOnly is whether v may see that it exists without seeing what it says:
// the operator's row for someone else's private item.
func nameOnly(v Viewer, it Item) bool {
	return v.Operator && roleOf(v, it) == ""
}

// ── who hears an item's events ──

// itemAudience is who may hear that an item changed: its owner, everyone it
// is shared with, and the operator unless it is private. An event built from
// the item before and after a change joins both audiences, so someone it was
// just taken from still hears to drop their row.
type itemAudience struct {
	Persons  []string `json:"persons,omitempty"`
	Teams    []string `json:"teams,omitempty"`
	Operator bool     `json:"operator,omitempty"`
}

// audienceOf joins the audiences of every item given.
func audienceOf(items ...Item) itemAudience {
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
func (v Viewer) hears(a itemAudience) bool {
	if v.Solo || (v.Operator && a.Operator) {
		return true
	}
	if v.Handle != "" && slices.Contains(a.Persons, v.Handle) {
		return true
	}
	return slices.ContainsFunc(a.Teams, func(t string) bool { return v.Teams[t] })
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
				return nil, invalidf("a person is shared with by their GitHub handle")
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
	return Item{Kind: ItemWorkstream, ID: ws.ID, Owner: ws.Owner, Shares: ws.Shares, Private: ws.Private}
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

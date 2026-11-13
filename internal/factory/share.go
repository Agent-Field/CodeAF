package factory

import "slices"

// ── WHO MAY DO WHAT TO AN ORGANIZATION ITEM ────────────────────────────────
//
// Every organization item (a workstream, a canvas, a document, a job's
// workstream) carries the same access shape: one owner, a list of shares to
// people or teams each with a role, and whether it is private. The server's
// access core (serve/item_access.go) reads only this shape, so one rule
// governs every kind of item and a new kind cannot grow its own.
//
// THE ROLES ARE RANKED, each one granting everything below it:
//
//   - view: read it.
//   - comment: view, and comment on it.
//   - edit: comment, and change what it says.
//   - full: edit, and share it, give it to another owner, archive it.
//
// The owner, a person or anyone in the owning team, holds full.

// The roles, lowest first.
const (
	RoleView    = "view"
	RoleComment = "comment"
	RoleEdit    = "edit"
	RoleFull    = "full"
)

// Roles are every role, lowest first.
var Roles = []string{RoleView, RoleComment, RoleEdit, RoleFull}

// The kinds of thing an item is shared with.
const (
	ShareToPerson = "person"
	ShareToTeam   = "team"
)

// Share gives one person (their handle) or one team (its id) a role on an
// item.
type Share struct {
	Kind string `json:"kind" jsonschema:"enum=person,enum=team"`
	ID   string `json:"id"`
	Role string `json:"role" jsonschema:"enum=view,enum=comment,enum=edit,enum=full"`
}

// RoleRank is role's place among [Roles], counting from one; zero for no
// role, so a missing role never outranks a real one.
func RoleRank(role string) int {
	return slices.Index(Roles, role) + 1
}

// HigherRole is whichever of a and b grants more.
func HigherRole(a, b string) string {
	if RoleRank(b) > RoleRank(a) {
		return b
	}
	return a
}

// ShareTeams is the ids of the teams shares name, in the order they are
// named: the wire's old shared_with, kept for clients that read only it.
func ShareTeams(shares []Share) []string {
	out := []string{}
	for _, s := range shares {
		if s.Kind == ShareToTeam {
			out = append(out, s.ID)
		}
	}
	return out
}

// TeamShares is the old teams-only share list read as shares: before roles a
// share was always a writer, so each team is given edit.
func TeamShares(teams []string) []Share {
	out := make([]Share, 0, len(teams))
	for _, t := range teams {
		out = append(out, Share{Kind: ShareToTeam, ID: t, Role: RoleEdit})
	}
	return out
}

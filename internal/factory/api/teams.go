package api

import "github.com/Agent-Field/codeaf/internal/factory/store"

// Team is one group of people inside an organization, as GET /v1/teams and
// every team write answer it. It is the shape the organization keeps.
type Team = store.Team

// Teams is GET /v1/teams: every team in the organization. Every member sees
// every team's name and members.
type Teams struct {
	Teams []Team `json:"teams"`
}

// TeamName is the body of POST /v1/teams and PATCH /v1/teams/{id}.
type TeamName struct {
	Name string `json:"name"`
}

// TeamChangedKind is the live event kind sent after any change to a team,
// with [OrgChanged] as its data, so a desk refreshes without polling.
const TeamChangedKind = "team.changed"

// OrgChanged is the data of an organization's change events: what kind of
// thing changed and its id. The desk reads it again by id.
type OrgChanged struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

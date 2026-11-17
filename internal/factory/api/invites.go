package api

import "time"

// ── INVITES (Settings › Organizations › <org> › People, v2 design J4) ──
//
// An invite is a seven-day link made for one person, carrying the team and
// the workstreams they join. Before they join they can already be given
// tasks: they go by Ref ("invite:<id>") wherever a person is named, and
// joining rewrites every such name to the handle they joined as.

// The states an invite reads.
const (
	InviteInvited = "invited"
	InviteJoined  = "joined"
	InviteExpired = "expired"
)

// InviteNew is POST /v1/invites's body. Email or Name is needed; an invite
// with no email is a link to copy and nothing is mailed. Team is a team id,
// Workstreams workstream ids.
type InviteNew struct {
	Email       string   `json:"email,omitempty"`
	Name        string   `json:"name,omitempty"`
	Team        string   `json:"team,omitempty"`
	Workstreams []string `json:"workstreams,omitempty"`
}

// Invite is one row of People's invites.
type Invite struct {
	ID              string     `json:"id"`
	Ref             string     `json:"ref"`
	Email           string     `json:"email,omitempty"`
	Name            string     `json:"name,omitempty"`
	Team            string     `json:"team,omitempty"`
	TeamName        string     `json:"team_name,omitempty"`
	Workstreams     []string   `json:"workstreams"`
	WorkstreamNames []string   `json:"workstream_names"`
	InvitedBy       string     `json:"invited_by,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	ExpiresAt       time.Time  `json:"expires_at"`
	State           string     `json:"state"`
	JoinedAt        *time.Time `json:"joined_at,omitempty"`
	Handle          string     `json:"handle,omitempty"`
	// Emailed is whether the link went out by mail the last time it was
	// made; false on a server with no mailer, where the link is copied.
	Emailed bool `json:"emailed"`
}

// Invites is GET /v1/invites. Mailer says whether this server can send the
// link by email; without one People offers only Copy an invite link.
type Invites struct {
	Invites []Invite `json:"invites"`
	Mailer  bool     `json:"mailer"`
}

// InviteMade is what POST /v1/invites and resend answer: the invite and its
// link, shown this once.
type InviteMade struct {
	Invite Invite `json:"invite"`
	URL    string `json:"url"`
}

// Welcome is GET /v1/me/welcome: the line a person reads on Work the first
// time after joining through an invite, "Priya added you to AgentField".
// Show is false once they pressed Got it, or when they came in another way.
type Welcome struct {
	Show            bool     `json:"show"`
	InvitedBy       string   `json:"invited_by,omitempty"`
	InvitedByName   string   `json:"invited_by_name,omitempty"`
	Workstreams     []string `json:"workstreams"`
	WorkstreamNames []string `json:"workstream_names"`
	Team            string   `json:"team,omitempty"`
	TeamName        string   `json:"team_name,omitempty"`
}

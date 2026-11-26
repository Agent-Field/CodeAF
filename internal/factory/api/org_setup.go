package api

import "github.com/Agent-Field/codeaf/internal/factory"

// ── AN ORGANIZATION'S OWN WORDS AND ITS FIRST SET-UP (v2 designs J2, J3) ───

// OrgProfile is GET /v1/org: the organization's name, what it does in a
// line (Afi reads it), and whether anyone has set it up yet. SetUp false is
// an organization whose operator lands on Afi's set-up conversation.
type OrgProfile struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	SetUp       bool   `json:"set_up"`
	ByHand      bool   `json:"by_hand,omitempty"`
	// Relay is the address of the relay this organization runs for its members' computers, when the operator named one
	// (`relay_url` in the server's config). It is how a member's codeaf finds it after signing in; nothing here is a secret.
	Relay string `json:"relay,omitempty"`
}

// OrgProfilePut is PUT /v1/org, the operator's "Start your organization".
// A field left out keeps what it was.
type OrgProfilePut struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// OrgDescriptionMost is the most characters the one line may have.
const OrgDescriptionMost = 200

// OrgSetupView is GET /v1/org/setup: the operator's own conversation with
// Afi about setting the organization up, the card Afi proposed for them, and
// what an earlier press made of it.
type OrgSetupView struct {
	Agent WorkstreamAgent       `json:"agent"`
	Card  *factory.OrgSetupCard `json:"card,omitempty"`
	Made  []OrgSetupMadePart    `json:"made"`
	SetUp bool                  `json:"set_up"`
}

// OrgSetupApply is POST /v1/org/setup/apply: the card as the person left it
// (unticked parts removed, renames and emails in place).
type OrgSetupApply struct {
	Card factory.OrgSetupCard `json:"card"`
}

// OrgSetupMadePart is one part a press made: a team, a workstream (Parent is
// the workstream it is inside) or an invite.
type OrgSetupMadePart struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	ID     string `json:"id"`
	Parent string `json:"parent,omitempty"`
	// URL is an invite's link, for one with no email to copy and send.
	URL string `json:"url,omitempty"`
}

// The kinds of part a set-up makes.
const (
	OrgSetupPartTeam       = "team"
	OrgSetupPartWorkstream = "workstream"
	OrgSetupPartInvite     = "invite"
)

// OrgSetupApplied is what one press made. Failed names the part that could
// not be made and why; a press again makes only what is left. Done is every
// part made: the organization is set up.
type OrgSetupApplied struct {
	Made   []OrgSetupMadePart `json:"made"`
	Failed string             `json:"failed,omitempty"`
	Done   bool               `json:"done"`
}

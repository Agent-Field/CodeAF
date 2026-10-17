package api

import "time"

// PhaseHeader is a response header on the factory's mutating admin routes
// (create or revoke an access link, revoke every session, end one session).
// While its value is PhasePreMutation the handler has not yet made its first
// requested change, so ANY status that carries it proves nothing was written.
// The handler clears it immediately before that first change, which means a
// 4xx without it may follow a stored write (the factory stores, then records
// the event, and either can fail with a 403, 404 or 409). A client treats a
// refusal as definite only with the marker and treats every other failure of
// those routes as uncertain.
const (
	PhaseHeader      = "X-Codeaf-Phase"
	PhasePreMutation = "pre-mutation"
)

// AccessLink exposes its secret URL only when the operator creates it.
type AccessLink struct {
	Operator  bool      `json:"operator,omitempty"`
	ID        string    `json:"id"`
	URL       string    `json:"url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	CreatedBy string    `json:"created_by"`
	Login     string    `json:"login,omitempty"`
	Name      string    `json:"name,omitempty"`
	Revoked   bool      `json:"revoked"`
	// MaxUses is how many people a human link admits; Used and Remaining are
	// the counts now. A link from before the limit existed reports one person.
	// Status is open, used_up, expired or revoked, judged by the factory's
	// clock, and EndedAt is when an ended link stopped admitting people.
	MaxUses   int        `json:"max_uses,omitempty"`
	Used      int        `json:"used"`
	Remaining int        `json:"remaining"`
	Status    string     `json:"status,omitempty" jsonschema:"enum=open,enum=used_up,enum=expired,enum=revoked"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	// UsedBy names who joined with the link: public GitHub handles only.
	UsedBy []string `json:"used_by,omitempty"`
	// Counted is set on a human link's row ONLY. True means the link has its
	// own use limit, so revoking it leaves the people who joined signed in;
	// false is a link from before the limit existed, whose revocation ends
	// their sessions too. MaxUses cannot say which (a missing limit reads as
	// one person), and a factory that omits the field has not said.
	Counted *bool `json:"counted,omitempty"`
}

// AccessLinks is the operator's link directory, with no reusable secrets.
type AccessLinks struct {
	Factory Factory      `json:"factory"`
	Links   []AccessLink `json:"links"`
}

// CreateAccessLink optionally names the invited GitHub login and expiry. A
// link lasts seven days when no expiry is given and never longer. MaxUses is
// one person when empty or 1, or up to that many people from 2 to 50.
type CreateAccessLink struct {
	MaxUses   int       `json:"max_uses,omitempty"`
	Login     string    `json:"login,omitempty"`
	Name      string    `json:"name,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

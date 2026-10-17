package api

import "time"

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

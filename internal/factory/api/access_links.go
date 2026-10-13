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
}

// AccessLinks is the operator's link directory, with no reusable secrets.
type AccessLinks struct {
	Factory Factory      `json:"factory"`
	Links   []AccessLink `json:"links"`
}

// CreateAccessLink optionally names the invited GitHub login and expiry.
type CreateAccessLink struct {
	Login     string    `json:"login,omitempty"`
	Name      string    `json:"name,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

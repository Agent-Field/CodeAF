package api

import "github.com/Agent-Field/codeaf/internal/factory/tokens"

// Profile is the GitHub identity checked at sign-in, never a client claim.
type Profile = tokens.Profile

// Me describes this caller. machines are that handle's own; another person's
// machine is not listed here.
type Me struct {
	Factory       Factory `json:"factory"`
	CodeafVersion string  `json:"codeaf_version"`
	Contract      int     `json:"contract"`
	Profile
	SessionID       string          `json:"session_id"`
	InvitedBy       string          `json:"invited_by,omitempty"`
	Machines        []Machine       `json:"machines"`
	TakesUnassigned map[string]bool `json:"takes_unassigned"`
	Access          string          `json:"access"`
	PollingEvery    string          `json:"polling_every,omitempty"`
	ServedBy        string          `json:"served_by"`
}

// Device describes the code to enter on GitHub and the opaque code to poll here.
type Device struct {
	Factory         Factory `json:"factory"`
	DeviceCode      string  `json:"device_code"`
	UserCode        string  `json:"user_code"`
	VerificationURI string  `json:"verification_uri"`
	Interval        int     `json:"interval"`
	ExpiresIn       int     `json:"expires_in"`
}

// DevicePoll exchanges a code issued to this caller only.
type DevicePoll struct {
	DeviceCode string `json:"device_code"`
}

// SignedIn returns a factory credential, never the GitHub access token.
type SignedIn struct {
	Factory     Factory `json:"factory"`
	AccessToken string  `json:"access_token"`
	TokenType   string  `json:"token_type"`
	ExpiresIn   int     `json:"expires_in"`
	Me          Me      `json:"me"`
}
